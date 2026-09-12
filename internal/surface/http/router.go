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
