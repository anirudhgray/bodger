package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// This file is issue #267's extended read-tier analytics tools: the
// app-layer methods #260 deliberately left unwired (docs/architecture.md
// §9's own #260 paragraph names them) — balance totals/net worth, and
// the M5 analytics suite beyond the plain category-spending report
// (get_category_breakdown, category_breakdown.go) #260 already covers.
// Every tool here is pure wiring, read-tier, and mirrors
// internal/surface/cli/balance.go's and internal/surface/cli/report.go's
// own view-struct shapes field for field, the same "read like the same
// data via a different transport" contract #260's own tools follow.

// ---- get_balance_totals ----

// accountKindTotalView is one ledger.AccountKind's balances, summed and
// converted into the reporting currency (app.AccountKindTotal) —
// mirrors internal/surface/cli/balance.go's own accountKindTotalView
// field for field.
type accountKindTotalView struct {
	Kind   string `json:"kind"`
	Amount string `json:"amount"`
}

// currencyTotalView is every account in one currency, summed in that
// currency — raw, unconverted (app.CurrencyTotal).
type currencyTotalView struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}

// balanceTotalsView is get_balance_totals' result shape
// (app.BalanceTotalsResult).
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
		AsOf:        r.AsOf.String(),
		Currency:    r.Options.ReportingCurrency,
		Overall:     r.Overall.AmountString(),
		ByCategory:  make([]accountKindTotalView, 0, len(r.ByCategory)),
		ByCurrency:  make([]currencyTotalView, 0, len(r.ByCurrency)),
		Unconverted: unconvertedBalanceViewsFrom(r.Unconverted),
	}
	for _, c := range r.ByCategory {
		v.ByCategory = append(v.ByCategory, accountKindTotalView{Kind: string(c.Kind), Amount: c.Total.AmountString()})
	}
	for _, c := range r.ByCurrency {
		v.ByCurrency = append(v.ByCurrency, currencyTotalView{Currency: c.Currency, Amount: c.Total.AmountString()})
	}
	return v
}

// balanceTotalsArgs is get_balance_totals' arguments: an optional as_of
// date, plus ADR-0004's conversion trio.
type balanceTotalsArgs struct {
	AsOf string `json:"as_of,omitempty"`
	analyticsOptionsArgs
}

// getBalanceTotalsTool wires app.Service.BalanceTotals -- the overall net
// balance, by account category, and by currency -- onto a read-tier MCP
// tool.
func getBalanceTotalsTool() ToolDef {
	return ToolDef{
		Name:        "get_balance_totals",
		Description: "Get the overall net balance across every account, broken down by account category and by currency.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				map[string]any{
					"as_of": map[string]any{
						"type":        "string",
						"description": "Compute totals as of this date. Defaults to today.",
					},
				},
				analyticsOptionsSchemaProperties(),
			),
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args balanceTotalsArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			opts := args.options()
			result, err := svc.BalanceTotals(ctx, app.BalanceTotalsQuery{
				ActorID:        actorID,
				AsOf:           args.AsOf,
				TargetCurrency: opts.ReportingCurrency,
				Policy:         opts.Policy,
				PinnedDate:     opts.PinnedDate,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(balanceTotalsViewFrom(result))
		},
	}
}

// ---- get_net_worth_over_time ----

// netWorthPointView is one period's net worth, as of that period's own
// end date (app.NetWorthPoint).
type netWorthPointView struct {
	Date   string `json:"date"`
	Amount string `json:"amount"`
}

// netWorthOverTimeView is get_net_worth_over_time's result shape
// (app.NetWorthOverTimeResult).
type netWorthOverTimeView struct {
	Currency    string                   `json:"currency"`
	Points      []netWorthPointView      `json:"points"`
	Unconverted []unconvertedBalanceView `json:"unconverted,omitempty"`
}

func netWorthOverTimeViewFrom(r app.NetWorthOverTimeResult) netWorthOverTimeView {
	v := netWorthOverTimeView{
		Currency:    r.Options.ReportingCurrency,
		Points:      make([]netWorthPointView, 0, len(r.Points)),
		Unconverted: unconvertedBalanceViewsFrom(r.Unconverted),
	}
	for _, p := range r.Points {
		v.Points = append(v.Points, netWorthPointView{Date: p.Date.String(), Amount: p.Amount.AmountString()})
	}
	return v
}

// netWorthOverTimeArgs is get_net_worth_over_time's arguments: a required
// date range, plus ADR-0004's conversion trio and a bucketing
// granularity. Every TransactionFilterInput dimension other than
// from/to is ignored server-side (net worth is a whole-ledger figure,
// not scoped to one account/category — NetWorthOverTimeQuery's own doc
// comment), so only from/to are exposed here rather than the full
// transactionFilterArgs shape.
type netWorthOverTimeArgs struct {
	From string `json:"from"`
	To   string `json:"to"`
	analyticsOptionsArgs
	Granularity string `json:"granularity,omitempty"`
}

// getNetWorthOverTimeTool wires app.Service.NetWorthOverTime -- the total
// balance across every account, plotted as a time series -- onto a
// read-tier MCP tool.
func getNetWorthOverTimeTool() ToolDef {
	return ToolDef{
		Name:        "get_net_worth_over_time",
		Description: "Get the total balance across every account, plotted at each period boundary within a date range.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				map[string]any{
					"from": map[string]any{
						"type":        "string",
						"description": "The inclusive start of the range.",
					},
					"to": map[string]any{
						"type":        "string",
						"description": "The inclusive end of the range.",
					},
					"granularity": granularitySchemaProperty(),
				},
				analyticsOptionsSchemaProperties(),
			),
			"required":             []string{"from", "to"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args netWorthOverTimeArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			opts := args.options()
			result, err := svc.NetWorthOverTime(ctx, app.NetWorthOverTimeQuery{
				ActorID:        actorID,
				Filter:         app.TransactionFilterInput{DateFrom: args.From, DateTo: args.To},
				TargetCurrency: opts.ReportingCurrency,
				Policy:         opts.Policy,
				PinnedDate:     opts.PinnedDate,
				Granularity:    app.Granularity(args.Granularity),
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(netWorthOverTimeViewFrom(result))
		},
	}
}

// ---- get_cash_flow ----

// cashFlowPointView is one period's totals, mirroring
// internal/surface/cli/report.go's own cashFlowPointView field for
// field.
type cashFlowPointView struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Inflow  string `json:"inflow"`
	Outflow string `json:"outflow"`
	Net     string `json:"net"`
}

// cashFlowView is get_cash_flow's result shape (app.CashFlowResult).
type cashFlowView struct {
	Currency    string                   `json:"currency"`
	Points      []cashFlowPointView      `json:"points"`
	Unconverted []unconvertedPostingView `json:"unconverted,omitempty"`
}

func cashFlowViewFrom(r app.CashFlowResult) cashFlowView {
	v := cashFlowView{
		Currency:    r.Options.ReportingCurrency,
		Points:      make([]cashFlowPointView, 0, len(r.Points)),
		Unconverted: unconvertedPostingViewsFrom(r.Unconverted),
	}
	for _, p := range r.Points {
		v.Points = append(v.Points, cashFlowPointView{
			From: p.From.String(), To: p.To.String(),
			Inflow: p.Inflow.AmountString(), Outflow: p.Outflow.AmountString(), Net: p.Net.AmountString(),
		})
	}
	return v
}

// cashFlowArgs is get_cash_flow's arguments: ADR-0009's filter
// dimensions, ADR-0004's conversion trio, and a bucketing granularity.
type cashFlowArgs struct {
	transactionFilterArgs
	analyticsOptionsArgs
	Granularity string `json:"granularity,omitempty"`
}

// getCashFlowTool wires app.Service.CashFlow -- inflow vs. outflow,
// bucketed by period -- onto a read-tier MCP tool.
func getCashFlowTool() ToolDef {
	return ToolDef{
		Name:        "get_cash_flow",
		Description: "Get inflow vs. outflow over a filtered set of transactions, bucketed by period.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				transactionFilterSchemaProperties(),
				analyticsOptionsSchemaProperties(),
				map[string]any{"granularity": granularitySchemaProperty()},
			),
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args cashFlowArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.CashFlow(ctx, app.CashFlowQuery{
				ActorID:     actorID,
				Filter:      args.input(),
				Options:     args.options(),
				Granularity: app.Granularity(args.Granularity),
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(cashFlowViewFrom(result))
		},
	}
}

// ---- get_trends ----

// trendPeriodView is one period's totals in a Trends comparison,
// mirroring internal/surface/cli/report.go's own trendPeriodView field
// for field.
type trendPeriodView struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Inflow  string `json:"inflow"`
	Outflow string `json:"outflow"`
	Net     string `json:"net"`
}

func trendPeriodViewFrom(p app.TrendPeriod) trendPeriodView {
	return trendPeriodView{
		From: p.From.String(), To: p.To.String(),
		Inflow: p.Inflow.AmountString(), Outflow: p.Outflow.AmountString(), Net: p.Net.AmountString(),
	}
}

// trendsView is get_trends' result shape (app.TrendsResult).
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

// trendsArgs is get_trends' arguments: ADR-0009's filter dimensions
// (ignored except for the granularity-custom case — TrendsQuery's own
// doc comment), ADR-0004's conversion trio, and a comparison
// granularity.
type trendsArgs struct {
	transactionFilterArgs
	analyticsOptionsArgs
	Granularity string `json:"granularity,omitempty"`
}

// getTrendsTool wires app.Service.Trends -- the current period vs. the
// immediately preceding one -- onto a read-tier MCP tool.
func getTrendsTool() ToolDef {
	return ToolDef{
		Name:        "get_trends",
		Description: "Compare the current period against the immediately preceding one, for inflow, outflow, and net.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				transactionFilterSchemaProperties(),
				analyticsOptionsSchemaProperties(),
				map[string]any{"granularity": granularitySchemaProperty()},
			),
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args trendsArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.Trends(ctx, app.TrendsQuery{
				ActorID:     actorID,
				Filter:      args.input(),
				Options:     args.options(),
				Granularity: app.Granularity(args.Granularity),
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(trendsViewFrom(result))
		},
	}
}

// ---- get_savings_rate ----

// savingsRateView is get_savings_rate's result shape
// (app.SavingsRateResult).
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

// savingsRateArgs is get_savings_rate's arguments: ADR-0009's filter
// dimensions plus ADR-0004's conversion trio.
type savingsRateArgs struct {
	transactionFilterArgs
	analyticsOptionsArgs
}

// getSavingsRateTool wires app.Service.SavingsRate -- (income − outflow)
// / income, over a date range -- onto a read-tier MCP tool.
func getSavingsRateTool() ToolDef {
	return ToolDef{
		Name:        "get_savings_rate",
		Description: "Get (income minus outflow) divided by income, over a filtered set of transactions.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				transactionFilterSchemaProperties(),
				analyticsOptionsSchemaProperties(),
			),
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args savingsRateArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.SavingsRate(ctx, app.SavingsRateQuery{
				ActorID: actorID,
				Filter:  args.input(),
				Options: args.options(),
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(savingsRateViewFrom(result))
		},
	}
}

// ---- get_top_transactions ----

// topTransactionsRowView is one transaction's contribution, mirroring
// internal/surface/cli/report.go's own topTransactionsRowView field for
// field.
type topTransactionsRowView struct {
	TransactionID string `json:"transaction_id"`
	Description   string `json:"description"`
	Date          string `json:"date"`
	Category      string `json:"category"`
	Amount        string `json:"amount"`
}

// topTransactionsView is get_top_transactions' result shape
// (app.TopTransactionsResult).
type topTransactionsView struct {
	Currency    string                   `json:"currency"`
	Rows        []topTransactionsRowView `json:"rows"`
	Unconverted []unconvertedPostingView `json:"unconverted,omitempty"`
}

func topTransactionsViewFrom(r app.TopTransactionsResult) topTransactionsView {
	v := topTransactionsView{
		Currency:    r.Options.ReportingCurrency,
		Rows:        make([]topTransactionsRowView, 0, len(r.Rows)),
		Unconverted: unconvertedPostingViewsFrom(r.Unconverted),
	}
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

// topTransactionsArgs is get_top_transactions' arguments: ADR-0009's
// filter dimensions, ADR-0004's conversion trio, and a top-N limit.
type topTransactionsArgs struct {
	transactionFilterArgs
	analyticsOptionsArgs
	Limit int `json:"limit,omitempty"`
}

// getTopTransactionsTool wires app.Service.TopTransactions -- the
// largest transactions in a period, by absolute amount -- onto a
// read-tier MCP tool.
func getTopTransactionsTool() ToolDef {
	return ToolDef{
		Name:        "get_top_transactions",
		Description: "Get the largest transactions matching a filter, by absolute amount.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				transactionFilterSchemaProperties(),
				analyticsOptionsSchemaProperties(),
				map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "The top-N count. Defaults to 10, capped at 100.",
					},
				},
			),
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args topTransactionsArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.TopTransactions(ctx, app.TopTransactionsQuery{
				ActorID: actorID,
				Filter:  args.input(),
				Options: args.options(),
				Limit:   args.Limit,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(topTransactionsViewFrom(result))
		},
	}
}

// ---- get_average_transaction_size ----

// averageTransactionSizeRowView is one bucket's count and mean
// magnitude, mirroring internal/surface/cli/report.go's own
// averageTransactionSizeRowView field for field.
type averageTransactionSizeRowView struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
	Average  string `json:"average"`
}

func averageTransactionSizeRowViewFrom(row app.AverageTransactionSizeRow) averageTransactionSizeRowView {
	name := "Uncategorized"
	if row.Category != nil {
		name = row.Category.Name()
	}
	return averageTransactionSizeRowView{Category: name, Count: row.Count, Average: row.Average.AmountString()}
}

// averageTransactionSizeView is get_average_transaction_size's result
// shape (app.AverageTransactionSizeResult).
type averageTransactionSizeView struct {
	Currency    string                          `json:"currency"`
	Overall     averageTransactionSizeRowView   `json:"overall"`
	ByCategory  []averageTransactionSizeRowView `json:"by_category"`
	Unconverted []unconvertedPostingView        `json:"unconverted,omitempty"`
}

func averageTransactionSizeViewFrom(r app.AverageTransactionSizeResult) averageTransactionSizeView {
	v := averageTransactionSizeView{
		Currency:    r.Options.ReportingCurrency,
		Overall:     averageTransactionSizeRowViewFrom(r.Overall),
		ByCategory:  make([]averageTransactionSizeRowView, 0, len(r.ByCategory)),
		Unconverted: unconvertedPostingViewsFrom(r.Unconverted),
	}
	for _, row := range r.ByCategory {
		v.ByCategory = append(v.ByCategory, averageTransactionSizeRowViewFrom(row))
	}
	return v
}

// averageTransactionSizeArgs is get_average_transaction_size's
// arguments: ADR-0009's filter dimensions plus ADR-0004's conversion
// trio.
type averageTransactionSizeArgs struct {
	transactionFilterArgs
	analyticsOptionsArgs
}

// getAverageTransactionSizeTool wires app.Service.AverageTransactionSize
// -- the mean transaction amount, overall and by top-level category --
// onto a read-tier MCP tool.
func getAverageTransactionSizeTool() ToolDef {
	return ToolDef{
		Name:        "get_average_transaction_size",
		Description: "Get the mean transaction amount over a filtered set of transactions, overall and by top-level category.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				transactionFilterSchemaProperties(),
				analyticsOptionsSchemaProperties(),
			),
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args averageTransactionSizeArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.AverageTransactionSize(ctx, app.AverageTransactionSizeQuery{
				ActorID: actorID,
				Filter:  args.input(),
				Options: args.options(),
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(averageTransactionSizeViewFrom(result))
		},
	}
}

// ---- get_category_trends ----

// categoryTrendDeltaView is one category's current-vs-previous
// comparison, mirroring internal/surface/cli/report.go's own
// categoryTrendDeltaView field for field. Current/Previous reuse
// category_breakdown.go's own categoryBreakdownRowView -- the same
// per-category spending/income/net shape get_category_breakdown already
// renders -- via categoryBreakdownRowViewFrom below, rather than
// declaring a second, identical row shape.
type categoryTrendDeltaView struct {
	Category          string                   `json:"category"`
	Current           categoryBreakdownRowView `json:"current"`
	Previous          categoryBreakdownRowView `json:"previous"`
	SpendingChangePct *float64                 `json:"spending_change_pct,omitempty"`
	IncomeChangePct   *float64                 `json:"income_change_pct,omitempty"`
}

// categoryBreakdownRowViewFrom converts one app.CategoryBreakdownRow into
// the categoryBreakdownRowView category_breakdown.go's get_category_breakdown
// tool already renders inline -- extracted here as its own function since
// get_category_trends needs it for both Current and Previous per row,
// not just once per top-level result.
func categoryBreakdownRowViewFrom(row app.CategoryBreakdownRow) categoryBreakdownRowView {
	name := "Uncategorized"
	if row.Category != nil {
		name = row.Category.Name()
	}
	return categoryBreakdownRowView{Category: name, Spending: row.Spending.AmountString(), Income: row.Income.AmountString(), Net: row.Net.AmountString()}
}

// categoryTrendsView is get_category_trends' result shape
// (app.CategoryTrendsResult).
type categoryTrendsView struct {
	Currency     string                   `json:"currency"`
	CurrentFrom  string                   `json:"current_from"`
	CurrentTo    string                   `json:"current_to"`
	PreviousFrom string                   `json:"previous_from"`
	PreviousTo   string                   `json:"previous_to"`
	Rows         []categoryTrendDeltaView `json:"rows"`
	Unconverted  []unconvertedPostingView `json:"unconverted,omitempty"`
}

func categoryTrendsViewFrom(r app.CategoryTrendsResult) categoryTrendsView {
	v := categoryTrendsView{
		Currency:     r.Options.ReportingCurrency,
		CurrentFrom:  r.CurrentFrom.String(),
		CurrentTo:    r.CurrentTo.String(),
		PreviousFrom: r.PreviousFrom.String(),
		PreviousTo:   r.PreviousTo.String(),
		Rows:         make([]categoryTrendDeltaView, 0, len(r.Rows)),
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

// categoryTrendsArgs is get_category_trends' arguments: ADR-0009's
// filter dimensions, ADR-0004's conversion trio, and a comparison
// granularity -- the same shape get_trends takes, applied per category.
type categoryTrendsArgs struct {
	transactionFilterArgs
	analyticsOptionsArgs
	Granularity string `json:"granularity,omitempty"`
}

// getCategoryTrendsTool wires app.Service.CategoryTrends -- per-category
// spending/income change between the current and immediately preceding
// period -- onto a read-tier MCP tool. Broader than get_category_breakdown
// (a single period's totals): this compares two.
func getCategoryTrendsTool() ToolDef {
	return ToolDef{
		Name:        "get_category_trends",
		Description: "Compare each top-level category's spending and income between the current period and the immediately preceding one.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				transactionFilterSchemaProperties(),
				analyticsOptionsSchemaProperties(),
				map[string]any{"granularity": granularitySchemaProperty()},
			),
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args categoryTrendsArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.CategoryTrends(ctx, app.CategoryTrendsQuery{
				ActorID:     actorID,
				Filter:      args.input(),
				Options:     args.options(),
				Granularity: app.Granularity(args.Granularity),
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(categoryTrendsViewFrom(result))
		},
	}
}
