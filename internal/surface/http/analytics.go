package http

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// parseReportFilterQuery decodes ADR-0009's shared TransactionFilterInput
// dimensions from q — the one place every /api/v1/analytics/* handler
// below does this, so the query-string convention (repeated
// "filter_currency"/"tag" params, everything else single-valued) stays
// identical across all four. "currency" itself is deliberately not read
// here: it names the *reporting* currency (parseReportOptionsQuery), not
// a filter dimension - "filter_currency" is this shared struct's
// Currencies field, kept under a different name so the two never collide
// on the wire.
func parseReportFilterQuery(q url.Values) app.TransactionFilterInput {
	return app.TransactionFilterInput{
		AccountRef:  q.Get("account"),
		CategoryRef: q.Get("category"),
		Kind:        q.Get("type"),
		DateFrom:    q.Get("from"),
		DateTo:      q.Get("to"),
		Currencies:  q["filter_currency"],
		AmountMin:   q.Get("amount_min"),
		AmountMax:   q.Get("amount_max"),
		Description: q.Get("description"),
		Tags:        q["tag"],
		TagMode:     q.Get("tag_mode"),
	}
}

// parseReportOptionsQuery decodes ADR-0004's conversion options every M5
// analytics query needs: the reporting currency every figure in the
// result is converted into, and which policy converts it.
func parseReportOptionsQuery(q url.Values) app.AnalyticsOptions {
	return app.AnalyticsOptions{
		ReportingCurrency: q.Get("currency"),
		Policy:            app.ConversionPolicy(q.Get("policy")),
		PinnedDate:        q.Get("pinned_date"),
	}
}

// parseGranularityQuery decodes "granularity" (issue #194) - a separate
// function from parseReportOptionsQuery/parseReportFilterQuery, read
// only by /cash-flow and /trends, since /category-breakdown and
// /savings-rate don't bucket or compare periods at all.
func parseGranularityQuery(q url.Values) app.Granularity {
	return app.Granularity(q.Get("granularity"))
}

// unconvertedPostingView names one posting an analytics query's requested
// conversion couldn't cover (app.UnconvertedPosting) - listed plainly with
// its reason, the same "not silently dropped from a total" rule
// balancesView.Unconverted already follows.
type unconvertedPostingView struct {
	TransactionID string `json:"transaction_id"`
	Amount        string `json:"amount" format:"money"`
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

// categoryBreakdownRowView is one category's totals. Category is
// "Uncategorized" for app.CategoryBreakdownRow's nil-Category bucket - a
// human-readable label, not a real category's name, since no such
// category exists to name (see app.CategoryBreakdownRow's own doc
// comment).
type categoryBreakdownRowView struct {
	Category string `json:"category"`
	Spending string `json:"spending" format:"money"`
	Income   string `json:"income" format:"money"`
	Net      string `json:"net" format:"money"`
}

type categoryBreakdownView struct {
	Currency    string                     `json:"currency"`
	Rows        []categoryBreakdownRowView `json:"rows"`
	Unconverted []unconvertedPostingView   `json:"unconverted,omitempty"`
}

func categoryBreakdownViewFrom(r app.CategoryBreakdownResult) categoryBreakdownView {
	v := categoryBreakdownView{
		Currency:    r.Options.ReportingCurrency,
		Rows:        []categoryBreakdownRowView{},
		Unconverted: unconvertedPostingViewsFrom(r.Unconverted),
	}
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

func (h *handlers) getCategoryBreakdown(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.svc.CategoryBreakdown(r.Context(), app.CategoryBreakdownQuery{
		ActorID: actorID(r),
		Filter:  parseReportFilterQuery(q),
		Options: parseReportOptionsQuery(q),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, categoryBreakdownViewFrom(result))
}

// cashFlowPointView is one period's totals, its bounds named From/To
// (issue #194) rather than a single calendar month - granularity-
// agnostic, since a week/year/custom bucket doesn't fit a single
// (year, month) pair the way a monthly one did.
type cashFlowPointView struct {
	From    string `json:"from" format:"date"`
	To      string `json:"to" format:"date"`
	Inflow  string `json:"inflow" format:"money"`
	Outflow string `json:"outflow" format:"money"`
	Net     string `json:"net" format:"money"`
}

type cashFlowView struct {
	Currency    string                   `json:"currency"`
	Points      []cashFlowPointView      `json:"points"`
	Unconverted []unconvertedPostingView `json:"unconverted,omitempty"`
}

func cashFlowViewFrom(r app.CashFlowResult) cashFlowView {
	v := cashFlowView{
		Currency:    r.Options.ReportingCurrency,
		Points:      []cashFlowPointView{},
		Unconverted: unconvertedPostingViewsFrom(r.Unconverted),
	}
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

func (h *handlers) getCashFlow(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.svc.CashFlow(r.Context(), app.CashFlowQuery{
		ActorID:     actorID(r),
		Filter:      parseReportFilterQuery(q),
		Options:     parseReportOptionsQuery(q),
		Granularity: parseGranularityQuery(q),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, cashFlowViewFrom(result))
}

// trendPeriodView is one of app.TrendsResult's two comparison periods.
type trendPeriodView struct {
	From    string `json:"from" format:"date"`
	To      string `json:"to" format:"date"`
	Inflow  string `json:"inflow" format:"money"`
	Outflow string `json:"outflow" format:"money"`
	Net     string `json:"net" format:"money"`
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

// trendsView is GET /api/v1/analytics/trends' response shape.
// InflowChangePct/OutflowChangePct are absent (not null, not zero) when
// app.TrendsResult's own field is nil - an undefined percentage change,
// per ADR-0009's "the web UI does no maths" rule: this surface renders
// exactly what the application layer decided, including "there is no
// meaningful number here".
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

func (h *handlers) getTrends(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.svc.Trends(r.Context(), app.TrendsQuery{
		ActorID:     actorID(r),
		Filter:      parseReportFilterQuery(q),
		Options:     parseReportOptionsQuery(q),
		Granularity: parseGranularityQuery(q),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, trendsViewFrom(result))
}

// savingsRateView is GET /api/v1/analytics/savings-rate's response shape.
// Rate is absent (not null, not zero) when app.SavingsRateResult.Rate is
// nil - income was zero, an undefined ratio (see SavingsRateResult's own
// doc comment).
type savingsRateView struct {
	Currency    string                   `json:"currency"`
	Income      string                   `json:"income" format:"money"`
	Outflow     string                   `json:"outflow" format:"money"`
	Net         string                   `json:"net" format:"money"`
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

func (h *handlers) getSavingsRate(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.svc.SavingsRate(r.Context(), app.SavingsRateQuery{
		ActorID: actorID(r),
		Filter:  parseReportFilterQuery(q),
		Options: parseReportOptionsQuery(q),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, savingsRateViewFrom(result))
}

// topTransactionsRowView is one transaction's contribution
// (app.TopTransactionsRow). Category is "Uncategorized" for a nil
// Category, the same convention categoryBreakdownRowView uses.
type topTransactionsRowView struct {
	TransactionID string `json:"transaction_id"`
	Description   string `json:"description"`
	Date          string `json:"date" format:"date"`
	Category      string `json:"category"`
	Amount        string `json:"amount" format:"money"`
}

type topTransactionsView struct {
	Currency    string                   `json:"currency"`
	Rows        []topTransactionsRowView `json:"rows"`
	Unconverted []unconvertedPostingView `json:"unconverted,omitempty"`
}

func topTransactionsViewFrom(r app.TopTransactionsResult) topTransactionsView {
	v := topTransactionsView{
		Currency:    r.Options.ReportingCurrency,
		Rows:        []topTransactionsRowView{},
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

// getTopTransactions handles GET /api/v1/analytics/top-transactions:
// issue #196's top-transactions use case. "limit" defaults to 10 and
// rejects anything above 100 (app.TopTransactions' own contract) —
// parsed here the same way listTransactions' own "limit" is (transactions.go).
func (h *handlers) getTopTransactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 0
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			h.respondError(w, r, errs.New(errs.InvalidInput).Explain("%q isn't a valid limit.", raw).Field("limit"))
			return
		}
		limit = n
	}

	result, err := h.svc.TopTransactions(r.Context(), app.TopTransactionsQuery{
		ActorID: actorID(r),
		Filter:  parseReportFilterQuery(q),
		Options: parseReportOptionsQuery(q),
		Limit:   limit,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, topTransactionsViewFrom(result))
}

// averageTransactionSizeRowView is one bucket's count and mean magnitude
// (app.AverageTransactionSizeRow).
type averageTransactionSizeRowView struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
	Average  string `json:"average" format:"money"`
}

// averageTransactionSizeView is GET
// /api/v1/analytics/average-transaction-size's response shape. Overall is
// its own field, not a row in ByCategory — app.AverageTransactionSizeResult's
// own doc comment explains why: an "Overall" row would collide in meaning
// with a nil-Category "Uncategorized" row.
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
	return averageTransactionSizeRowView{
		Category: name,
		Count:    row.Count,
		Average:  row.Average.AmountString(),
	}
}

func averageTransactionSizeViewFrom(r app.AverageTransactionSizeResult) averageTransactionSizeView {
	v := averageTransactionSizeView{
		Currency:    r.Options.ReportingCurrency,
		Overall:     averageTransactionSizeRowViewFrom(r.Overall),
		ByCategory:  []averageTransactionSizeRowView{},
		Unconverted: unconvertedPostingViewsFrom(r.Unconverted),
	}
	for _, row := range r.ByCategory {
		v.ByCategory = append(v.ByCategory, averageTransactionSizeRowViewFrom(row))
	}
	return v
}

func (h *handlers) getAverageTransactionSize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.svc.AverageTransactionSize(r.Context(), app.AverageTransactionSizeQuery{
		ActorID: actorID(r),
		Filter:  parseReportFilterQuery(q),
		Options: parseReportOptionsQuery(q),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, averageTransactionSizeViewFrom(result))
}

// categoryTrendDeltaView is one category's current-vs-previous comparison
// (app.CategoryTrendDelta).
type categoryTrendDeltaView struct {
	Category          string                   `json:"category"`
	Current           categoryBreakdownRowView `json:"current"`
	Previous          categoryBreakdownRowView `json:"previous"`
	SpendingChangePct *float64                 `json:"spending_change_pct,omitempty"`
	IncomeChangePct   *float64                 `json:"income_change_pct,omitempty"`
}

// categoryTrendsView is GET /api/v1/analytics/category-trends' response
// shape: the resolved current/previous period bounds (mirrors trendsView's
// own current/previous, one level more granular) and one row per
// top-level category present in either period.
type categoryTrendsView struct {
	Currency     string                   `json:"currency"`
	CurrentFrom  string                   `json:"current_from" format:"date"`
	CurrentTo    string                   `json:"current_to" format:"date"`
	PreviousFrom string                   `json:"previous_from" format:"date"`
	PreviousTo   string                   `json:"previous_to" format:"date"`
	Rows         []categoryTrendDeltaView `json:"rows"`
	Unconverted  []unconvertedPostingView `json:"unconverted,omitempty"`
}

func categoryBreakdownRowViewFrom(row app.CategoryBreakdownRow) categoryBreakdownRowView {
	name := "Uncategorized"
	if row.Category != nil {
		name = row.Category.Name()
	}
	return categoryBreakdownRowView{
		Category: name,
		Spending: row.Spending.AmountString(),
		Income:   row.Income.AmountString(),
		Net:      row.Net.AmountString(),
	}
}

func categoryTrendsViewFrom(r app.CategoryTrendsResult) categoryTrendsView {
	v := categoryTrendsView{
		Currency:     r.Options.ReportingCurrency,
		CurrentFrom:  r.CurrentFrom.String(),
		CurrentTo:    r.CurrentTo.String(),
		PreviousFrom: r.PreviousFrom.String(),
		PreviousTo:   r.PreviousTo.String(),
		Rows:         []categoryTrendDeltaView{},
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

func (h *handlers) getCategoryTrends(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.svc.CategoryTrends(r.Context(), app.CategoryTrendsQuery{
		ActorID:     actorID(r),
		Filter:      parseReportFilterQuery(q),
		Options:     parseReportOptionsQuery(q),
		Granularity: parseGranularityQuery(q),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, categoryTrendsViewFrom(result))
}
