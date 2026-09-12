package http

import (
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
)

// netWorthPointView is one period's net worth, as of that period's own
// end date (app.NetWorthPoint) — "net worth as of the end of this
// week/month/year".
type netWorthPointView struct {
	Date   string `json:"date" format:"date"`
	Amount string `json:"amount" format:"money"`
}

// netWorthOverTimeView is GET /api/v1/balances/net-worth-over-time's
// response shape: the reporting currency every point is expressed in, one
// point per period boundary in the requested range, and any account some
// period's own conversion couldn't cover — reported once, not once per
// period (app.NetWorthOverTimeResult.Unconverted's own doc comment).
type netWorthOverTimeView struct {
	Currency    string                   `json:"currency"`
	Points      []netWorthPointView      `json:"points"`
	Unconverted []unconvertedBalanceView `json:"unconverted,omitempty"`
}

func netWorthOverTimeViewFrom(r app.NetWorthOverTimeResult) netWorthOverTimeView {
	v := netWorthOverTimeView{
		Currency: r.Options.ReportingCurrency,
		Points:   []netWorthPointView{},
	}
	for _, p := range r.Points {
		v.Points = append(v.Points, netWorthPointView{
			Date:   p.Date.String(),
			Amount: p.Amount.AmountString(),
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

// getNetWorthOverTime handles GET /api/v1/balances/net-worth-over-time:
// issue #196's net-worth-over-time use case — the total balance across
// every account, plotted as a time series over "from".."to" (both
// required: there's no sensible default range for a series, unlike
// GET /api/v1/balances' own single "as_of" date). "currency" is optional,
// resolving through ADR-0004's ladder server-side the same way GET
// /api/v1/balances/totals' own "currency" does. "policy" is required.
// Every filter dimension other than "from"/"to" is ignored: net worth is
// a whole-ledger figure, not scoped to one account or category.
func (h *handlers) getNetWorthOverTime(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.svc.NetWorthOverTime(r.Context(), app.NetWorthOverTimeQuery{
		ActorID: actorID(r),
		Filter: app.TransactionFilterInput{
			DateFrom: q.Get("from"),
			DateTo:   q.Get("to"),
		},
		TargetCurrency: q.Get("currency"),
		Policy:         app.ConversionPolicy(q.Get("policy")),
		PinnedDate:     q.Get("pinned_date"),
		Granularity:    app.Granularity(q.Get("granularity")),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, netWorthOverTimeViewFrom(result))
}
