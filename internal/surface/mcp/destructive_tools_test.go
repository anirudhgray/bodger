package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/app/importparse"
	"github.com/anirudhgray/bodger/internal/ports"
)

// newDestructiveDispatcher wires a Dispatcher with every tool Tools()
// returns, registered with --allow-destructive so this file's own
// destructive-tier tools actually register -- callTool (read_tools_test.go)
// deliberately always uses allowDestructive: false, since none of the
// read/write tool tests need a destructive tool present.
func newDestructiveDispatcher(svc *app.Service) *Dispatcher {
	d := NewDispatcher(svc, true, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	return d
}

// issueToken drives name's first (unconfirmed) call and returns the
// confirmation_token it issued, failing the test if the call didn't
// succeed or didn't carry one.
func issueToken(t *testing.T, d *Dispatcher, name string, args map[string]any) (string, *sdkmcp.CallToolResult) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("json.Marshal(args): %v", err)
	}
	result, err := d.Dispatch(context.Background(), name, raw)
	if err != nil {
		t.Fatalf("Dispatch(%s) issue: %v", name, err)
	}
	if result.IsError {
		text, _ := textContent(result)
		t.Fatalf("Dispatch(%s) issuing call failed: %s", name, text)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("Dispatch(%s) issuing call has no StructuredContent: %+v", name, result)
	}
	token, ok := structured["confirmation_token"].(string)
	if !ok || token == "" {
		t.Fatalf("Dispatch(%s) issuing call did not return a confirmation_token: %+v", name, structured)
	}
	return token, result
}

func confirm(t *testing.T, d *Dispatcher, name, token string, args map[string]any) *sdkmcp.CallToolResult {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	args["confirmation_token"] = token
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("json.Marshal(args): %v", err)
	}
	result, err := d.Dispatch(context.Background(), name, raw)
	if err != nil {
		t.Fatalf("Dispatch(%s) confirm: %v", name, err)
	}
	return result
}

// --- delete_transaction ---

func TestDeleteTransaction_TwoCallFlow(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)
	d := newDestructiveDispatcher(svc)

	args := map[string]any{"transaction_id": fixture.transactionID}

	token, issued := issueToken(t, d, "delete_transaction", args)
	issuedText, _ := textContent(issued)
	if issuedText == "" {
		t.Fatalf("issuing call returned no description")
	}

	// No side effect yet: the transaction is still listed.
	listed, err := svc.ListTransactions(context.Background(), app.ListTransactionsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(listed.Transactions) != 1 {
		t.Fatalf("ListTransactions after issuing call = %d, want 1 (issuing must not delete anything)", len(listed.Transactions))
	}

	result := confirm(t, d, "delete_transaction", token, map[string]any{"transaction_id": fixture.transactionID})
	if result.IsError {
		text, _ := textContent(result)
		t.Fatalf("confirming call failed: %s", text)
	}

	listed, err = svc.ListTransactions(context.Background(), app.ListTransactionsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(listed.Transactions) != 0 {
		t.Fatalf("ListTransactions after confirming call = %d, want 0 (soft-deleted transaction is excluded)", len(listed.Transactions))
	}
}

func TestDeleteTransaction_StaleTokenIsRejected(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)
	d := newDestructiveDispatcher(svc)

	args := map[string]any{"transaction_id": fixture.transactionID}
	token, _ := issueToken(t, d, "delete_transaction", args)

	first := confirm(t, d, "delete_transaction", token, map[string]any{"transaction_id": fixture.transactionID})
	if first.IsError {
		text, _ := textContent(first)
		t.Fatalf("first confirming call failed: %s", text)
	}

	// The token was single-use: replaying it must be rejected, not
	// re-execute (which would try to delete an already-deleted
	// transaction).
	second := confirm(t, d, "delete_transaction", token, map[string]any{"transaction_id": fixture.transactionID})
	if !second.IsError {
		t.Fatalf("reusing a consumed token succeeded, want it rejected")
	}
}

func TestDeleteTransaction_WrongTokenIsRejected(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)
	d := newDestructiveDispatcher(svc)

	result := confirm(t, d, "delete_transaction", "not-a-real-token", map[string]any{"transaction_id": fixture.transactionID})
	if !result.IsError {
		t.Fatalf("confirming with an unknown token succeeded, want it rejected")
	}
}

func TestDeleteTransaction_UnknownTransactionIsAValidationError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := newDestructiveDispatcher(svc)

	result, err := d.Dispatch(context.Background(), "delete_transaction", json.RawMessage(`{"transaction_id":"does-not-exist"}`))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("issuing call for an unknown transaction succeeded, want a validation error")
	}
}

// --- commit_import / rollback_import ---

// stageBasicImport stages one CSV row against a fresh account, ready to
// commit -- the shared fixture commit_import's and rollback_import's own
// tests build on.
func stageBasicImport(t *testing.T, svc *app.Service) (accountID, importID string) {
	t.Helper()
	ctx := context.Background()

	account, err := svc.CreateAccount(ctx, app.CreateAccountCommand{
		ActorID:  ports.SeededUserID,
		Name:     "Checking",
		Kind:     "bank",
		Currency: "USD",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	data := "Date,Description,Amount\n2026-08-01,Coffee Shop,-4.50\n"
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID:      ports.SeededUserID,
		AccountRef:   account.Account.ID(),
		Filename:     "statement.csv",
		SourceFormat: "csv",
		FileContent:  []byte(data),
		ColumnMapping: importparse.ColumnMapping{
			DateColumn: "Date", DescriptionColumn: "Description", AmountColumn: "Amount",
		},
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	return account.Account.ID(), staged.Batch.ID()
}

func TestCommitImport_TwoCallFlow(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	_, importID := stageBasicImport(t, svc)
	d := newDestructiveDispatcher(svc)

	args := map[string]any{"import_id": importID}
	token, issued := issueToken(t, d, "commit_import", args)
	issuedText, _ := textContent(issued)
	if issuedText == "" {
		t.Fatalf("issuing call returned no description")
	}

	// No side effect yet.
	batch, err := svc.GetImportBatch(context.Background(), app.GetImportBatchQuery{ActorID: ports.SeededUserID, ImportBatchRef: importID})
	if err != nil {
		t.Fatalf("GetImportBatch: %v", err)
	}
	if string(batch.Batch.Status()) == "committed" {
		t.Fatalf("import batch already committed after issuing call, want it untouched")
	}

	result := confirm(t, d, "commit_import", token, map[string]any{"import_id": importID})
	if result.IsError {
		text, _ := textContent(result)
		t.Fatalf("confirming call failed: %s", text)
	}

	var view importCommitView
	text, _ := textContent(result)
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Batch.Status != "committed" {
		t.Errorf("Batch.Status = %q, want committed", view.Batch.Status)
	}
	if len(view.Transactions) != 1 {
		t.Errorf("len(Transactions) = %d, want 1", len(view.Transactions))
	}
}

func TestCommitImport_StaleTokenIsRejected(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	_, importID := stageBasicImport(t, svc)
	d := newDestructiveDispatcher(svc)

	args := map[string]any{"import_id": importID}
	token, _ := issueToken(t, d, "commit_import", args)

	first := confirm(t, d, "commit_import", token, map[string]any{"import_id": importID})
	if first.IsError {
		text, _ := textContent(first)
		t.Fatalf("first confirming call failed: %s", text)
	}

	second := confirm(t, d, "commit_import", token, map[string]any{"import_id": importID})
	if !second.IsError {
		t.Fatalf("reusing a consumed token succeeded, want it rejected")
	}
}

func TestCommitImport_UnknownBatchIsAValidationError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := newDestructiveDispatcher(svc)

	result, err := d.Dispatch(context.Background(), "commit_import", json.RawMessage(`{"import_id":"does-not-exist"}`))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("issuing call for an unknown import batch succeeded, want a validation error")
	}
}

func TestRollbackImport_TwoCallFlow(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	_, importID := stageBasicImport(t, svc)
	d := newDestructiveDispatcher(svc)

	// Commit first (directly through the app layer, not the dispatcher)
	// so there's something to roll back.
	if _, err := svc.CommitImportBatch(context.Background(), app.CommitImportBatchCommand{ActorID: ports.SeededUserID, ImportBatchRef: importID}); err != nil {
		t.Fatalf("CommitImportBatch: %v", err)
	}

	args := map[string]any{"import_id": importID}
	token, issued := issueToken(t, d, "rollback_import", args)
	issuedText, _ := textContent(issued)
	if issuedText == "" {
		t.Fatalf("issuing call returned no description")
	}

	// No side effect yet: the committed transaction is still listed.
	listed, err := svc.ListTransactions(context.Background(), app.ListTransactionsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(listed.Transactions) != 1 {
		t.Fatalf("ListTransactions after issuing call = %d, want 1 (issuing must not roll back anything)", len(listed.Transactions))
	}

	result := confirm(t, d, "rollback_import", token, map[string]any{"import_id": importID})
	if result.IsError {
		text, _ := textContent(result)
		t.Fatalf("confirming call failed: %s", text)
	}

	listed, err = svc.ListTransactions(context.Background(), app.ListTransactionsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(listed.Transactions) != 0 {
		t.Fatalf("ListTransactions after confirming call = %d, want 0 (the created transaction was soft-deleted)", len(listed.Transactions))
	}
}

// TestRollbackImport_NotYetCommittedFailsOnConfirm confirms a staged
// (never committed) batch can be described (GetImportBatch/
// ImportBatchTransactions both succeed for any existing batch, reporting
// zero transactions), but the actual precondition failure --
// RollbackImportBatch's own "can't roll back a batch that was never
// committed" check -- only surfaces on the confirming call, once Execute
// actually runs. Describe deliberately doesn't duplicate that check
// (issue #262's scope: no new app-layer logic, describe with what the
// existing read methods already expose).
func TestRollbackImport_NotYetCommittedFailsOnConfirm(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	_, importID := stageBasicImport(t, svc)
	d := newDestructiveDispatcher(svc)

	args := map[string]any{"import_id": importID}
	token, issued := issueToken(t, d, "rollback_import", args)
	issuedText, _ := textContent(issued)
	if issuedText == "" {
		t.Fatalf("issuing call returned no description")
	}

	result := confirm(t, d, "rollback_import", token, map[string]any{"import_id": importID})
	if !result.IsError {
		t.Fatalf("confirming rollback of a never-committed batch succeeded, want a validation error")
	}
}

// --- restore_snapshot ---

func TestRestoreSnapshot_TwoCallFlow(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)
	d := newDestructiveDispatcher(svc)

	document, err := svc.ExportJSON(context.Background(), app.ExportJSONQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	var doc any
	if err := json.Unmarshal(document, &doc); err != nil {
		t.Fatalf("unmarshal exported document: %v", err)
	}
	args := map[string]any{"document": doc}

	token, issued := issueToken(t, d, "restore_snapshot", args)
	issuedText, _ := textContent(issued)
	if issuedText == "" {
		t.Fatalf("issuing call returned no description")
	}

	// No side effect yet: the original transaction is still there,
	// unreplaced, with its original ID.
	if _, err := svc.GetTransaction(context.Background(), app.GetTransactionQuery{ActorID: ports.SeededUserID, TransactionRef: fixture.transactionID}); err != nil {
		t.Fatalf("GetTransaction after issuing call: %v (transaction should be untouched)", err)
	}

	result := confirm(t, d, "restore_snapshot", token, map[string]any{"document": doc})
	if result.IsError {
		text, _ := textContent(result)
		t.Fatalf("confirming call failed: %s", text)
	}

	var view restoreSnapshotView
	text, _ := textContent(result)
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Transactions != 1 {
		t.Errorf("Transactions = %d, want 1", view.Transactions)
	}

	// Every surrogate ID is regenerated on restore, so the original
	// transaction ID no longer resolves.
	if _, err := svc.GetTransaction(context.Background(), app.GetTransactionQuery{ActorID: ports.SeededUserID, TransactionRef: fixture.transactionID}); err == nil {
		t.Fatalf("GetTransaction found the original transaction ID after restore, want it replaced with a new surrogate ID")
	}
}

func TestRestoreSnapshot_StaleTokenIsRejected(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)
	d := newDestructiveDispatcher(svc)

	document, err := svc.ExportJSON(context.Background(), app.ExportJSONQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var doc any
	if err := json.Unmarshal(document, &doc); err != nil {
		t.Fatalf("unmarshal exported document: %v", err)
	}
	args := map[string]any{"document": doc}

	token, _ := issueToken(t, d, "restore_snapshot", args)

	first := confirm(t, d, "restore_snapshot", token, map[string]any{"document": doc})
	if first.IsError {
		text, _ := textContent(first)
		t.Fatalf("first confirming call failed: %s", text)
	}

	second := confirm(t, d, "restore_snapshot", token, map[string]any{"document": doc})
	if !second.IsError {
		t.Fatalf("reusing a consumed token succeeded, want it rejected")
	}
}

func TestRestoreSnapshot_MissingDocumentIsAValidationError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := newDestructiveDispatcher(svc)

	result, err := d.Dispatch(context.Background(), "restore_snapshot", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("issuing call with no document succeeded, want a validation error")
	}
}

// --- --allow-destructive gating ---

// TestDestructiveTools_NotRegisteredWithoutAllowDestructive confirms
// issue #262's own scope item: the generic gate #259 built
// (Dispatcher.Register) already covers every real destructive tool this
// issue adds, not just the synthetic ones dispatcher_test.go exercises.
func TestDestructiveTools_NotRegisteredWithoutAllowDestructive(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}

	for _, name := range []string{"delete_transaction", "commit_import", "rollback_import", "restore_snapshot"} {
		if _, err := d.Dispatch(context.Background(), name, json.RawMessage(`{}`)); err == nil {
			t.Errorf("Dispatch(%s) succeeded without --allow-destructive, want it absent from the registry", name)
		}
	}

	server := d.BuildServer(&sdkmcp.Implementation{Name: "test", Version: "0"})
	if server == nil {
		t.Fatal("BuildServer returned nil")
	}
}

// TestDestructiveTools_RegisteredWithAllowDestructive is the same check's
// positive case: every destructive tool this issue adds is reachable once
// the dispatcher was constructed with allowDestructive.
func TestDestructiveTools_RegisteredWithAllowDestructive(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := newDestructiveDispatcher(svc)

	for _, name := range []string{"delete_transaction", "commit_import", "rollback_import", "restore_snapshot"} {
		// An issuing call (no confirmation_token) with bogus/empty
		// arguments should fail validation, not "no such tool" -- proving
		// the tool is actually registered rather than merely not
		// erroring for an unrelated reason.
		result, err := d.Dispatch(context.Background(), name, json.RawMessage(`{}`))
		if err != nil {
			t.Errorf("Dispatch(%s) = %v, want the tool registered (even if it then reports a validation error)", name, err)
			continue
		}
		if !result.IsError {
			t.Errorf("Dispatch(%s) with empty args succeeded, want a validation error", name)
		}
	}
}
