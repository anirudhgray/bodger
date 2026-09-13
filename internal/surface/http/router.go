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
	// Ignored when RawResponseContentType is set.
	Response any

	// RawResponseContentType marks a route whose success response is a
	// raw byte stream — a file download — rather than the usual
	// {"data": ...} JSON envelope every other route in this table uses:
	// "application/json" for the canonical export document, "text/csv"
	// for the CSV export. Response is unused for such a route; the
	// generated OpenAPI document describes the response body as an opaque
	// binary blob at this content type instead of reflecting a Go type.
	RawResponseContentType string

	// RawRequestContentType marks a route whose request body is a raw
	// byte stream — a file upload — rather than the usual JSON body a
	// Request DTO describes: "text/csv" for POST /api/v1/imports, the
	// only such route today. Request is unused for such a route; the
	// generated OpenAPI document describes the request body as an opaque
	// binary blob at this content type instead of reflecting a Go type.
	RawRequestContentType string

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

	// Public marks a route that skips requireAuth's credential check
	// entirely (issue #56): /healthz (a liveness probe has no actor to
	// authenticate as) and /api/v1/auth/login (the one route whose whole
	// purpose is to establish a credential in the first place). Every
	// other route needs a resolved ActorID before its handler ever runs.
	Public bool
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
		Public:   true,
	},

	{
		Method: http.MethodPost, Pattern: "/api/v1/auth/login",
		Handler: func(h *handlers) http.HandlerFunc { return h.login },

		OperationID: "login", Summary: "Sign in and start a session.",
		Description:   "On success, sets an HttpOnly session cookie the browser holds and resends automatically - the response body carries no credential to store.",
		SuccessStatus: http.StatusOK, SuccessDescription: "Signed in.",
		Request: loginRequest{}, Response: authView{},
		Errors: []int{http.StatusUnauthorized},
		Public: true,
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/auth/logout",
		Handler: func(h *handlers) http.HandlerFunc { return h.logout },

		OperationID: "logout", Summary: "End the current session.",
		Description:   "Requires a session cookie; a bearer-token request has no session to end - revoke the API token instead.",
		SuccessStatus: http.StatusOK, SuccessDescription: "Signed out.",
		Response: okView{},
		Errors:   []int{http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/auth/logout-all",
		Handler: func(h *handlers) http.HandlerFunc { return h.logoutAll },

		OperationID: "logoutAllSessions", Summary: "End every session the acting user has open.",
		Description:   "Revokes every session, including the one that made this request, if any.",
		SuccessStatus: http.StatusOK, SuccessDescription: "Every session was revoked.",
		Response: okView{},
	},

	{
		Method: http.MethodPost, Pattern: "/api/v1/auth/password",
		Handler: func(h *handlers) http.HandlerFunc { return h.changePassword },

		OperationID: "changePassword", Summary: "Change the acting user's password.",
		Description:   "Revokes every session the acting user has open, including whichever one made this request - it will need to sign in again afterward.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The password was changed.",
		Request: changePasswordRequest{}, Response: okView{},
		Errors: []int{http.StatusUnprocessableEntity},
	},

	{
		Method: http.MethodPost, Pattern: "/api/v1/auth/tokens",
		Handler: func(h *handlers) http.HandlerFunc { return h.createAPIToken },

		OperationID: "createAPIToken", Summary: "Issue a new API token.",
		Description:   "The plaintext token is returned exactly once, in this response - it can't be retrieved again.",
		SuccessStatus: http.StatusCreated, SuccessDescription: "The created token, including its one-time plaintext value.",
		Request: createAPITokenRequest{}, Response: createAPITokenResponse{},
		Errors: []int{http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/auth/tokens",
		Handler: func(h *handlers) http.HandlerFunc { return h.listAPITokens },

		OperationID: "listAPITokens", Summary: "List every API token the acting user owns.",
		Description:   "Includes revoked and expired tokens, for history - not only what's currently live.",
		SuccessStatus: http.StatusOK, SuccessDescription: "Every API token, newest first.",
		Response: []apiTokenView{},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/v1/auth/tokens/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.revokeAPIToken },

		OperationID: "revokeAPIToken", Summary: "Revoke an API token.",
		Description:   "The token stops authenticating immediately, but remains listed (soft revocation).",
		SuccessStatus: http.StatusOK, SuccessDescription: "The token was revoked.",
		Response: okView{},
		Errors:   []int{http.StatusNotFound},
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
		Description:   "A transfer between accounts in different currencies is allowed: the implied exchange rate between the two legs is derived and recorded automatically, from \"to_amount\" when given or from \"amount\"'s raw digits reinterpreted in the to-currency otherwise.",
		SuccessStatus: http.StatusCreated, SuccessDescription: "The recorded transfer.",
		Request: createTransferRequest{}, Response: transactionView{},
		Errors: []int{http.StatusUnprocessableEntity},
	},

	{
		Method: http.MethodGet, Pattern: "/api/v1/balances",
		Handler: func(h *handlers) http.HandlerFunc { return h.getBalances },

		OperationID: "getBalances", Summary: "Every account's balance as of a date.",
		Description:   "Setting \"currency\" converts every account's balance into it, under \"policy\" - the response's \"balances[].converted\" carries the converted figure and its full provenance, and any account the conversion couldn't cover is reported under \"unconverted\" rather than silently dropped.",
		SuccessStatus: http.StatusOK, SuccessDescription: "Every account's balance as of the resolved date.",
		Response: balancesView{},
		Errors:   []int{http.StatusUnprocessableEntity},
		Query: []queryParam{
			{Name: "as_of", Description: "Defaults to today in the actor's own timezone when omitted.", Type: "string", Format: "date"},
			{Name: "currency", Description: "Convert every account's balance into this currency.", Type: "string"},
			{Name: "policy", Description: "Which conversion policy to use when \"currency\" is set.", Type: "string", Enum: []string{"transaction_date", "current", "pinned"}},
			{Name: "pinned_date", Description: "The pinned date to convert at (required when \"policy\" is \"pinned\" and \"currency\" is set).", Type: "string", Format: "date"},
		},
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/balances/totals",
		Handler: func(h *handlers) http.HandlerFunc { return h.getBalanceTotals },

		OperationID: "getBalanceTotals", Summary: "Totals overview: overall net balance, per category, per currency.",
		Description:   "The overall net balance and the per-category breakdown are converted into \"currency\" under \"policy\" - left unset, \"currency\" resolves to the actor's own reporting-currency preference, falling back to the instance default. The per-currency breakdown is always raw and unconverted. Any account the conversion couldn't cover is reported under \"unconverted\" rather than silently dropped or excluded without explanation.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The totals overview as of the resolved date.",
		Response: balanceTotalsView{},
		Errors:   []int{http.StatusUnprocessableEntity},
		Query: []queryParam{
			{Name: "as_of", Description: "Defaults to today in the actor's own timezone when omitted.", Type: "string", Format: "date"},
			{Name: "currency", Description: "Convert the overall and per-category totals into this currency. Defaults to the actor's own reporting currency.", Type: "string"},
			{Name: "policy", Description: "Which conversion policy to use (required).", Type: "string", Enum: []string{"transaction_date", "current", "pinned"}},
			{Name: "pinned_date", Description: "The pinned date to convert at (required when \"policy\" is \"pinned\").", Type: "string", Format: "date"},
		},
	},

	{
		Method: http.MethodGet, Pattern: "/api/v1/balances/net-worth-over-time",
		Handler: func(h *handlers) http.HandlerFunc { return h.getNetWorthOverTime },

		OperationID: "getNetWorthOverTime", Summary: "Total balance across every account, as a time series.",
		Description:   "The total balance across every account, converted into \"currency\" under \"policy\", at each \"granularity\" period boundary within \"from\"..\"to\" (both required - there's no sensible default range for a time series). Every filter dimension other than \"from\"/\"to\" is ignored: net worth is a whole-ledger figure, not scoped to one account or category. Any account some period's conversion couldn't cover is reported once under \"unconverted\", not once per period.",
		SuccessStatus: http.StatusOK, SuccessDescription: "One point per period boundary in the requested range, ascending.",
		Response: netWorthOverTimeView{},
		Errors:   []int{http.StatusUnprocessableEntity},
		Query: []queryParam{
			{Name: "from", Description: "The inclusive start of the range (required).", Type: "string", Format: "date"},
			{Name: "to", Description: "The inclusive end of the range (required).", Type: "string", Format: "date"},
			{Name: "currency", Description: "Convert every point into this currency. Defaults to the actor's own reporting currency.", Type: "string"},
			{Name: "policy", Description: "Which conversion policy to use (required).", Type: "string", Enum: []string{"transaction_date", "current", "pinned"}},
			{Name: "pinned_date", Description: "The pinned date to convert at (required when \"policy\" is \"pinned\").", Type: "string", Format: "date"},
			granularityQueryParam,
		},
	},

	{
		Method: http.MethodGet, Pattern: "/api/v1/fx/rates",
		Handler: func(h *handlers) http.HandlerFunc { return h.listFxRates },

		OperationID: "listFxRates", Summary: "Look up the exchange rate between two currencies.",
		Description:   "A pure read over already-stored rates - this never calls out to the configured FX provider (use POST /api/v1/fx/rates/fetch for that). Setting \"amount\" also returns the converted figure.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The resolved rate and (when \"amount\" was set) the converted figure.",
		Response: fxRateView{},
		Errors:   []int{http.StatusNotFound, http.StatusUnprocessableEntity},
		Query: []queryParam{
			{Name: "from", Description: "The currency to look up a rate for (required).", Type: "string"},
			{Name: "to", Description: "The currency it's quoted against (required).", Type: "string"},
			{Name: "policy", Description: "Which conversion policy to use (required).", Type: "string", Enum: []string{"transaction_date", "current", "pinned"}},
			{Name: "transaction_date", Description: "The transaction date to look the rate up at (required with policy=transaction_date).", Type: "string", Format: "date"},
			{Name: "pinned_date", Description: "The pinned date to look the rate up at (required with policy=pinned).", Type: "string", Format: "date"},
			{Name: "amount", Description: "An amount in \"from\"'s currency to convert (optional).", Type: "string"},
		},
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/fx/rates/fetch",
		Handler: func(h *handlers) http.HandlerFunc { return h.fetchFxRates },

		OperationID: "fetchFxRates", Summary: "Fetch and store exchange rates from the configured provider.",
		Description:   "With no \"pairs\", fetches every currency pair actually in use across the actor's accounts and transactions, quoted against their reporting currency, at today's date. Pass \"pairs\" to restrict the fetch to specific base currencies, and \"from\"/\"to\" together for a historical backfill instead of just today.",
		SuccessStatus: http.StatusOK, SuccessDescription: "Every rate fetched and stored.",
		Request: fetchFxRatesRequest{}, Response: fxFetchView{},
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	},

	{
		Method: http.MethodGet, Pattern: "/api/v1/analytics/category-breakdown",
		Handler: func(h *handlers) http.HandlerFunc { return h.getCategoryBreakdown },

		OperationID: "getCategoryBreakdown", Summary: "Spending and income, by top-level category.",
		Description:   "Every matching posting rolls up into its top-level category (a posting under Groceries or Restaurants both count toward \"Food\") - an \"Uncategorized\" row covers postings with no category at all. Transfers are excluded by construction.",
		SuccessStatus: http.StatusOK, SuccessDescription: "One row per top-level category with at least one matching posting.",
		Response: categoryBreakdownView{},
		Errors:   []int{http.StatusUnprocessableEntity},
		Query:    reportQueryParams,
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/analytics/cash-flow",
		Handler: func(h *handlers) http.HandlerFunc { return h.getCashFlow },

		OperationID: "getCashFlow", Summary: "Inflow vs. outflow, bucketed by period.",
		Description:   "When the filter sets both \"from\" and \"to\", every period in that range is included, even one with no matching postings. \"granularity\" selects the bucket size - week, month (the default), year, or custom (the whole \"from\"..\"to\" range as one bucket, which then requires both). Transfers are excluded by construction.",
		SuccessStatus: http.StatusOK, SuccessDescription: "One point per period, ascending.",
		Response: cashFlowView{},
		Errors:   []int{http.StatusUnprocessableEntity},
		Query:    append(append([]queryParam{}, reportQueryParams...), granularityQueryParam),
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/analytics/trends",
		Handler: func(h *handlers) http.HandlerFunc { return h.getTrends },

		OperationID: "getTrends", Summary: "The current period vs. the immediately preceding one.",
		Description:   "\"granularity\" selects the comparison period - week, month (the default), year, or custom. For week/month/year, \"from\"/\"to\" are ignored: this compares the current period against the previous one, in the actor's own timezone. For custom, \"from\"/\"to\" (both required) name the current period, compared against the immediately preceding period of the same length. Every other filter dimension still applies to both periods identically.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The current and previous period's totals, plus each one's percentage change.",
		Response: trendsView{},
		Errors:   []int{http.StatusUnprocessableEntity},
		Query:    append(append([]queryParam{}, reportQueryParams...), granularityQueryParam),
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/analytics/savings-rate",
		Handler: func(h *handlers) http.HandlerFunc { return h.getSavingsRate },

		OperationID: "getSavingsRate", Summary: "(Income − outflow) / income, over the filter's date range.",
		Description:   "\"rate\" is absent when income is zero - an undefined ratio, not zero.",
		SuccessStatus: http.StatusOK, SuccessDescription: "Income, outflow, net, and the savings rate over the resolved filter.",
		Response: savingsRateView{},
		Errors:   []int{http.StatusUnprocessableEntity},
		Query:    reportQueryParams,
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/analytics/top-transactions",
		Handler: func(h *handlers) http.HandlerFunc { return h.getTopTransactions },

		OperationID: "getTopTransactions", Summary: "The largest transactions in a period, by absolute amount.",
		Description:   "\"limit\" is the top-N count (defaults to 10, capped at 100) - every matching posting is still fetched and converted first, and only then truncated to the largest \"limit\" by magnitude. \"amount\" stays signed (negative for an outflow) even though the sort itself is by magnitude. Transfers are excluded by construction.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The largest matching transactions, descending by absolute amount.",
		Response: topTransactionsView{},
		Errors:   []int{http.StatusUnprocessableEntity},
		Query:    append(append([]queryParam{}, reportQueryParams...), queryParam{Name: "limit", Description: "The top-N count. Defaults to 10, capped at 100.", Type: "integer", Min: intPtr(1), Max: intPtr(100)}),
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/analytics/average-transaction-size",
		Handler: func(h *handlers) http.HandlerFunc { return h.getAverageTransactionSize },

		OperationID: "getAverageTransactionSize", Summary: "The mean transaction amount, overall and by top-level category.",
		Description:   "\"average\" is the mean of each matching posting's absolute amount (a magnitude, not a signed net) - \"overall\" covers every matching posting; \"by_category\" breaks that down by top-level category, with an \"Uncategorized\" row for postings with no category at all. Transfers are excluded by construction.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The overall and per-category mean transaction size.",
		Response: averageTransactionSizeView{},
		Errors:   []int{http.StatusUnprocessableEntity},
		Query:    reportQueryParams,
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/analytics/category-trends",
		Handler: func(h *handlers) http.HandlerFunc { return h.getCategoryTrends },

		OperationID: "getCategoryTrends", Summary: "Month-over-month (or period-over-period) spending/income change, per category.",
		Description:   "Extends /trends' aggregate-only comparison to one row per top-level category: \"granularity\" selects the comparison period the same way it does for /trends (week, month - the default, year, or custom). A category present in only one of the two periods still gets a row, zeroed on the side with nothing to report, rather than being omitted. \"spending_change_pct\"/\"income_change_pct\" are absent when the previous period's corresponding figure is zero - an undefined ratio, not zero.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The resolved current/previous period bounds and one row per top-level category present in either period.",
		Response: categoryTrendsView{},
		Errors:   []int{http.StatusUnprocessableEntity},
		Query:    append(append([]queryParam{}, reportQueryParams...), granularityQueryParam),
	},

	{
		Method: http.MethodGet, Pattern: "/api/v1/reporting-currency",
		Handler: func(h *handlers) http.HandlerFunc { return h.getReportingCurrency },

		OperationID: "getReportingCurrency", Summary: "See the acting user's configured reporting currency.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The configured reporting currency, or that none is set.",
		Response: reportingCurrencyView{},
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/reporting-currency",
		Handler: func(h *handlers) http.HandlerFunc { return h.setReportingCurrency },

		OperationID: "setReportingCurrency", Summary: "Set the acting user's reporting currency.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The reporting currency was set.",
		Request: setReportingCurrencyRequest{}, Response: okView{},
		Errors: []int{http.StatusUnprocessableEntity},
	},

	{
		Method: http.MethodGet, Pattern: "/api/v1/export/json",
		Handler: func(h *handlers) http.HandlerFunc { return h.exportJSON },

		OperationID: "exportJSON", Summary: "Download a complete backup as canonical JSON.",
		Description:   "Every account, category, and transaction the actor owns, unfiltered - independent of the database schema and deterministic across repeated calls with the same underlying data, so it's safe to use as a backup. Unlike every other route in this API, the response body is the export document itself, not wrapped in the usual {\"data\": ...} envelope.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The canonical JSON export document.",
		RawResponseContentType: "application/json",
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/export/csv",
		Handler: func(h *handlers) http.HandlerFunc { return h.exportCSV },

		OperationID: "exportCSV", Summary: "Download transactions as CSV, one row per posting.",
		Description:   "Flat and lossy by design: a split transaction's postings become separate rows sharing the same transaction-level fields, and this format can't represent a split unambiguously enough to import back in. Optionally scoped by the same filter dimensions as /api/v1/analytics/*; omitting every filter parameter exports every transaction. Not wrapped in the usual {\"data\": ...} envelope.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The CSV export.",
		RawResponseContentType: "text/csv",
		Query:                  exportCSVQueryParams,
	},

	{
		Method: http.MethodPost, Pattern: "/api/v1/restore",
		Handler: func(h *handlers) http.HandlerFunc { return h.restoreSnapshot },

		OperationID: "restoreSnapshot", Summary: "Replace everything with a canonical JSON backup.",
		Description:   `Wipes and reloads the actor's entire ledger - every account, category, and transaction - from "document", a canonical JSON backup exactly as produced by GET /api/v1/export/json. This can't be undone and has no preview step: the request must set "confirm": true, or it's rejected before anything is touched.`,
		SuccessStatus: http.StatusOK, SuccessDescription: "The restore completed; how many accounts, categories, and transactions were installed.",
		Request: restoreSnapshotRequest{}, Response: restoreSnapshotView{},
		Errors: []int{http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/imports",
		Handler: func(h *handlers) http.HandlerFunc { return h.createImportBatch },

		OperationID: "createImportBatch", Summary: "Upload a file and stage it for import.",
		Description: "The request body is the file's own raw bytes - not wrapped in the usual {\"data\": ...} envelope or JSON. " +
			"account, filename, and the column mapping are given as query parameters rather than in the body, since the body " +
			"is already spoken for by the file itself. Nothing is written to your accounts, categories, or transactions by " +
			"this call: every row is only staged for review (see GET /api/v1/imports/{id}/records), and stays that way " +
			"until you commit it.",
		SuccessStatus: http.StatusCreated, SuccessDescription: "The staged import and every record it staged.",
		RawRequestContentType: "text/csv",
		Response:              importBatchWithRecordsView{},
		Errors:                []int{http.StatusUnprocessableEntity},
		Query:                 importUploadQueryParams,
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/imports",
		Handler: func(h *handlers) http.HandlerFunc { return h.listImportBatches },

		OperationID: "listImportBatches", Summary: "List every import, most recently created first.",
		SuccessStatus: http.StatusOK, SuccessDescription: "Every import.",
		Response: []importBatchView{},
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/imports/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.getImportBatch },

		OperationID: "getImportBatch", Summary: "Look up one import's status.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The import.",
		Response: importBatchView{},
		Errors:   []int{http.StatusNotFound},
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/imports/{id}/records",
		Handler: func(h *handlers) http.HandlerFunc { return h.listImportRecords },

		OperationID: "listImportRecords", Summary: "List an import's staged records, with their duplicate/transfer flags.",
		Description:   "Every row from the uploaded file, in its own original order - including a row already excluded as an exact duplicate, and one still awaiting your decision on a suspected duplicate (see POST /api/v1/import-records/{id}/resolve).",
		SuccessStatus: http.StatusOK, SuccessDescription: "Every staged record.",
		Response: []importRecordView{},
		Errors:   []int{http.StatusNotFound},
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/imports/{id}/commit",
		Handler: func(h *handlers) http.HandlerFunc { return h.commitImportBatch },

		OperationID: "commitImportBatch", Summary: "Commit a staged import: write its cleared records as real transactions.",
		Description:   "Refused if any staged record is still awaiting a decision on a suspected duplicate. Every record is written in one all-or-nothing step.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The committed import and the transactions it created.",
		Response: importCommitView{},
		Errors:   []int{http.StatusNotFound, http.StatusPreconditionFailed},
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/imports/{id}/rollback",
		Handler: func(h *handlers) http.HandlerFunc { return h.rollbackImportBatch },

		OperationID: "rollbackImportBatch", Summary: "Undo a committed import: delete the transactions it created.",
		Description:   "Only a currently committed import can be rolled back. The deleted transactions keep their history, the same as deleting any other transaction.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The rolled-back import and the IDs of the deleted transactions.",
		Response: importRollbackView{},
		Errors:   []int{http.StatusNotFound, http.StatusPreconditionFailed},
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/import-records/{id}/resolve",
		Handler: func(h *handlers) http.HandlerFunc { return h.resolveImportRecord },

		OperationID: "resolveImportRecord", Summary: "Record your decision on a staged record's suspected duplicate.",
		Description:   `"confirmed_duplicate" excludes the record from commit; "not_duplicate" clears it for commit. Refused for a record with no suspected duplicate to resolve, or one already resolved.`,
		SuccessStatus: http.StatusOK, SuccessDescription: "The updated record.",
		Request: resolveImportRecordRequest{}, Response: importRecordView{},
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusPreconditionFailed},
	},

	{
		Method: http.MethodGet, Pattern: "/api/v1/budgets",
		Handler: func(h *handlers) http.HandlerFunc { return h.listBudgets },

		OperationID: "listBudgets", Summary: "List every budget.",
		Description:   "Includes archived budgets, the same as GET /api/v1/accounts includes archived accounts - filtering to \"current\" is left to the caller.",
		SuccessStatus: http.StatusOK, SuccessDescription: "Every budget, most recently created first.",
		Response: []budgetView{},
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/budgets",
		Handler: func(h *handlers) http.HandlerFunc { return h.createBudget },

		OperationID: "createBudget", Summary: "Create a budget.",
		Description:   "Optionally with an initial batch of lines - a line can also be added afterward via POST .../lines. The budget's period type is always monthly today.",
		SuccessStatus: http.StatusCreated, SuccessDescription: "The created budget.",
		Request: createBudgetRequest{}, Response: budgetView{},
		Errors: []int{http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/budgets/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.getBudget },

		OperationID: "getBudget", Summary: "Fetch one budget by ID.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The budget, with its lines.",
		Response: budgetView{},
		Errors:   []int{http.StatusNotFound},
	},
	{
		Method: http.MethodPatch, Pattern: "/api/v1/budgets/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.patchBudget },

		OperationID: "patchBudget", Summary: "Rename a budget and set its starts-on date.",
		Description:   `This is a full replacement of both fields, not a partial patch: omitting "starts_on" resolves it to today, not to the budget's existing value - send its current value back explicitly to change only the name. A budget's currency and period type can't change.`,
		SuccessStatus: http.StatusOK, SuccessDescription: "The updated budget.",
		Request: patchBudgetRequest{}, Response: budgetView{},
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/v1/budgets/{id}",
		Handler: func(h *handlers) http.HandlerFunc { return h.archiveBudget },

		OperationID: "archiveBudget", Summary: "Archive a budget.",
		Description:   "This hides the budget from listings; its history (including past actuals) remains fully queryable.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The archived budget.",
		Response: budgetView{},
		Errors:   []int{http.StatusNotFound},
	},
	{
		Method: http.MethodPost, Pattern: "/api/v1/budgets/{id}/lines",
		Handler: func(h *handlers) http.HandlerFunc { return h.addBudgetLine },

		OperationID: "addBudgetLine", Summary: "Add a line to a budget.",
		SuccessStatus: http.StatusCreated, SuccessDescription: "The updated budget, with its new line.",
		Request: budgetLineRequest{}, Response: budgetView{},
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodPatch, Pattern: "/api/v1/budgets/{id}/lines/{lineId}",
		Handler: func(h *handlers) http.HandlerFunc { return h.patchBudgetLine },

		OperationID: "patchBudgetLine", Summary: "Update a budget line's amount and rollover flag.",
		Description:   "A line's category can't change - remove it and add a new one for a different category instead.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The updated budget.",
		Request: patchBudgetLineRequest{}, Response: budgetView{},
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/v1/budgets/{id}/lines/{lineId}",
		Handler: func(h *handlers) http.HandlerFunc { return h.removeBudgetLine },

		OperationID: "removeBudgetLine", Summary: "Remove a line from a budget.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The updated budget, with the line removed.",
		Response: budgetView{},
		Errors:   []int{http.StatusNotFound},
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/budgets/{id}/actuals",
		Handler: func(h *handlers) http.HandlerFunc { return h.getBudgetActuals },

		OperationID: "getBudgetActuals", Summary: "Actual spend against a budget's lines, for one period.",
		Description:   "For each line, actual is net spend against its category subtree over the resolved period (spending minus any inflow, e.g. a refund, in the same subtree), converted into the budget's own currency. remaining is budgeted minus actual; utilisation is actual / budgeted.",
		SuccessStatus: http.StatusOK, SuccessDescription: "The resolved period and each line's plan-vs-actual.",
		Response: budgetActualsView{},
		Errors:   []int{http.StatusNotFound, http.StatusUnprocessableEntity},
		Query: []queryParam{
			{Name: "period", Description: "Any date within the target month. Defaults to the current month.", Type: "string", Format: "date"},
		},
	},
	{
		Method: http.MethodGet, Pattern: "/api/v1/budgets/{id}/history",
		Handler: func(h *handlers) http.HandlerFunc { return h.getBudgetHistory },

		OperationID: "getBudgetHistory", Summary: "Actual-vs-budget over several consecutive months.",
		Description:   "A repeated actuals computation over \"months\" consecutive calendar months ending at \"period\"'s month, oldest first. A budget's own starts_on clamps how far back this goes, so the result may hold fewer than \"months\" entries for a budget that hasn't existed that long.",
		SuccessStatus: http.StatusOK, SuccessDescription: "One period per month, ascending.",
		Response: budgetHistoryView{},
		Errors:   []int{http.StatusNotFound, http.StatusUnprocessableEntity},
		Query: []queryParam{
			{Name: "period", Description: "Any date within the most recent month to include. Defaults to the current month.", Type: "string", Format: "date"},
			{Name: "months", Description: "How many consecutive months to include. Defaults to 6.", Type: "integer", Min: intPtr(1)},
		},
	},
}

// importUploadQueryParams is POST /api/v1/imports' own query string: the
// target account and file metadata, since the request body itself is the
// uploaded file's raw bytes rather than a JSON object these could live
// in. date_column/description_column/amount_column are required for the
// only supported format today ("csv"); the rest are optional per-column
// hints the same CSV parser (internal/app/importparse) uses when present.
var importUploadQueryParams = []queryParam{
	{Name: "account", Description: "The account new transactions from this import will post against, by ID or unique name (required).", Type: "string"},
	{Name: "filename", Description: "The uploaded file's own name, for display and history (required).", Type: "string"},
	{Name: "format", Description: `The uploaded file's format. Only "csv" is supported today; omit this to use it.`, Type: "string", Enum: []string{"csv"}},
	{Name: "date_column", Description: "The column holding each row's booked date (required for csv).", Type: "string"},
	{Name: "description_column", Description: "The column holding each row's description (required for csv).", Type: "string"},
	{Name: "amount_column", Description: "The column holding each row's signed amount (required for csv).", Type: "string"},
	{Name: "posted_date_column", Description: "The column holding each row's posted date, when the file distinguishes it from the booked date.", Type: "string"},
	{Name: "currency_column", Description: "The column holding each row's currency, when the file has more than one. Omit to use the account's own currency for every row.", Type: "string"},
	{Name: "external_id_column", Description: "The column holding each row's own ID from the source, used to detect an exact duplicate on a re-import.", Type: "string"},
	{Name: "category_column", Description: "The column holding a hint for each row's category, matched against your existing category names.", Type: "string"},
}

func intPtr(n int) *int { return &n }

// reportQueryParams is every /api/v1/analytics/* route's shared query
// string: ADR-0009's TransactionFilterInput dimensions (account through
// tag_mode) plus ADR-0004's conversion options (currency through
// pinned_date) - the exact same list on all four routes, since
// parseReportFilterQuery/parseReportOptionsQuery (analytics.go) read them
// identically regardless of which metric is being requested.
var reportQueryParams = []queryParam{
	{Name: "account", Description: "An account's ID or unique name.", Type: "string"},
	{Name: "category", Description: "A category's ID or unique name; its whole subtree is included.", Type: "string"},
	{Name: "type", Description: `One of "outflow", "inflow", or "transfer" - a transfer never contributes to an analytics result (transfers are excluded by construction), so filtering to "transfer" alone always returns an empty result.`, Type: "string", Enum: []string{"outflow", "inflow", "transfer"}},
	{Name: "from", Description: "The inclusive start of a booked-date range. Ignored by /trends except when its own granularity is \"custom\".", Type: "string", Format: "date"},
	{Name: "to", Description: "The inclusive end of a booked-date range. Ignored by /trends except when its own granularity is \"custom\".", Type: "string", Format: "date"},
	{Name: "filter_currency", Description: "Only include this transaction currency (repeatable).", Type: "string"},
	{Name: "amount_min", Description: "Only include transactions at or above this amount (absolute value).", Type: "string"},
	{Name: "amount_max", Description: "Only include transactions at or below this amount (absolute value).", Type: "string"},
	{Name: "description", Description: "Only include transactions whose description contains this text.", Type: "string"},
	{Name: "tag", Description: "Only include transactions carrying this tag (repeatable).", Type: "string"},
	{Name: "tag_mode", Description: `How multiple "tag" values combine.`, Type: "string", Enum: []string{"any", "all"}},
	{Name: "currency", Description: "Convert every figure in the result into this currency (required).", Type: "string"},
	{Name: "policy", Description: "Which conversion policy to use (required).", Type: "string", Enum: []string{"transaction_date", "current", "pinned"}},
	{Name: "pinned_date", Description: "The pinned date to convert at (required when \"policy\" is \"pinned\").", Type: "string", Format: "date"},
}

// exportCSVQueryParams is /api/v1/export/csv's own query string:
// reportQueryParams' filter dimensions (account through tag_mode) only —
// not its trailing currency/policy/pinned_date conversion options, which
// don't apply here: the CSV export never converts an amount, it writes
// each posting's own currency and amount unchanged (ADR-0008's "flat and
// lossy by design"). Kept as its own explicit list rather than a slice of
// reportQueryParams, so a future reordering of that list can't silently
// change which of its entries this route inherits.
var exportCSVQueryParams = []queryParam{
	{Name: "account", Description: "An account's ID or unique name.", Type: "string"},
	{Name: "category", Description: "A category's ID or unique name; its whole subtree is included.", Type: "string"},
	{Name: "type", Description: `One of "outflow", "inflow", or "transfer".`, Type: "string", Enum: []string{"outflow", "inflow", "transfer"}},
	{Name: "from", Description: "The inclusive start of a booked-date range.", Type: "string", Format: "date"},
	{Name: "to", Description: "The inclusive end of a booked-date range.", Type: "string", Format: "date"},
	{Name: "filter_currency", Description: "Only include this transaction currency (repeatable).", Type: "string"},
	{Name: "amount_min", Description: "Only include transactions at or above this amount (absolute value).", Type: "string"},
	{Name: "amount_max", Description: "Only include transactions at or below this amount (absolute value).", Type: "string"},
	{Name: "description", Description: "Only include transactions whose description contains this text.", Type: "string"},
	{Name: "tag", Description: "Only include transactions carrying this tag (repeatable).", Type: "string"},
	{Name: "tag_mode", Description: `How multiple "tag" values combine.`, Type: "string", Enum: []string{"any", "all"}},
}

// granularityQueryParam is /cash-flow and /trends' own extra query
// parameter (issue #194) - not part of reportQueryParams since
// /category-breakdown and /savings-rate don't bucket or compare periods
// at all.
var granularityQueryParam = queryParam{
	Name: "granularity", Description: "How to bucket/compare periods. Defaults to \"month\".", Type: "string",
	Enum: []string{"week", "month", "year", "custom"},
}

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
		handler := rt.Handler(h)
		if !rt.Public {
			handler = h.requireAuth(handler)
		}
		mux.HandleFunc(rt.Method+" "+rt.Pattern, handler)
	}
	return mux
}
