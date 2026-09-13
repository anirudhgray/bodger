// This file extends this package's conformance coverage to issue #244:
// `bodger budgets` and /api/v1/budgets must agree field-for-field across
// the whole lifecycle internal/app/budgets.go (issue #242) and
// internal/app/budget_actuals.go (issue #243) expose — create, add/update/
// remove a line, update, get actuals, get history, and archive.
//
// Unlike restore (restore_conformance_test.go), none of these operations
// wipe an actor's ledger, so both surfaces can safely run against one
// shared harness (the same pattern cases_test.go's transaction-verb table
// uses) — each surface creates and drives its own, separate budget, and
// the two results are compared field-for-field, IDs excluded (a budget ID,
// line ID, and category ID resolved independently by each surface's own
// create/add call are never expected to literally match, unlike a shared
// seeded fixture's ID).
package conformance

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// budgetLineConformanceView is one line's wire shape, common to both
// surfaces' budgetView (internal/surface/cli/budgets.go,
// internal/surface/http/budgets.go) — category_id is included since both
// surfaces resolve "groceries" against the same seeded category on this
// shared harness, so it's directly comparable despite being an ID.
type budgetLineConformanceView struct {
	CategoryID string `json:"category_id"`
	Amount     string `json:"amount"`
	Rollover   bool   `json:"rollover"`
}

// budgetConformanceView is a budget's wire shape, ID excluded (see this
// file's own doc comment for why).
type budgetConformanceView struct {
	Name       string                      `json:"name"`
	PeriodType string                      `json:"period_type"`
	Currency   string                      `json:"currency"`
	StartsOn   string                      `json:"starts_on"`
	Archived   bool                        `json:"archived"`
	ArchivedAt string                      `json:"archived_at,omitempty"`
	Lines      []budgetLineConformanceView `json:"lines"`
}

// budgetLineActualsConformanceView is one line's plan-vs-actual, category_id
// included for the same reason budgetLineConformanceView's is.
type budgetLineActualsConformanceView struct {
	CategoryID  string  `json:"category_id"`
	Budgeted    string  `json:"budgeted"`
	Actual      string  `json:"actual"`
	Remaining   string  `json:"remaining"`
	Utilisation float64 `json:"utilisation"`
}

type budgetActualsConformanceView struct {
	Currency string                             `json:"currency"`
	From     string                             `json:"from"`
	To       string                             `json:"to"`
	Lines    []budgetLineActualsConformanceView `json:"lines"`
}

type budgetHistoryConformanceView struct {
	Periods []budgetActualsConformanceView `json:"periods"`
}

// reencode round-trips v through JSON into a budgetConformanceView (or
// whatever out points to) — the same technique
// fx_fetch_conformance_test.go's fxFetchResultFromHTTP uses to make a
// runHTTP call's generic map[string]any shape directly comparable to a
// runCLI call's own decoded --json envelope.
func reencode(t *testing.T, v any, out any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("reencode: marshal: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("reencode: unmarshal into %T: %v (data: %s)", out, err, raw)
	}
}

func cliBudgetView(t *testing.T, h *harness, args ...string) (id string, v budgetConformanceView) {
	t.Helper()
	out, err := h.runCLI(append(args, "--json")...)
	if err != nil {
		t.Fatalf("CLI %v: unexpected error: %v (output: %s)", args, err, out)
	}
	data := decodeEnvelope(t, out)
	reencode(t, data, &v)
	id, _ = data["id"].(string)
	return id, v
}

func httpBudgetView(t *testing.T, h *harness, method, path string, body any) (id string, v budgetConformanceView) {
	t.Helper()
	status, decoded := h.runHTTP(method, path, body)
	if status >= 300 {
		t.Fatalf("HTTP %s %s: unexpected error status %d: %v", method, path, status, decoded)
	}
	data := h.httpData(decoded)
	reencode(t, data, &v)
	id, _ = data["id"].(string)
	return id, v
}

// TestBudgetSurfacesConformance drives `bodger budgets`/`bodger budgets
// lines`/`bodger budgets actuals`/`bodger budgets history` and their HTTP
// counterparts through the same lifecycle — create, add a line, update the
// budget, update the line, read actuals, read history, remove the line,
// archive — each surface against its own budget, on one shared harness,
// and asserts every step's result agrees field-for-field (IDs excluded).
func TestBudgetSurfacesConformance(t *testing.T) {
	h := newHarness(t)
	h.seed() // "groceries" (expense) among others

	// ---- create ----
	cliID, cliBudget := cliBudgetView(t, h, "budgets", "add", "Groceries Budget", "--currency", "USD", "--starts-on", "2026-08-01")
	httpID, httpBudget := httpBudgetView(t, h, "POST", "/api/v1/budgets", map[string]any{
		"name": "Groceries Budget", "currency": "USD", "starts_on": "2026-08-01",
	})
	if cliID == "" || httpID == "" {
		t.Fatalf("create: got empty id(s): cli=%q http=%q", cliID, httpID)
	}
	assertBudgetsEqual(t, "create", cliBudget, httpBudget)

	// ---- add a line ----
	cliID, cliBudget = cliBudgetView(t, h, "budgets", "lines", "add", cliID, "groceries", "500")
	httpID, httpBudget = httpBudgetView(t, h, "POST", "/api/v1/budgets/"+httpID+"/lines", map[string]any{
		"category_ref": "groceries", "amount": "500",
	})
	assertBudgetsEqual(t, "add line", cliBudget, httpBudget)
	if len(cliBudget.Lines) != 1 {
		t.Fatalf("add line: cli lines = %+v, want exactly one", cliBudget.Lines)
	}

	// Both surfaces resolve "groceries" against the very same seeded
	// category on this shared harness, so the two lines' own IDs (fetched
	// back through a fresh `show`/GET, since the add response above didn't
	// carry the line's own ID into budgetLineConformanceView) must also
	// literally agree — proving each surface is finding the same line, not
	// just budgets shaped the same way.
	cliLineID := onlyLineID(t, cliRawBudget(t, h, cliID))
	httpLineID := onlyLineID(t, httpRawBudget(t, h, httpID))
	if cliLineID == "" || httpLineID == "" {
		t.Fatalf("add line: got empty line id(s): cli=%q http=%q", cliLineID, httpLineID)
	}

	// ---- update the budget (full replacement: name + starts_on) ----
	cliID, cliBudget = cliBudgetView(t, h, "budgets", "update", cliID, "Groceries Budget (updated)", "--starts-on", "2026-08-01")
	httpID, httpBudget = httpBudgetView(t, h, "PATCH", "/api/v1/budgets/"+httpID, map[string]any{
		"name": "Groceries Budget (updated)", "starts_on": "2026-08-01",
	})
	assertBudgetsEqual(t, "update budget", cliBudget, httpBudget)

	// ---- update the line ----
	cliID, cliBudget = cliBudgetView(t, h, "budgets", "lines", "update", cliID, cliLineID, "600", "--rollover")
	httpID, httpBudget = httpBudgetView(t, h, "PATCH", "/api/v1/budgets/"+httpID+"/lines/"+httpLineID, map[string]any{
		"amount": "600", "rollover": true,
	})
	assertBudgetsEqual(t, "update line", cliBudget, httpBudget)
	if !cliBudget.Lines[0].Rollover || cliBudget.Lines[0].Amount != "600.00" {
		t.Fatalf("update line: cli line = %+v, want amount 600.00 with rollover", cliBudget.Lines[0])
	}

	// ---- actuals (no transactions posted yet: actual is zero) ----
	cliOut, cliErr := h.runCLI("budgets", "actuals", cliID, "--period", "2026-08-15", "--json")
	if cliErr != nil {
		t.Fatalf("CLI budgets actuals: unexpected error: %v (output: %s)", cliErr, cliOut)
	}
	var cliActuals budgetActualsConformanceView
	reencode(t, decodeEnvelope(t, cliOut), &cliActuals)

	httpStatus, httpDecoded := h.runHTTP("GET", "/api/v1/budgets/"+httpID+"/actuals?period=2026-08-15", nil)
	if httpStatus != http.StatusOK {
		t.Fatalf("GET .../actuals: status = %d, want 200: %+v", httpStatus, httpDecoded)
	}
	var httpActuals budgetActualsConformanceView
	reencode(t, h.httpData(httpDecoded), &httpActuals)

	assertActualsEqual(t, "actuals", cliActuals, httpActuals)
	if len(cliActuals.Lines) != 1 || cliActuals.Lines[0].Actual != "0.00" || cliActuals.Lines[0].Remaining != "600.00" {
		t.Fatalf("actuals: cli lines = %+v, want one zero-actual line remaining 600.00", cliActuals.Lines)
	}

	// ---- history ----
	cliOut, cliErr = h.runCLI("budgets", "history", cliID, "--period", "2026-08-15", "--months", "2", "--json")
	if cliErr != nil {
		t.Fatalf("CLI budgets history: unexpected error: %v (output: %s)", cliErr, cliOut)
	}
	var cliHistory budgetHistoryConformanceView
	reencode(t, decodeEnvelope(t, cliOut), &cliHistory)

	httpStatus, httpDecoded = h.runHTTP("GET", "/api/v1/budgets/"+httpID+"/history?period=2026-08-15&months=2", nil)
	if httpStatus != http.StatusOK {
		t.Fatalf("GET .../history: status = %d, want 200: %+v", httpStatus, httpDecoded)
	}
	var httpHistory budgetHistoryConformanceView
	reencode(t, h.httpData(httpDecoded), &httpHistory)

	cliJSON, _ := json.MarshalIndent(cliHistory, "", "  ")
	httpJSON, _ := json.MarshalIndent(httpHistory, "", "  ")
	if string(cliJSON) != string(httpJSON) {
		t.Errorf("history: CLI and HTTP disagree:\nCLI:\n%s\nHTTP:\n%s", cliJSON, httpJSON)
	}
	// The budget started 2026-08-01 (this harness's "today", per
	// hostileInstant); asking for 2 months ending August must clamp to
	// exactly one period rather than erroring or padding with a
	// nonexistent July period.
	if len(cliHistory.Periods) != 1 {
		t.Fatalf("history: periods = %+v, want exactly 1 (clamped at the budget's own starts_on)", cliHistory.Periods)
	}

	// ---- remove the line ----
	cliID, cliBudget = cliBudgetView(t, h, "budgets", "lines", "remove", cliID, cliLineID)
	httpID, httpBudget = httpBudgetView(t, h, "DELETE", "/api/v1/budgets/"+httpID+"/lines/"+httpLineID, nil)
	assertBudgetsEqual(t, "remove line", cliBudget, httpBudget)
	if len(cliBudget.Lines) != 0 {
		t.Fatalf("remove line: cli lines = %+v, want none", cliBudget.Lines)
	}

	// ---- archive ----
	_, cliBudget = cliBudgetView(t, h, "budgets", "archive", cliID)
	_, httpBudget = httpBudgetView(t, h, "DELETE", "/api/v1/budgets/"+httpID, nil)
	assertBudgetsEqual(t, "archive", cliBudget, httpBudget)
	if !cliBudget.Archived || cliBudget.ArchivedAt == "" {
		t.Fatalf("archive: cli budget = %+v, want archived with archived_at set", cliBudget)
	}
}

// assertBudgetsEqual compares two budgetConformanceViews field for field,
// failing with step named in the message on any mismatch.
func assertBudgetsEqual(t *testing.T, step string, cli, httpV budgetConformanceView) {
	t.Helper()
	cliJSON, _ := json.MarshalIndent(cli, "", "  ")
	httpJSON, _ := json.MarshalIndent(httpV, "", "  ")
	if string(cliJSON) != string(httpJSON) {
		t.Errorf("%s: CLI and HTTP disagree:\nCLI:\n%s\nHTTP:\n%s", step, cliJSON, httpJSON)
	}
}

func assertActualsEqual(t *testing.T, step string, cli, httpV budgetActualsConformanceView) {
	t.Helper()
	cliJSON, _ := json.MarshalIndent(cli, "", "  ")
	httpJSON, _ := json.MarshalIndent(httpV, "", "  ")
	if string(cliJSON) != string(httpJSON) {
		t.Errorf("%s: CLI and HTTP disagree:\nCLI:\n%s\nHTTP:\n%s", step, cliJSON, httpJSON)
	}
}

// cliRawBudget and httpRawBudget re-fetch a budget through `budgets show`/
// GET /api/v1/budgets/{id} as a bare map, so onlyLineID can read the one
// field (a line's own "id") that budgetLineConformanceView deliberately
// omits from the create/add/update responses this test otherwise compares.
func cliRawBudget(t *testing.T, h *harness, id string) map[string]any {
	t.Helper()
	out, err := h.runCLI("budgets", "show", id, "--json")
	if err != nil {
		t.Fatalf("CLI budgets show %s: unexpected error: %v (output: %s)", id, err, out)
	}
	return decodeEnvelope(t, out)
}

func httpRawBudget(t *testing.T, h *harness, id string) map[string]any {
	t.Helper()
	status, decoded := h.runHTTP("GET", "/api/v1/budgets/"+id, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/budgets/%s: status = %d, want 200: %+v", id, status, decoded)
	}
	return h.httpData(decoded)
}

// onlyLineID extracts the single line's "id" field from a raw decoded
// budget, failing the test outright if there isn't exactly one line.
func onlyLineID(t *testing.T, budget map[string]any) string {
	t.Helper()
	lines, ok := budget["lines"].([]any)
	if !ok || len(lines) != 1 {
		t.Fatalf("onlyLineID: budget %+v has no exactly-one-line \"lines\"", budget)
	}
	line, ok := lines[0].(map[string]any)
	if !ok {
		t.Fatalf("onlyLineID: line %+v is not an object", lines[0])
	}
	id, _ := line["id"].(string)
	return id
}

// TestBudgetSurfacesConformance_Errors covers the invalid-input paths both
// surfaces must reject identically: an unknown budget ID, and adding a
// second line for a category the budget already has one for.
func TestBudgetSurfacesConformance_Errors(t *testing.T) {
	h := newHarness(t)
	h.seed()

	t.Run("unknown budget id", func(t *testing.T) {
		cliOut, cliErr := h.runCLI("budgets", "show", "does-not-exist", "--json")
		if cliErr == nil {
			t.Fatalf("CLI: expected an error, got success (output: %s)", cliOut)
		}
		if code := h.cliErrorCode(cliErr); code != errs.NotFound {
			t.Errorf("CLI error code = %s, want %s", code, errs.NotFound)
		}

		httpStatus, httpDecoded := h.runHTTP("GET", "/api/v1/budgets/does-not-exist", nil)
		if httpStatus != http.StatusNotFound {
			t.Fatalf("HTTP: status = %d, want 404: %+v", httpStatus, httpDecoded)
		}
		if code := h.httpErrorCode(httpDecoded); code != errs.NotFound {
			t.Errorf("HTTP error code = %s, want %s", code, errs.NotFound)
		}
	})

	t.Run("duplicate category line", func(t *testing.T) {
		cliID, _ := cliBudgetView(t, h, "budgets", "add", "Dup Test CLI", "--currency", "USD", "--starts-on", "2026-08-01")
		if out, err := h.runCLI("budgets", "lines", "add", cliID, "groceries", "500", "--json"); err != nil {
			t.Fatalf("seed first line (CLI): unexpected error: %v (output: %s)", err, out)
		}
		cliOut, cliErr := h.runCLI("budgets", "lines", "add", cliID, "groceries", "100", "--json")
		if cliErr == nil {
			t.Fatalf("CLI: expected an error adding a duplicate-category line, got success (output: %s)", cliOut)
		}
		if code := h.cliErrorCode(cliErr); code != errs.InvalidInput {
			t.Errorf("CLI error code = %s, want %s", code, errs.InvalidInput)
		}

		httpID, _ := httpBudgetView(t, h, "POST", "/api/v1/budgets", map[string]any{
			"name": "Dup Test HTTP", "currency": "USD", "starts_on": "2026-08-01",
		})
		if status, decoded := h.runHTTP("POST", "/api/v1/budgets/"+httpID+"/lines", map[string]any{
			"category_ref": "groceries", "amount": "500",
		}); status != http.StatusCreated {
			t.Fatalf("seed first line (HTTP): status = %d, want 201: %+v", status, decoded)
		}
		httpStatus, httpDecoded := h.runHTTP("POST", "/api/v1/budgets/"+httpID+"/lines", map[string]any{
			"category_ref": "groceries", "amount": "100",
		})
		if httpStatus != http.StatusUnprocessableEntity {
			t.Fatalf("HTTP: status = %d, want 422: %+v", httpStatus, httpDecoded)
		}
		if code := h.httpErrorCode(httpDecoded); code != errs.InvalidInput {
			t.Errorf("HTTP error code = %s, want %s", code, errs.InvalidInput)
		}

		cliField := h.cliErrorField(cliErr)
		httpField := h.httpErrorField(httpDecoded)
		if cliField != httpField {
			t.Errorf("CLI and HTTP disagree on which field this error attaches to: CLI %q, HTTP %q", cliField, httpField)
		}
	})
}
