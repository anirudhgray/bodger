package http

import (
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
)

// route is one entry in routeTable: the method and pattern a
// stdlib net/http.ServeMux registers (Go 1.22+ method-and-pattern
// routing — ADR-0001, no framework), paired with the handlers method that
// answers it. It exists so routeTable can be walked by both NewMux (to
// build the real server) and openapi_test.go (to assert the checked-in
// OpenAPI description matches exactly what's registered) without the two
// ever being able to drift against each other — see openapi.go.
type route struct {
	Method  string
	Pattern string
	Handler func(*handlers) http.HandlerFunc
}

// routeTable is the single source of truth for every route this surface
// serves — the "central route registry" issue #8 calls for. Adding a
// route means adding one row here; NewMux and the OpenAPI conformance
// test both walk this table rather than each hard-coding their own copy
// of it.
var routeTable = []route{
	{http.MethodGet, "/healthz", func(h *handlers) http.HandlerFunc { return h.healthz }},

	{http.MethodGet, "/api/v1/accounts", func(h *handlers) http.HandlerFunc { return h.listAccounts }},
	{http.MethodPost, "/api/v1/accounts", func(h *handlers) http.HandlerFunc { return h.createAccount }},
	{http.MethodGet, "/api/v1/accounts/{id}", func(h *handlers) http.HandlerFunc { return h.getAccount }},
	{http.MethodPatch, "/api/v1/accounts/{id}", func(h *handlers) http.HandlerFunc { return h.patchAccount }},
	{http.MethodDelete, "/api/v1/accounts/{id}", func(h *handlers) http.HandlerFunc { return h.archiveAccount }},

	{http.MethodGet, "/api/v1/categories", func(h *handlers) http.HandlerFunc { return h.listCategories }},
	{http.MethodPost, "/api/v1/categories", func(h *handlers) http.HandlerFunc { return h.createCategory }},
	{http.MethodGet, "/api/v1/categories/{id}", func(h *handlers) http.HandlerFunc { return h.getCategory }},
	{http.MethodPatch, "/api/v1/categories/{id}", func(h *handlers) http.HandlerFunc { return h.patchCategory }},
	{http.MethodDelete, "/api/v1/categories/{id}", func(h *handlers) http.HandlerFunc { return h.archiveCategory }},

	{http.MethodGet, "/api/v1/transactions", func(h *handlers) http.HandlerFunc { return h.listTransactions }},
	{http.MethodPost, "/api/v1/transactions", func(h *handlers) http.HandlerFunc { return h.createTransaction }},
	{http.MethodGet, "/api/v1/transactions/{id}", func(h *handlers) http.HandlerFunc { return h.getTransaction }},
	{http.MethodPatch, "/api/v1/transactions/{id}", func(h *handlers) http.HandlerFunc { return h.editTransaction }},
	{http.MethodDelete, "/api/v1/transactions/{id}", func(h *handlers) http.HandlerFunc { return h.deleteTransaction }},

	{http.MethodPost, "/api/v1/transfers", func(h *handlers) http.HandlerFunc { return h.createTransfer }},

	{http.MethodGet, "/api/v1/balances", func(h *handlers) http.HandlerFunc { return h.getBalances }},
}

// NewMux builds bodger's REST API as a stdlib *http.ServeMux, wiring
// every routeTable entry to svc. It has no other side effect — no
// listener, no goroutine — so tests can exercise it directly with
// httptest.NewRecorder/httptest.NewServer without going through
// ServeCommand at all.
func NewMux(svc *app.Service) *http.ServeMux {
	h := &handlers{svc: svc}
	mux := http.NewServeMux()
	for _, rt := range routeTable {
		mux.HandleFunc(rt.Method+" "+rt.Pattern, rt.Handler(h))
	}
	return mux
}
