package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// reportFilterFlags holds the flag variables every `report` subcommand
// binds identically - this CLI's own mirror of app.TransactionFilterInput,
// since ADR-0009 defines one filter shape every M5 analytics query
// shares.
type reportFilterFlags struct {
	account        string
	category       string
	txnType        string
	from, to       string
	filterCurrency []string
	amountMin      string
	amountMax      string
	description    string
	tags           []string
	tagMode        string
}

func (f *reportFilterFlags) input() app.TransactionFilterInput {
	return app.TransactionFilterInput{
		AccountRef:  f.account,
		CategoryRef: f.category,
		Kind:        f.txnType,
		DateFrom:    f.from,
		DateTo:      f.to,
		Currencies:  f.filterCurrency,
		AmountMin:   f.amountMin,
		AmountMax:   f.amountMax,
		Description: f.description,
		Tags:        f.tags,
		TagMode:     f.tagMode,
	}
}

// bindReportFilterFlags registers ADR-0009's filter dimensions on cmd,
// shared verbatim across every `report` subcommand. includeDateRange is
// false only for `report trends`, which defines its own two comparison
// periods and ignores from/to entirely (app.TrendsQuery's own doc
// comment) - omitting the flags there, rather than silently accepting
// and ignoring them, avoids a flag that looks like it does something but
// doesn't.
func bindReportFilterFlags(cmd *cobra.Command, includeDateRange bool) *reportFilterFlags {
	f := &reportFilterFlags{}
	cmd.Flags().StringVar(&f.account, "account", "", "only include this account")
	cmd.Flags().StringVar(&f.category, "category", "", "only include this category (and its subtree)")
	cmd.Flags().StringVar(&f.txnType, "type", "", `only include this type: "outflow" or "inflow"`)
	if includeDateRange {
		cmd.Flags().StringVar(&f.from, "from", "", "the inclusive start of a booked-date range")
		cmd.Flags().StringVar(&f.to, "to", "", "the inclusive end of a booked-date range")
	}
	cmd.Flags().StringArrayVar(&f.filterCurrency, "filter-currency", nil, "only include this transaction currency (repeatable)")
	cmd.Flags().StringVar(&f.amountMin, "amount-min", "", "only include transactions at or above this amount")
	cmd.Flags().StringVar(&f.amountMax, "amount-max", "", "only include transactions at or below this amount")
	cmd.Flags().StringVar(&f.description, "description", "", "only include transactions whose description contains this text")
	cmd.Flags().StringArrayVar(&f.tags, "tag", nil, "only include transactions carrying this tag (repeatable)")
	cmd.Flags().StringVar(&f.tagMode, "tag-mode", "", `how multiple --tag values combine: "any" (default) or "all"`)
	return f
}

// reportOptionsFlags holds the flag variables every `report` subcommand
// binds for ADR-0004's conversion options: the same three flags `bodger
// balance --currency/--policy/--pinned-date` already uses. Unlike
// balance, every M5 analytics result is a single converted aggregate, so
// conversion itself isn't optional here - but --currency still isn't
// required on the command line: a blank value falls through
// app.resolveAnalyticsOptions' ADR-0004 ladder (the actor's own reporting
// currency preference, then the instance default), the same way `bodger
// balance` and `bodger fx rates fetch` already resolve theirs. --policy
// has no such ladder - there's no sensible default conversion policy, so
// it stays required.
type reportOptionsFlags struct {
	currency   string
	policy     string
	pinnedDate string
}

func (f *reportOptionsFlags) options() app.AnalyticsOptions {
	return app.AnalyticsOptions{
		ReportingCurrency: f.currency,
		Policy:            app.ConversionPolicy(f.policy),
		PinnedDate:        f.pinnedDate,
	}
}

func bindReportOptionsFlags(cmd *cobra.Command) *reportOptionsFlags {
	f := &reportOptionsFlags{}
	cmd.Flags().StringVar(&f.currency, "currency", "", "convert every figure into this currency (defaults to your reporting currency)")
	cmd.Flags().StringVar(&f.policy, "policy", "", "which conversion policy to use: transaction_date, current, or pinned (required)")
	cmd.Flags().StringVar(&f.pinnedDate, "pinned-date", "", "the pinned date to convert at (required with --policy pinned)")
	return f
}

// unconvertedPostingView names one posting an analytics query's requested
// conversion couldn't cover (app.UnconvertedPosting) - listed plainly with
// its reason, the same "not silently dropped from a total" rule
// balance.go's balancesView.Unconverted already follows.
type unconvertedPostingView struct {
	TransactionID string `json:"transaction_id"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	Reason        string `json:"reason"`
}

func unconvertedPostingViewsFrom(u []app.UnconvertedPosting) []unconvertedPostingView {
	views := make([]unconvertedPostingView, 0, len(u))
	for _, p := range u {
		views = append(views, unconvertedPostingView{
			TransactionID: p.TransactionID,
			Amount:        p.Amount.AmountString(),
			Currency:      p.Amount.Currency(),
			Reason:        p.Reason,
		})
	}
	return views
}

func printUnconverted(w io.Writer, u []unconvertedPostingView) {
	if len(u) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Couldn't convert (fetch the missing rate with `bodger fx rates fetch`):")
	for _, p := range u {
		_, _ = fmt.Fprintf(w, "  %s %s: %s\n", p.Amount, p.Currency, p.Reason)
	}
}

// ---- category-breakdown ----

type categoryBreakdownRowView struct {
	Category string `json:"category"`
	Spending string `json:"spending"`
	Income   string `json:"income"`
	Net      string `json:"net"`
}

type categoryBreakdownView struct {
	Currency    string                     `json:"currency"`
	Rows        []categoryBreakdownRowView `json:"rows"`
	Unconverted []unconvertedPostingView   `json:"unconverted,omitempty"`
}

func categoryBreakdownViewFrom(r app.CategoryBreakdownResult) categoryBreakdownView {
	v := categoryBreakdownView{Currency: r.Options.ReportingCurrency, Unconverted: unconvertedPostingViewsFrom(r.Unconverted)}
	for _, row := range r.Rows {
		name := "Uncategorized"
		if row.Category != nil {
			name = row.Category.Name()
		}
		v.Rows = append(v.Rows, categoryBreakdownRowView{
			Category: name,
			Spending: row.Spending.AmountString(),
			Income:   row.Income.AmountString(),
			Net:      row.Net.AmountString(),
		})
	}
	return v
}

func printCategoryBreakdown(w io.Writer, v categoryBreakdownView) {
	if len(v.Rows) == 0 {
		_, _ = fmt.Fprintln(w, "No matching transactions.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "CATEGORY\tSPENDING\tINCOME\tNET\n")
	for _, row := range v.Rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s %s\t%s %s\t%s %s\n", row.Category, row.Spending, v.Currency, row.Income, v.Currency, row.Net, v.Currency)
	}
	_ = tw.Flush()
	printUnconverted(w, v.Unconverted)
}

func newReportCategoryBreakdownCmd(factory ServiceFactory) *cobra.Command {
	var filter *reportFilterFlags
	var opts *reportOptionsFlags
	cmd := &cobra.Command{
		Use:   "category-breakdown",
		Short: "Spending and income, by top-level category",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.CategoryBreakdown(ctx, app.CategoryBreakdownQuery{
				ActorID: ports.SeededUserID,
				Filter:  filter.input(),
				Options: opts.options(),
			})
			if err != nil {
				return err
			}
			view := categoryBreakdownViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printCategoryBreakdown(w, view) })
		},
	}
	filter = bindReportFilterFlags(cmd, true)
	opts = bindReportOptionsFlags(cmd)
	return cmd
}

// ---- cash-flow ----

type cashFlowPointView struct {
	Month   string `json:"month"`
	Inflow  string `json:"inflow"`
	Outflow string `json:"outflow"`
	Net     string `json:"net"`
}

type cashFlowView struct {
	Currency    string                   `json:"currency"`
	Points      []cashFlowPointView      `json:"points"`
	Unconverted []unconvertedPostingView `json:"unconverted,omitempty"`
}

func cashFlowViewFrom(r app.CashFlowResult) cashFlowView {
	v := cashFlowView{Currency: r.Options.ReportingCurrency, Unconverted: unconvertedPostingViewsFrom(r.Unconverted)}
	for _, p := range r.Points {
		v.Points = append(v.Points, cashFlowPointView{
			Month:   fmt.Sprintf("%04d-%02d", p.Year, int(p.Month)),
			Inflow:  p.Inflow.AmountString(),
			Outflow: p.Outflow.AmountString(),
			Net:     p.Net.AmountString(),
		})
	}
	return v
}

func printCashFlow(w io.Writer, v cashFlowView) {
	if len(v.Points) == 0 {
		_, _ = fmt.Fprintln(w, "No matching transactions.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "MONTH\tINFLOW\tOUTFLOW\tNET\n")
	for _, p := range v.Points {
		_, _ = fmt.Fprintf(tw, "%s\t%s %s\t%s %s\t%s %s\n", p.Month, p.Inflow, v.Currency, p.Outflow, v.Currency, p.Net, v.Currency)
	}
	_ = tw.Flush()
	printUnconverted(w, v.Unconverted)
}

func newReportCashFlowCmd(factory ServiceFactory) *cobra.Command {
	var filter *reportFilterFlags
	var opts *reportOptionsFlags
	cmd := &cobra.Command{
		Use:   "cash-flow",
		Short: "Inflow vs. outflow, by calendar month",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.CashFlow(ctx, app.CashFlowQuery{
				ActorID: ports.SeededUserID,
				Filter:  filter.input(),
				Options: opts.options(),
			})
			if err != nil {
				return err
			}
			view := cashFlowViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printCashFlow(w, view) })
		},
	}
	filter = bindReportFilterFlags(cmd, true)
	opts = bindReportOptionsFlags(cmd)
	return cmd
}

// ---- trends ----

type trendPeriodView struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Inflow  string `json:"inflow"`
	Outflow string `json:"outflow"`
	Net     string `json:"net"`
}

func trendPeriodViewFrom(p app.TrendPeriod) trendPeriodView {
	return trendPeriodView{
		From:    p.From.String(),
		To:      p.To.String(),
		Inflow:  p.Inflow.AmountString(),
		Outflow: p.Outflow.AmountString(),
		Net:     p.Net.AmountString(),
	}
}

type trendsView struct {
	Currency         string                   `json:"currency"`
	Current          trendPeriodView          `json:"current"`
	Previous         trendPeriodView          `json:"previous"`
	InflowChangePct  *float64                 `json:"inflow_change_pct,omitempty"`
	OutflowChangePct *float64                 `json:"outflow_change_pct,omitempty"`
	Unconverted      []unconvertedPostingView `json:"unconverted,omitempty"`
}

func trendsViewFrom(r app.TrendsResult) trendsView {
	return trendsView{
		Currency:         r.Options.ReportingCurrency,
		Current:          trendPeriodViewFrom(r.Current),
		Previous:         trendPeriodViewFrom(r.Previous),
		InflowChangePct:  r.InflowChangePct,
		OutflowChangePct: r.OutflowChangePct,
		Unconverted:      unconvertedPostingViewsFrom(r.Unconverted),
	}
}

// formatChangePct renders a nil-safe percentage change - "n/a" when the
// previous period was zero (an undefined ratio, not zero; app.changePct's
// own doc comment), matching balance.go's own "there is nothing to print
// here" convention for an absent optional figure.
func formatChangePct(pct *float64) string {
	if pct == nil {
		return "n/a"
	}
	return fmt.Sprintf("%+.1f%%", *pct)
}

func printTrends(w io.Writer, v trendsView) {
	_, _ = fmt.Fprintf(w, "Previous (%s to %s): inflow %s %s, outflow %s %s, net %s %s\n",
		v.Previous.From, v.Previous.To, v.Previous.Inflow, v.Currency, v.Previous.Outflow, v.Currency, v.Previous.Net, v.Currency)
	_, _ = fmt.Fprintf(w, "Current  (%s to %s): inflow %s %s (%s), outflow %s %s (%s), net %s %s\n",
		v.Current.From, v.Current.To, v.Current.Inflow, v.Currency, formatChangePct(v.InflowChangePct),
		v.Current.Outflow, v.Currency, formatChangePct(v.OutflowChangePct), v.Current.Net, v.Currency)
	printUnconverted(w, v.Unconverted)
}

func newReportTrendsCmd(factory ServiceFactory) *cobra.Command {
	var filter *reportFilterFlags
	var opts *reportOptionsFlags
	cmd := &cobra.Command{
		Use:   "trends",
		Short: "This calendar month vs. the previous one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.Trends(ctx, app.TrendsQuery{
				ActorID: ports.SeededUserID,
				Filter:  filter.input(),
				Options: opts.options(),
			})
			if err != nil {
				return err
			}
			view := trendsViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printTrends(w, view) })
		},
	}
	filter = bindReportFilterFlags(cmd, false)
	opts = bindReportOptionsFlags(cmd)
	return cmd
}

// ---- savings-rate ----

type savingsRateView struct {
	Currency    string                   `json:"currency"`
	Income      string                   `json:"income"`
	Outflow     string                   `json:"outflow"`
	Net         string                   `json:"net"`
	Rate        *float64                 `json:"rate,omitempty"`
	Unconverted []unconvertedPostingView `json:"unconverted,omitempty"`
}

func savingsRateViewFrom(r app.SavingsRateResult) savingsRateView {
	return savingsRateView{
		Currency:    r.Options.ReportingCurrency,
		Income:      r.Income.AmountString(),
		Outflow:     r.Outflow.AmountString(),
		Net:         r.Net.AmountString(),
		Rate:        r.Rate,
		Unconverted: unconvertedPostingViewsFrom(r.Unconverted),
	}
}

func printSavingsRate(w io.Writer, v savingsRateView) {
	rate := "n/a"
	if v.Rate != nil {
		rate = fmt.Sprintf("%.1f%%", *v.Rate*100)
	}
	_, _ = fmt.Fprintf(w, "Income %s %s, outflow %s %s, net %s %s, savings rate %s\n",
		v.Income, v.Currency, v.Outflow, v.Currency, v.Net, v.Currency, rate)
	printUnconverted(w, v.Unconverted)
}

func newReportSavingsRateCmd(factory ServiceFactory) *cobra.Command {
	var filter *reportFilterFlags
	var opts *reportOptionsFlags
	cmd := &cobra.Command{
		Use:   "savings-rate",
		Short: "(Income − outflow) / income, over a date range",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.SavingsRate(ctx, app.SavingsRateQuery{
				ActorID: ports.SeededUserID,
				Filter:  filter.input(),
				Options: opts.options(),
			})
			if err != nil {
				return err
			}
			view := savingsRateViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printSavingsRate(w, view) })
		},
	}
	filter = bindReportFilterFlags(cmd, true)
	opts = bindReportOptionsFlags(cmd)
	return cmd
}

// newReportCmd builds "report", the parent command for every M5
// analytics query - docs/architecture.md §8's "machine-readable output
// from the CLI" line, exposed as `bodger report <metric> --json` over
// the same root --json flag every other command already uses.
func newReportCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Analytics over your transactions: spending by category, cash flow, trends, and savings rate",
	}
	cmd.AddCommand(newReportCategoryBreakdownCmd(factory))
	cmd.AddCommand(newReportCashFlowCmd(factory))
	cmd.AddCommand(newReportTrendsCmd(factory))
	cmd.AddCommand(newReportSavingsRateCmd(factory))
	return cmd
}
