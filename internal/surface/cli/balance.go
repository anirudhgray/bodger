package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// convertedBalanceView renders a ConvertedAmount inline on a balance
// entry: ADR-0004's "every surface must render provenance somewhere" rule
// applied to this one — rate, rate_date, rate_source, and policy travel
// alongside the converted figure itself, never just the bare number.
type convertedBalanceView struct {
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	Rate       string `json:"rate"`
	RateDate   string `json:"rate_date"`
	RateSource string `json:"rate_source"`
	Stale      bool   `json:"stale"`
	Policy     string `json:"policy"`
}

type balanceEntryView struct {
	AccountID string                `json:"account_id"`
	Account   string                `json:"account"`
	Currency  string                `json:"currency"`
	Balance   string                `json:"balance"`
	Archived  bool                  `json:"archived"`
	Converted *convertedBalanceView `json:"converted,omitempty"`
}

// unconvertedBalanceView names an account issue #136's --currency
// conversion couldn't cover (AccountBalancesResult.Unconverted) — listed
// plainly with its reason, per the issue's "not silently dropped from a
// total" rule.
type unconvertedBalanceView struct {
	Account string `json:"account"`
	Reason  string `json:"reason"`
}

type balancesView struct {
	AsOf        string                   `json:"as_of"`
	Balances    []balanceEntryView       `json:"balances"`
	Unconverted []unconvertedBalanceView `json:"unconverted,omitempty"`
}

func balancesViewFrom(r app.AccountBalancesResult) balancesView {
	v := balancesView{AsOf: r.AsOf.String()}
	for _, b := range r.Balances {
		entry := balanceEntryView{
			AccountID: b.Account.ID(),
			Account:   b.Account.Name(),
			Currency:  b.Balance.Currency(),
			Balance:   b.Balance.AmountString(),
			Archived:  b.Account.Archived(),
		}
		if b.Converted != nil {
			entry.Converted = &convertedBalanceView{
				Amount:     b.Converted.Amount.AmountString(),
				Currency:   b.Converted.Amount.Currency(),
				Rate:       b.Converted.Rate.Value().String(),
				RateDate:   b.Converted.RateDate.String(),
				RateSource: b.Converted.RateSource,
				Stale:      b.Converted.Stale,
				Policy:     string(b.Converted.Policy),
			}
		}
		v.Balances = append(v.Balances, entry)
	}
	for _, u := range r.Unconverted {
		v.Unconverted = append(v.Unconverted, unconvertedBalanceView{
			Account: u.Account.Name(),
			Reason:  u.Reason,
		})
	}
	return v
}

// formatConverted renders c's converted figure alongside its full
// provenance — the rate, its source, the policy it was resolved under, and
// the date it was recorded for — flagging a stale rate explicitly with the
// issue's own worked example phrasing ("(stale, as of 2026-08-30)"). An
// identity conversion (same-currency, RateSource == "") has no rate or
// source to report — ConvertedAmount's own doc comment says as much — so
// this renders only the converted figure for that case.
func formatConverted(c convertedBalanceView) string {
	if c.RateSource == "" {
		return fmt.Sprintf("%s %s", c.Amount, c.Currency)
	}
	line := fmt.Sprintf("%s %s (rate %s, %s policy, source %s, rate dated %s)",
		c.Amount, c.Currency, c.Rate, c.Policy, c.RateSource, c.RateDate)
	if c.Stale {
		line += fmt.Sprintf(" (stale, as of %s)", c.RateDate)
	}
	return line
}

func printBalances(w io.Writer, v balancesView) {
	if len(v.Balances) == 0 {
		_, _ = fmt.Fprintln(w, "No accounts yet. Add one with `bodger accounts add`.")
		return
	}
	_, _ = fmt.Fprintf(w, "As of %s:\n", v.AsOf)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	for _, b := range v.Balances {
		if b.Converted != nil {
			_, _ = fmt.Fprintf(tw, "%s\t%s %s\t≈ %s\n", b.Account, b.Balance, b.Currency, formatConverted(*b.Converted))
			continue
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s %s\n", b.Account, b.Balance, b.Currency)
	}
	_ = tw.Flush()

	if len(v.Unconverted) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Couldn't convert (fetch the missing rate with `bodger fx rates fetch`):")
		for _, u := range v.Unconverted {
			_, _ = fmt.Fprintf(w, "  %s: %s\n", u.Account, u.Reason)
		}
	}
}

// newBalanceCmd builds "balance" — docs/ux-principles.md §1's "what do I
// have available?" question, answered directly. --currency and --policy
// (issue #136) turn on ADR-0004-governed conversion of every account's
// balance into one common currency; leaving --currency empty keeps the
// original single-currency behaviour exactly, unconverted.
func newBalanceCmd(factory ServiceFactory) *cobra.Command {
	var on, currency, policy, pinnedDate string
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

			result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{
				ActorID:        ports.SeededUserID,
				AsOf:           on,
				TargetCurrency: currency,
				Policy:         app.ConversionPolicy(policy),
				PinnedDate:     pinnedDate,
			})
			if err != nil {
				return err
			}
			view := balancesViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printBalances(w, view) })
		},
	}
	cmd.Flags().StringVar(&on, "on", "", "compute balances as of this date (defaults to today)")
	cmd.Flags().StringVar(&currency, "currency", "", "convert every account's balance into this currency")
	cmd.Flags().StringVar(&policy, "policy", "",
		"which conversion policy to use when --currency is set: transaction_date, current, or pinned")
	cmd.Flags().StringVar(&pinnedDate, "pinned-date", "",
		"the pinned date to convert at (required when --policy pinned is used with --currency)")
	return cmd
}
