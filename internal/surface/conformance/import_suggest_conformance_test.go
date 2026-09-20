// This file extends this package's conformance coverage to issue #306's
// suggestion surface: `bodger import suggest`, GET
// /api/v1/imports/{id}/suggestions, and the get_import_suggestions MCP
// tool — all three wired onto the same read-only
// Service.SuggestForImportBatch (internal/app/import_suggest.go, issue
// #305, ADR-0015). Unlike cases_test.go's record-verb table, this method
// writes nothing at all, so every case here reads the *same* staged batch
// through all three surfaces rather than staging separately per surface —
// there's no write to isolate between them.
package conformance

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/app/importparse"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// fakeSuggestionProvider is an in-memory ports.SuggestionProvider for this
// package's own tests — the conformance-package sibling of
// internal/app/memrepo_test.go's own fake of the same name (that one
// lives in package app's test-only file and isn't importable from here).
// Mirrors its documented contract exactly, per
// ports.SuggestionProvider's own doc comment: an answer set per RecordID,
// and a per-RecordID failure switch Suggest silently omits from its
// result — an error is returned only when every row passed to one call
// failed (ADR-0015 "Partial success is a success").
type fakeSuggestionProvider struct {
	answers map[string]ports.RowSuggestion
	fail    map[string]bool
}

func newFakeSuggestionProvider() *fakeSuggestionProvider {
	return &fakeSuggestionProvider{answers: map[string]ports.RowSuggestion{}, fail: map[string]bool{}}
}

func (f *fakeSuggestionProvider) setAnswer(answer ports.RowSuggestion) {
	f.answers[answer.RecordID] = answer
}

func (f *fakeSuggestionProvider) setFails(recordID string) {
	f.fail[recordID] = true
}

func (f *fakeSuggestionProvider) Suggest(_ context.Context, rows []ports.SuggestionRow) ([]ports.RowSuggestion, ports.SuggestOutcome, error) {
	var out []ports.RowSuggestion
	failed := 0
	for _, row := range rows {
		if f.fail[row.RecordID] {
			failed++
			continue
		}
		if answer, ok := f.answers[row.RecordID]; ok {
			out = append(out, answer)
		}
	}

	var outcome ports.SuggestOutcome
	if failed > 0 {
		outcome = ports.SuggestOutcome{FailedRows: failed, FailureReason: "provider_unreachable"}
	}

	if len(rows) > 0 && failed == len(rows) {
		return out, outcome, errs.New(errs.Unavailable).Explain("fake suggestion provider: simulated failure").With("reason", "provider_unreachable")
	}
	return out, outcome, nil
}

var _ ports.SuggestionProvider = (*fakeSuggestionProvider)(nil)

// stageOneUncategorizedRecord stages, directly through the application
// layer (fixture setup, the same judgment call seedImportCollision and
// stageOccurrenceMatchViaSurface already make), a single-row CSV import
// with no category column — so the staged record has no resolved
// category and is eligible for a category suggestion (ADR-0015's own
// eligibility rule: "only rows with no resolved category are sent for
// categorisation"). Returns the batch ID and the one record's ID.
func stageOneUncategorizedRecord(t *testing.T, h *harness, accountRef string) (batchID, recordID string) {
	t.Helper()
	ctx := context.Background()

	csv := "Date,Description,Amount\n2026-08-10,Trader Joe's,-42.00\n"
	result, err := h.svc.StageImport(ctx, app.StageImportCommand{
		ActorID: seededUserID, AccountRef: accountRef, Filename: "statement.csv", FileContent: []byte(csv),
		ColumnMapping: importparse.ColumnMapping{DateColumn: "Date", DescriptionColumn: "Description", AmountColumn: "Amount"},
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("StageImport: len(Records) = %d, want 1", len(result.Records))
	}
	return result.Batch.ID(), result.Records[0].ID()
}

// groceryCategoryID looks up h's seeded "groceries" expense category's
// ID — the category a negative-amount (expense) staged row is eligible
// to be suggested, per SuggestForImportBatch's own CategoryKind filter.
func groceryCategoryID(t *testing.T, h *harness) string {
	t.Helper()
	categories, err := h.svc.Categories.List(context.Background(), seededUserID)
	if err != nil {
		t.Fatalf("Categories.List: %v", err)
	}
	for _, c := range categories {
		if c.Name() == "groceries" {
			return c.ID()
		}
	}
	t.Fatal(`seeded category "groceries" not found`)
	return ""
}

// runMCPSuggest calls get_import_suggestions for batchID and decodes its
// JSON result, failing the test outright if the call errors.
func runMCPSuggest(t *testing.T, h *harness, batchID string) map[string]any {
	t.Helper()
	result := h.runMCP(context.Background(), "get_import_suggestions", map[string]any{"import_id": batchID})
	if result.IsError {
		t.Fatalf("get_import_suggestions: unexpected error result: %+v", result)
	}
	return h.mcpData(result)
}

// TestImportSuggestionsConformance_Unconfigured drives the "no key set"
// state (ADR-0015: "Configured: false... not an error") through all
// three surfaces against the same staged batch, and asserts each reports
// it identically and without error — a fresh harness's Service.Suggestions
// is nil by construction (newHarness never sets it), so no fake provider
// is wired in here at all.
func TestImportSuggestionsConformance_Unconfigured(t *testing.T) {
	h := newHarness(t)
	h.seed()
	batchID, _ := stageOneUncategorizedRecord(t, h, "Cash")

	cliOut, cliErr := h.runCLI("import", "suggest", batchID)
	if cliErr != nil {
		t.Fatalf("CLI: unexpected error: %v (output: %s)", cliErr, cliOut)
	}
	cliData := decodeEnvelope(t, cliOut)
	if configured, _ := cliData["configured"].(bool); configured {
		t.Errorf("CLI: configured = true, want false")
	}

	httpStatus, httpDecoded := h.runHTTP(http.MethodGet, "/api/v1/imports/"+batchID+"/suggestions", nil)
	if httpStatus != http.StatusOK {
		t.Fatalf("HTTP: unexpected status %d: %+v", httpStatus, httpDecoded)
	}
	httpData := h.httpData(httpDecoded)
	if configured, _ := httpData["configured"].(bool); configured {
		t.Errorf("HTTP: configured = true, want false")
	}

	mcpData := runMCPSuggest(t, h, batchID)
	if configured, _ := mcpData["configured"].(bool); configured {
		t.Errorf("MCP: configured = true, want false")
	}

	// Every surface must agree, not just each individually report false.
	if cliData["configured"] != httpData["configured"] || httpData["configured"] != mcpData["configured"] {
		t.Errorf("surfaces disagree on configured: CLI=%v HTTP=%v MCP=%v", cliData["configured"], httpData["configured"], mcpData["configured"])
	}

	// The staged row must still be fully listable — an unconfigured
	// instance never makes the review screen unusable.
	out, err := h.runCLI("import", "records", batchID)
	if err != nil {
		t.Fatalf("import records after unconfigured suggest: %v (output: %s)", err, out)
	}
}

// TestImportSuggestionsConformance_Configured wires a fake
// ports.SuggestionProvider with a canned answer for the staged row and
// asserts all three surfaces render the same suggestion: same category
// ID, same confidence, and — the point of this feature — attributed to
// typesafe.ai by name on every surface (docs/ux-principles.md §6).
func TestImportSuggestionsConformance_Configured(t *testing.T) {
	h := newHarness(t)
	h.seed()
	batchID, recordID := stageOneUncategorizedRecord(t, h, "Cash")
	categoryID := groceryCategoryID(t, h)

	fake := newFakeSuggestionProvider()
	fake.setAnswer(ports.RowSuggestion{RecordID: recordID, CategoryID: categoryID, CategoryConfidence: 0.87})
	h.svc.Suggestions = fake

	cliOut, cliErr := h.runCLI("import", "suggest", batchID)
	if cliErr != nil {
		t.Fatalf("CLI: unexpected error: %v (output: %s)", cliErr, cliOut)
	}
	cliData := decodeEnvelope(t, cliOut)

	httpStatus, httpDecoded := h.runHTTP(http.MethodGet, "/api/v1/imports/"+batchID+"/suggestions", nil)
	if httpStatus != http.StatusOK {
		t.Fatalf("HTTP: unexpected status %d: %+v", httpStatus, httpDecoded)
	}
	httpData := h.httpData(httpDecoded)

	mcpData := runMCPSuggest(t, h, batchID)

	for name, data := range map[string]map[string]any{"CLI": cliData, "HTTP": httpData, "MCP": mcpData} {
		if configured, _ := data["configured"].(bool); !configured {
			t.Fatalf("%s: configured = false, want true", name)
		}
		suggestions, ok := data["suggestions"].([]any)
		if !ok || len(suggestions) != 1 {
			t.Fatalf("%s: suggestions = %v, want exactly 1", name, data["suggestions"])
		}
		row, ok := suggestions[0].(map[string]any)
		if !ok {
			t.Fatalf("%s: suggestion[0] isn't an object: %v", name, suggestions[0])
		}
		if row["record_id"] != recordID {
			t.Errorf("%s: record_id = %v, want %q", name, row["record_id"], recordID)
		}
		if row["source"] != "typesafe.ai" {
			t.Errorf("%s: source = %v, want \"typesafe.ai\" (docs/ux-principles.md §6 attribution)", name, row["source"])
		}
		cat, ok := row["category"].(map[string]any)
		if !ok {
			t.Fatalf("%s: category is missing or not an object: %v", name, row["category"])
		}
		if cat["category_id"] != categoryID {
			t.Errorf("%s: category_id = %v, want %q", name, cat["category_id"], categoryID)
		}
		if conf, _ := cat["confidence"].(float64); conf != 0.87 {
			t.Errorf("%s: confidence = %v, want 0.87", name, cat["confidence"])
		}
		if row["occurrence"] != nil {
			t.Errorf("%s: occurrence = %v, want nil (no occurrence candidate was ever offered for this row)", name, row["occurrence"])
		}
	}
}

// TestImportSuggestionsConformance_ProviderFailure wires a fake provider
// that fails every row and asserts all three surfaces still report a
// non-error, still-fully-reviewable result: Configured stays true (the
// call reached the provider, unlike the unconfigured case), RowsFailed
// reflects the failure, failure_reason reaches every surface identically
// (the gap #306 flagged and this change closes — an operator can now
// tell a rejected credential from throttling from an unreachable
// provider on every surface, not just an aggregate count), Suggestions is
// empty, and — the acceptance criterion this proves — the staged row is
// still fully listable afterward (ADR-0015 "a network error must never
// become an error page over data that is sitting in SQLite").
func TestImportSuggestionsConformance_ProviderFailure(t *testing.T) {
	h := newHarness(t)
	h.seed()
	batchID, recordID := stageOneUncategorizedRecord(t, h, "Cash")

	fake := newFakeSuggestionProvider()
	fake.setFails(recordID)
	h.svc.Suggestions = fake

	cliOut, cliErr := h.runCLI("import", "suggest", batchID)
	if cliErr != nil {
		t.Fatalf("CLI: unexpected error: %v (output: %s)", cliErr, cliOut)
	}
	cliData := decodeEnvelope(t, cliOut)

	httpStatus, httpDecoded := h.runHTTP(http.MethodGet, "/api/v1/imports/"+batchID+"/suggestions", nil)
	if httpStatus != http.StatusOK {
		t.Fatalf("HTTP: unexpected status %d: %+v", httpStatus, httpDecoded)
	}
	httpData := h.httpData(httpDecoded)

	mcpData := runMCPSuggest(t, h, batchID)

	for name, data := range map[string]map[string]any{"CLI": cliData, "HTTP": httpData, "MCP": mcpData} {
		if configured, _ := data["configured"].(bool); !configured {
			t.Fatalf("%s: configured = false, want true (the provider was reached, just failed)", name)
		}
		if suggestions, _ := data["suggestions"].([]any); len(suggestions) != 0 {
			t.Errorf("%s: suggestions = %v, want empty", name, suggestions)
		}
		rowsFailed, _ := data["rows_failed"].(float64)
		if rowsFailed != 1 {
			t.Errorf("%s: rows_failed = %v, want 1", name, data["rows_failed"])
		}
		if reason, _ := data["failure_reason"].(string); reason != "provider_unreachable" {
			t.Errorf("%s: failure_reason = %v, want %q", name, data["failure_reason"], "provider_unreachable")
		}
	}

	// The review must stay fully usable: the staged row is still listable
	// through the ordinary records read, unaffected by the suggestion
	// provider's failure.
	out, err := h.runCLI("import", "records", batchID)
	if err != nil {
		t.Fatalf("import records after provider failure: %v (output: %s)", err, out)
	}
	var recordsResult struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &recordsResult); err != nil {
		t.Fatalf("decode import records output: %v (output: %s)", err, out)
	}
	if len(recordsResult.Data) != 1 || recordsResult.Data[0].ID != recordID {
		t.Errorf("import records after provider failure = %+v, want the one staged record still listed", recordsResult.Data)
	}
}
