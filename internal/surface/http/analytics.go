package http

import (
	"net/http"
	"net/url"

	"github.com/anirudhgray/bodger/internal/app"
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
