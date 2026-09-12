package app_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// mutateDocument round-trips doc through a generic map so a test can tweak
// one top-level field (or delete it) without needing access to this
// package's unexported jsonEnvelope wire type -- restore_test.go lives in
// app_test, outside internal/app, the same boundary export_json_test.go
// already works within via map[string]any.
func mutateDocument(t *testing.T, doc []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(doc, &m); err != nil {
		t.Fatalf("mutateDocument: %v", err)
	}
	mutate(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("mutateDocument: %v", err)
	}
	return out
}

func TestRestoreSnapshot_RequiresActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.RestoreSnapshot(context.Background(), app.RestoreSnapshotQuery{
		Document: []byte(`{"format":"bodger.export/v1","accounts":[],"categories":[],"transactions":[]}`),
	})
	if err == nil {
		t.Fatal("want an error for a missing actor ID")
	}
}

func TestRestoreSnapshot_RejectsMalformedJSON(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.RestoreSnapshot(context.Background(), app.RestoreSnapshotQuery{
		ActorID: testActorID, Document: []byte(`{"format": "bodger.export/v1", "accounts": [`),
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestRestoreSnapshot_RejectsMissingFormatVersion(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.RestoreSnapshot(context.Background(), app.RestoreSnapshotQuery{
		ActorID: testActorID, Document: []byte(`{"accounts":[],"categories":[],"transactions":[]}`),
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestRestoreSnapshot_RejectsUnknownFormatVersion(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.RestoreSnapshot(context.Background(), app.RestoreSnapshotQuery{
		ActorID:  testActorID,
		Document: []byte(`{"format":"bodger.export/v2","accounts":[],"categories":[],"transactions":[]}`),
	})
	wantErrCode(t, err, errs.InvalidInput)
}

// TestRestoreSnapshot_RegeneratesIDsAndRemapsReferences is the core of
// issue #226: a restore never reuses the document's own IDs, and every
// reference between restored entities (a posting's account/category, a
// transaction's tags) is remapped to point at the freshly generated ones.
func TestRestoreSnapshot_RegeneratesIDsAndRemapsReferences(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	acc := mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")
	cat := mustCategoryFixture(t, svc, "Groceries", "expense")
	created, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "500", CategoryRef: cat.Category.ID(),
		Date: "2026-08-14", Description: "Big Bazaar", Tags: []string{"food"},
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	oldAccountID, oldCategoryID, oldTxnID := acc.Account.ID(), cat.Category.ID(), created.Transaction.ID()

	doc, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	result, err := svc.RestoreSnapshot(ctx, app.RestoreSnapshotQuery{ActorID: testActorID, Document: doc})
	if err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	if result.Accounts != 1 || result.Categories != 1 || result.Transactions != 1 {
		t.Fatalf("RestoreSnapshot result = %+v, want 1/1/1", result)
	}

	accounts, err := svc.Accounts.List(ctx, testActorID)
	if err != nil {
		t.Fatalf("Accounts.List: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("len(accounts) = %d, want 1", len(accounts))
	}
	newAccountID := accounts[0].ID()
	if newAccountID == oldAccountID {
		t.Errorf("restored account ID = %q, want a freshly generated ID, not the export's own %q", newAccountID, oldAccountID)
	}
	if accounts[0].Name() != "HDFC Savings" {
		t.Errorf("restored account name = %q, want HDFC Savings", accounts[0].Name())
	}

	categories, err := svc.Categories.List(ctx, testActorID)
	if err != nil {
		t.Fatalf("Categories.List: %v", err)
	}
	if len(categories) != 1 {
		t.Fatalf("len(categories) = %d, want 1", len(categories))
	}
	newCategoryID := categories[0].ID()
	if newCategoryID == oldCategoryID {
		t.Errorf("restored category ID = %q, want a freshly generated ID, not the export's own %q", newCategoryID, oldCategoryID)
	}

	listResult, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(listResult.Transactions) != 1 {
		t.Fatalf("len(transactions) = %d, want 1", len(listResult.Transactions))
	}
	restoredTxn := listResult.Transactions[0]
	if restoredTxn.ID() == oldTxnID {
		t.Errorf("restored transaction ID = %q, want a freshly generated ID, not the export's own %q", restoredTxn.ID(), oldTxnID)
	}
	postings := restoredTxn.Postings()
	if len(postings) != 1 {
		t.Fatalf("len(postings) = %d, want 1", len(postings))
	}
	if postings[0].AccountID() != newAccountID {
		t.Errorf("restored posting AccountID = %q, want the newly generated account ID %q, not a stale reference", postings[0].AccountID(), newAccountID)
	}
	postingCategoryID, ok := postings[0].CategoryID()
	if !ok || postingCategoryID != newCategoryID {
		t.Errorf("restored posting CategoryID = (%q, %v), want the newly generated category ID %q", postingCategoryID, ok, newCategoryID)
	}

	restoredTxnFull, tags, err := svc.Transactions.Get(ctx, testActorID, restoredTxn.ID())
	if err != nil {
		t.Fatalf("Transactions.Get: %v", err)
	}
	if len(tags) != 1 || tags[0].String() != "food" {
		t.Errorf("restored tags = %+v, want [food]", tags)
	}
	if restoredTxnFull.Description() != "Big Bazaar" {
		t.Errorf("restored description = %q, want Big Bazaar", restoredTxnFull.Description())
	}
}

// TestRestoreSnapshot_ReplaceIsFullStateNotMerge proves "replace" really
// means replace: an account that existed only because it was created after
// the document was exported must be gone once the restore completes, not
// merged alongside the document's own accounts.
func TestRestoreSnapshot_ReplaceIsFullStateNotMerge(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	mustAccountFixture(t, svc, "Savings", "bank", "INR")
	doc, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	// Created after the snapshot was taken -- a restore from that snapshot
	// must make this account disappear.
	mustAccountFixture(t, svc, "Checking", "bank", "INR")

	if _, err := svc.RestoreSnapshot(ctx, app.RestoreSnapshotQuery{ActorID: testActorID, Document: doc}); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}

	accounts, err := svc.Accounts.List(ctx, testActorID)
	if err != nil {
		t.Fatalf("Accounts.List: %v", err)
	}
	if len(accounts) != 1 || accounts[0].Name() != "Savings" {
		t.Fatalf("accounts after restore = %+v, want exactly [Savings] (Checking merged in, not replaced away)", accounts)
	}
}

// TestRestoreSnapshot_UnresolvableReferenceLeavesExistingDataUntouched is
// this issue's partial-failure atomicity test at the application layer: a
// document whose structure is broken (a posting pointing at an account ID
// the document's own accounts section never declares) must be rejected
// before anything is written, leaving whatever the actor already had
// completely unchanged.
func TestRestoreSnapshot_UnresolvableReferenceLeavesExistingDataUntouched(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	acc := mustAccountFixture(t, svc, "Savings", "bank", "INR")
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Date: "2026-08-14", Description: "Coffee",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	before, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportJSON (before): %v", err)
	}

	broken := mutateDocument(t, before, func(m map[string]any) {
		txns, _ := m["transactions"].([]any)
		if len(txns) != 1 {
			t.Fatalf("test fixture: want exactly one transaction to corrupt, got %d", len(txns))
		}
		postings, _ := txns[0].(map[string]any)["postings"].([]any)
		if len(postings) != 1 {
			t.Fatalf("test fixture: want exactly one posting to corrupt, got %d", len(postings))
		}
		postings[0].(map[string]any)["account_id"] = "does-not-exist-in-this-document"
	})

	if _, err := svc.RestoreSnapshot(ctx, app.RestoreSnapshotQuery{ActorID: testActorID, Document: broken}); err == nil {
		t.Fatal("RestoreSnapshot with a dangling account reference: want an error, got nil")
	} else {
		wantErrCode(t, err, errs.InvalidInput)
	}

	after, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportJSON (after): %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("existing data changed after a rejected restore:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
