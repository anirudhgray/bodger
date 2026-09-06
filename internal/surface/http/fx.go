// This file implements issue #137's `GET /api/v1/fx/rates` and
// `POST /api/v1/fx/rates/fetch` routes, wired onto
// internal/app/list_fx_rates.go and internal/app/fetch_fx_rates.go's use
// cases (issue #135) — the HTTP-surface equivalent of
// internal/surface/cli/fx.go, which its view shapes deliberately mirror
// field-for-field so the two surfaces stay in conformance.
package http

import (
	"fmt"
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
)

// fetchedRateView renders one app.FetchedRate. Mirrors
// internal/surface/cli/fx.go's fetchedRateView.
type fetchedRateView struct {
	Pair   string `json:"pair" doc:"The currency pair fetched, as \"<base>/<quote>\"."`
	Rate   string `json:"rate"`
	Date   string `json:"date" format:"date"`
	Source string `json:"source"`
}

func fetchedRateViewFrom(f app.FetchedRate) fetchedRateView {
	return fetchedRateView{
		Pair:   fmt.Sprintf("%s/%s", f.Rate.Base(), f.Rate.Quote()),
		Rate:   f.Rate.Value().String(),
		Date:   f.Date.String(),
		Source: f.Source,
	}
}

// fetchFxRatesRequest is POST /api/v1/fx/rates/fetch's request body —
// every field optional, matching app.FetchFxRatesCommand's own optional
// fields exactly: no pairs fetches every in-use pair, and from/to together
// ask for a historical backfill instead of just today (both-or-neither is
// FetchFxRates' own validation to enforce, not duplicated here).
type fetchFxRatesRequest struct {
	Pairs []string `json:"pairs,omitempty" doc:"Restrict the fetch to these base currencies. Omit to fetch every in-use pair (quote is ignored in that case)."`
	Quote string   `json:"quote,omitempty" doc:"Quote currency for every pairs entry, instead of the resolved reporting currency. Ignored when pairs is omitted."`
	From  string   `json:"from,omitempty" doc:"Backfill range start date, inclusive (requires \"to\")." format:"date"`
	To    string   `json:"to,omitempty" doc:"Backfill range end date, inclusive (requires \"from\")." format:"date"`
}

// fxFetchView is POST /api/v1/fx/rates/fetch's response shape. Mirrors
// internal/surface/cli/fx.go's fxFetchView.
type fxFetchView struct {
	ReportingCurrency string            `json:"reporting_currency"`
	Fetched           []fetchedRateView `json:"fetched"`
}

func (h *handlers) fetchFxRates(w http.ResponseWriter, r *http.Request) {
	var body fetchFxRatesRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	result, err := h.svc.FetchFxRates(r.Context(), app.FetchFxRatesCommand{
		ActorID: actorID(r),
		Pairs:   body.Pairs,
		Quote:   body.Quote,
		From:    body.From,
		To:      body.To,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	view := fxFetchView{ReportingCurrency: result.ReportingCurrency}
	for _, f := range result.Fetched {
		view.Fetched = append(view.Fetched, fetchedRateViewFrom(f))
	}
	respond(w, http.StatusOK, view)
}

// fxRateView is GET /api/v1/fx/rates' response shape: the resolved rate
// and its full ADR-0004 provenance, plus (only when an amount was given)
// the converted figure. Mirrors internal/surface/cli/fx.go's fxRateView
// field-for-field.
type fxRateView struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Rate       string `json:"rate"`
	RateDate   string `json:"rate_date" format:"date"`
	RateSource string `json:"rate_source"`
	Stale      bool   `json:"stale"`
	Policy     string `json:"policy" enum:"conversion_policy"`
	Amount     string `json:"amount,omitempty" doc:"The requested amount, in the from currency. Set only when \"amount\" was given."`
	Converted  string `json:"converted,omitempty" doc:"The converted figure, in the to currency. Set only when \"amount\" was given."`
}

func fxRateViewFrom(from, to string, r app.ListFxRatesResult) fxRateView {
	v := fxRateView{
		From:       from,
		To:         to,
		Rate:       r.Rate.Value().String(),
		RateDate:   r.RateDate.String(),
		RateSource: r.RateSource,
		Stale:      r.Stale,
		Policy:     string(r.Policy),
	}
	if r.Converted != nil {
		v.Amount = fmt.Sprintf("%s %s", r.Converted.ConvertedFrom.AmountString(), r.Converted.ConvertedFrom.Currency())
		v.Converted = fmt.Sprintf("%s %s", r.Converted.Amount.AmountString(), r.Converted.Amount.Currency())
	}
	return v
}

// listFxRates implements GET /api/v1/fx/rates: issue #135's pure,
// stored-data-only read — no network call, ever (ListFxRates never calls
// Service.FxProvider). "amount" is forwarded as a raw string, parsed
// app-side via normalize.Amount — this package can't construct a
// money.Money itself (internal/lint's TestImportGraph bars it from
// importing internal/domain).
func (h *handlers) listFxRates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to := q.Get("from"), q.Get("to")

	result, err := h.svc.ListFxRates(r.Context(), app.ListFxRatesQuery{
		From:            from,
		To:              to,
		Policy:          app.ConversionPolicy(q.Get("policy")),
		TransactionDate: q.Get("transaction_date"),
		PinnedDate:      q.Get("pinned_date"),
		Amount:          q.Get("amount"),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, fxRateViewFrom(from, to, result))
}
