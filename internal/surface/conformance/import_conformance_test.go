// This file extends this package's conformance coverage to issue #212's
// import surface: upload, list/show, list records, resolve, commit, and
// rollback, driven through `bodger import ...` and
// /api/v1/imports*/api/v1/import-records* against the same shared
// application-layer Service. Unlike cases_test.go's single table (built
// around one shape: an amount/date/account/category), an import
// operation's inputs vary too much between steps (raw file bytes for
// upload, a batch ID for commit, a resolution enum for resolve) to fit
// one row shape, so this follows balance_conformance_test.go and
// export_conformance_test.go's own precedent instead: a handful of
// focused test functions, each still driving both surfaces' real entry
// points against the same harness and asserting the observable result
// is identical.
package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/anirudhgray/bodger/internal/app"
)

// runHTTPUpload sends a raw-bytes POST to path (its query string already
// encoded in) and returns the decoded {"data": ...} envelope — the
// upload route's own request shape (imports.go's own doc comment: the
// body is the file's raw bytes, not JSON), so runHTTP's own
// json.Marshal-a-body convention doesn't apply here.
func (h *harness) runHTTPUpload(path, contentType string, data []byte) (status int, decoded map[string]any) {
	h.t.Helper()

	req, err := http.NewRequest(http.MethodPost, h.srv.URL+path, strings.NewReader(string(data)))
	if err != nil {
		h.t.Fatalf("http.NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+h.token)

	resp, err := h.srv.Client().Do(req)
	if err != nil {
		h.t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("POST %s: read response body: %v", path, err)
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		h.t.Fatalf("POST %s: decode response body: %v", path, err)
	}
	return resp.StatusCode, decoded
}

// importUploadQuery builds POST /api/v1/imports' query string for a
// basic three-column CSV mapping against accountRef.
func importUploadQuery(accountRef, filename string) string {
	q := url.Values{
		"account": {accountRef}, "filename": {filename},
		"date_column": {"Date"}, "description_column": {"Description"}, "amount_column": {"Amount"},
	}
	return q.Encode()
}

// TestImportUploadConformance stages the same CSV through both surfaces'
// real upload entry points and asserts they produce the same staged
// batch shape: same source format, same status, same record count, same
// per-record amounts and statuses. Each surface uploads into its own
// account (staging is a write, so the two calls can't share one batch),
// but both accounts and both CSVs are identical, so the results must be
// too.
func TestImportUploadConformance(t *testing.T) {
	h := newHarness(t)
	h.seed()

	csv := "Date,Description,Amount\n" +
		"2026-08-01,Coffee Shop,-4.50\n" +
		"2026-08-02,Paycheck,1500.00\n"

	cliOut, cliErr := h.runCLI("import", "upload", writeCSVFixture(t, csv),
		"--account", "Cash", "--filename", "statement.csv",
		"--date-column", "Date", "--description-column", "Description", "--amount-column", "Amount")
	if cliErr != nil {
		t.Fatalf("CLI: unexpected error: %v (output: %s)", cliErr, cliOut)
	}
	var cliResult struct {
		Data struct {
			Batch struct {
				SourceFormat string `json:"source_format"`
				Status       string `json:"status"`
			} `json:"batch"`
			Records []struct {
				Amount string `json:"amount"`
				Status string `json:"status"`
			} `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(cliOut), &cliResult); err != nil {
		t.Fatalf("decode CLI output: %v (output: %s)", err, cliOut)
	}

	httpStatus, httpDecoded := h.runHTTPUpload("/api/v1/imports?"+importUploadQuery("Wallet", "statement.csv"), "text/csv", []byte(csv))
	if httpStatus != http.StatusCreated {
		t.Fatalf("HTTP: unexpected status %d (body: %+v)", httpStatus, httpDecoded)
	}
	httpData := h.httpData(httpDecoded)
	httpBatch, ok := httpData["batch"].(map[string]any)
	if !ok {
		t.Fatalf("HTTP response has no \"batch\" object: %+v", httpData)
	}
	httpRecords, ok := httpData["records"].([]any)
	if !ok {
		t.Fatalf("HTTP response has no \"records\" array: %+v", httpData)
	}

	if cliResult.Data.Batch.SourceFormat != httpBatch["source_format"] {
		t.Errorf("source_format: CLI = %q, HTTP = %v", cliResult.Data.Batch.SourceFormat, httpBatch["source_format"])
	}
	if cliResult.Data.Batch.Status != httpBatch["status"] {
		t.Errorf("batch status: CLI = %q, HTTP = %v", cliResult.Data.Batch.Status, httpBatch["status"])
	}
	if len(cliResult.Data.Records) != len(httpRecords) {
		t.Fatalf("record count: CLI = %d, HTTP = %d", len(cliResult.Data.Records), len(httpRecords))
	}
	for i, cliRec := range cliResult.Data.Records {
		httpRec, ok := httpRecords[i].(map[string]any)
		if !ok {
			t.Fatalf("HTTP record %d isn't an object: %+v", i, httpRecords[i])
		}
		if cliRec.Amount != httpRec["amount"] {
			t.Errorf("record %d amount: CLI = %q, HTTP = %v", i, cliRec.Amount, httpRec["amount"])
		}
		if cliRec.Status != httpRec["status"] {
			t.Errorf("record %d status: CLI = %q, HTTP = %v", i, cliRec.Status, httpRec["status"])
		}
	}

	// Not a vacuous pass: every record must actually have cleared for
	// commit, on both surfaces, or the amount/status comparison above
	// would trivially "agree" on an all-empty result.
	if len(cliResult.Data.Records) == 0 {
		t.Fatal("CLI staged zero records — the comparison above would pass vacuously")
	}
	for i, rec := range cliResult.Data.Records {
		if rec.Status != "ready" {
			t.Errorf("CLI record %d status = %q, want ready (a clean row with no existing transaction to collide with)", i, rec.Status)
		}
	}
}

// TestImportReviewCommitRollbackConformance drives a full
// upload -> review -> resolve -> commit -> rollback cycle through each
// surface independently (against its own account and its own seeded
// collision, since every step after upload writes state that can't be
// shared between the two runs) and asserts each step's observable result
// agrees: same statuses, same transition refusals, same final
// transaction/record counts.
func TestImportReviewCommitRollbackConformance(t *testing.T) {
	for _, surface := range []string{"cli", "http"} {
		t.Run(surface, func(t *testing.T) {
			h := newHarness(t)
			h.seed()

			// Both surfaces resolve an account by name (ADR-0005's
			// normalize.Ref), so accountRef is used as-is by either run —
			// a separate account per surface since staging and committing
			// are writes neither run can share with the other.
			accountRef := map[string]string{"cli": "Cash", "http": "Wallet"}[surface]
			h.seedImportCollision(surface, accountRef)

			csv := "Date,Description,Amount\n" +
				"2026-08-03,Starbucks Coffee,-4.50\n" +
				"2026-08-05,Paycheck,1500.00\n"

			var batchID string
			var pendingRecordID string
			var recordCount int

			switch surface {
			case "cli":
				out, err := h.runCLI("import", "upload", writeCSVFixture(t, csv),
					"--account", accountRef, "--filename", "statement.csv",
					"--date-column", "Date", "--description-column", "Description", "--amount-column", "Amount")
				if err != nil {
					t.Fatalf("upload: %v (output: %s)", err, out)
				}
				var result struct {
					Data struct {
						Batch struct {
							ID string `json:"id"`
						} `json:"batch"`
						Records []struct {
							ID             string `json:"id"`
							Status         string `json:"status"`
							DuplicateMatch *struct {
								Tier string `json:"tier"`
							} `json:"duplicate_match"`
						} `json:"records"`
					} `json:"data"`
				}
				if err := json.Unmarshal([]byte(out), &result); err != nil {
					t.Fatalf("decode upload output: %v (output: %s)", err, out)
				}
				batchID = result.Data.Batch.ID
				recordCount = len(result.Data.Records)
				for _, rec := range result.Data.Records {
					if rec.Status == "pending" {
						pendingRecordID = rec.ID
						if rec.DuplicateMatch == nil || rec.DuplicateMatch.Tier != "suspected_duplicate" {
							t.Fatalf("pending record's duplicate match = %+v, want tier suspected_duplicate", rec.DuplicateMatch)
						}
					}
				}
			case "http":
				status, decoded := h.runHTTPUpload("/api/v1/imports?"+importUploadQuery(accountRef, "statement.csv"), "text/csv", []byte(csv))
				if status != http.StatusCreated {
					t.Fatalf("upload status = %d (body: %+v)", status, decoded)
				}
				data := h.httpData(decoded)
				batchID = data["batch"].(map[string]any)["id"].(string)
				records := data["records"].([]any)
				recordCount = len(records)
				for _, raw := range records {
					rec := raw.(map[string]any)
					if rec["status"] == "pending" {
						pendingRecordID = rec["id"].(string)
						dm, ok := rec["duplicate_match"].(map[string]any)
						if !ok || dm["tier"] != "suspected_duplicate" {
							t.Fatalf("pending record's duplicate match = %+v, want tier suspected_duplicate", rec["duplicate_match"])
						}
					}
				}
			}

			if batchID == "" {
				t.Fatal("no batch ID captured")
			}
			if pendingRecordID == "" {
				t.Fatalf("no pending record found among %d staged records", recordCount)
			}
			if recordCount != 2 {
				t.Fatalf("recordCount = %d, want 2", recordCount)
			}

			// Committing before resolving the suspected duplicate must be
			// refused, on both surfaces, with the same error code.
			switch surface {
			case "cli":
				_, err := h.runCLI("import", "commit", batchID)
				if err == nil {
					t.Fatal("premature commit succeeded, want an error")
				}
				if code := h.cliErrorCode(err); code != "precondition_failed" {
					t.Errorf("premature commit error code = %s, want precondition_failed", code)
				}
			case "http":
				status, decoded := h.runHTTP(http.MethodPost, "/api/v1/imports/"+batchID+"/commit", nil)
				if status != http.StatusPreconditionFailed {
					t.Fatalf("premature commit status = %d, want 412: %+v", status, decoded)
				}
				if code := h.httpErrorCode(decoded); code != "precondition_failed" {
					t.Errorf("premature commit error code = %s, want precondition_failed", code)
				}
			}

			// Resolve the suspected duplicate as not a duplicate, clearing
			// it for commit.
			switch surface {
			case "cli":
				out, err := h.runCLI("import", "resolve", pendingRecordID, "--resolution", "not_duplicate")
				if err != nil {
					t.Fatalf("resolve: %v (output: %s)", err, out)
				}
			case "http":
				status, decoded := h.runHTTP(http.MethodPost, "/api/v1/import-records/"+pendingRecordID+"/resolve", map[string]any{"resolution": "not_duplicate"})
				if status != http.StatusOK {
					t.Fatalf("resolve status = %d, want 200: %+v", status, decoded)
				}
			}

			// Commit, and check the resulting batch status and transaction
			// count agree.
			var commitStatus string
			var committedTxnCount int
			switch surface {
			case "cli":
				out, err := h.runCLI("import", "commit", batchID)
				if err != nil {
					t.Fatalf("commit: %v (output: %s)", err, out)
				}
				var result struct {
					Data struct {
						Batch struct {
							Status string `json:"status"`
						} `json:"batch"`
						Transactions []struct {
							ID string `json:"id"`
						} `json:"transactions"`
					} `json:"data"`
				}
				if err := json.Unmarshal([]byte(out), &result); err != nil {
					t.Fatalf("decode commit output: %v (output: %s)", err, out)
				}
				commitStatus = result.Data.Batch.Status
				committedTxnCount = len(result.Data.Transactions)
			case "http":
				status, decoded := h.runHTTP(http.MethodPost, "/api/v1/imports/"+batchID+"/commit", nil)
				if status != http.StatusOK {
					t.Fatalf("commit status = %d, want 200: %+v", status, decoded)
				}
				data := h.httpData(decoded)
				commitStatus = data["batch"].(map[string]any)["status"].(string)
				committedTxnCount = len(data["transactions"].([]any))
			}
			if commitStatus != "committed" {
				t.Errorf("commit status = %q, want committed", commitStatus)
			}
			if committedTxnCount != 2 {
				t.Errorf("committed transaction count = %d, want 2", committedTxnCount)
			}

			// Roll back, and check the resulting batch status and deleted
			// transaction count agree.
			var rollbackStatus string
			var rolledBackTxnCount int
			switch surface {
			case "cli":
				out, err := h.runCLI("import", "rollback", batchID)
				if err != nil {
					t.Fatalf("rollback: %v (output: %s)", err, out)
				}
				var result struct {
					Data struct {
						Batch struct {
							Status string `json:"status"`
						} `json:"batch"`
						TransactionIDs []string `json:"transaction_ids"`
					} `json:"data"`
				}
				if err := json.Unmarshal([]byte(out), &result); err != nil {
					t.Fatalf("decode rollback output: %v (output: %s)", err, out)
				}
				rollbackStatus = result.Data.Batch.Status
				rolledBackTxnCount = len(result.Data.TransactionIDs)
			case "http":
				status, decoded := h.runHTTP(http.MethodPost, "/api/v1/imports/"+batchID+"/rollback", nil)
				if status != http.StatusOK {
					t.Fatalf("rollback status = %d, want 200: %+v", status, decoded)
				}
				data := h.httpData(decoded)
				rollbackStatus = data["batch"].(map[string]any)["status"].(string)
				rolledBackTxnCount = len(data["transaction_ids"].([]any))
			}
			if rollbackStatus != "rolled_back" {
				t.Errorf("rollback status = %q, want rolled_back", rollbackStatus)
			}
			if rolledBackTxnCount != 2 {
				t.Errorf("rolled-back transaction count = %d, want 2", rolledBackTxnCount)
			}

			// A second rollback of the same batch must be refused, on both
			// surfaces, with the same error code.
			switch surface {
			case "cli":
				_, err := h.runCLI("import", "rollback", batchID)
				if err == nil {
					t.Fatal("second rollback succeeded, want an error")
				}
				if code := h.cliErrorCode(err); code != "precondition_failed" {
					t.Errorf("second rollback error code = %s, want precondition_failed", code)
				}
			case "http":
				status, decoded := h.runHTTP(http.MethodPost, "/api/v1/imports/"+batchID+"/rollback", nil)
				if status != http.StatusPreconditionFailed {
					t.Fatalf("second rollback status = %d, want 412: %+v", status, decoded)
				}
				if code := h.httpErrorCode(decoded); code != "precondition_failed" {
					t.Errorf("second rollback error code = %s, want precondition_failed", code)
				}
			}
		})
	}
}

// seedImportCollision records, directly through the application layer
// (the same fixture-setup judgment call harness.seed makes), an existing
// transaction on accountRef that TestImportReviewCommitRollbackConformance's
// own CSV row plausibly matches under ADR-0008's tier-2 heuristic — same
// account/amount/currency, booked_date within +/-3 days, similar
// description. Named per-surface only so each subtest's log output is
// traceable to which run produced it; the seeded transaction itself is
// identical either way.
func (h *harness) seedImportCollision(surface, accountRef string) {
	h.t.Helper()
	if _, err := h.svc.RecordOutflow(context.Background(), app.RecordOutflowCommand{
		ActorID: seededUserID, AccountRef: accountRef, Amount: "4.50", Date: "2026-08-01",
		Description: "STARBUCKS COFFEE 4521", Notes: fmt.Sprintf("seeded for %s", surface),
	}); err != nil {
		h.t.Fatalf("seedImportCollision: %v", err)
	}
}

// writeCSVFixture writes contents to a temp file and returns its path —
// `bodger import upload` reads a real file from disk, so a CLI-surface
// case needs one to point it at.
func writeCSVFixture(t *testing.T, contents string) string {
	t.Helper()
	path := t.TempDir() + "/statement.csv"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write CSV fixture: %v", err)
	}
	return path
}
