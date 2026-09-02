package http

import (
	"log/slog"
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
)

// route is one entry in routeTable: the method and pattern a
// stdlib net/http.ServeMux registers (Go 1.22+ method-and-pattern
// routing — ADR-0001, no framework), paired with the handlers method that
// answers it. It exists so routeTable can be walked by both NewMux (to
// build the real server) and openapi_gen.go (to generate openapi.json)
// without the two ever being able to drift against each other — see
// openapi_gen.go and openapigen_test.go.
//
// Every field from OperationID down is OpenAPI documentation metadata
// only: openapi_gen.go's GenerateOpenAPIDocument walks routeTable a
// second time to build the document from exactly these fields, and
// NewMux below never reads them. Issue #36 calls this "a small companion
// table of per-route request/response types and a summary/description" —
// it lives on route itself, rather than as a second table keyed
// separately, so the generated document can never name a route
// routeTable doesn't register (or vice versa): there is only ever one
// table to fall out of sync with itself.
type route struct {
	Method  string
	Pattern string
	Handler func(*handlers) http.HandlerFunc

	// OperationID is the OpenAPI operation's "operationId" — stable
	// per-endpoint identifiers a generated client method-names itself
	// after. It can't be derived from Handler (an anonymous closure
	// literal, not the bound method itself), so it's named here once,
	// the same word as the handlers method it wires to.
	OperationID string
	// Summary is the OpenAPI operation's one-line "summary".
	Summary string
	// Description is the OpenAPI operation's longer-form "description" —
	// optional; "" omits the field entirely rather than emitting an
	// empty string.
	Description string

	// SuccessStatus and SuccessDescription describe this route's one
	// success response. Every route in routeTable has exactly one: none
	// of this surface's handlers ever choose between two different
	// success shapes for the same route.
	SuccessStatus      int
	SuccessDescription string

	// Request is nil, or the zero value of the request body type this
	// route's handler decodes (e.g. createAccountRequest{}) — reflected
	// over by openapi_gen.go, never constructed for real.
	Request any
	// Response is the zero value of the success response body type this
	// route's handler encodes as dataEnvelope.Data (e.g. accountView{},
	// or []accountView{} for a list) — likewise only ever reflected over.
	Response any

	// Errors are the additional, non-success status codes this route can
	// answer with, beyond the one every route implicitly documents via
	// errorResponses' fallback (see openapi_gen.go): 404 for "no such
	// resource", 422 for "the request failed validation".
	Errors []int

	// Query is this route's query-string parameters, in declared order —
	// order matters here (unlike every map-keyed part of the generated
	// document, which encoding/json always emits key-sorted regardless
	// of insertion order).
	Query []queryParam
}

// queryParam is one query-string parameter's OpenAPI documentation:
// enough of openapi3.Schema's shape to describe every query parameter
// this surface actually has, not a general-purpose schema builder.
type queryParam struct {
	Name        string
	Description string
	// Type is "string" or "integer".
	Type string
	// Format is an OpenAPI schema format ("date"), or "" for none.
	Format string
	// Enum lists the closed set of values this parameter accepts, or nil
	// for an unconstrained string.
	Enum []string
	// Min and Max bound an integer parameter; nil for unbounded.
	Min, Max *int
}

// routeTable is the single source of truth for every route this surface
// serves — the "central route registry" issue #8 calls for. Adding a
// route means adding one row here; NewMux, openapi_gen.go, and
// openapigen_test.go's freshness check all walk this table rather than
// each hard-coding their own copy of it.
var routeTable = []route{
	{
		Method: http.MethodGet, Pattern: "/healthz",
		Handler: func(h *handlers) http.HandlerFunc { return h.healthz },

		OperationID: "healthz", Summary: "Report that the server is running.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The server is up.",
		Response: healthzView{},
	},

	{
		Method: http.MethodGet, Pattern: "/api/v1/accounts",
		Handler: func(h *handlers) http.HandlerFunc { return h.listAccounts },

		OperationID: "listAccounts", Summary: "List every account.",
		SuccessStatus: http.StatusOK, SuccessDescription: "Every account, in name order.",
		Response: []accountView{},
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/accounts",
		Handler: func(h *handlers) http.HandlerFunc { return h.createAccount },

		OperationID: "createAccount", Summary: "Create an account.",
		SuccessStatus: http.StatusCreated, SuccessDescription: "The created account.",
		Request: createAccountRequest{}, Response: accountView{},
		Errors: []int{http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/accounts/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.getAccount },

		OperationID: "getAccount", Summary: "Fetch one account by ID or unique name.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The account.",
		Response: accountView{},
		Errors:   []int{http.StatusNotFound},
	},
	{
		Method: http.MethodPatch, Pattern: "/api/v1/accounts/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.patchAccount },

		OperationID: "patchAccount", Summary: "Rename an account, or re-declare its opening balance.",
		Description:   `The request body must set exactly one of "name" or "opening_balance" - these are two separate operations at the application layer, and a request naming both or neither is rejected.`,
		SuccessStatus: http.StatusOK, SuccessDescription: "The updated account.",
		Request: patchAccountRequest{}, Response: accountView{},
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/v1/accounts/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.archiveAccount },

		OperationID: "archiveAccount", Summary: "Archive an account.",
		Description:   "This hides the account from pickers; its history remains. There is no hard delete for an account with any history.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The archived account.",
		Response: accountView{},
		Errors:   []int{http.StatusNotFound},
	},

	{
		Method: http.MethodGet, Pattern: "/api/v1/categories",
		Handler: func(h *handlers) http.HandlerFunc { return h.listCategories },

		OperationID: "listCategories", Summary: "List every category, flat.",
		SuccessStatus: http.StatusOK, SuccessDescription: "Every category.",
		Response: []categoryView{},
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/categories",
		Handler: func(h *handlers) http.HandlerFunc { return h.createCategory },

		OperationID: "createCategory", Summary: "Create a category.",
		SuccessStatus: http.StatusCreated, SuccessDescription: "The created category.",
		Request: createCategoryRequest{}, Response: categoryView{},
		Errors: []int{http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/categories/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.getCategory },

		OperationID: "getCategory", Summary: "Fetch one category by ID or unique name.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The category.",
		Response: categoryView{},
		Errors:   []int{http.StatusNotFound},
	},
	{
		Method: http.MethodPatch, Pattern: "/api/v1/categories/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.patchCategory },

		OperationID: "patchCategory", Summary: "Rename a category, or move it to a new parent.",
		Description:   `The request body must set exactly one of "name" or "parent" (an empty "parent" moves the category to top-level) - these are two separate operations at the application layer, and a request naming both or neither is rejected.`,
		SuccessStatus: http.StatusOK, SuccessDescription: "The updated category.",
		Request: patchCategoryRequest{}, Response: categoryView{},
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/v1/categories/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.archiveCategory },

		OperationID: "archiveCategory", Summary: "Archive a category.",
		Description:   "This hides the category from pickers; transactions that already reference it remain valid.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The archived category.",
		Response: categoryView{},
		Errors:   []int{http.StatusNotFound},
	},

	{
		Method: http.MethodGet, Pattern: "/api/v1/transactions",
		Handler: func(h *handlers) http.HandlerFunc { return h.listTransactions },

		OperationID: "listTransactions", Summary: "List transactions, newest first, with an optional filter.",
		SuccessStatus: http.StatusOK, SuccessDescription: "A page of matching transactions.",
		Response: transactionListView{},
		Errors:   []int{http.StatusUnprocessableEntity},
		Query: []queryParam{
			{Name: "account", Description: "An account's ID or unique name.", Type: "string"},
			{Name: "category", Description: "A category's ID or unique name; its whole subtree is included.", Type: "string"},
			{Name: "type", Description: `One of "outflow", "inflow", or "transfer".`, Type: "string", Enum: []string{"outflow", "inflow", "transfer"}},
			{Name: "from", Description: "The inclusive start of a booked-date range.", Type: "string", Format: "date"},
			{Name: "to", Description: "The inclusive end of a booked-date range.", Type: "string", Format: "date"},
			{Name: "limit", Description: "Page size; defaults to 50 and caps at 200.", Type: "integer", Min: intPtr(1), Max: intPtr(200)},
			{Name: "cursor", Description: `An opaque token from a previous page's "next_cursor". Its encoding is not part of the API's contract and may change.`, Type: "string"},
		},
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/transactions",
		Handler: func(h *handlers) http.HandlerFunc { return h.createTransaction },

		OperationID: "createTransaction", Summary: "Record an outflow or an inflow.",
		Description:   `For a transfer between two of the actor's own accounts, use POST /api/v1/transfers instead. When "date" is omitted, the server books the transaction to today in the actor's own timezone - never the client's.`,
		SuccessStatus: http.StatusCreated, SuccessDescription: "The recorded transaction.",
		Request: createTransactionRequest{}, Response: transactionView{},
		Errors: []int{http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/transactions/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.getTransaction },

		OperationID: "getTransaction", Summary: "Fetch one transaction by ID.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The transaction.",
		Response: transactionView{},
		Errors:   []int{http.StatusNotFound},
	},
	{
		Method: http.MethodPatch, Pattern: "/api/v1/transactions/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.editTransaction },

		OperationID: "editTransaction", Summary: "Replace a transaction's editable fields.",
		Description:   `This is a full replacement of every field in the request body, not a partial patch: send every field's current value, not only the one that's changing. A transaction's type (outflow, inflow, transfer) can't change.`,
		SuccessStatus: http.StatusOK, SuccessDescription: "The updated transaction.",
		Request: editTransactionRequest{}, Response: transactionView{},
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/v1/transactions/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.deleteTransaction },

		OperationID: "deleteTransaction", Summary: "Delete a transaction.",
		Description:   "The transaction stops appearing in lists and balances, but its history is retained.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The deleted transaction.",
		Response: transactionView{},
		Errors:   []int{http.StatusNotFound},
	},

	{
		Method: http.MethodPost, Pattern: "/api/v1/transfers",
		Handler: func(h *handlers) http.HandlerFunc { return h.createTransfer },

		OperationID: "createTransfer", Summary: "Move money between two of the actor's own accounts.",
		Description:   "A transfer between accounts in different currencies is rejected; cross-currency transfers aren't supported yet.",
		SuccessStatus: http.StatusCreated, SuccessDescription: "The recorded transfer.",
		Request: createTransferRequest{}, Response: transactionView{},
		Errors: []int{http.StatusUnprocessableEntity},
	},

	{
		Method: http.MethodGet, Pattern: "/api/v1/balances",
		Handler: func(h *handlers) http.HandlerFunc { return h.getBalances },

		OperationID: "getBalances", Summary: "Every account's balance as of a date.",
		SuccessStatus: http.StatusOK, SuccessDescription: "Every account's balance as of the resolved date.",
		Response: balancesView{},
		Query: []queryParam{
			{Name: "as_of", Description: "Defaults to today in the actor's own timezone when omitted.", Type: "string", Format: "date"},
		},
	},
}

func intPtr(n int) *int { return &n }

// NewMux builds bodger's REST API as a stdlib *http.ServeMux, wiring
// every routeTable entry to svc. It has no other side effect — no
// listener, no goroutine — so tests can exercise it directly with
// httptest.NewRecorder/httptest.NewServer without going through
// ServeCommand at all.
//
// logger is what respondError (respond.go) logs an *errs.Error's cause
// chain through before rendering its safe message (ADR-0011; issue #43);
// a nil logger is accepted (respondError checks) so existing callers that
// don't care about logging can keep passing nil rather than wiring one up.
func NewMux(svc *app.Service, logger *slog.Logger) *http.ServeMux {
	h := &handlers{svc: svc, logger: logger}
	mux := http.NewServeMux()
	for _, rt := range routeTable {
		mux.HandleFunc(rt.Method+" "+rt.Pattern, rt.Handler(h))
	}
	return mux
}
