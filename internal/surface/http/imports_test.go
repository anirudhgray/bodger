package http_test

// Thin surface tests for issue #212's import routes: decoding/encoding
// only, per docs/architecture.md §7 — behavioural agreement between this
// surface and the CLI (does StageImport actually get called correctly,
// does a suspected duplicate actually get excluded on commit, and so on)
// is internal/surface/conformance's job, not this file's.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// doUpload sends a raw-bytes POST to path (with query already encoded
// into it) — the one route in this package whose request body isn't a
// JSON object do's own json.Marshal could produce (see
// internal/surface/http/imports.go's own doc comment on why an upload's
// body is raw file bytes, not a JSON-wrapped one).
func doUpload(t *testing.T, srv *testServer, path string, contentType string, data []byte) (int, map[string]any) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+srv.token)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("POST %s: read response body: %v", path, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		t.Fatalf("POST %s: decode response body: %v", path, err)
	}

	validateResponse(t, req, resp, respBody)
	return resp.StatusCode, decoded
}

// TestImportLifecycle_UploadReviewCommit uploads a small CSV with one
// clean row and one row that suspiciously matches an existing
// transaction, reviews it, resolves the suspected duplicate, and commits
// — exercising every route this issue adds except rollback (its own
// test below), and checking the response shape at each step rather than
// re-proving StageImport/CommitImportBatch's own behaviour.
func TestImportLifecycle_UploadReviewCommit(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{
		"name": "Checking", "type": "bank", "currency": "USD",
	})
	if status != http.StatusCreated {
		t.Fatalf("create account status = %d, want 201: %+v", status, decoded)
	}
	accountID, _ := dataOf(t, decoded)["id"].(string)

	// An existing transaction for the suspected-duplicate row to collide
	// with (ADR-0008's tier-2 heuristic: same account/amount/currency,
	// booked_date within +/-3 days, similar description).
	status, decoded = do(t, srv, http.MethodPost, "/api/v1/transactions", map[string]any{
		"type": "outflow", "account": accountID, "amount": "4.50", "date": "2026-08-01", "description": "STARBUCKS COFFEE 4521",
	})
	if status != http.StatusCreated {
		t.Fatalf("seed transaction status = %d, want 201: %+v", status, decoded)
	}

	csv := "Date,Description,Amount\n" +
		"2026-08-03,Starbucks Coffee,-4.50\n" +
		"2026-08-05,Paycheck,1500.00\n"
	q := url.Values{
		"account": {accountID}, "filename": {"statement.csv"},
		"date_column": {"Date"}, "description_column": {"Description"}, "amount_column": {"Amount"},
	}
	status, decoded = doUpload(t, srv, "/api/v1/imports?"+q.Encode(), "text/csv", []byte(csv))
	if status != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201: %+v", status, decoded)
	}
	uploadData := dataOf(t, decoded)
	batch, ok := uploadData["batch"].(map[string]any)
	if !ok {
		t.Fatalf("upload response has no \"batch\" object: %+v", uploadData)
	}
	batchID, _ := batch["id"].(string)
	if batchID == "" {
		t.Fatalf("uploaded batch has no id: %+v", batch)
	}
	if batch["status"] != "staged" {
		t.Errorf("batch status = %v, want staged", batch["status"])
	}
	records, ok := uploadData["records"].([]any)
	if !ok || len(records) != 2 {
		t.Fatalf("upload records = %+v, want a 2-element array", uploadData["records"])
	}

	status, decoded = do(t, srv, http.MethodGet, "/api/v1/imports", nil)
	if status != http.StatusOK {
		t.Fatalf("list imports status = %d, want 200: %+v", status, decoded)
	}
	list, ok := decoded["data"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("list imports data = %+v, want a single-element array", decoded["data"])
	}

	status, decoded = do(t, srv, http.MethodGet, "/api/v1/imports/"+batchID, nil)
	if status != http.StatusOK {
		t.Fatalf("get import status = %d, want 200: %+v", status, decoded)
	}
	if dataOf(t, decoded)["id"] != batchID {
		t.Errorf("get import returned a different batch")
	}

	status, decoded = do(t, srv, http.MethodGet, "/api/v1/imports/"+batchID+"/records", nil)
	if status != http.StatusOK {
		t.Fatalf("list records status = %d, want 200: %+v", status, decoded)
	}
	recordList, ok := decoded["data"].([]any)
	if !ok || len(recordList) != 2 {
		t.Fatalf("list records data = %+v, want a 2-element array", decoded["data"])
	}

	var pendingRecordID string
	for _, raw := range recordList {
		rec, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("record isn't an object: %+v", raw)
		}
		if rec["status"] == "pending" {
			id, _ := rec["id"].(string)
			pendingRecordID = id
			dm, ok := rec["duplicate_match"].(map[string]any)
			if !ok {
				t.Fatalf("pending record has no duplicate_match: %+v", rec)
			}
			if dm["tier"] != "suspected_duplicate" {
				t.Errorf("duplicate_match.tier = %v, want suspected_duplicate", dm["tier"])
			}
		}
	}
	if pendingRecordID == "" {
		t.Fatalf("no pending record found in %+v", recordList)
	}

	// Committing before resolving the suspected duplicate must be refused.
	status, decoded = do(t, srv, http.MethodPost, "/api/v1/imports/"+batchID+"/commit", nil)
	if status != http.StatusPreconditionFailed {
		t.Fatalf("premature commit status = %d, want 412: %+v", status, decoded)
	}

	status, decoded = do(t, srv, http.MethodPost, "/api/v1/import-records/"+pendingRecordID+"/resolve", map[string]any{
		"resolution": "not_duplicate",
	})
	if status != http.StatusOK {
		t.Fatalf("resolve status = %d, want 200: %+v", status, decoded)
	}
	if dataOf(t, decoded)["status"] != "ready" {
		t.Errorf("resolved record status = %v, want ready", dataOf(t, decoded)["status"])
	}

	// Resolving the same record again must be refused.
	status, decoded = do(t, srv, http.MethodPost, "/api/v1/import-records/"+pendingRecordID+"/resolve", map[string]any{
		"resolution": "confirmed_duplicate",
	})
	if status != http.StatusPreconditionFailed {
		t.Fatalf("re-resolve status = %d, want 412: %+v", status, decoded)
	}

	status, decoded = do(t, srv, http.MethodPost, "/api/v1/imports/"+batchID+"/commit", nil)
	if status != http.StatusOK {
		t.Fatalf("commit status = %d, want 200: %+v", status, decoded)
	}
	commitData := dataOf(t, decoded)
	if commitData["batch"].(map[string]any)["status"] != "committed" {
		t.Errorf("committed batch status = %v, want committed", commitData["batch"])
	}
	txns, ok := commitData["transactions"].([]any)
	if !ok || len(txns) != 2 {
		t.Fatalf("commit transactions = %+v, want a 2-element array", commitData["transactions"])
	}
}

// TestImportRollback stages and commits a clean batch, then rolls it
// back — checking the rollback route's own response shape.
func TestImportRollback(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{
		"name": "Checking", "type": "bank", "currency": "USD",
	})
	if status != http.StatusCreated {
		t.Fatalf("create account status = %d, want 201: %+v", status, decoded)
	}
	accountID, _ := dataOf(t, decoded)["id"].(string)

	csv := "Date,Description,Amount\n2026-08-01,Coffee Shop,-4.50\n"
	q := url.Values{
		"account": {accountID}, "filename": {"statement.csv"},
		"date_column": {"Date"}, "description_column": {"Description"}, "amount_column": {"Amount"},
	}
	status, decoded = doUpload(t, srv, "/api/v1/imports?"+q.Encode(), "text/csv", []byte(csv))
	if status != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201: %+v", status, decoded)
	}
	batchID, _ := dataOf(t, decoded)["batch"].(map[string]any)["id"].(string)

	status, decoded = do(t, srv, http.MethodPost, "/api/v1/imports/"+batchID+"/commit", nil)
	if status != http.StatusOK {
		t.Fatalf("commit status = %d, want 200: %+v", status, decoded)
	}

	// Rolling back a batch that isn't committed yet must be refused.
	status, decoded = do(t, srv, http.MethodPost, "/api/v1/imports/"+batchID+"/rollback", nil)
	if status != http.StatusOK {
		t.Fatalf("rollback status = %d, want 200: %+v", status, decoded)
	}
	rollbackData := dataOf(t, decoded)
	if rollbackData["batch"].(map[string]any)["status"] != "rolled_back" {
		t.Errorf("rolled-back batch status = %v, want rolled_back", rollbackData["batch"])
	}
	ids, ok := rollbackData["transaction_ids"].([]any)
	if !ok || len(ids) != 1 {
		t.Fatalf("transaction_ids = %+v, want a 1-element array", rollbackData["transaction_ids"])
	}

	// A second rollback of the same, already-rolled-back batch must be
	// refused.
	status, decoded = do(t, srv, http.MethodPost, "/api/v1/imports/"+batchID+"/rollback", nil)
	if status != http.StatusPreconditionFailed {
		t.Fatalf("re-rollback status = %d, want 412: %+v", status, decoded)
	}
}

func TestImportUpload_UnsupportedFormatIs422(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{
		"name": "Checking", "type": "bank", "currency": "USD",
	})
	if status != http.StatusCreated {
		t.Fatalf("create account status = %d, want 201: %+v", status, decoded)
	}
	accountID, _ := dataOf(t, decoded)["id"].(string)

	q := url.Values{"account": {accountID}, "filename": {"statement.ofx"}, "format": {"ofx"}}
	status, decoded = doUpload(t, srv, "/api/v1/imports?"+q.Encode(), "application/octet-stream", []byte("garbage"))
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %+v", status, decoded)
	}
	if code := errorCodeOf(t, decoded); code != "invalid_input" {
		t.Errorf("error code = %q, want invalid_input", code)
	}
}
