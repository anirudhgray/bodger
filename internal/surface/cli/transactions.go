package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// The application layer's own names for the three kinds of transaction.
// They're spelled out here, as plain strings, because this package never
// imports internal/domain/ledger (see cli.go's package doc) — and because
// they never reach a user: every user-facing edge of this CLI says
// spend/receive/move instead (docs/ux-principles.md §7's "one vocabulary,
// not two"), and transactionTypeFor/transactionKindFor are the two ends of
// that translation.
const (
	kindOutflow  = "outflow"
	kindInflow   = "inflow"
	kindTransfer = "transfer"
)

// transactionTypeFor maps an application-layer transaction kind onto the
// verb this CLI already uses for it — the same verb spend, receive, and
// move stamp on their own results (entries.go, move.go), so a transaction
// listed here is described the same way it was when it was recorded. An
// unrecognized kind passes through as-is rather than being hidden.
func transactionTypeFor(kind string) string {
	switch kind {
	case kindOutflow:
		return entryTypeSpend
	case kindInflow:
		return entryTypeReceive
	case kindTransfer:
		return entryTypeMove
	default:
		return kind
	}
}

// transactionKindFor is transactionTypeFor's inverse, used by
// `transactions list --type`. Rejecting an unknown verb here rather than
// passing it through is presentation-layer routing between this CLI's
// vocabulary and the application layer's, the same way the REST surface
// picks between RecordOutflow and RecordInflow on its own "type" field —
// not a business rule this package is inventing.
func transactionKindFor(txnType string) (string, error) {
	switch txnType {
	case "":
		return "", nil
	case entryTypeSpend:
		return kindOutflow, nil
	case entryTypeReceive:
		return kindInflow, nil
	case entryTypeMove:
		return kindTransfer, nil
	default:
		return "", errs.New(errs.InvalidInput).
			Explain("%q isn't something you can filter by. Use spend, receive, or move.", txnType).
			Field("type")
	}
}

// transactionView is the general shape of any transaction, whichever way
// it was recorded — the one this package's list, edit, and delete commands
// render. spend/receive have their own narrower entryView and move its own
// moveView (both carry the account and category *names* the user typed,
// which those commands have on hand and these don't), so this is the shape
// for the commands that work on a transaction they didn't just record.
//
// As in entryView, there's no "line items" array here
// (docs/ux-principles.md §2): money spent or received always has exactly
// one account and at most one category, and a move always has exactly two
// accounts and no category, so account_id/category_id and
// from_account_id/to_account_id are populated according to the
// transaction's own type and the pair that doesn't apply is omitted.
type transactionView struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Date          string   `json:"date"`
	Description   string   `json:"description"`
	Notes         string   `json:"notes,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	AccountID     string   `json:"account_id,omitempty"`
	CategoryID    string   `json:"category_id,omitempty"`
	FromAccountID string   `json:"from_account_id,omitempty"`
	ToAccountID   string   `json:"to_account_id,omitempty"`
	Amount        string   `json:"amount"`
	Currency      string   `json:"currency"`
}

func transactionViewFrom(r app.TransactionResult) transactionView {
	t := r.Transaction
	v := transactionView{
		ID:          t.ID(),
		Type:        transactionTypeFor(string(t.Kind())),
		Date:        t.BookedDate().String(),
		Description: t.Description(),
		Notes:       t.Notes(),
	}
	for _, tag := range r.Tags {
		v.Tags = append(v.Tags, tag.String())
	}

	// Which end of a move is "from" and which is "to" is decided by the
	// amount's sign (the source leg is always the negative one), never by
	// position in the slice — the same rule moveViewFrom follows, for the
	// same reason.
	for _, p := range t.Postings() {
		switch {
		case v.Type == entryTypeMove && p.Amount().IsNegative():
			v.FromAccountID = p.AccountID()
		case v.Type == entryTypeMove:
			v.ToAccountID = p.AccountID()
			v.Amount = p.Amount().AmountString()
			v.Currency = p.Currency()
		default:
			v.AccountID = p.AccountID()
			if cid, ok := p.CategoryID(); ok {
				v.CategoryID = cid
			}
			v.Amount = p.Amount().Abs().AmountString()
			v.Currency = p.Currency()
		}
	}
	return v
}

func printTransaction(w io.Writer, v transactionView) {
	_, _ = fmt.Fprintf(w, "%s %s %s on %s — %s\n", entryVerb(v.Type), v.Amount, v.Currency, v.Date, v.Description)
	_, _ = fmt.Fprintf(w, "id: %s\n", v.ID)
}

// transactionListView is `transactions list`'s result shape. It carries
// limit and offset alongside the transactions because they're what a
// script needs to ask for the next page, and because they're the values
// the application layer actually applied — an unset or oversized --limit
// is defaulted and clamped there, so echoing them back is the only way a
// caller learns the page size it really got.
type transactionListView struct {
	Transactions []transactionView `json:"transactions"`
	Limit        int               `json:"limit"`
	Offset       int               `json:"offset"`
}

func printTransactionTable(w io.Writer, v transactionListView) {
	if len(v.Transactions) == 0 {
		_, _ = fmt.Fprintln(w, "No transactions found.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "DATE\tTYPE\tAMOUNT\tDESCRIPTION\tID")
	for _, t := range v.Transactions {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s %s\t%s\t%s\n", t.Date, t.Type, t.Amount, t.Currency, t.Description, t.ID)
	}
	_ = tw.Flush()

	// A page that came back exactly full is the only signal this query
	// gives that there might be more (nothing counts the total), so say
	// "there may be more" rather than claiming there is.
	if len(v.Transactions) == v.Limit {
		_, _ = fmt.Fprintf(w, "\nThere may be more — see the next page with `--offset %d`.\n", v.Offset+len(v.Transactions))
	}
}

func newTransactionsCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transactions",
		Short: "See and correct what you've recorded",
	}
	cmd.AddCommand(
		newTransactionsListCmd(factory),
		newTransactionsEditCmd(factory),
		newTransactionsDeleteCmd(factory),
	)
	return cmd
}

type transactionListFlags struct {
	account  string
	category string
	txnType  string
	since    string
	until    string
	limit    int
	offset   int
}

// newTransactionsListCmd builds "transactions list".
//
// Two naming decisions worth recording. The date range is --since/--until
// rather than the REST API's from/to, because --from and --to already mean
// "which account" everywhere else in this CLI (`move`, and this command's
// own sibling `edit`), and one flag pair meaning two different things on
// one surface is exactly the drift docs/ux-principles.md §2 is there to
// stop. And paging is a plain --limit/--offset rather than the REST API's
// opaque cursor: the use case underneath is offset-paginated, a cursor
// only exists over HTTP to keep the wire format free to change, and a
// number you can type is friendlier at a shell prompt than a token you
// have to copy.
func newTransactionsListCmd(factory ServiceFactory) *cobra.Command {
	var f transactionListFlags
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the transactions you've recorded",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kind, err := transactionKindFor(f.txnType)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{
				ActorID:     ports.SeededUserID,
				AccountRef:  f.account,
				CategoryRef: f.category,
				Kind:        kind,
				DateFrom:    f.since,
				DateTo:      f.until,
				Limit:       f.limit,
				Offset:      f.offset,
			})
			if err != nil {
				return err
			}

			view := transactionListView{
				Transactions: make([]transactionView, 0, len(result.Transactions)),
				Limit:        result.Limit,
				Offset:       result.Offset,
			}
			for _, t := range result.Transactions {
				view.Transactions = append(view.Transactions, transactionViewFrom(app.TransactionResult{Transaction: t}))
			}
			return render(cmd, view, func(w io.Writer) { printTransactionTable(w, view) })
		},
	}
	cmd.Flags().StringVar(&f.account, "account", "", "only transactions touching this account")
	cmd.Flags().StringVar(&f.category, "category", "", "only transactions in this category, including anything under it")
	cmd.Flags().StringVar(&f.txnType, "type", "", "only one type: spend, receive, or move")
	cmd.Flags().StringVar(&f.since, "since", "", "only transactions on or after this date")
	cmd.Flags().StringVar(&f.until, "until", "", "only transactions on or before this date")
	cmd.Flags().IntVar(&f.limit, "limit", 0, "how many to show at once")
	cmd.Flags().IntVar(&f.offset, "offset", 0, "how many to skip, for paging through a long list")
	return cmd
}

type transactionEditFlags struct {
	account     string
	category    string
	currency    string
	from        string
	to          string
	amount      string
	on          string
	description string
	note        string
	tags        []string
}

// newTransactionsEditCmd builds "transactions edit".
//
// This is a full replacement of everything an edit can change, not a
// patch of the flags you happened to pass: a flag you leave out is set to
// nothing, not left alone. That's the application layer's contract rather
// than a choice made here — its command fields already use the empty
// string to mean specific things ("today" for a date, "no category" for a
// category), so they can't also mean "unchanged" — and the REST API's
// equivalent behaves identically. Re-passing every value you want kept is
// what the help text tells the user to do, since finding out afterwards
// would be a nasty surprise.
//
// What you can't change is what kind of transaction it is: money you
// spent stays money you spent. That's also what decides which flags apply
// — --account/--category/--currency for money spent or received,
// --from/--to for a move — so passing both sets is refused here rather
// than silently ignored, the same way the REST API refuses a request body
// that expresses two different update intents at once.
func newTransactionsEditCmd(factory ServiceFactory) *cobra.Command {
	var f transactionEditFlags
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Correct a transaction you've already recorded",
		Long: "Correct a transaction you've already recorded.\n\n" +
			"This replaces everything about the transaction, so pass every value you want it to end up with — " +
			"anything you leave out is cleared, not kept. Run `bodger transactions list` first to see what's " +
			"there now.\n\n" +
			"You can't change what kind of transaction it is. Use --account, --category, and --currency for " +
			"money you spent or received, and --from and --to for a move.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entrySide := f.account != "" || f.category != "" || f.currency != ""
			moveSide := f.from != "" || f.to != ""
			if entrySide && moveSide {
				return errs.New(errs.InvalidInput).
					Explain("Use --account, --category, and --currency for money you spent or received, or --from and --to for a move — not both in one edit.")
			}

			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.EditTransaction(ctx, app.EditTransactionCommand{
				ActorID:        ports.SeededUserID,
				TransactionRef: args[0],
				AccountRef:     f.account,
				Currency:       f.currency,
				CategoryRef:    f.category,
				FromAccountRef: f.from,
				ToAccountRef:   f.to,
				Amount:         f.amount,
				Date:           f.on,
				Description:    f.description,
				Notes:          f.note,
				Tags:           f.tags,
			})
			if err != nil {
				return err
			}
			view := transactionViewFrom(result)
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintln(w, "Updated:")
				printTransaction(w, view)
			})
		},
	}
	cmd.Flags().StringVar(&f.amount, "amount", "", "the amount it should be (required)")
	cmd.Flags().StringVar(&f.description, "description", "", "what it was for (required)")
	cmd.Flags().StringVar(&f.account, "account", "", "the account it should affect")
	cmd.Flags().StringVar(&f.category, "category", "", "the category it belongs in")
	cmd.Flags().StringVar(&f.currency, "currency", "", "the currency of the amount (defaults to the account's)")
	cmd.Flags().StringVar(&f.from, "from", "", "for a move: the account money leaves")
	cmd.Flags().StringVar(&f.to, "to", "", "for a move: the account money arrives in")
	cmd.Flags().StringVar(&f.on, "on", "", "the date it happened (defaults to today)")
	cmd.Flags().StringVar(&f.note, "note", "", "a free-text note to attach")
	cmd.Flags().StringArrayVar(&f.tags, "tag", nil, "a tag to attach (repeatable)")
	_ = cmd.MarkFlagRequired("amount")
	_ = cmd.MarkFlagRequired("description")
	return cmd
}

// newTransactionsDeleteCmd builds "transactions delete". There's no
// confirmation prompt: deleting is reversible in the database (the
// transaction stops counting towards your balances and disappears from
// lists, but nothing is erased), and docs/ux-principles.md §5 reserves
// confirmation for genuinely hard-to-undo actions.
func newTransactionsDeleteCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a transaction you recorded by mistake",
		Long: "Delete a transaction you recorded by mistake.\n\n" +
			"It stops counting towards your balances and drops out of your lists straight away. Nothing is " +
			"erased from your database, so the history of what was recorded stays intact.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.DeleteTransaction(ctx, app.DeleteTransactionCommand{
				ActorID:        ports.SeededUserID,
				TransactionRef: args[0],
			})
			if err != nil {
				return err
			}
			view := transactionViewFrom(result)
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintln(w, "Deleted:")
				printTransaction(w, view)
			})
		},
	}
}
