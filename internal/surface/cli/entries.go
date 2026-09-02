package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// entryFlags are the flags spend and receive share: everything but the
// amount and category, which are positional (docs/ux-principles.md §7:
// "positional amount and category, because that's the order the sentence
// goes in").
type entryFlags struct {
	account string
	on      string
	note    string
	tags    []string
}

func addEntryFlags(cmd *cobra.Command, f *entryFlags) {
	cmd.Flags().StringVar(&f.account, "account", "", "which account this affects (defaults to your only account, if you have exactly one)")
	cmd.Flags().StringVar(&f.on, "on", "", "the date this happened (defaults to today)")
	cmd.Flags().StringVar(&f.note, "note", "", "a free-text note to attach")
	cmd.Flags().StringArrayVar(&f.tags, "tag", nil, "a tag to attach (repeatable)")
}

// entryView renders a spend or receive result. There is no "postings"
// concept exposed here (docs/ux-principles.md §2 bans that word from
// every user-facing string, JSON field names included) — a spend/receive
// entry always has exactly one account and (optionally) one category, so
// this exposes those directly instead of a generic line-items array.
type entryView struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Date        string   `json:"date"`
	Description string   `json:"description"`
	Notes       string   `json:"notes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Account     string   `json:"account"`
	AccountID   string   `json:"account_id"`
	Category    string   `json:"category,omitempty"`
	CategoryID  string   `json:"category_id,omitempty"`
	Amount      string   `json:"amount"`
	Currency    string   `json:"currency"`
}

// The three verbs this CLI describes a transaction with, wherever one is
// rendered — spend and receive here, move in move.go, and all three in
// transactions.go, which renders transactions it didn't record itself.
const (
	entryTypeSpend   = "spend"
	entryTypeReceive = "receive"
	entryTypeMove    = "move"
)

// entryViewFrom builds an entryView from a RecordOutflow/RecordInflow
// result. accountLabel and categoryLabel are the raw text the user typed
// (or, when an account was defaulted, the account's real name resolved
// while defaulting it — see resolveDefaultAccount) — display text this
// package already has on hand, never a second application-layer lookup
// done just to render a result.
func entryViewFrom(entryType, accountLabel, categoryLabel string, r app.TransactionResult) entryView {
	v := entryView{
		ID:          r.Transaction.ID(),
		Type:        entryType,
		Date:        r.Transaction.BookedDate().String(),
		Description: r.Transaction.Description(),
		Notes:       r.Transaction.Notes(),
		Account:     accountLabel,
		Category:    categoryLabel,
	}
	for _, t := range r.Tags {
		v.Tags = append(v.Tags, t.String())
	}
	if postings := r.Transaction.Postings(); len(postings) > 0 {
		p := postings[0]
		v.AccountID = p.AccountID()
		v.Amount = p.Amount().Abs().AmountString()
		v.Currency = p.Currency()
		if cid, ok := p.CategoryID(); ok {
			v.CategoryID = cid
		}
	}
	return v
}

func entryVerb(entryType string) string {
	switch entryType {
	case entryTypeSpend:
		return "Spent"
	case entryTypeReceive:
		return "Received"
	case entryTypeMove:
		return "Moved"
	default:
		return entryType
	}
}

func printEntry(w io.Writer, v entryView) {
	verb := entryVerb(v.Type)
	if v.Category != "" {
		_, _ = fmt.Fprintf(w, "%s %s %s on %s (%s, %s)\n", verb, v.Amount, v.Currency, v.Category, v.Account, v.Date)
	} else {
		_, _ = fmt.Fprintf(w, "%s %s %s (%s, %s)\n", verb, v.Amount, v.Currency, v.Account, v.Date)
	}
	_, _ = fmt.Fprintf(w, "id: %s\n", v.ID)
}

// resolveDefaultAccount implements docs/ux-principles.md §3's "account
// defaults... to the only one if there's one" for spend and receive. It's
// the one deliberate exception to this package's "call exactly one
// application method" rule — see cli.go's package doc for why issue #7
// calls this specific case in scope. "Most recently used" (the other half
// of §3's rule) isn't implemented: nothing in the application layer
// currently tracks last-used accounts, so implementing that half here
// would mean this package inventing and owning that state itself, which
// is exactly the kind of decision docs/architecture.md §3 reserves to the
// application layer. That's noted as a gap for a future issue, not solved
// by guessing here.
func resolveDefaultAccount(ctx context.Context, svc *app.Service) (accountRef, accountLabel string, err error) {
	result, err := svc.ListAccounts(ctx, app.ListAccountsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		return "", "", err
	}
	switch len(result.Accounts) {
	case 0:
		return "", "", errs.New(errs.InvalidInput).
			Explain("You don't have any accounts yet. Add one first, e.g. `bodger accounts add \"Cash\" --type cash`.").
			Field("account")
	case 1:
		return result.Accounts[0].ID(), result.Accounts[0].Name(), nil
	default:
		names := make([]string, len(result.Accounts))
		for i, a := range result.Accounts {
			names[i] = a.Name()
		}
		return "", "", errs.New(errs.InvalidInput).
			Explain("You have more than one account — say which one with --account.").
			Field("account").
			With("accounts", names)
	}
}

// runEntryCommand is spend and receive's shared RunE body: open the
// service, resolve the account (explicit --account, or the default), call
// record (a closure the caller builds with its own RecordOutflow/
// RecordInflow call), and render the result. category is the raw
// positional text the user typed — used both to build the view and,
// implicitly, as the command's Description default (see newSpendCmd's doc
// comment for that decision).
func runEntryCommand(cmd *cobra.Command, factory ServiceFactory, entryType, category string, f *entryFlags, record func(ctx context.Context, svc *app.Service, accountRef string) (app.TransactionResult, error)) error {
	ctx := cmd.Context()
	svc, closeDB, err := factory(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(cmd, closeDB)

	accountRef, accountLabel := f.account, f.account
	if accountRef == "" {
		accountRef, accountLabel, err = resolveDefaultAccount(ctx, svc)
		if err != nil {
			return err
		}
	}

	result, err := record(ctx, svc, accountRef)
	if err != nil {
		return err
	}
	view := entryViewFrom(entryType, accountLabel, category, result)
	return render(cmd, view, func(w io.Writer) { printEntry(w, view) })
}

// newSpendCmd builds "spend": a single-posting outflow.
//
// Description-defaulting decision (issue #7 flags this ambiguity
// explicitly): RecordOutflowCommand.Description is required and
// non-empty at the application layer (resolveCommonFields in
// internal/app/transactions_record.go), but the issue's own worked
// example — `bodger spend 800 groceries`, no description flag or
// positional shown — and docs/ux-principles.md §3's "three required
// inputs" budget leave no room for a fourth argument. This command
// resolves that by having the category positional do double duty: it's
// both CategoryRef and, when nothing else is available, the entry's
// Description. There's no --description flag in this issue's scope
// either, so that's the description on every spend/receive entry this
// command records — editing it is deferred to whatever future issue
// exposes EditTransaction as a command.
func newSpendCmd(factory ServiceFactory) *cobra.Command {
	var f entryFlags
	cmd := &cobra.Command{
		Use:   "spend <amount> <category>",
		Short: "Record money you spent",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			amount, category := args[0], args[1]
			return runEntryCommand(cmd, factory, entryTypeSpend, category, &f, func(ctx context.Context, svc *app.Service, accountRef string) (app.TransactionResult, error) {
				return svc.RecordOutflow(ctx, app.RecordOutflowCommand{
					ActorID:     ports.SeededUserID,
					AccountRef:  accountRef,
					Amount:      amount,
					CategoryRef: category,
					Date:        f.on,
					Description: category,
					Notes:       f.note,
					Tags:        f.tags,
				})
			})
		},
	}
	addEntryFlags(cmd, &f)
	return cmd
}

// newReceiveCmd builds "receive": a single-posting inflow. See
// newSpendCmd's doc comment for the Description-defaulting decision this
// command follows identically.
func newReceiveCmd(factory ServiceFactory) *cobra.Command {
	var f entryFlags
	cmd := &cobra.Command{
		Use:   "receive <amount> <category>",
		Short: "Record money you received",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			amount, category := args[0], args[1]
			return runEntryCommand(cmd, factory, entryTypeReceive, category, &f, func(ctx context.Context, svc *app.Service, accountRef string) (app.TransactionResult, error) {
				return svc.RecordInflow(ctx, app.RecordInflowCommand{
					ActorID:     ports.SeededUserID,
					AccountRef:  accountRef,
					Amount:      amount,
					CategoryRef: category,
					Date:        f.on,
					Description: category,
					Notes:       f.note,
					Tags:        f.tags,
				})
			})
		},
	}
	addEntryFlags(cmd, &f)
	return cmd
}
