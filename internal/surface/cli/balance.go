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
	cmd.AddCommand(newBalanceTotalsCmd(factory))
	return cmd
}

// accountKindTotalView is one ledger.AccountKind's balances, summed and
// converted into the reporting currency (app.AccountKindTotal).
type accountKindTotalView struct {
	Kind   string `json:"kind"`
	Amount string `json:"amount"`
}

// currencyTotalView is every account in one currency, summed in that
// currency -- raw, unconverted (app.CurrencyTotal).
type currencyTotalView struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}

// balanceTotalsView is `balance totals`'s output shape: mirrors
// internal/surface/http/balance_totals.go's balanceTotalsView field for
// field (ADR-0005's normalise-once contract; internal/surface/conformance
// proves the two agree).
type balanceTotalsView struct {
	AsOf        string                   `json:"as_of"`
	Currency    string                   `json:"currency"`
	Overall     string                   `json:"overall"`
	ByCategory  []accountKindTotalView   `json:"by_category"`
	ByCurrency  []currencyTotalView      `json:"by_currency"`
	Unconverted []unconvertedBalanceView `json:"unconverted,omitempty"`
}

func balanceTotalsViewFrom(r app.BalanceTotalsResult) balanceTotalsView {
	v := balanceTotalsView{
		AsOf:       r.AsOf.String(),
		Currency:   r.Options.ReportingCurrency,
		Overall:    r.Overall.AmountString(),
		ByCategory: []accountKindTotalView{},
		ByCurrency: []currencyTotalView{},
	}
	for _, c := range r.ByCategory {
		v.ByCategory = append(v.ByCategory, accountKindTotalView{
			Kind:   string(c.Kind),
			Amount: c.Total.AmountString(),
		})
	}
	for _, c := range r.ByCurrency {
		v.ByCurrency = append(v.ByCurrency, currencyTotalView{
			Currency: c.Currency,
			Amount:   c.Total.AmountString(),
		})
	}
	for _, u := range r.Unconverted {
		v.Unconverted = append(v.Unconverted, unconvertedBalanceView{
			Account: u.Account.Name(),
			Reason:  u.Reason,
		})
	}
	return v
}

func printBalanceTotals(w io.Writer, v balanceTotalsView) {
	_, _ = fmt.Fprintf(w, "As of %s:\n", v.AsOf)
	_, _ = fmt.Fprintf(w, "Overall: %s %s\n", v.Overall, v.Currency)

	if len(v.ByCategory) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "By category:")
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		for _, c := range v.ByCategory {
			_, _ = fmt.Fprintf(tw, "  %s\t%s %s\n", c.Kind, c.Amount, v.Currency)
		}
		_ = tw.Flush()
	}

	if len(v.ByCurrency) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "By currency (raw, unconverted):")
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		for _, c := range v.ByCurrency {
			_, _ = fmt.Fprintf(tw, "  %s\t%s\n", c.Currency, c.Amount)
		}
		_ = tw.Flush()
	}

	if len(v.Unconverted) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Couldn't convert (fetch the missing rate with `bodger fx rates fetch`):")
		for _, u := range v.Unconverted {
			_, _ = fmt.Fprintf(w, "  %s: %s\n", u.Account, u.Reason)
		}
	}
}

// newBalanceTotalsCmd builds "balance totals" -- issue #195's totals
// overview: the overall net balance, a per-account-category breakdown,
// and a per-currency (raw) breakdown. Unlike `balance` itself,
// --currency and --policy aren't optional together: --policy is always
// required (Overall and ByCategory are always converted aggregates, the
// same "report is a required-conversion shape" reasoning
// reportOptionsFlags' own doc comment gives), while --currency stays
// optional -- left unset, it resolves through ADR-0004's ladder
// server-side (Service.resolveBalanceTotalsCurrency) rather than
// requiring the caller to already know their own reporting currency.
func newBalanceTotalsCmd(factory ServiceFactory) *cobra.Command {
	var on, currency, policy, pinnedDate string
	cmd := &cobra.Command{
		Use:   "totals",
		Short: "Overall net balance, by category, and by currency",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.BalanceTotals(ctx, app.BalanceTotalsQuery{
				ActorID:        ports.SeededUserID,
				AsOf:           on,
				TargetCurrency: currency,
				Policy:         app.ConversionPolicy(policy),
				PinnedDate:     pinnedDate,
			})
			if err != nil {
				return err
			}
			view := balanceTotalsViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printBalanceTotals(w, view) })
		},
	}
	cmd.Flags().StringVar(&on, "on", "", "compute totals as of this date (defaults to today)")
	cmd.Flags().StringVar(&currency, "currency", "",
		"convert the overall and per-category totals into this currency (defaults to your reporting currency)")
	cmd.Flags().StringVar(&policy, "policy", "",
		"which conversion policy to use: transaction_date, current, or pinned (required)")
	cmd.Flags().StringVar(&pinnedDate, "pinned-date", "",
		"the pinned date to convert at (required when --policy pinned is used)")
	return cmd
}
