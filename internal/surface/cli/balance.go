package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

type balanceEntryView struct {
	AccountID string `json:"account_id"`
	Account   string `json:"account"`
	Currency  string `json:"currency"`
	Balance   string `json:"balance"`
	Archived  bool   `json:"archived"`
}

type balancesView struct {
	AsOf     string             `json:"as_of"`
	Balances []balanceEntryView `json:"balances"`
}

func balancesViewFrom(r app.AccountBalancesResult) balancesView {
	v := balancesView{AsOf: r.AsOf.String()}
	for _, b := range r.Balances {
		v.Balances = append(v.Balances, balanceEntryView{
			AccountID: b.Account.ID(),
			Account:   b.Account.Name(),
			Currency:  b.Balance.Currency(),
			Balance:   b.Balance.AmountString(),
			Archived:  b.Account.Archived(),
		})
	}
	return v
}

func printBalances(w io.Writer, v balancesView) {
	if len(v.Balances) == 0 {
		_, _ = fmt.Fprintln(w, "No accounts yet. Add one with `bodger accounts add`.")
		return
	}
	_, _ = fmt.Fprintf(w, "As of %s:\n", v.AsOf)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	for _, b := range v.Balances {
		_, _ = fmt.Fprintf(tw, "%s\t%s %s\n", b.Account, b.Balance, b.Currency)
	}
	_ = tw.Flush()
}

// newBalanceCmd builds "balance" — docs/ux-principles.md §1's "what do I
// have available?" question, answered directly.
func newBalanceCmd(factory ServiceFactory) *cobra.Command {
	var on string
	cmd := &cobra.Command{
		Use:   "balance",
		Short: "See what you have in each account",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{ActorID: ports.SeededUserID, AsOf: on})
			if err != nil {
				return err
			}
			view := balancesViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printBalances(w, view) })
		},
	}
	cmd.Flags().StringVar(&on, "on", "", "compute balances as of this date (defaults to today)")
	return cmd
}
