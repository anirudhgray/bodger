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
// false only for `report trends` with its default granularity, which
// defines its own two comparison periods and ignores from/to entirely
// (app.TrendsQuery's own doc comment) - omitting the flags there, rather
// than silently accepting and ignoring them, avoids a flag that looks
// like it does something but doesn't. `report trends` still binds them
// (with dateRangeHelp overridden) since --granularity custom needs them
// (issue #194).
func bindReportFilterFlags(cmd *cobra.Command, includeDateRange bool, dateRangeHelp ...string) *reportFilterFlags {
	f := &reportFilterFlags{}
	cmd.Flags().StringVar(&f.account, "account", "", "only include this account")
	cmd.Flags().StringVar(&f.category, "category", "", "only include this category (and its subtree)")
	cmd.Flags().StringVar(&f.txnType, "type", "", `only include this type: "outflow" or "inflow"`)
	if includeDateRange {
		fromHelp, toHelp := "the inclusive start of a booked-date range", "the inclusive end of a booked-date range"
		if len(dateRangeHelp) == 2 {
			fromHelp, toHelp = dateRangeHelp[0], dateRangeHelp[1]
		}
		cmd.Flags().StringVar(&f.from, "from", "", fromHelp)
		cmd.Flags().StringVar(&f.to, "to", "", toHelp)
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

// bindGranularityFlag registers --granularity (issue #194), shared by
// `report cash-flow` and `report trends` only - `report
// category-breakdown`/`report savings-rate` already take an arbitrary
// date range and have no period to bucket or compare.
func bindGranularityFlag(cmd *cobra.Command) *string {
	var granularity string
	cmd.Flags().StringVar(&granularity, "granularity", "", `how to bucket/compare periods: "week", "month" (default), "year", or "custom"`)
	return &granularity
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

// cashFlowPointView is one period's totals, its bounds named From/To
// (issue #194) - granularity-agnostic, since a week/year/custom bucket
// doesn't fit a single calendar month the way the original month-only
// bucketing did.
type cashFlowPointView struct {
	From    string `json:"from"`
	To      string `json:"to"`
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
			From:    p.From.String(),
			To:      p.To.String(),
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
	_, _ = fmt.Fprintf(tw, "FROM\tTO\tINFLOW\tOUTFLOW\tNET\n")
	for _, p := range v.Points {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s %s\t%s %s\t%s %s\n", p.From, p.To, p.Inflow, v.Currency, p.Outflow, v.Currency, p.Net, v.Currency)
	}
	_ = tw.Flush()
	printUnconverted(w, v.Unconverted)
}

func newReportCashFlowCmd(factory ServiceFactory) *cobra.Command {
	var filter *reportFilterFlags
	var opts *reportOptionsFlags
	var granularity *string
	cmd := &cobra.Command{
		Use:   "cash-flow",
		Short: "Inflow vs. outflow, bucketed by period",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.CashFlow(ctx, app.CashFlowQuery{
				ActorID:     ports.SeededUserID,
				Filter:      filter.input(),
				Options:     opts.options(),
				Granularity: app.Granularity(*granularity),
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
	granularity = bindGranularityFlag(cmd)
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
	var granularity *string
	cmd := &cobra.Command{
		Use:   "trends",
		Short: "The current period vs. the immediately preceding one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.Trends(ctx, app.TrendsQuery{
				ActorID:     ports.SeededUserID,
				Filter:      filter.input(),
				Options:     opts.options(),
				Granularity: app.Granularity(*granularity),
			})
			if err != nil {
				return err
			}
			view := trendsViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printTrends(w, view) })
		},
	}
	filter = bindReportFilterFlags(cmd, true,
		`the inclusive start of the current period (required, and only used, with --granularity custom)`,
		`the inclusive end of the current period (required, and only used, with --granularity custom)`)
	opts = bindReportOptionsFlags(cmd)
	granularity = bindGranularityFlag(cmd)
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

// ---- top-transactions ----

type topTransactionsRowView struct {
	TransactionID string `json:"transaction_id"`
	Description   string `json:"description"`
	Date          string `json:"date"`
	Category      string `json:"category"`
	Amount        string `json:"amount"`
}

type topTransactionsView struct {
	Currency    string                   `json:"currency"`
	Rows        []topTransactionsRowView `json:"rows"`
	Unconverted []unconvertedPostingView `json:"unconverted,omitempty"`
}

func topTransactionsViewFrom(r app.TopTransactionsResult) topTransactionsView {
	v := topTransactionsView{Currency: r.Options.ReportingCurrency, Unconverted: unconvertedPostingViewsFrom(r.Unconverted)}
	for _, row := range r.Rows {
		name := "Uncategorized"
		if row.Category != nil {
			name = row.Category.Name()
		}
		v.Rows = append(v.Rows, topTransactionsRowView{
			TransactionID: row.TransactionID,
			Description:   row.Description,
			Date:          row.BookedDate.String(),
			Category:      name,
			Amount:        row.Amount.AmountString(),
		})
	}
	return v
}

func printTopTransactions(w io.Writer, v topTransactionsView) {
	if len(v.Rows) == 0 {
		_, _ = fmt.Fprintln(w, "No matching transactions.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "DATE\tDESCRIPTION\tCATEGORY\tAMOUNT\n")
	for _, row := range v.Rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s %s\n", row.Date, row.Description, row.Category, row.Amount, v.Currency)
	}
	_ = tw.Flush()
	printUnconverted(w, v.Unconverted)
}

func newReportTopTransactionsCmd(factory ServiceFactory) *cobra.Command {
	var filter *reportFilterFlags
	var opts *reportOptionsFlags
	var limit int
	cmd := &cobra.Command{
		Use:   "top-transactions",
		Short: "The largest transactions in a period, by absolute amount",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.TopTransactions(ctx, app.TopTransactionsQuery{
				ActorID: ports.SeededUserID,
				Filter:  filter.input(),
				Options: opts.options(),
				Limit:   limit,
			})
			if err != nil {
				return err
			}
			view := topTransactionsViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printTopTransactions(w, view) })
		},
	}
	filter = bindReportFilterFlags(cmd, true)
	opts = bindReportOptionsFlags(cmd)
	cmd.Flags().IntVar(&limit, "limit", 0, "the top-N count (defaults to 10, capped at 100)")
	return cmd
}

// ---- average-transaction-size ----

type averageTransactionSizeRowView struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
	Average  string `json:"average"`
}

type averageTransactionSizeView struct {
	Currency    string                          `json:"currency"`
	Overall     averageTransactionSizeRowView   `json:"overall"`
	ByCategory  []averageTransactionSizeRowView `json:"by_category"`
	Unconverted []unconvertedPostingView        `json:"unconverted,omitempty"`
}

func averageTransactionSizeRowViewFrom(row app.AverageTransactionSizeRow) averageTransactionSizeRowView {
	name := "Uncategorized"
	if row.Category != nil {
		name = row.Category.Name()
	}
	return averageTransactionSizeRowView{Category: name, Count: row.Count, Average: row.Average.AmountString()}
}

func averageTransactionSizeViewFrom(r app.AverageTransactionSizeResult) averageTransactionSizeView {
	v := averageTransactionSizeView{
		Currency:    r.Options.ReportingCurrency,
		Overall:     averageTransactionSizeRowViewFrom(r.Overall),
		Unconverted: unconvertedPostingViewsFrom(r.Unconverted),
	}
	for _, row := range r.ByCategory {
		v.ByCategory = append(v.ByCategory, averageTransactionSizeRowViewFrom(row))
	}
	return v
}

func printAverageTransactionSize(w io.Writer, v averageTransactionSizeView) {
	_, _ = fmt.Fprintf(w, "Overall: %s %s (%d transactions)\n", v.Overall.Average, v.Currency, v.Overall.Count)
	if len(v.ByCategory) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "By category:")
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		_, _ = fmt.Fprintf(tw, "CATEGORY\tCOUNT\tAVERAGE\n")
		for _, row := range v.ByCategory {
			_, _ = fmt.Fprintf(tw, "%s\t%d\t%s %s\n", row.Category, row.Count, row.Average, v.Currency)
		}
		_ = tw.Flush()
	}
	printUnconverted(w, v.Unconverted)
}

func newReportAverageTransactionSizeCmd(factory ServiceFactory) *cobra.Command {
	var filter *reportFilterFlags
	var opts *reportOptionsFlags
	cmd := &cobra.Command{
		Use:   "average-transaction-size",
		Short: "The mean transaction amount, overall and by top-level category",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.AverageTransactionSize(ctx, app.AverageTransactionSizeQuery{
				ActorID: ports.SeededUserID,
				Filter:  filter.input(),
				Options: opts.options(),
			})
			if err != nil {
				return err
			}
			view := averageTransactionSizeViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printAverageTransactionSize(w, view) })
		},
	}
	filter = bindReportFilterFlags(cmd, true)
	opts = bindReportOptionsFlags(cmd)
	return cmd
}

// ---- category-trends ----

type categoryTrendDeltaView struct {
	Category          string                   `json:"category"`
	Current           categoryBreakdownRowView `json:"current"`
	Previous          categoryBreakdownRowView `json:"previous"`
	SpendingChangePct *float64                 `json:"spending_change_pct,omitempty"`
	IncomeChangePct   *float64                 `json:"income_change_pct,omitempty"`
}

type categoryTrendsView struct {
	Currency     string                   `json:"currency"`
	CurrentFrom  string                   `json:"current_from"`
	CurrentTo    string                   `json:"current_to"`
	PreviousFrom string                   `json:"previous_from"`
	PreviousTo   string                   `json:"previous_to"`
	Rows         []categoryTrendDeltaView `json:"rows"`
	Unconverted  []unconvertedPostingView `json:"unconverted,omitempty"`
}

func categoryBreakdownRowViewFrom(row app.CategoryBreakdownRow) categoryBreakdownRowView {
	name := "Uncategorized"
	if row.Category != nil {
		name = row.Category.Name()
	}
	return categoryBreakdownRowView{Category: name, Spending: row.Spending.AmountString(), Income: row.Income.AmountString(), Net: row.Net.AmountString()}
}

func categoryTrendsViewFrom(r app.CategoryTrendsResult) categoryTrendsView {
	v := categoryTrendsView{
		Currency:     r.Options.ReportingCurrency,
		CurrentFrom:  r.CurrentFrom.String(),
		CurrentTo:    r.CurrentTo.String(),
		PreviousFrom: r.PreviousFrom.String(),
		PreviousTo:   r.PreviousTo.String(),
		Unconverted:  unconvertedPostingViewsFrom(r.Unconverted),
	}
	for _, row := range r.Rows {
		name := "Uncategorized"
		if row.Category != nil {
			name = row.Category.Name()
		}
		v.Rows = append(v.Rows, categoryTrendDeltaView{
			Category:          name,
			Current:           categoryBreakdownRowViewFrom(row.Current),
			Previous:          categoryBreakdownRowViewFrom(row.Previous),
			SpendingChangePct: row.SpendingChangePct,
			IncomeChangePct:   row.IncomeChangePct,
		})
	}
	return v
}

func printCategoryTrends(w io.Writer, v categoryTrendsView) {
	if len(v.Rows) == 0 {
		_, _ = fmt.Fprintln(w, "No matching transactions.")
		return
	}
	_, _ = fmt.Fprintf(w, "Previous (%s to %s) vs. current (%s to %s):\n", v.PreviousFrom, v.PreviousTo, v.CurrentFrom, v.CurrentTo)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "CATEGORY\tSPENDING\tCHANGE\tINCOME\tCHANGE\n")
	for _, row := range v.Rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s %s\t%s\t%s %s\t%s\n",
			row.Category, row.Current.Spending, v.Currency, formatChangePct(row.SpendingChangePct),
			row.Current.Income, v.Currency, formatChangePct(row.IncomeChangePct))
	}
	_ = tw.Flush()
	printUnconverted(w, v.Unconverted)
}

func newReportCategoryTrendsCmd(factory ServiceFactory) *cobra.Command {
	var filter *reportFilterFlags
	var opts *reportOptionsFlags
	var granularity *string
	cmd := &cobra.Command{
		Use:   "category-trends",
		Short: "Month-over-month (or period-over-period) spending/income change, per category",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.CategoryTrends(ctx, app.CategoryTrendsQuery{
				ActorID:     ports.SeededUserID,
				Filter:      filter.input(),
				Options:     opts.options(),
				Granularity: app.Granularity(*granularity),
			})
			if err != nil {
				return err
			}
			view := categoryTrendsViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printCategoryTrends(w, view) })
		},
	}
	filter = bindReportFilterFlags(cmd, true,
		`the inclusive start of the current period (required, and only used, with --granularity custom)`,
		`the inclusive end of the current period (required, and only used, with --granularity custom)`)
	opts = bindReportOptionsFlags(cmd)
	granularity = bindGranularityFlag(cmd)
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
	cmd.AddCommand(newReportTopTransactionsCmd(factory))
	cmd.AddCommand(newReportAverageTransactionSizeCmd(factory))
	cmd.AddCommand(newReportCategoryTrendsCmd(factory))
	return cmd
}
