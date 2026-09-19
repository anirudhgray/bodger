// This file extends this package's conformance coverage to issue #281:
// `bodger recurring`, /api/v1/recurring-rules, /api/v1/recurring-occurrences,
// /api/v1/forecast, and their MCP tool counterparts must agree
// field-for-field across rule create/update/list/archive, occurrence
// list/materialise/skip, and forecast — the CLI/HTTP/MCP triple
// budgets_conformance_test.go established for budgets, extended here with
// the MCP leg for every read/write-tier operation (ADR-0013's own note
// that a table of non-record-verb MCP cases is "left to whichever issue
// first needs one" — this is that issue, per its own explicit
// "conformance rows... across all three surfaces (REST/CLI/MCP)" scope).
//
// archive_recurring_rule is the one operation compared CLI-vs-HTTP only,
// not MCP: it's destructive-tier (recurring_destructive.go's own doc
// comment explains why, unlike archive_budget's write-tier precedent),
// and this package's shared harness builds its MCP dispatcher with
// allowDestructive=false (harness_test.go's own doc comment: "this
// package's cases are all record-verb (write-tier), so no destructive
// tool needs to be registered") — the same reason no other destructive
// MCP tool (delete_transaction, commit_import, restore_snapshot, ...) has
// an MCP leg in this package today either. Extending the harness itself
// to also stand up a second, destructive-enabled MCP session is a
// reasonable follow-up but out of this issue's own scope.
//
// Occurrence generation (GenerateOccurrences) has no surface of its own
// by design (internal/app/recurring_occurrences.go's own doc comment:
// "likely NOT directly exposed as a user-facing op"), so every test below
// that needs a pending occurrence to act on seeds one via the application
// layer directly, the same way harness.seed() creates accounts and
// categories directly rather than through a surface.
package conformance

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// mcpDataArray decodes a successful runMCP result's single TextContent
// block as a JSON array — harness.mcpData's counterpart for a list-shaped
// tool result (list_recurring_rules, list_scheduled_occurrences), since
// json.Unmarshal into map[string]any fails outright for a JSON array.
func (h *harness) mcpDataArray(result *sdkmcp.CallToolResult) []any {
	h.t.Helper()
	if result.IsError {
		h.t.Fatalf("mcpDataArray: result is an error: %+v", result)
	}
	if len(result.Content) == 0 {
		h.t.Fatalf("mcpDataArray: result has no content: %+v", result)
	}
	text, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		h.t.Fatalf("mcpDataArray: content[0] = %T, want *sdkmcp.TextContent", result.Content[0])
	}
	var data []any
	if err := json.Unmarshal([]byte(text.Text), &data); err != nil {
		h.t.Fatalf("mcpDataArray: decode %q: %v", text.Text, err)
	}
	return data
}

// ---- shared comparable views (ID excluded, same reason budgets_conformance_test.go excludes it) ----

type scheduleConformanceView struct {
	Frequency  string `json:"frequency"`
	Interval   int    `json:"interval"`
	Weekday    *int   `json:"weekday,omitempty"`
	DayOfMonth *int   `json:"day_of_month,omitempty"`
	Month      *int   `json:"month,omitempty"`
}

type recurringRuleConformanceView struct {
	AccountID   string                  `json:"account_id"`
	CategoryID  string                  `json:"category_id"`
	Amount      string                  `json:"amount"`
	Currency    string                  `json:"currency"`
	Description string                  `json:"description"`
	Schedule    scheduleConformanceView `json:"schedule"`
	StartsOn    string                  `json:"starts_on"`
	EndsOn      string                  `json:"ends_on,omitempty"`
	Archived    bool                    `json:"archived"`
	ArchivedAt  string                  `json:"archived_at,omitempty"`
}

// scheduledOccurrenceConformanceView excludes id and rule_id — each
// surface below creates and generates occurrences for its own,
// independently-resolved rule, so those IDs are never expected to
// literally match (the same reason recurringRuleConformanceView excludes
// its own id).
type scheduledOccurrenceConformanceView struct {
	OccurrenceDate string `json:"occurrence_date"`
	Status         string `json:"status"`
}

func assertJSONEqual(t *testing.T, step string, cli, other any) {
	t.Helper()
	cliJSON, _ := json.MarshalIndent(cli, "", "  ")
	otherJSON, _ := json.MarshalIndent(other, "", "  ")
	if string(cliJSON) != string(otherJSON) {
		t.Errorf("%s: surfaces disagree:\nfirst:\n%s\nsecond:\n%s", step, cliJSON, otherJSON)
	}
}

// ---- create/update/list/archive views per surface ----

func cliRecurringRuleView(t *testing.T, h *harness, args ...string) (id string, v recurringRuleConformanceView) {
	t.Helper()
	out, err := h.runCLI(args...)
	if err != nil {
		t.Fatalf("CLI %v: unexpected error: %v (output: %s)", args, err, out)
	}
	data := decodeEnvelope(t, out)
	reencode(t, data, &v)
	id, _ = data["id"].(string)
	return id, v
}

func httpRecurringRuleView(t *testing.T, h *harness, method, path string, body any) (id string, v recurringRuleConformanceView) {
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

func mcpRecurringRuleView(t *testing.T, h *harness, tool string, args map[string]any) (id string, v recurringRuleConformanceView) {
	t.Helper()
	result := h.runMCP(context.Background(), tool, args)
	if result.IsError {
		t.Fatalf("MCP %s: unexpected error result: %+v", tool, result)
	}
	data := h.mcpData(result)
	reencode(t, data, &v)
	id, _ = data["id"].(string)
	return id, v
}

// scheduleArgs is the one concrete schedule (monthly, day 1) every case
// below uses — exhaustive frequency coverage is the app layer's own job
// (internal/app/recurring_rules_test.go); this suite's job is proving the
// three surfaces decode/encode the same schedule identically, not
// re-testing every frequency variant three times over.
var scheduleArgs = map[string]any{"frequency": "monthly", "interval": 1, "day_of_month": 1}

func cliScheduleFlags() []string {
	return []string{"--frequency", "monthly", "--interval", "1", "--day-of-month", "1"}
}

// TestRecurringRuleSurfacesConformance drives `bodger recurring`,
// /api/v1/recurring-rules, and their MCP tool counterparts through the
// same create -> update -> list lifecycle, each surface against its own
// rule, and asserts every step's result agrees field-for-field (IDs
// excluded). Archive is compared CLI-vs-HTTP only — see this file's own
// doc comment for why.
func TestRecurringRuleSurfacesConformance(t *testing.T) {
	h := newHarness(t)
	h.seed() // "HDFC Savings" (INR), "groceries" (expense)

	// ---- create ----
	cliID, cliRule := cliRecurringRuleView(t, h, append([]string{
		"recurring", "create", "HDFC Savings", "groceries", "500", "Groceries subscription",
		"--starts-on", "2026-08-01",
	}, cliScheduleFlags()...)...)
	httpID, httpRule := httpRecurringRuleView(t, h, "POST", "/api/v1/recurring-rules", map[string]any{
		"account_ref": "HDFC Savings", "category_ref": "groceries", "amount": "500",
		"description": "Groceries subscription", "schedule": scheduleArgs, "starts_on": "2026-08-01",
	})
	mcpID, mcpRule := mcpRecurringRuleView(t, h, "create_recurring_rule", map[string]any{
		"account_ref": "HDFC Savings", "category_ref": "groceries", "amount": "500",
		"description": "Groceries subscription", "schedule": scheduleArgs, "starts_on": "2026-08-01",
	})
	if cliID == "" || httpID == "" || mcpID == "" {
		t.Fatalf("create: got empty id(s): cli=%q http=%q mcp=%q", cliID, httpID, mcpID)
	}
	assertJSONEqual(t, "create (CLI vs HTTP)", cliRule, httpRule)
	assertJSONEqual(t, "create (CLI vs MCP)", cliRule, mcpRule)
	if cliRule.Currency != "INR" {
		t.Fatalf("create: currency = %q, want INR (HDFC Savings' own currency)", cliRule.Currency)
	}

	// ---- update (full replacement: amount + description + schedule + ends_on) ----
	cliID, cliRule = cliRecurringRuleView(t, h, append([]string{
		"recurring", "update", cliID, "600", "Groceries subscription (updated)",
	}, cliScheduleFlags()...)...)
	httpID, httpRule = httpRecurringRuleView(t, h, "PATCH", "/api/v1/recurring-rules/"+httpID, map[string]any{
		"amount": "600", "description": "Groceries subscription (updated)", "schedule": scheduleArgs,
	})
	mcpID, mcpRule = mcpRecurringRuleView(t, h, "update_recurring_rule", map[string]any{
		"rule_id": mcpID, "amount": "600", "description": "Groceries subscription (updated)", "schedule": scheduleArgs,
	})
	assertJSONEqual(t, "update (CLI vs HTTP)", cliRule, httpRule)
	assertJSONEqual(t, "update (CLI vs MCP)", cliRule, mcpRule)
	if cliRule.Amount != "600.00" {
		t.Fatalf("update: amount = %q, want 600.00", cliRule.Amount)
	}

	// ---- list ----
	cliOut, cliErr := h.runCLI("recurring", "list")
	if cliErr != nil {
		t.Fatalf("CLI recurring list: unexpected error: %v (output: %s)", cliErr, cliOut)
	}
	cliRules := decodeEnvelopeArray(t, cliOut)

	httpStatus, httpDecoded := h.runHTTP("GET", "/api/v1/recurring-rules", nil)
	if httpStatus != http.StatusOK {
		t.Fatalf("GET /api/v1/recurring-rules: status = %d, want 200: %+v", httpStatus, httpDecoded)
	}
	httpRules, ok := httpDecoded["data"].([]any)
	if !ok {
		t.Fatalf("GET /api/v1/recurring-rules: data isn't an array: %+v", httpDecoded)
	}

	mcpResult := h.runMCP(context.Background(), "list_recurring_rules", nil)
	if mcpResult.IsError {
		t.Fatalf("MCP list_recurring_rules: unexpected error result: %+v", mcpResult)
	}
	mcpRules := h.mcpDataArray(mcpResult)

	// All three lists read the same underlying rows for the same actor —
	// this test's own create step above added exactly one rule via each
	// surface into the one shared harness — so all three must agree not
	// just on shape but on count and order ("most recently created
	// first": index 0 is the MCP-created rule in every one of them).
	if len(cliRules) != 3 || len(httpRules) != 3 || len(mcpRules) != 3 {
		t.Fatalf("list: got %d/%d/%d rules (CLI/HTTP/MCP), want exactly 3 each (one per surface's own create step above)", len(cliRules), len(httpRules), len(mcpRules))
	}
	var cliListed, httpListed, mcpListed recurringRuleConformanceView
	reencode(t, cliRules[0], &cliListed)
	reencode(t, httpRules[0], &httpListed)
	reencode(t, mcpRules[0], &mcpListed)
	assertJSONEqual(t, "list (CLI vs HTTP)", cliListed, httpListed)
	assertJSONEqual(t, "list (CLI vs MCP)", cliListed, mcpListed)

	// ---- archive (CLI vs HTTP only — see this file's own doc comment) ----
	_, cliRule = cliRecurringRuleView(t, h, "recurring", "archive", cliID)
	_, httpRule = httpRecurringRuleView(t, h, "DELETE", "/api/v1/recurring-rules/"+httpID, nil)
	assertJSONEqual(t, "archive (CLI vs HTTP)", cliRule, httpRule)
	if !cliRule.Archived || cliRule.ArchivedAt == "" {
		t.Fatalf("archive: cli rule = %+v, want archived with archived_at set", cliRule)
	}
	_ = mcpID
}

// TestRecurringOccurrenceSurfacesConformance seeds one rule per surface
// (each independently created through that surface's own create call),
// generates its occurrences directly via the application layer (see this
// file's own doc comment for why), then drives occurrences list,
// materialise, and skip through all three surfaces and asserts agreement.
func TestRecurringOccurrenceSurfacesConformance(t *testing.T) {
	h := newHarness(t)
	h.seed()
	ctx := context.Background()

	cliRuleID, _ := cliRecurringRuleView(t, h, append([]string{
		"recurring", "create", "HDFC Savings", "groceries", "500", "CLI rule", "--starts-on", "2026-08-01",
	}, cliScheduleFlags()...)...)
	httpRuleID, _ := httpRecurringRuleView(t, h, "POST", "/api/v1/recurring-rules", map[string]any{
		"account_ref": "HDFC Savings", "category_ref": "groceries", "amount": "500",
		"description": "HTTP rule", "schedule": scheduleArgs, "starts_on": "2026-08-01",
	})
	mcpRuleID, _ := mcpRecurringRuleView(t, h, "create_recurring_rule", map[string]any{
		"account_ref": "HDFC Savings", "category_ref": "groceries", "amount": "500",
		"description": "MCP rule", "schedule": scheduleArgs, "starts_on": "2026-08-01",
	})

	for _, ruleID := range []string{cliRuleID, httpRuleID, mcpRuleID} {
		if _, err := h.svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: seededUserID, RuleID: ruleID}); err != nil {
			t.Fatalf("GenerateOccurrences(%s): %v", ruleID, err)
		}
	}

	// ---- occurrences list ----
	cliOut, cliErr := h.runCLI("recurring", "occurrences", "list", "--rule", cliRuleID)
	if cliErr != nil {
		t.Fatalf("CLI recurring occurrences list: unexpected error: %v (output: %s)", cliErr, cliOut)
	}
	cliOccs := decodeEnvelopeArray(t, cliOut)

	httpStatus, httpDecoded := h.runHTTP("GET", "/api/v1/recurring-occurrences?rule_id="+httpRuleID, nil)
	if httpStatus != http.StatusOK {
		t.Fatalf("GET /api/v1/recurring-occurrences: status = %d, want 200: %+v", httpStatus, httpDecoded)
	}
	httpOccs, ok := httpDecoded["data"].([]any)
	if !ok {
		t.Fatalf("GET /api/v1/recurring-occurrences: data isn't an array: %+v", httpDecoded)
	}

	mcpResult := h.runMCP(ctx, "list_scheduled_occurrences", map[string]any{"rule_id": mcpRuleID})
	if mcpResult.IsError {
		t.Fatalf("MCP list_scheduled_occurrences: unexpected error result: %+v", mcpResult)
	}
	mcpOccs := h.mcpDataArray(mcpResult)

	if len(cliOccs) == 0 || len(httpOccs) != len(cliOccs) || len(mcpOccs) != len(cliOccs) {
		t.Fatalf("occurrences list: got %d/%d/%d occurrences (CLI/HTTP/MCP), want a matching nonzero count", len(cliOccs), len(httpOccs), len(mcpOccs))
	}
	var cliFirst, httpFirst, mcpFirst scheduledOccurrenceConformanceView
	reencode(t, cliOccs[0], &cliFirst)
	reencode(t, httpOccs[0], &httpFirst)
	reencode(t, mcpOccs[0], &mcpFirst)
	assertJSONEqual(t, "occurrences list (CLI vs HTTP)", cliFirst, httpFirst)
	assertJSONEqual(t, "occurrences list (CLI vs MCP)", cliFirst, mcpFirst)
	if cliFirst.OccurrenceDate != "2026-08-01" || cliFirst.Status != "pending" {
		t.Fatalf("occurrences list: first = %+v, want 2026-08-01/pending", cliFirst)
	}

	// Each occurrence id is surface-specific (independently generated for
	// each surface's own rule), fetched by re-listing rather than reused
	// from the list responses above (which — like recurringRuleConformanceView
	// — carry no id in the comparable view).
	cliOccID := stringField(cliOccs[0].(map[string]any), "id")
	httpOccID := stringField(httpOccs[0].(map[string]any), "id")
	mcpOccID := stringField(mcpOccs[0].(map[string]any), "id")
	if cliOccID == "" || httpOccID == "" || mcpOccID == "" {
		t.Fatalf("occurrences list: got empty occurrence id(s): cli=%q http=%q mcp=%q", cliOccID, httpOccID, mcpOccID)
	}

	// ---- skip (on the second occurrence, so materialise below still has one to act on) ----
	cliSecondID := stringField(cliOccs[1].(map[string]any), "id")
	httpSecondID := stringField(httpOccs[1].(map[string]any), "id")
	mcpSecondID := stringField(mcpOccs[1].(map[string]any), "id")

	cliOut, cliErr = h.runCLI("recurring", "skip", cliSecondID)
	if cliErr != nil {
		t.Fatalf("CLI recurring skip: unexpected error: %v (output: %s)", cliErr, cliOut)
	}
	var cliSkipped scheduledOccurrenceConformanceView
	reencode(t, decodeEnvelope(t, cliOut), &cliSkipped)

	httpStatus, httpDecoded = h.runHTTP("POST", "/api/v1/recurring-occurrences/"+httpSecondID+"/skip", nil)
	if httpStatus != http.StatusOK {
		t.Fatalf("POST .../skip: status = %d, want 200: %+v", httpStatus, httpDecoded)
	}
	var httpSkipped scheduledOccurrenceConformanceView
	reencode(t, h.httpData(httpDecoded), &httpSkipped)

	mcpResult = h.runMCP(ctx, "skip_occurrence", map[string]any{"occurrence_id": mcpSecondID})
	if mcpResult.IsError {
		t.Fatalf("MCP skip_occurrence: unexpected error result: %+v", mcpResult)
	}
	var mcpSkipped scheduledOccurrenceConformanceView
	reencode(t, h.mcpData(mcpResult), &mcpSkipped)

	assertJSONEqual(t, "skip (CLI vs HTTP)", cliSkipped, httpSkipped)
	assertJSONEqual(t, "skip (CLI vs MCP)", cliSkipped, mcpSkipped)
	if cliSkipped.Status != "skipped" {
		t.Fatalf("skip: status = %q, want skipped", cliSkipped.Status)
	}

	// ---- materialise (on the first occurrence) ----
	cliOut, cliErr = h.runCLI("recurring", "materialise", cliOccID)
	if cliErr != nil {
		t.Fatalf("CLI recurring materialise: unexpected error: %v (output: %s)", cliErr, cliOut)
	}
	cliMaterialised := decodeEnvelope(t, cliOut)
	var cliMatOcc scheduledOccurrenceConformanceView
	reencode(t, cliMaterialised["occurrence"], &cliMatOcc)

	httpStatus, httpDecoded = h.runHTTP("POST", "/api/v1/recurring-occurrences/"+httpOccID+"/materialise", nil)
	if httpStatus != http.StatusOK {
		t.Fatalf("POST .../materialise: status = %d, want 200: %+v", httpStatus, httpDecoded)
	}
	httpMaterialised := h.httpData(httpDecoded)
	var httpMatOcc scheduledOccurrenceConformanceView
	reencode(t, httpMaterialised["occurrence"], &httpMatOcc)

	mcpResult = h.runMCP(ctx, "materialise_occurrence", map[string]any{"occurrence_id": mcpOccID})
	if mcpResult.IsError {
		t.Fatalf("MCP materialise_occurrence: unexpected error result: %+v", mcpResult)
	}
	mcpMaterialised := h.mcpData(mcpResult)
	var mcpMatOcc scheduledOccurrenceConformanceView
	reencode(t, mcpMaterialised["occurrence"], &mcpMatOcc)

	assertJSONEqual(t, "materialise occurrence (CLI vs HTTP)", cliMatOcc, httpMatOcc)
	assertJSONEqual(t, "materialise occurrence (CLI vs MCP)", cliMatOcc, mcpMatOcc)
	if cliMatOcc.Status != "materialised" {
		t.Fatalf("materialise: status = %q, want materialised", cliMatOcc.Status)
	}

	// The three surfaces' resulting transactions must also agree on the
	// comparable fields cases_test.go's own record-verb table already
	// checks (amount, currency, account, category, date, description) —
	// each surface's transaction is its own rule's own materialisation,
	// so this compares shape, not a shared ID.
	cliTxn := comparableFields(cliMaterialised["transaction"].(map[string]any))
	httpTxn := comparableFields(httpMaterialised["transaction"].(map[string]any))
	mcpTxn := comparableFields(mcpMaterialised["transaction"].(map[string]any))
	assertJSONEqual(t, "materialise transaction (CLI vs HTTP)", cliTxn, httpTxn)
	assertJSONEqual(t, "materialise transaction (CLI vs MCP)", cliTxn, mcpTxn)
}

// forecastConformanceView is app.ForecastResult's comparable shape:
// currency and points only, since each surface's own rule (and therefore
// its own account/category IDs) differ — this compares the projected
// totals' shape, not a shared identity.
type forecastConformanceView struct {
	Currency string `json:"currency"`
	Points   []struct {
		From             string `json:"from"`
		To               string `json:"to"`
		ProjectedInflow  string `json:"projected_inflow"`
		ProjectedOutflow string `json:"projected_outflow"`
		ProjectedNet     string `json:"projected_net"`
	} `json:"points"`
}

// TestForecastSurfacesConformance seeds one rule per surface with an
// identical schedule/amount/starts_on, generates its occurrences directly
// via the application layer, then drives `bodger recurring forecast`,
// GET /api/v1/forecast, and get_forecast over the same date range and
// asserts the three results agree field-for-field.
func TestForecastSurfacesConformance(t *testing.T) {
	h := newHarness(t)
	h.seed()
	ctx := context.Background()

	cliRuleID, _ := cliRecurringRuleView(t, h, append([]string{
		"recurring", "create", "HDFC Savings", "groceries", "500", "CLI rule", "--starts-on", "2026-08-01",
	}, cliScheduleFlags()...)...)
	httpRuleID, _ := httpRecurringRuleView(t, h, "POST", "/api/v1/recurring-rules", map[string]any{
		"account_ref": "HDFC Savings", "category_ref": "groceries", "amount": "500",
		"description": "HTTP rule", "schedule": scheduleArgs, "starts_on": "2026-08-01",
	})
	mcpRuleID, _ := mcpRecurringRuleView(t, h, "create_recurring_rule", map[string]any{
		"account_ref": "HDFC Savings", "category_ref": "groceries", "amount": "500",
		"description": "MCP rule", "schedule": scheduleArgs, "starts_on": "2026-08-01",
	})
	for _, ruleID := range []string{cliRuleID, httpRuleID, mcpRuleID} {
		if _, err := h.svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: seededUserID, RuleID: ruleID}); err != nil {
			t.Fatalf("GenerateOccurrences(%s): %v", ruleID, err)
		}
	}

	cliOut, cliErr := h.runCLI("recurring", "forecast", "--from", "2026-08-01", "--to", "2026-10-31", "--currency", "INR", "--policy", "current")
	if cliErr != nil {
		t.Fatalf("CLI recurring forecast: unexpected error: %v (output: %s)", cliErr, cliOut)
	}
	var cliForecast forecastConformanceView
	reencode(t, decodeEnvelope(t, cliOut), &cliForecast)

	httpStatus, httpDecoded := h.runHTTP("GET", "/api/v1/forecast?from=2026-08-01&to=2026-10-31&currency=INR&policy=current", nil)
	if httpStatus != http.StatusOK {
		t.Fatalf("GET /api/v1/forecast: status = %d, want 200: %+v", httpStatus, httpDecoded)
	}
	var httpForecast forecastConformanceView
	reencode(t, h.httpData(httpDecoded), &httpForecast)

	mcpResult := h.runMCP(ctx, "get_forecast", map[string]any{
		"from": "2026-08-01", "to": "2026-10-31", "currency": "INR", "policy": "current",
	})
	if mcpResult.IsError {
		t.Fatalf("MCP get_forecast: unexpected error result: %+v", mcpResult)
	}
	var mcpForecast forecastConformanceView
	reencode(t, h.mcpData(mcpResult), &mcpForecast)

	assertJSONEqual(t, "forecast (CLI vs HTTP)", cliForecast, httpForecast)
	assertJSONEqual(t, "forecast (CLI vs MCP)", cliForecast, mcpForecast)
	if len(cliForecast.Points) != 3 {
		t.Fatalf("forecast: got %d points, want 3 (Aug/Sep/Oct)", len(cliForecast.Points))
	}
	// Forecast has no per-rule filter — it aggregates every pending
	// occurrence the actor owns (app.ForecastQuery's own doc comment), so
	// this test's three same-shaped, same-schedule rules (one per surface,
	// all sharing this harness's one actor) sum to 3 * 500.
	if cliForecast.Points[0].ProjectedOutflow != "1500.00" {
		t.Fatalf("forecast: August projected_outflow = %q, want 1500.00 (3 rules x 500)", cliForecast.Points[0].ProjectedOutflow)
	}
}

// TestRecurringSurfacesConformance_Errors covers the invalid-input paths
// all three surfaces must reject identically: an unknown rule ID, and an
// unsupported schedule frequency.
func TestRecurringSurfacesConformance_Errors(t *testing.T) {
	h := newHarness(t)
	h.seed()

	t.Run("unknown rule id", func(t *testing.T) {
		cliOut, cliErr := h.runCLI("recurring", "update", "does-not-exist", "500", "x", "--frequency", "monthly", "--interval", "1", "--day-of-month", "1")
		if cliErr == nil {
			t.Fatalf("CLI: expected an error, got success (output: %s)", cliOut)
		}
		if code := h.cliErrorCode(cliErr); code != errs.NotFound {
			t.Errorf("CLI error code = %s, want %s", code, errs.NotFound)
		}

		httpStatus, httpDecoded := h.runHTTP("PATCH", "/api/v1/recurring-rules/does-not-exist", map[string]any{
			"amount": "500", "description": "x", "schedule": scheduleArgs,
		})
		if httpStatus != http.StatusNotFound {
			t.Fatalf("HTTP: status = %d, want 404: %+v", httpStatus, httpDecoded)
		}
		if code := h.httpErrorCode(httpDecoded); code != errs.NotFound {
			t.Errorf("HTTP error code = %s, want %s", code, errs.NotFound)
		}

		mcpResult := h.runMCP(context.Background(), "update_recurring_rule", map[string]any{
			"rule_id": "does-not-exist", "amount": "500", "description": "x", "schedule": scheduleArgs,
		})
		if !mcpResult.IsError {
			t.Fatalf("MCP: expected an error result, got success: %+v", mcpResult)
		}
		if code := h.mcpErrorCode(mcpResult); code != errs.NotFound {
			t.Errorf("MCP error code = %s, want %s", code, errs.NotFound)
		}
	})

	t.Run("unsupported frequency", func(t *testing.T) {
		cliOut, cliErr := h.runCLI("recurring", "create", "HDFC Savings", "groceries", "500", "x", "--frequency", "daily", "--interval", "1")
		if cliErr == nil {
			t.Fatalf("CLI: expected an error, got success (output: %s)", cliOut)
		}
		if code := h.cliErrorCode(cliErr); code != errs.InvalidInput {
			t.Errorf("CLI error code = %s, want %s", code, errs.InvalidInput)
		}

		httpStatus, httpDecoded := h.runHTTP("POST", "/api/v1/recurring-rules", map[string]any{
			"account_ref": "HDFC Savings", "category_ref": "groceries", "amount": "500", "description": "x",
			"schedule": map[string]any{"frequency": "daily", "interval": 1},
		})
		if httpStatus != http.StatusUnprocessableEntity {
			t.Fatalf("HTTP: status = %d, want 422: %+v", httpStatus, httpDecoded)
		}
		if code := h.httpErrorCode(httpDecoded); code != errs.InvalidInput {
			t.Errorf("HTTP error code = %s, want %s", code, errs.InvalidInput)
		}

		mcpResult := h.runMCP(context.Background(), "create_recurring_rule", map[string]any{
			"account_ref": "HDFC Savings", "category_ref": "groceries", "amount": "500", "description": "x",
			"schedule": map[string]any{"frequency": "daily", "interval": 1},
		})
		if !mcpResult.IsError {
			t.Fatalf("MCP: expected an error result, got success: %+v", mcpResult)
		}
		if code := h.mcpErrorCode(mcpResult); code != errs.InvalidInput {
			t.Errorf("MCP error code = %s, want %s", code, errs.InvalidInput)
		}

		cliField := h.cliErrorField(cliErr)
		httpField := h.httpErrorField(httpDecoded)
		if cliField != httpField {
			t.Errorf("CLI and HTTP disagree on which field this error attaches to: CLI %q, HTTP %q", cliField, httpField)
		}
	})
}
