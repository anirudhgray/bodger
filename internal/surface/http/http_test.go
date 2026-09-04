// Package http_test exercises internal/surface/http end to end: real HTTP
// requests, through NewMux, against an app.Service backed by the real
// SQLite adapter (the same newSQLiteTestService pattern
// internal/app/sqlite_integration_test.go uses) — not a mocked handler
// layer. docs/architecture.md §7 calls surface tests "thin... enough to
// prove decoding and encoding"; this stays in that spirit by asserting on
// HTTP status and JSON shape rather than re-testing use-case behaviour
// internal/app's own tests already cover.
//
// Every response the do helper receives is additionally validated
// against openapi.json itself (issue #36), via openapi3filter — the
// piece static generation alone can't catch: a handler whose actual
// response doesn't match the schema its own route declares. See
// openAPIRouter and validateResponse below.
package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"

	"github.com/anirudhgray/bodger/internal/adapters/sqlite"
	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	"github.com/anirudhgray/bodger/internal/ports"
	httpsurface "github.com/anirudhgray/bodger/internal/surface/http"
)

// openAPIRouter matches a request to the operation openapi.json declares
// for it — built once, from the embedded document itself
// (httpsurface.OpenAPIDocument), so these tests validate against exactly
// what ships, not a separately-loaded copy that could drift from it.
var openAPIRouter routers.Router

func init() {
	doc, err := openapi3.NewLoader().LoadFromData(httpsurface.OpenAPIDocument())
	if err != nil {
		panic(fmt.Sprintf("http_test: load embedded openapi.json: %v", err))
	}
	if err := doc.Validate(context.Background()); err != nil {
		panic(fmt.Sprintf("http_test: embedded openapi.json is invalid: %v", err))
	}
	openAPIRouter, err = legacyrouter.NewRouter(doc)
	if err != nil {
		panic(fmt.Sprintf("http_test: build openapi router: %v", err))
	}
}

// validateResponse checks that resp's status, headers, and body actually
// match what openapi.json declares for req's route — the response-side
// half of issue #36's contract test; decodeJSON/respond.go's own
// encoding is what request-side validation would otherwise duplicate, so
// only the response is checked here. It reports failures via t.Errorf,
// not t.Fatalf: a schema mismatch is worth surfacing without aborting
// whatever assertions the calling test still wants to make on the same
// response.
func validateResponse(t *testing.T, req *http.Request, resp *http.Response, body []byte) {
	t.Helper()

	// openapi.json's one server is the relative URL "/" (no scheme or
	// host: this surface never claims to know its own base URL — see
	// openapi_gen.go). The legacy router's server matching only strips a
	// server prefix off a request URL that's already relative, so it's
	// given routeReq — req's method and path, with srv's scheme and host
	// stripped off — rather than req itself.
	routeReq := &http.Request{Method: req.Method, URL: &url.URL{Path: req.URL.Path, RawQuery: req.URL.RawQuery}}
	route, pathParams, err := openAPIRouter.FindRoute(routeReq)
	if err != nil {
		t.Errorf("%s %s: not described in openapi.json: %v", req.Method, req.URL.Path, err)
		return
	}

	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
		},
		Status: resp.StatusCode,
		Header: resp.Header,
	}
	input.SetBodyBytes(body)

	if err := openapi3filter.ValidateResponse(req.Context(), input); err != nil {
		t.Errorf("%s %s -> %d: response doesn't match its declared OpenAPI schema: %v", req.Method, req.URL.Path, resp.StatusCode, err)
	}
}

// newTestService builds an *app.Service wired to the real SQLite adapter
// against a fresh, fully-migrated temp-file database — the HTTP-surface
// equivalent of internal/app/sqlite_integration_test.go's
// newSQLiteTestService. Split out from newTestServer so a test can build
// a handler other than NewMux (webui_handler_test.go's NewServerHandler
// case) from the same real service without duplicating this setup.
func newTestService(t *testing.T, frozenAt time.Time, tz string) *app.Service {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "bodger.db")
	clk := clock.NewFrozen(frozenAt)

	db, err := sqlite.Open(clk, path)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})
	if err := db.MigrateUp(context.Background()); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}

	cfg := config.Defaults
	cfg.UserTimezone = tz

	svc, err := app.NewService(
		clk, cfg, idgen.New(),
		sqlite.NewAccountRepository(db), sqlite.NewCategoryRepository(db),
		sqlite.NewTransactionRepository(db), sqlite.NewTagRepository(db),
		sqlite.NewUserRepository(db), sqlite.NewSessionRepository(db), sqlite.NewAPITokenRepository(db),
	)
	if err != nil {
		t.Fatalf("app.NewService: %v", err)
	}
	return svc
}

// testServer bundles the real *httptest.Server every test in this file
// exercises with a bearer API token already minted for the seeded user
// (see newTestServer) — every route but /healthz and /api/v1/auth/login
// requires a resolved ActorID now (issue #56), so do (below) authenticates
// every request with it by default rather than making each test case set
// up its own credential.
type testServer struct {
	*httptest.Server
	token string
}

// newTestServer builds a real *httptest.Server serving
// httpsurface.NewMux(svc, nil) over a newTestService, with a password set
// on the seeded user and a bearer API token minted for it — the
// credential do (below) presents on every request by default. Tests that
// specifically exercise authentication itself (auth_test.go) build their
// own server from newTestService instead, so they control credentials
// directly.
func newTestServer(t *testing.T, frozenAt time.Time, tz string) *testServer {
	t.Helper()

	svc := newTestService(t, frozenAt, tz)
	ctx := context.Background()
	if err := svc.SetPassword(ctx, app.SetPasswordCommand{ActorID: ports.SeededUserID, NewPassword: "test-password-long-enough"}); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	tok, err := svc.CreateAPIToken(ctx, app.CreateAPITokenCommand{ActorID: ports.SeededUserID, Name: "http_test"})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	srv := httptest.NewServer(httpsurface.NewMux(svc, nil))
	t.Cleanup(srv.Close)
	return &testServer{Server: srv, token: tok.PlaintextToken}
}

// do sends method/path (with an optional JSON body) to srv and returns
// the response's status code and its decoded body — an envelope shaped
// {"data": ...} on success or {"error": {...}} on failure, per
// respond.go. t fails the test outright on any transport or JSON-decode
// failure, since those would mean this package's own encoding broke, not
// that a test case is exercising expected-failure behaviour. Every
// response also goes through validateResponse before returning, so every
// caller gets openapi3filter's schema check for free (issue #36) without
// asking for it.
func do(t *testing.T, srv *testServer, method, path string, body any) (int, map[string]any) {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal(body): %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+srv.token)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: read response body: %v", method, path, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		t.Fatalf("%s %s: decode response body: %v", method, path, err)
	}

	validateResponse(t, req, resp, respBody)

	return resp.StatusCode, decoded
}

// dataOf extracts and type-asserts decoded["data"], failing the test with
// a readable message if the response wasn't a success envelope.
func dataOf(t *testing.T, decoded map[string]any) map[string]any {
	t.Helper()
	data, ok := decoded["data"].(map[string]any)
	if !ok {
		t.Fatalf("response has no \"data\" object: %+v", decoded)
	}
	return data
}

// errorCodeOf extracts decoded["error"]["code"], failing the test if the
// response wasn't an error envelope.
func errorCodeOf(t *testing.T, decoded map[string]any) string {
	t.Helper()
	errObj, ok := decoded["error"].(map[string]any)
	if !ok {
		t.Fatalf("response has no \"error\" object: %+v", decoded)
	}
	code, _ := errObj["code"].(string)
	return code
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")

	status, decoded := do(t, srv, http.MethodGet, "/healthz", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	data := dataOf(t, decoded)
	if data["status"] != "ok" {
		t.Errorf("status field = %v, want \"ok\"", data["status"])
	}
}

func TestAccountsLifecycle(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{
		"name": "HDFC Savings", "type": "bank", "currency": "INR",
	})
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %+v", status, decoded)
	}
	created := dataOf(t, decoded)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created account has no id: %+v", created)
	}
	if created["opening_balance"] != "0.00" {
		t.Errorf("opening_balance = %v, want a string \"0.00\" (never a JSON number)", created["opening_balance"])
	}
	if created["currency"] != "INR" {
		t.Errorf("currency = %v, want INR", created["currency"])
	}

	status, decoded = do(t, srv, http.MethodGet, "/api/v1/accounts", nil)
	if status != http.StatusOK {
		t.Fatalf("list status = %d, want 200", status)
	}
	list, ok := decoded["data"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("list data = %+v, want a single-element array", decoded["data"])
	}

	status, decoded = do(t, srv, http.MethodGet, "/api/v1/accounts/"+id, nil)
	if status != http.StatusOK {
		t.Fatalf("get status = %d, want 200", status)
	}
	if dataOf(t, decoded)["id"] != id {
		t.Errorf("get returned a different account")
	}

	status, decoded = do(t, srv, http.MethodPatch, "/api/v1/accounts/"+id, map[string]any{"name": "HDFC Savings (renamed)"})
	if status != http.StatusOK {
		t.Fatalf("patch (rename) status = %d, want 200: %+v", status, decoded)
	}
	if dataOf(t, decoded)["name"] != "HDFC Savings (renamed)" {
		t.Errorf("name after rename = %v", dataOf(t, decoded)["name"])
	}

	status, decoded = do(t, srv, http.MethodPatch, "/api/v1/accounts/"+id, map[string]any{"opening_balance": "500"})
	if status != http.StatusOK {
		t.Fatalf("patch (opening balance) status = %d, want 200: %+v", status, decoded)
	}
	if dataOf(t, decoded)["opening_balance"] != "500.00" {
		t.Errorf("opening_balance after set = %v, want 500.00", dataOf(t, decoded)["opening_balance"])
	}

	status, decoded = do(t, srv, http.MethodPatch, "/api/v1/accounts/"+id, map[string]any{"name": "x", "opening_balance": "1"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("patch with both fields status = %d, want 422: %+v", status, decoded)
	}
	if code := errorCodeOf(t, decoded); code != "invalid_input" {
		t.Errorf("error code = %q, want invalid_input", code)
	}

	status, decoded = do(t, srv, http.MethodPatch, "/api/v1/accounts/"+id, map[string]any{})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("patch with neither field status = %d, want 422: %+v", status, decoded)
	}

	status, decoded = do(t, srv, http.MethodDelete, "/api/v1/accounts/"+id, nil)
	if status != http.StatusOK {
		t.Fatalf("archive status = %d, want 200: %+v", status, decoded)
	}
	if dataOf(t, decoded)["archived"] != true {
		t.Errorf("archived = %v, want true", dataOf(t, decoded)["archived"])
	}

	status, decoded = do(t, srv, http.MethodGet, "/api/v1/accounts/nonexistent-account", nil)
	if status != http.StatusNotFound {
		t.Fatalf("get unknown account status = %d, want 404: %+v", status, decoded)
	}
	if code := errorCodeOf(t, decoded); code != "not_found" {
		t.Errorf("error code = %q, want not_found", code)
	}
}

func TestCategoriesLifecycle(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/categories", map[string]any{"name": "Food", "type": "expense"})
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %+v", status, decoded)
	}
	parent := dataOf(t, decoded)
	parentID, _ := parent["id"].(string)

	status, decoded = do(t, srv, http.MethodPost, "/api/v1/categories", map[string]any{
		"name": "Groceries", "type": "expense", "parent": parentID,
	})
	if status != http.StatusCreated {
		t.Fatalf("create child status = %d, want 201: %+v", status, decoded)
	}
	child := dataOf(t, decoded)
	childID, _ := child["id"].(string)
	if child["parent_id"] != parentID {
		t.Errorf("parent_id = %v, want %v", child["parent_id"], parentID)
	}

	status, decoded = do(t, srv, http.MethodPatch, "/api/v1/categories/"+childID, map[string]any{"parent": ""})
	if status != http.StatusOK {
		t.Fatalf("reparent to top-level status = %d, want 200: %+v", status, decoded)
	}
	if _, hasParent := dataOf(t, decoded)["parent_id"]; hasParent {
		t.Errorf("parent_id present after reparenting to top-level: %+v", dataOf(t, decoded))
	}

	status, decoded = do(t, srv, http.MethodPatch, "/api/v1/categories/"+childID, map[string]any{"name": "x", "parent": "y"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("patch with both fields status = %d, want 422: %+v", status, decoded)
	}

	status, decoded = do(t, srv, http.MethodDelete, "/api/v1/categories/"+childID, nil)
	if status != http.StatusOK {
		t.Fatalf("archive status = %d, want 200: %+v", status, decoded)
	}
	if dataOf(t, decoded)["archived"] != true {
		t.Errorf("archived = %v, want true", dataOf(t, decoded)["archived"])
	}
}

// TestCreateTransaction_NoDateBooksToActorsToday is issue #8's headline
// "done when" item: POST /api/v1/transactions with no "date" field books
// to the correct date in the actor's timezone, decided server-side. The
// frozen instant is 2026-07-31T18:45:00Z — 2026-08-01 in Asia/Kolkata —
// the same deliberately hostile instant ADR-0005's conformance suite
// uses, so a handler that resolved "today" itself (in the wrong zone, or
// via a wall-clock read) would produce 2026-07-31 instead and fail this.
func TestCreateTransaction_NoDateBooksToActorsToday(t *testing.T) {
	frozenAt := time.Date(2026, time.July, 31, 18, 45, 0, 0, time.UTC)
	srv := newTestServer(t, frozenAt, "Asia/Kolkata")

	_, decoded := do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Cash", "type": "cash", "currency": "INR"})
	accountID := dataOf(t, decoded)["id"].(string)
	_, decoded = do(t, srv, http.MethodPost, "/api/v1/categories", map[string]any{"name": "Groceries", "type": "expense"})
	categoryID := dataOf(t, decoded)["id"].(string)

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/transactions", map[string]any{
		"type": "outflow", "account": accountID, "category": categoryID,
		"amount": "800", "description": "Groceries",
	})
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %+v", status, decoded)
	}
	if got := dataOf(t, decoded)["date"]; got != "2026-08-01" {
		t.Errorf("date = %v, want 2026-08-01 (today in Asia/Kolkata at the frozen instant)", got)
	}
}

func TestTransactionsAndTransfersLifecycle(t *testing.T) {
	frozenAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	srv := newTestServer(t, frozenAt, "UTC")

	_, decoded := do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Checking", "type": "bank", "currency": "USD"})
	checking := dataOf(t, decoded)["id"].(string)
	_, decoded = do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Savings", "type": "bank", "currency": "USD"})
	savings := dataOf(t, decoded)["id"].(string)
	_, decoded = do(t, srv, http.MethodPost, "/api/v1/categories", map[string]any{"name": "Groceries", "type": "expense"})
	groceries := dataOf(t, decoded)["id"].(string)
	_, decoded = do(t, srv, http.MethodPost, "/api/v1/categories", map[string]any{"name": "Salary", "type": "income"})
	salary := dataOf(t, decoded)["id"].(string)

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/transactions", map[string]any{
		"type": "outflow", "account": checking, "category": groceries,
		"amount": "800", "date": "2026-08-14", "description": "Groceries", "tags": []string{"#Food", "food"},
	})
	if status != http.StatusCreated {
		t.Fatalf("record outflow status = %d, want 201: %+v", status, decoded)
	}
	outflow := dataOf(t, decoded)
	if outflow["amount"] != "800.00" || outflow["currency"] != "USD" {
		t.Errorf("outflow amount/currency = %v/%v, want 800.00/USD", outflow["amount"], outflow["currency"])
	}
	tags, _ := outflow["tags"].([]any)
	if len(tags) != 1 || tags[0] != "food" {
		t.Errorf("tags = %v, want deduplicated, normalised [\"food\"]", outflow["tags"])
	}
	outflowID := outflow["id"].(string)

	status, decoded = do(t, srv, http.MethodPost, "/api/v1/transactions", map[string]any{
		"type": "inflow", "account": checking, "category": salary,
		"amount": "150000", "date": "2026-08-01", "description": "Salary",
	})
	if status != http.StatusCreated {
		t.Fatalf("record inflow status = %d, want 201: %+v", status, decoded)
	}

	status, decoded = do(t, srv, http.MethodPost, "/api/v1/transactions", map[string]any{
		"type": "transfer", "account": checking, "amount": "100", "description": "not a real type",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid transaction type status = %d, want 422: %+v", status, decoded)
	}

	status, decoded = do(t, srv, http.MethodPost, "/api/v1/transfers", map[string]any{
		"from_account": checking, "to_account": savings, "amount": "200", "date": "2026-08-14", "description": "To savings",
	})
	if status != http.StatusCreated {
		t.Fatalf("record transfer status = %d, want 201: %+v", status, decoded)
	}
	transfer := dataOf(t, decoded)
	if transfer["from_account_id"] != checking || transfer["to_account_id"] != savings {
		t.Errorf("transfer from/to = %v/%v, want %v/%v", transfer["from_account_id"], transfer["to_account_id"], checking, savings)
	}
	if transfer["amount"] != "200.00" {
		t.Errorf("transfer amount = %v, want 200.00", transfer["amount"])
	}

	status, decoded = do(t, srv, http.MethodGet, "/api/v1/transactions/"+outflowID, nil)
	if status != http.StatusOK {
		t.Fatalf("get status = %d, want 200: %+v", status, decoded)
	}
	if dataOf(t, decoded)["id"] != outflowID {
		t.Errorf("get returned the wrong transaction")
	}

	status, decoded = do(t, srv, http.MethodPatch, "/api/v1/transactions/"+outflowID, map[string]any{
		"account": checking, "category": groceries, "amount": "950", "date": "2026-08-15",
		"description": "Groceries (corrected)",
	})
	if status != http.StatusOK {
		t.Fatalf("edit status = %d, want 200: %+v", status, decoded)
	}
	edited := dataOf(t, decoded)
	if edited["amount"] != "950.00" || edited["date"] != "2026-08-15" || edited["description"] != "Groceries (corrected)" {
		t.Errorf("edited transaction = %+v", edited)
	}

	status, decoded = do(t, srv, http.MethodDelete, "/api/v1/transactions/"+outflowID, nil)
	if status != http.StatusOK {
		t.Fatalf("delete status = %d, want 200: %+v", status, decoded)
	}

	status, decoded = do(t, srv, http.MethodGet, "/api/v1/transactions/"+outflowID, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get soft-deleted transaction status = %d, want 404: %+v", status, decoded)
	}

	status, decoded = do(t, srv, http.MethodGet, "/api/v1/balances", nil)
	if status != http.StatusOK {
		t.Fatalf("balances status = %d, want 200: %+v", status, decoded)
	}
	if decoded["data"] == nil {
		t.Fatalf("balances response has no data: %+v", decoded)
	}
}

func TestListTransactions_CursorPagination(t *testing.T) {
	frozenAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	srv := newTestServer(t, frozenAt, "UTC")

	_, decoded := do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Cash", "type": "cash", "currency": "USD"})
	account := dataOf(t, decoded)["id"].(string)

	for i := range 5 {
		status, decoded := do(t, srv, http.MethodPost, "/api/v1/transactions", map[string]any{
			"type": "outflow", "account": account, "amount": "10",
			"date": fmt.Sprintf("2026-08-%02d", i+1), "description": fmt.Sprintf("txn %d", i),
		})
		if status != http.StatusCreated {
			t.Fatalf("create txn %d status = %d: %+v", i, status, decoded)
		}
	}

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		path := "/api/v1/transactions?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		status, decoded := do(t, srv, http.MethodGet, path, nil)
		if status != http.StatusOK {
			t.Fatalf("list status = %d, want 200: %+v", status, decoded)
		}
		page := dataOf(t, decoded)
		rows, _ := page["data"].([]any)
		for _, row := range rows {
			id := row.(map[string]any)["id"].(string)
			if seen[id] {
				t.Fatalf("transaction %s returned on more than one page", id)
			}
			seen[id] = true
		}
		pages++
		if pages > 10 {
			t.Fatalf("pagination did not terminate after 10 pages")
		}
		next, _ := page["next_cursor"].(string)
		if next == "" {
			break
		}
		cursor = next
	}

	if len(seen) != 5 {
		t.Errorf("saw %d distinct transactions across all pages, want 5", len(seen))
	}
	if pages < 3 {
		t.Errorf("got %d pages at page size 2 for 5 rows, want at least 3", pages)
	}
}

func TestCrossCurrencyTransferRejected(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")

	_, decoded := do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "USD Account", "type": "bank", "currency": "USD"})
	usd := dataOf(t, decoded)["id"].(string)
	_, decoded = do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "INR Account", "type": "bank", "currency": "INR"})
	inr := dataOf(t, decoded)["id"].(string)

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/transfers", map[string]any{
		"from_account": usd, "to_account": inr, "amount": "100", "description": "cross-currency",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %+v", status, decoded)
	}
}

func TestMalformedRequestBody(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/accounts", bytes.NewReader([]byte("{not json")))
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+srv.token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code := errorCodeOf(t, decoded); code != "invalid_input" {
		t.Errorf("error code = %q, want invalid_input", code)
	}
	validateResponse(t, req, resp, respBody)
}
