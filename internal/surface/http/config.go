// This file implements issue #137's reporting-currency get/set routes,
// wired onto internal/app/reporting_currency.go's use cases (issue #132)
// the same way internal/surface/cli/config.go's `config
// reporting-currency` commands are — a singleton, actor-scoped setting,
// so there is no {id} in either route's path.
package http

import "net/http"

// reportingCurrencyView is GET /api/v1/reporting-currency's response
// shape. IsSet distinguishes "never configured" from a currency that
// happens to render the same either way — mirrors
// internal/surface/cli/config.go's reportingCurrencyView.
type reportingCurrencyView struct {
	Currency string `json:"currency"`
	IsSet    bool   `json:"is_set" doc:"False when this actor has never set a reporting currency; the instance default is used instead."`
}

func (h *handlers) getReportingCurrency(w http.ResponseWriter, r *http.Request) {
	currency, err := h.svc.GetReportingCurrency(r.Context(), actorID(r))
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, reportingCurrencyView{Currency: currency, IsSet: currency != ""})
}

// setReportingCurrencyRequest is POST /api/v1/reporting-currency's request
// body.
type setReportingCurrencyRequest struct {
	Currency string `json:"currency"`
}

// setReportingCurrency implements POST /api/v1/reporting-currency,
// following auth.go's changePassword precedent for a mutating action on a
// singleton, actor-scoped resource: POST, not PUT, and an okView response.
func (h *handlers) setReportingCurrency(w http.ResponseWriter, r *http.Request) {
	var body setReportingCurrencyRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}
	if err := h.svc.SetReportingCurrency(r.Context(), actorID(r), body.Currency); err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, okView{OK: true})
}
