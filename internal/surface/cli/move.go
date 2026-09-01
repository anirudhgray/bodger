package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// moveView renders a transfer result. Like entryView, this exposes "from"
// and "to" directly rather than a generic line-items array — see
// entries.go's entryView doc comment for why (docs/ux-principles.md §2
// bans "posting" from every user-facing string).
type moveView struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Date          string   `json:"date"`
	Description   string   `json:"description"`
	Notes         string   `json:"notes,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	From          string   `json:"from"`
	FromAccountID string   `json:"from_account_id"`
	To            string   `json:"to"`
	ToAccountID   string   `json:"to_account_id"`
	Amount        string   `json:"amount"`
	Currency      string   `json:"currency"`
}

// moveViewFrom builds a moveView from a RecordTransfer result. fromLabel
// and toLabel are the raw --from/--to text the user typed — this package
// already has it on hand, so there's no need for a second lookup just to
// render a name. Which of the transaction's two postings is "from" and
// which is "to" is determined by amount sign (negative is always the
// source, per data-model.md §5's "opposite signs" invariant), not by
// array position — safer than assuming the application layer's own
// internal construction order.
func moveViewFrom(fromLabel, toLabel string, r app.TransactionResult) moveView {
	v := moveView{
		ID:          r.Transaction.ID(),
		Type:        "move",
		Date:        r.Transaction.BookedDate().String(),
		Description: r.Transaction.Description(),
		Notes:       r.Transaction.Notes(),
		From:        fromLabel,
		To:          toLabel,
	}
	for _, t := range r.Tags {
		v.Tags = append(v.Tags, t.String())
	}
	for _, p := range r.Transaction.Postings() {
		if p.Amount().IsNegative() {
			v.FromAccountID = p.AccountID()
			continue
		}
		v.ToAccountID = p.AccountID()
		v.Amount = p.Amount().AmountString()
		v.Currency = p.Currency()
	}
	return v
}

func printMove(w io.Writer, v moveView) {
	fmt.Fprintf(w, "Moved %s %s from %s to %s (%s)\n", v.Amount, v.Currency, v.From, v.To, v.Date)
	fmt.Fprintf(w, "id: %s\n", v.ID)
}

type moveFlags struct {
	from string
	to   string
	on   string
	note string
	tags []string
}

// newMoveCmd builds "move": a transfer between two of the actor's own
// accounts.
//
// Description-defaulting decision: like spend/receive (see entries.go's
// newSpendCmd doc comment), RecordTransferCommand.Description is required
// and non-empty, and the issue's worked example
// (`bodger move 20000 --from Savings --to Checking`) shows no description
// input. There's no positional category to reuse here, so this defaults
// to "Transfer from <raw --from text> to <raw --to text>" — the same
// sentence docs/ux-principles.md §7 uses to motivate the verb itself
// ("I moved ₹20,000 from savings to checking"), built only from text the
// user already typed.
func newMoveCmd(factory ServiceFactory) *cobra.Command {
	var f moveFlags
	cmd := &cobra.Command{
		Use:   "move <amount>",
		Short: "Move money from one account to another",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			description := fmt.Sprintf("Transfer from %s to %s", f.from, f.to)
			result, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
				ActorID:        ports.SeededUserID,
				FromAccountRef: f.from,
				ToAccountRef:   f.to,
				Amount:         args[0],
				Date:           f.on,
				Description:    description,
				Notes:          f.note,
				Tags:           f.tags,
			})
			if err != nil {
				return err
			}
			view := moveViewFrom(f.from, f.to, result)
			return render(cmd, view, func(w io.Writer) { printMove(w, view) })
		},
	}
	cmd.Flags().StringVar(&f.from, "from", "", "the account money leaves (required)")
	cmd.Flags().StringVar(&f.to, "to", "", "the account money arrives in (required)")
	cmd.Flags().StringVar(&f.on, "on", "", "the date this happened (defaults to today)")
	cmd.Flags().StringVar(&f.note, "note", "", "a free-text note to attach")
	cmd.Flags().StringArrayVar(&f.tags, "tag", nil, "a tag to attach (repeatable)")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}
