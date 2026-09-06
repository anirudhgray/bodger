// This file implements issue #136's `bodger fx rates` surface, wired onto
// internal/app/fetch_fx_rates.go and internal/app/list_fx_rates.go's use
// cases (issue #135) the same one-command, one-application-call way as
// every other command in this package.
//
// Judgment call: internal/app/list_fx_rates.go's ListFxRatesQuery.Amount
// field is typed *money.Money (a domain type internal/domain/money owns).
// This package is never permitted to import internal/domain or any of its
// subpackages (see this package's own doc comment in cli.go, and
// internal/lint's TestImportGraph, which fails the build if it does) —
// every other command in this codebase that accepts a user-typed amount
// passes it through to internal/app as a raw string field instead (see
// e.g. RecordTransferCommand.Amount), which internal/app itself parses
// into a money.Money via normalize.Amount + money.NewMoney. ListFxRates
// has no such string-typed alternative: it requires an already-constructed
// *money.Money, which this package has no way to build without either
// violating that import rule or internal/app growing a small exported
// parsing helper (out of scope for this CLI-surface-only issue — flagged
// back to the issue rather than added here). So "rates list" implements
// the read-only rate lookup (every policy, staleness flagging) but not the
// issue's optional --amount conversion; a --amount flag is not offered.
package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// newFxCmd builds the "fx" command group: "rates", per issue #136.
func newFxCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fx",
		Short: "Fetch and look up exchange rates",
	}
	cmd.AddCommand(newFxRatesCmd(factory))
	return cmd
}

// newFxRatesCmd builds the "fx rates" subgroup: "fetch" and "list", one per
// internal/app's #135 use cases.
func newFxRatesCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rates",
		Short: "Fetch and look up exchange rates",
	}
	cmd.AddCommand(
		newFxRatesFetchCmd(factory),
		newFxRatesListCmd(factory),
	)
	return cmd
}

// fetchedRateView renders one FetchedRate.
type fetchedRateView struct {
	Pair   string `json:"pair"`
	Rate   string `json:"rate"`
	Date   string `json:"date"`
	Source string `json:"source"`
}

type fxFetchView struct {
	ReportingCurrency string            `json:"reporting_currency"`
	Fetched           []fetchedRateView `json:"fetched"`
}

func fxFetchViewFrom(r app.FetchFxRatesResult) fxFetchView {
	v := fxFetchView{ReportingCurrency: r.ReportingCurrency}
	for _, f := range r.Fetched {
		v.Fetched = append(v.Fetched, fetchedRateView{
			Pair:   fmt.Sprintf("%s/%s", f.Rate.Base(), f.Rate.Quote()),
			Rate:   f.Rate.Value().String(),
			Date:   f.Date.String(),
			Source: f.Source,
		})
	}
	return v
}

func printFxFetch(w io.Writer, v fxFetchView) {
	if len(v.Fetched) == 0 {
		_, _ = fmt.Fprintln(w, "Nothing to fetch — no in-use currency pairs found.")
		return
	}
	_, _ = fmt.Fprintf(w, "Fetched %d rate(s) against %s:\n", len(v.Fetched), v.ReportingCurrency)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PAIR\tRATE\tDATE\tSOURCE")
	for _, f := range v.Fetched {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.Pair, f.Rate, f.Date, f.Source)
	}
	_ = tw.Flush()
}

// newFxRatesFetchCmd builds "fx rates fetch": issue #135's fetch-and-store
// use case. With no --pair flags it fetches every currency pair actually
// in use (FetchFxRatesCommand.Pairs left empty); --from/--to together ask
// for a historical backfill instead of just today — both-or-neither is
// FetchFxRates' own validation to enforce (InvalidInput, field "from"),
// not duplicated here.
func newFxRatesFetchCmd(factory ServiceFactory) *cobra.Command {
	var pairs []string
	var from, to string
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Fetch and store exchange rates from the configured provider",
		Long: "Fetch and store exchange rates from the configured provider.\n\n" +
			"With no --pair flags, fetches every currency pair actually in use across your accounts and " +
			"transactions, quoted against your reporting currency, at today's date. Pass --pair to restrict " +
			"the fetch to specific base currencies, and --from/--to together for a historical backfill " +
			"instead of just today.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.FetchFxRates(ctx, app.FetchFxRatesCommand{
				ActorID: ports.SeededUserID,
				Pairs:   pairs,
				From:    from,
				To:      to,
			})
			if err != nil {
				return err
			}
			view := fxFetchViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printFxFetch(w, view) })
		},
	}
	cmd.Flags().StringArrayVar(&pairs, "pair", nil,
		"restrict the fetch to this base currency, quoted against your reporting currency (repeatable; defaults to every in-use pair)")
	cmd.Flags().StringVar(&from, "from", "", "backfill range start date, inclusive (requires --to)")
	cmd.Flags().StringVar(&to, "to", "", "backfill range end date, inclusive (requires --from)")
	return cmd
}

// fxRateView renders a ListFxRatesResult: the resolved rate and its full
// ADR-0004 provenance.
type fxRateView struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Rate       string `json:"rate"`
	RateDate   string `json:"rate_date"`
	RateSource string `json:"rate_source"`
	Stale      bool   `json:"stale"`
	Policy     string `json:"policy"`
}

func fxRateViewFrom(from, to string, r app.ListFxRatesResult) fxRateView {
	return fxRateView{
		From:       from,
		To:         to,
		Rate:       r.Rate.Value().String(),
		RateDate:   r.RateDate.String(),
		RateSource: r.RateSource,
		Stale:      r.Stale,
		Policy:     string(r.Policy),
	}
}

func printFxRate(w io.Writer, v fxRateView) {
	_, _ = fmt.Fprintf(w, "1 %s = %s %s (policy: %s, as of %s", v.From, v.Rate, v.To, v.Policy, v.RateDate)
	if v.RateSource != "" {
		_, _ = fmt.Fprintf(w, ", source: %s", v.RateSource)
	}
	if v.Stale {
		_, _ = fmt.Fprintf(w, ", stale, as of %s", v.RateDate)
	}
	_, _ = fmt.Fprintln(w, ")")
}

// newFxRatesListCmd builds "fx rates list": issue #135's pure, stored-data
// read (ListFxRates), a single pair/policy lookup rather than an
// enumeration despite the "list" name. See this file's doc comment for why
// --amount isn't offered.
func newFxRatesListCmd(factory ServiceFactory) *cobra.Command {
	var from, to, policy, transactionDate, pinnedDate string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Look up the exchange rate between two currencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ListFxRates(ctx, app.ListFxRatesQuery{
				From:            from,
				To:              to,
				Policy:          app.ConversionPolicy(policy),
				TransactionDate: transactionDate,
				PinnedDate:      pinnedDate,
			})
			if err != nil {
				return err
			}
			view := fxRateViewFrom(from, to, result)
			return render(cmd, view, func(w io.Writer) { printFxRate(w, view) })
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "the currency to look up a rate for (required)")
	cmd.Flags().StringVar(&to, "to", "", "the currency it's quoted against (required)")
	cmd.Flags().StringVar(&policy, "policy", "",
		"which conversion policy to use: transaction_date, current, or pinned (required)")
	cmd.Flags().StringVar(&transactionDate, "transaction-date", "",
		"the transaction date to look the rate up at (required with --policy transaction_date)")
	cmd.Flags().StringVar(&pinnedDate, "pinned-date", "",
		"the pinned date to look the rate up at (required with --policy pinned)")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	_ = cmd.MarkFlagRequired("policy")
	return cmd
}
