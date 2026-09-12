package cli_test

// Thin surface tests for issue #212's "import" command group: decoding
// flags into commands and encoding results back out, per
// docs/architecture.md §7. Behavioural agreement with the HTTP surface
// (does resolving a suspected duplicate actually clear it for commit, and
// so on) is internal/surface/conformance's job, not this file's.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

func writeTempCSV(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "statement.csv")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write temp CSV: %v", err)
	}
	return path
}

// TestImportLifecycle_UploadReviewCommit uploads a small CSV with a clean
// row and a suspected-duplicate row, lists it, reviews its records,
// resolves the suspected duplicate, and commits — the CLI's counterpart
// to http package's TestImportLifecycle_UploadReviewCommit.
func TestImportLifecycle_UploadReviewCommit(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "accounts", "add", "Checking", "--type", "bank", "--currency", "USD")

	// Seeded directly through the application layer, not `spend`: that
	// command always sets Description to the category name (entries.go's
	// own doc comment), and this fixture needs a specific description for
	// the CSV row below to plausibly match under ADR-0008's tier-2
	// heuristic — seeding fixture data this way is the same judgment call
	// internal/surface/conformance's harness makes for its own seed().
	svc, closeDB, err := factory(context.Background())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if _, err := svc.RecordOutflow(context.Background(), app.RecordOutflowCommand{
		ActorID: ports.SeededUserID, AccountRef: "Checking", Amount: "4.50", Date: "2026-08-01", Description: "STARBUCKS COFFEE 4521",
	}); err != nil {
		t.Fatalf("seed outflow: %v", err)
	}
	if err := closeDB(); err != nil {
		t.Fatalf("closeDB: %v", err)
	}

	csvPath := writeTempCSV(t, "Date,Description,Amount\n"+
		"2026-08-03,Starbucks Coffee,-4.50\n"+
		"2026-08-05,Paycheck,1500.00\n")

	stdout := mustRun(t, factory, "import", "upload", csvPath,
		"--account", "Checking",
		"--date-column", "Date", "--description-column", "Description", "--amount-column", "Amount",
		"--json")

	var uploaded struct {
		Batch struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"batch"`
		Records []struct {
			ID             string `json:"id"`
			Status         string `json:"status"`
			DuplicateMatch *struct {
				Tier string `json:"tier"`
			} `json:"duplicate_match"`
		} `json:"records"`
	}
	decodeData(t, stdout, &uploaded)

	if uploaded.Batch.Status != "staged" {
		t.Errorf("batch status = %q, want staged", uploaded.Batch.Status)
	}
	if len(uploaded.Records) != 2 {
		t.Fatalf("len(Records) = %d, want 2", len(uploaded.Records))
	}

	stdout = mustRun(t, factory, "import", "list", "--json")
	var batches []struct {
		ID string `json:"id"`
	}
	decodeData(t, stdout, &batches)
	if len(batches) != 1 || batches[0].ID != uploaded.Batch.ID {
		t.Fatalf("import list = %+v, want a single entry matching %q", batches, uploaded.Batch.ID)
	}

	stdout = mustRun(t, factory, "import", "show", uploaded.Batch.ID, "--json")
	var shown struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	decodeData(t, stdout, &shown)
	if shown.ID != uploaded.Batch.ID {
		t.Errorf("import show returned a different batch")
	}

	stdout = mustRun(t, factory, "import", "records", uploaded.Batch.ID, "--json")
	var records []struct {
		ID             string `json:"id"`
		Status         string `json:"status"`
		DuplicateMatch *struct {
			Tier       string `json:"tier"`
			Resolution string `json:"resolution"`
		} `json:"duplicate_match"`
	}
	decodeData(t, stdout, &records)
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}

	var pendingID string
	for _, r := range records {
		if r.Status == "pending" {
			pendingID = r.ID
			if r.DuplicateMatch == nil || r.DuplicateMatch.Tier != "suspected_duplicate" {
				t.Errorf("pending record's duplicate match = %+v, want tier suspected_duplicate", r.DuplicateMatch)
			}
		}
	}
	if pendingID == "" {
		t.Fatalf("no pending record found in %+v", records)
	}

	// Committing before resolving the suspected duplicate must be refused.
	if _, _, err := run(t, factory, "import", "commit", uploaded.Batch.ID); err == nil {
		t.Fatal("commit before resolving succeeded, want an error")
	} else {
		wantErrCode(t, err, "precondition_failed")
	}

	stdout = mustRun(t, factory, "import", "resolve", pendingID, "--resolution", "not_duplicate", "--json")
	var resolved struct {
		Status string `json:"status"`
	}
	decodeData(t, stdout, &resolved)
	if resolved.Status != "ready" {
		t.Errorf("resolved record status = %q, want ready", resolved.Status)
	}

	stdout = mustRun(t, factory, "import", "commit", uploaded.Batch.ID, "--json")
	var committed struct {
		Batch struct {
			Status string `json:"status"`
		} `json:"batch"`
		Transactions []struct {
			ID string `json:"id"`
		} `json:"transactions"`
	}
	decodeData(t, stdout, &committed)
	if committed.Batch.Status != "committed" {
		t.Errorf("committed batch status = %q, want committed", committed.Batch.Status)
	}
	if len(committed.Transactions) != 2 {
		t.Fatalf("len(Transactions) = %d, want 2", len(committed.Transactions))
	}
}

// TestImportRollback stages and commits a clean batch, then rolls it
// back, checking a second rollback is refused.
func TestImportRollback(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "accounts", "add", "Checking", "--type", "bank", "--currency", "USD")

	csvPath := writeTempCSV(t, "Date,Description,Amount\n2026-08-01,Coffee Shop,-4.50\n")
	stdout := mustRun(t, factory, "import", "upload", csvPath,
		"--account", "Checking",
		"--date-column", "Date", "--description-column", "Description", "--amount-column", "Amount",
		"--json")
	var uploaded struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
	}
	decodeData(t, stdout, &uploaded)

	mustRun(t, factory, "import", "commit", uploaded.Batch.ID)

	stdout = mustRun(t, factory, "import", "rollback", uploaded.Batch.ID, "--json")
	var rolledBack struct {
		Batch struct {
			Status string `json:"status"`
		} `json:"batch"`
		TransactionIDs []string `json:"transaction_ids"`
	}
	decodeData(t, stdout, &rolledBack)
	if rolledBack.Batch.Status != "rolled_back" {
		t.Errorf("rolled-back batch status = %q, want rolled_back", rolledBack.Batch.Status)
	}
	if len(rolledBack.TransactionIDs) != 1 {
		t.Fatalf("len(TransactionIDs) = %d, want 1", len(rolledBack.TransactionIDs))
	}

	if _, _, err := run(t, factory, "import", "rollback", uploaded.Batch.ID); err == nil {
		t.Fatal("second rollback succeeded, want an error")
	} else {
		wantErrCode(t, err, "precondition_failed")
	}
}
