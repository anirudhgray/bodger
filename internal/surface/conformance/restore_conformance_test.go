// This file extends this package's conformance coverage to issue #227:
// `bodger restore` and POST /api/v1/restore must both (a) refuse to run
// without an explicit confirmation, leaving existing data untouched, and
// (b) once confirmed, install identical data from the same canonical
// JSON document.
//
// Restore is the one write-mutating operation this package
// conformance-tests that wipes an actor's *entire* ledger rather than
// adding to it, so it can't reuse export_conformance_test.go's pattern
// of driving both surfaces against one shared harness: a second restore
// into that same database would just wipe what the first one just
// installed, proving nothing about whether the two surfaces agree.
// Instead, each surface gets its own freshly-seeded harness (newHarness
// already gives every test its own on-disk SQLite database — see its own
// doc comment), both are restored from the exact same document, and each
// one's resulting state is compared against the same expected values —
// derived from the source data the document was built from, not
// hardcoded here, so this test can't quietly drift out of sync with
// whatever seedExportFixtures actually seeds.
//
// Every surrogate ID (account, category, transaction, posting) is
// regenerated fresh on every restore (ADR-0008's round-trip guarantee
// explicitly allows this), so the two harnesses' resulting IDs are never
// comparable to each other — every comparison below is over names and
// transaction fields only, never IDs.
package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// seedRestoreStrayFixtures records data that never appears in
// buildRestoreDocument's own document — so a confirmed restore that
// actually wipes (rather than merges into) the actor's existing data
// makes it disappear, and a refused restore leaves it exactly as it was.
func (h *harness) seedRestoreStrayFixtures() {
	h.t.Helper()
	ctx := context.Background()
	if _, err := h.svc.CreateAccount(ctx, app.CreateAccountCommand{
		ActorID: seededUserID, Name: "Legacy Wallet", Kind: "cash", Currency: "USD",
	}); err != nil {
		h.t.Fatalf("seed stray account: %v", err)
	}
	if _, err := h.svc.CreateCategory(ctx, app.CreateCategoryCommand{
		ActorID: seededUserID, Name: "legacy-category", Kind: "expense",
	}); err != nil {
		h.t.Fatalf("seed stray category: %v", err)
	}
}

// decodeEnvelopeArray is decodeEnvelope's counterpart for a CLI --json
// command whose "data" is itself a bare array (`accounts list`,
// `categories list`) rather than an object (unlike `transactions list`,
// whose "data" is an object with its own "transactions" key) —
// decodeEnvelope's `Data map[string]any` can't decode a JSON array.
func decodeEnvelopeArray(t *testing.T, output string) []any {
	t.Helper()
	var envelope struct {
		Data []any `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode CLI --json output: %v (output: %s)", err, output)
	}
	return envelope.Data
}

// stringField reads key out of m as a string, or "" if it's absent or
// not a string.
func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// namesOf extracts and sorts each row's "name" field — accounts list and
// categories list, on both surfaces, name every entry the same way.
func namesOf(rows []any) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if m, ok := row.(map[string]any); ok {
			names = append(names, stringField(m, "name"))
		}
	}
	sort.Strings(names)
	return names
}

// canonicalTxnType normalises the CLI's own "type" vocabulary
// (spend/receive/move — docs/ux-principles.md §7's "one vocabulary, not
// two", internal/surface/cli/transactions.go's transactionTypeFor) onto
// the application layer's (outflow/inflow/transfer), which the HTTP
// surface's transactionView reports as-is. Comparing the two surfaces'
// listings by transaction kind needs one shared vocabulary; an
// unrecognized value (i.e. already one of the application layer's own,
// from the HTTP surface) passes through unchanged.
func canonicalTxnType(t string) string {
	switch t {
	case "spend":
		return "outflow"
	case "receive":
		return "inflow"
	case "move":
		return "transfer"
	default:
		return t
	}
}

// transactionSignature reduces one decoded transactionView (from either
// surface) to exactly the fields ADR-0008's round-trip guarantee
// promises a restore preserves: kind, date, description, and each leg's
// amount and currency — kind normalised through canonicalTxnType so the
// CLI's and HTTP's differing "type" vocabularies compare equal.
// Deliberately excludes id, account_id/category_id, and
// from/to_account_id — every one of those is a surrogate ID restore
// regenerates fresh on every load, and is therefore never comparable
// across two independent restores of the same document.
func transactionSignature(m map[string]any) string {
	kind := canonicalTxnType(stringField(m, "type"))
	base := fmt.Sprintf("%s|%s|%s", kind, stringField(m, "date"), stringField(m, "description"))
	if kind == "transfer" {
		return base + fmt.Sprintf("|%s|%s|%s|%s", stringField(m, "amount"), stringField(m, "currency"), stringField(m, "to_amount"), stringField(m, "to_currency"))
	}
	return base + fmt.Sprintf("|%s|%s", stringField(m, "amount"), stringField(m, "currency"))
}

func signaturesOf(rows []any) []string {
	sigs := make([]string, 0, len(rows))
	for _, row := range rows {
		if m, ok := row.(map[string]any); ok {
			sigs = append(sigs, transactionSignature(m))
		}
	}
	sort.Strings(sigs)
	return sigs
}

// cliAccountNames, cliCategoryNames, and cliTransactionSignatures read
// h's current state back through the real CLI, the same way every other
// case in this package only ever reaches state through a real surface
// entry point rather than the application layer directly (h.seed and
// h.seedRestoreStrayFixtures/seedExportFixtures are the deliberate
// exceptions, as fixture setup rather than part of what's under test).
func (h *harness) cliAccountNames(t *testing.T) []string {
	t.Helper()
	out, err := h.runCLI("accounts", "list")
	if err != nil {
		t.Fatalf("accounts list: unexpected error: %v (output: %s)", err, out)
	}
	return namesOf(decodeEnvelopeArray(t, out))
}

func (h *harness) cliCategoryNames(t *testing.T) []string {
	t.Helper()
	out, err := h.runCLI("categories", "list")
	if err != nil {
		t.Fatalf("categories list: unexpected error: %v (output: %s)", err, out)
	}
	return namesOf(decodeEnvelopeArray(t, out))
}

func (h *harness) cliTransactionSignatures(t *testing.T) []string {
	t.Helper()
	out, err := h.runCLI("transactions", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("transactions list: unexpected error: %v (output: %s)", err, out)
	}
	data := decodeEnvelope(t, out)
	rows, _ := data["transactions"].([]any)
	return signaturesOf(rows)
}

// httpAccountNames, httpCategoryNames, and httpTransactionSignatures are
// cliAccountNames/cliCategoryNames/cliTransactionSignatures' HTTP-surface
// counterparts.
func (h *harness) httpAccountNames(t *testing.T) []string {
	t.Helper()
	status, decoded := h.runHTTP("GET", "/api/v1/accounts", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/accounts: status = %d, want 200: %+v", status, decoded)
	}
	rows, _ := decoded["data"].([]any)
	return namesOf(rows)
}

func (h *harness) httpCategoryNames(t *testing.T) []string {
	t.Helper()
	status, decoded := h.runHTTP("GET", "/api/v1/categories", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/categories: status = %d, want 200: %+v", status, decoded)
	}
	rows, _ := decoded["data"].([]any)
	return namesOf(rows)
}

func (h *harness) httpTransactionSignatures(t *testing.T) []string {
	t.Helper()
	status, decoded := h.runHTTP("GET", "/api/v1/transactions?limit=200", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/transactions: status = %d, want 200: %+v", status, decoded)
	}
	page := h.httpData(decoded)
	rows, _ := page["data"].([]any)
	return signaturesOf(rows)
}

// buildRestoreDocument builds a canonical bodger.export/v1 document (raw
// bytes) from one independent, throwaway harness's real ExportJSON entry
// point, plus the expected account names, category names, and
// transaction signatures that document's own data resolves to — read
// back from that same source harness through the real CLI, rather than
// hardcoded here, so a future change to seed/seedExportFixtures can't
// silently desync this test's expectations from what it's actually
// restoring.
func buildRestoreDocument(t *testing.T) (document []byte, wantAccounts, wantCategories, wantTxns []string) {
	t.Helper()
	src := newHarness(t)
	src.seed()
	src.seedExportFixtures()

	data, err := src.svc.ExportJSON(context.Background(), app.ExportJSONQuery{ActorID: seededUserID})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	return data, src.cliAccountNames(t), src.cliCategoryNames(t), src.cliTransactionSignatures(t)
}

// TestRestoreSnapshotConformance drives `bodger restore` and POST
// /api/v1/restore, each against its own fresh harness seeded with stray
// data the document doesn't contain, and asserts:
//
//  1. Neither surface restores without an explicit confirmation
//     (--yes / "confirm": true) — the stray data survives the refusal
//     untouched on both.
//  2. Once confirmed, both surfaces report installing the same counts,
//     and both replace their stray data with exactly the document's
//     accounts, categories, and transactions — proving CLI and HTTP
//     agree, not just that each one happens to work in isolation.
func TestRestoreSnapshotConformance(t *testing.T) {
	document, wantAccounts, wantCategories, wantTxns := buildRestoreDocument(t)
	if len(wantAccounts) == 0 || len(wantCategories) == 0 || len(wantTxns) == 0 {
		t.Fatalf("fixture document is empty: accounts=%v categories=%v transactions=%v", wantAccounts, wantCategories, wantTxns)
	}

	t.Run("CLI", func(t *testing.T) {
		h := newHarness(t)
		h.seedRestoreStrayFixtures()

		backupPath := filepath.Join(t.TempDir(), "backup.json")
		if err := os.WriteFile(backupPath, document, 0o600); err != nil {
			t.Fatalf("write fixture backup: %v", err)
		}

		// Refused without --yes: an *errs.Error, and the stray data is
		// still exactly there.
		out, err := h.runCLI("restore", backupPath)
		if err == nil {
			t.Fatalf("restore without --yes: want an error, got success (output: %s)", out)
		}
		if code := h.cliErrorCode(err); code != errs.InvalidInput {
			t.Errorf("restore without --yes: error code = %s, want %s", code, errs.InvalidInput)
		}
		if got := h.cliAccountNames(t); len(got) != 1 || got[0] != "Legacy Wallet" {
			t.Fatalf("restore without --yes touched data: accounts = %v, want just [Legacy Wallet]", got)
		}

		// Confirmed with --yes: succeeds, reports the document's own
		// counts, and replaces the stray data with the document's.
		out, err = h.runCLI("restore", backupPath, "--yes")
		if err != nil {
			t.Fatalf("restore --yes: unexpected error: %v (output: %s)", err, out)
		}
		result := decodeEnvelope(t, out)
		if got := int(result["accounts"].(float64)); got != len(wantAccounts) {
			t.Errorf("CLI restore result accounts = %d, want %d", got, len(wantAccounts))
		}
		if got := int(result["categories"].(float64)); got != len(wantCategories) {
			t.Errorf("CLI restore result categories = %d, want %d", got, len(wantCategories))
		}
		if got := int(result["transactions"].(float64)); got != len(wantTxns) {
			t.Errorf("CLI restore result transactions = %d, want %d", got, len(wantTxns))
		}

		if got := h.cliAccountNames(t); !equalStrings(got, wantAccounts) {
			t.Errorf("CLI post-restore account names = %v, want %v", got, wantAccounts)
		}
		if got := h.cliCategoryNames(t); !equalStrings(got, wantCategories) {
			t.Errorf("CLI post-restore category names = %v, want %v", got, wantCategories)
		}
		if got := h.cliTransactionSignatures(t); !equalStrings(got, wantTxns) {
			t.Errorf("CLI post-restore transaction signatures = %v, want %v", got, wantTxns)
		}
	})

	t.Run("HTTP", func(t *testing.T) {
		h := newHarness(t)
		h.seedRestoreStrayFixtures()

		var raw json.RawMessage = document

		// Refused without "confirm": true: a 422 *errs.Error, and the
		// stray data is still exactly there.
		status, decoded := h.runHTTP("POST", "/api/v1/restore", map[string]any{"confirm": false, "document": raw})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("restore without confirm: status = %d, want %d: %+v", status, http.StatusUnprocessableEntity, decoded)
		}
		if code := h.httpErrorCode(decoded); code != errs.InvalidInput {
			t.Errorf("restore without confirm: error code = %s, want %s", code, errs.InvalidInput)
		}
		if got := h.httpAccountNames(t); len(got) != 1 || got[0] != "Legacy Wallet" {
			t.Fatalf("restore without confirm touched data: accounts = %v, want just [Legacy Wallet]", got)
		}

		// Confirmed with "confirm": true: succeeds, reports the
		// document's own counts, and replaces the stray data with the
		// document's.
		status, decoded = h.runHTTP("POST", "/api/v1/restore", map[string]any{"confirm": true, "document": raw})
		if status != http.StatusOK {
			t.Fatalf("restore with confirm: status = %d, want 200: %+v", status, decoded)
		}
		result := h.httpData(decoded)
		if got := int(result["accounts"].(float64)); got != len(wantAccounts) {
			t.Errorf("HTTP restore result accounts = %d, want %d", got, len(wantAccounts))
		}
		if got := int(result["categories"].(float64)); got != len(wantCategories) {
			t.Errorf("HTTP restore result categories = %d, want %d", got, len(wantCategories))
		}
		if got := int(result["transactions"].(float64)); got != len(wantTxns) {
			t.Errorf("HTTP restore result transactions = %d, want %d", got, len(wantTxns))
		}

		if got := h.httpAccountNames(t); !equalStrings(got, wantAccounts) {
			t.Errorf("HTTP post-restore account names = %v, want %v", got, wantAccounts)
		}
		if got := h.httpCategoryNames(t); !equalStrings(got, wantCategories) {
			t.Errorf("HTTP post-restore category names = %v, want %v", got, wantCategories)
		}
		if got := h.httpTransactionSignatures(t); !equalStrings(got, wantTxns) {
			t.Errorf("HTTP post-restore transaction signatures = %v, want %v", got, wantTxns)
		}
	})
}

// equalStrings reports whether got and want hold the same elements in
// the same order — both namesOf and signaturesOf already sort their
// output, so this is a plain positional comparison.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
