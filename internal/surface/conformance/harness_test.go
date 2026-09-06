// Package conformance drives bodger's two M1 surfaces — the CLI
// (internal/surface/cli) and the REST API (internal/surface/http) —
// through their real entry points against one shared application-layer
// Service, and asserts the observable result is identical: the same
// stored transaction fields for the same logical input, and the same
// error code for the same invalid input. This is issue #9's enforcement
// of ADR-0005's normalise-once contract — everything in
// docs/decisions/0005-shared-application-layer.md promises is only a
// constraint if a divergence between the two surfaces fails the build.
//
// Every file in this package ends in _test.go, including this one: the
// package exists purely to be run by `go test`, has nothing for
// production code to import, and — not incidentally — is therefore
// exempt from internal/lint's import-graph check the same way every
// other test file in the repository is (FindImportViolations only scans
// non-test files). That exemption is what lets this package legitimately
// import internal/adapters/sqlite to build a real, shared database for
// both surfaces to hit, which neither surface's own production code may
// do.
//
// Adding a third surface (MCP, M7) costs one wiring step: a
// runMCP-shaped method on harness next to runCLI and runHTTP below, and
// a third field on recordResult's expected comparison in cases_test.go.
// Every existing table row then exercises it for free.
package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/adapters/fxprovider"
	"github.com/anirudhgray/bodger/internal/adapters/sqlite"
	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	"github.com/anirudhgray/bodger/internal/ports"
	clisurface "github.com/anirudhgray/bodger/internal/surface/cli"
	httpsurface "github.com/anirudhgray/bodger/internal/surface/http"
)

// hostileInstant is issue #9's deliberately hostile frozen instant:
// 2026-07-31T18:45:00Z is already 2026-08-01 in actorTimezone. A surface
// that resolved "today" itself (instead of delegating to
// internal/app/normalize.DateOf), or that resolved it in the process's
// own UTC zone instead of the actor's, would book a transaction dated
// 2026-07-31 while the other surface booked 2026-08-01 — a divergence
// this package's comparisons catch as a plain date mismatch.
var hostileInstant = time.Date(2026, time.July, 31, 18, 45, 0, 0, time.UTC)

// actorTimezone is the zone every case in this package resolves "today"
// and "yesterday" against — chosen specifically so it disagrees with UTC
// at hostileInstant (see above).
const actorTimezone = "Asia/Kolkata"

// harness wires one shared *app.Service — one SQLite database, frozen at
// hostileInstant, resolving relative dates in actorTimezone — to both
// surfaces' real entry points. Both surfaces read and write the same
// database, so an account or category name each surface resolves
// independently resolves to the *same* real ID, and the two results a
// test compares are directly comparable rather than merely
// structurally similar.
type harness struct {
	t     *testing.T
	svc   *app.Service
	srv   *httptest.Server
	token string
}

// newHarness builds a fresh, empty, fully-migrated database and the
// Service around it — the same wiring internal/surface/cli/cli_test.go's
// newTestFactory and internal/surface/http/http_test.go's newTestServer
// each already do for their own package, unified here so a single
// service instance backs both surfaces at once.
func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWithFxProvider(t, fxprovider.New("", nil))
}

// newHarnessWithFxProvider is newHarness, but with the given FX provider
// wired in instead of the real Frankfurter adapter — for a case (like
// fx_fetch_conformance_test.go's) that needs FetchFxRates to actually
// resolve a canned rate rather than reach the network, the same reason
// internal/surface/cli/fx_test.go's and internal/surface/http/fx_test.go's
// own newTestFactoryWithFxProvider/newTestServerWithFxProvider exist.
func newHarnessWithFxProvider(t *testing.T, provider ports.FxRateProvider) *harness {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "bodger.db")
	clk := clock.NewFrozen(hostileInstant)

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
	cfg.UserTimezone = actorTimezone

	svc, err := app.NewService(
		clk, cfg, idgen.New(),
		sqlite.NewAccountRepository(db), sqlite.NewCategoryRepository(db),
		sqlite.NewTransactionRepository(db), sqlite.NewTagRepository(db),
		sqlite.NewUserRepository(db), sqlite.NewSessionRepository(db), sqlite.NewAPITokenRepository(db),
		sqlite.NewFxRateRepository(db),
		provider,
	)
	if err != nil {
		t.Fatalf("app.NewService: %v", err)
	}

	srv := httptest.NewServer(httpsurface.NewMux(svc, nil))
	t.Cleanup(srv.Close)

	// Every route runHTTP exercises now requires a resolved ActorID
	// (issue #56) — a bearer API token, minted here once, is what
	// authenticates every runHTTP call below; conformance cares that both
	// surfaces resolve the same ActorID and observe the same result, not
	// which credential type got it there.
	if err := svc.SetPassword(context.Background(), app.SetPasswordCommand{
		ActorID: seededUserID, NewPassword: "conformance-test-password",
	}); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	tok, err := svc.CreateAPIToken(context.Background(), app.CreateAPITokenCommand{
		ActorID: seededUserID, Name: "conformance",
	})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	return &harness{t: t, svc: svc, srv: srv, token: tok.PlaintextToken}
}

// seed creates the accounts and categories this package's case table
// (cases_test.go) resolves by name. It runs once per harness, before any
// case-under-test, using the application layer directly rather than
// either surface — seeding is fixture setup, not part of what a case is
// proving.
func (h *harness) seed() {
	h.t.Helper()
	ctx := context.Background()

	mustAccount := func(name, kind, currency string) {
		if _, err := h.svc.CreateAccount(ctx, app.CreateAccountCommand{
			ActorID: seededUserID, Name: name, Kind: kind, Currency: currency,
		}); err != nil {
			h.t.Fatalf("seed account %q: %v", name, err)
		}
	}
	mustCategory := func(name, kind string) {
		if _, err := h.svc.CreateCategory(ctx, app.CreateCategoryCommand{
			ActorID: seededUserID, Name: name, Kind: kind,
		}); err != nil {
			h.t.Fatalf("seed category %q: %v", name, err)
		}
	}

	mustAccount("HDFC Savings", "bank", "INR")
	mustAccount("ICICI Checking", "bank", "INR") // same currency as HDFC Savings, for the transfer case
	mustAccount("Cash", "cash", "USD")
	mustAccount("Wallet", "cash", "USD")
	mustAccount("WALLET", "cash", "USD") // deliberate case-variant sibling of Wallet, see the ambiguous-name case
	mustCategory("groceries", "expense")
	mustCategory("salary", "income")
}

// seededUserID mirrors internal/ports.SeededUserID's value without
// importing internal/ports for it: harness deliberately drives the
// application layer directly only for seed data (above), and every case
// otherwise reaches the actor ID only by way of a real surface (a CLI
// command never takes one as a flag; an HTTP request never takes one as
// a body field) — see runCLI and runHTTP.
const seededUserID = "00000000-0000-0000-0000-000000000001"

// cliFactory always returns h's single shared Service — the same Service
// on every call, unlike production's real factory, which opens a fresh
// database connection per invocation. Both surfaces sharing one Service
// is the point (see harness's doc comment).
func (h *harness) cliFactory() clisurface.ServiceFactory {
	return func(context.Context) (*app.Service, func() error, error) {
		return h.svc, func() error { return nil }, nil
	}
}

// runCLI executes args against a fresh cobra root tree — a fresh tree
// per call for the reason internal/surface/cli/cli_test.go's run gives:
// cobra binds flag variables once at construction, so reusing a tree
// across calls would leak flag values between what must behave as
// separate process invocations. It returns combined stdout+stderr and
// the error Execute returned, exactly as a real `bodger` invocation's
// caller would see them.
func (h *harness) runCLI(args ...string) (output string, err error) {
	h.t.Helper()
	root := &cobra.Command{Use: "bodger", SilenceUsage: true, SilenceErrors: true}
	clisurface.Register(root, h.cliFactory())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append(args, "--json"))
	err = root.Execute()
	return buf.String(), err
}

// cliErrorCode extracts the *errs.Error code a failed runCLI call
// returned, failing the test outright if err isn't one — every error
// internal/app returns is already an *errs.Error (ADR-0011), so anything
// else reaching here means this harness itself is broken, not that a
// case is exercising expected-failure behaviour.
func (h *harness) cliErrorCode(err error) errs.Code {
	h.t.Helper()
	var e *errs.Error
	if !errors.As(err, &e) {
		h.t.Fatalf("cliErrorCode: %v is not an *errs.Error", err)
	}
	return e.Code
}

// cliErrorField extracts the *errs.Error field path a failed runCLI call
// returned, or "" if it set none.
func (h *harness) cliErrorField(err error) string {
	h.t.Helper()
	var e *errs.Error
	if !errors.As(err, &e) {
		h.t.Fatalf("cliErrorField: %v is not an *errs.Error", err)
	}
	return e.FieldPath
}

// runHTTP sends method/path with an optional JSON body to h's server and
// returns the response's status code and decoded body — an envelope
// shaped {"data": ...} on success or {"error": {...}} on failure, per
// internal/surface/http/respond.go.
func (h *harness) runHTTP(method, path string, body any) (status int, decoded map[string]any) {
	h.t.Helper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("json.Marshal(body): %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, h.srv.URL+path, reader)
	if err != nil {
		h.t.Fatalf("http.NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+h.token)

	resp, err := h.srv.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("%s %s: read response body: %v", method, path, err)
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		h.t.Fatalf("%s %s: decode response body: %v", method, path, err)
	}
	return resp.StatusCode, decoded
}

// httpErrorCode extracts the "code" field from a runHTTP error envelope,
// failing the test outright if decoded isn't shaped like one.
func (h *harness) httpErrorCode(decoded map[string]any) errs.Code {
	h.t.Helper()
	errObj, ok := decoded["error"].(map[string]any)
	if !ok {
		h.t.Fatalf("httpErrorCode: %v has no \"error\" object", decoded)
	}
	code, ok := errObj["code"].(string)
	if !ok {
		h.t.Fatalf("httpErrorCode: %v has no \"code\" string", errObj)
	}
	return errs.Code(code)
}

// httpErrorField extracts the "field" key from a runHTTP error envelope,
// or "" if the error carried none (it's omitempty on the wire — see
// internal/platform/errs/error.go's wireError).
func (h *harness) httpErrorField(decoded map[string]any) string {
	h.t.Helper()
	errObj, ok := decoded["error"].(map[string]any)
	if !ok {
		h.t.Fatalf("httpErrorField: %v has no \"error\" object", decoded)
	}
	field, _ := errObj["field"].(string)
	return field
}

// httpData extracts the "data" object from a runHTTP success envelope,
// failing the test outright if decoded isn't shaped like one.
func (h *harness) httpData(decoded map[string]any) map[string]any {
	h.t.Helper()
	data, ok := decoded["data"].(map[string]any)
	if !ok {
		h.t.Fatalf("httpData: %v has no \"data\" object", decoded)
	}
	return data
}
