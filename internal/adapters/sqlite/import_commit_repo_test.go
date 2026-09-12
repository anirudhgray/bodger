package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// seedCategoryOnly creates categoryID for userID, without touching
// accounts — for tests where the target account already exists (e.g. via
// seedImportBatch) and only a category is missing to satisfy postings'
// foreign key.
func seedCategoryOnly(t *testing.T, db *DB, userID, categoryID string) {
	t.Helper()
	cat := mustCategory(t, categoryID, userID, nil, categoryID+"-name", ledger.CategoryKindExpense)
	if err := NewCategoryRepository(db).Create(context.Background(), userID, cat); err != nil {
		t.Fatalf("seed category %q: %v", categoryID, err)
	}
}

// readyImportRecord builds an ImportRecord already resolved to accountID/
// categoryID and marked ImportRecordStatusReady — the shape
// ImportCommitRepository.Commit expects records to already be in by the
// time it's called (the app layer, not this repository, is what walks a
// batch's records and decides which ones are ready).
func readyImportRecord(t *testing.T, id, userID, batchID, accountID, categoryID string, minor int64, sortOrder int) importing.ImportRecord {
	t.Helper()
	catID := categoryID
	r, err := importing.NewImportRecord(
		id, userID, batchID, `{"raw":"row"}`, mustDate(t, 2026, time.August, 14), id+"-description",
		mustMoney(t, minor, "USD"), sortOrder,
		importing.WithResolvedAccount(accountID), importing.WithResolvedCategory(catID),
	)
	if err != nil {
		t.Fatalf("importing.NewImportRecord: %v", err)
	}
	ready, err := r.MarkReady()
	if err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	return ready
}

// commitTransactionFor builds the ledger.Transaction a commit produces for
// rec — an outflow for a negative amount, matching
// app.buildImportCommitTransaction's own sign dispatch — and marks rec
// committed against it, mirroring exactly what app.CommitImportBatch hands
// ImportCommitRepository.Commit.
func commitTransactionFor(t *testing.T, userID string, rec importing.ImportRecord, txnID string) (ledger.Transaction, importing.ImportRecord) {
	t.Helper()
	accountID, _ := rec.ResolvedAccountID()
	categoryID, _ := rec.ResolvedCategoryID()
	catID := categoryID
	posting, err := ledger.NewPosting(txnID+"-posting", accountID, rec.Amount(), &catID, 0)
	if err != nil {
		t.Fatalf("ledger.NewPosting: %v", err)
	}
	opts := []ledger.TransactionOption{ledger.WithImportProvenance(rec.ID(), "")}
	txn, err := ledger.NewOutflow(txnID, userID, rec.BookedDate(), rec.Description(), []ledger.Posting{posting}, opts...)
	if err != nil {
		t.Fatalf("ledger.NewOutflow: %v", err)
	}
	committedRec, err := rec.MarkCommitted(txnID)
	if err != nil {
		t.Fatalf("MarkCommitted: %v", err)
	}
	return txn, committedRec
}

func TestImportCommitRepository_Commit_WritesTransactionsRecordsAndBatch(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")
	// seedImportBatch already created "acc-1" as the batch's target
	// account; only the category is missing.
	seedCategoryOnly(t, db, ports.SeededUserID, "cat-1")

	recordRepo := NewImportRecordRepository(db)
	rec1 := readyImportRecord(t, "record-1", ports.SeededUserID, batch.ID(), "acc-1", "cat-1", -450, 0)
	rec2 := readyImportRecord(t, "record-2", ports.SeededUserID, batch.ID(), "acc-1", "cat-1", -1200, 1)
	if err := recordRepo.CreateBatch(ctx, ports.SeededUserID, []importing.ImportRecord{rec1, rec2}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	txn1, committed1 := commitTransactionFor(t, ports.SeededUserID, rec1, "txn-1")
	txn2, committed2 := commitTransactionFor(t, ports.SeededUserID, rec2, "txn-2")

	reviewed, err := batch.MarkReviewed()
	if err != nil {
		t.Fatalf("MarkReviewed: %v", err)
	}
	committedBatch, err := reviewed.MarkCommitted()
	if err != nil {
		t.Fatalf("MarkCommitted: %v", err)
	}

	repo := NewImportCommitRepository(db)
	if err := repo.Commit(ctx, ports.SeededUserID, committedBatch,
		[]importing.ImportRecord{committed1, committed2}, []ledger.Transaction{txn1, txn2}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Both transactions actually exist, and carry ImportRecordID for
	// provenance.
	txnRepo := NewTransactionRepository(db)
	gotTxn1, _, err := txnRepo.Get(ctx, ports.SeededUserID, "txn-1")
	if err != nil {
		t.Fatalf("Get(txn-1): %v", err)
	}
	if id, ok := gotTxn1.ImportRecordID(); !ok || id != "record-1" {
		t.Errorf("txn-1 ImportRecordID() = (%q, %v), want (%q, true)", id, ok, "record-1")
	}
	if _, _, err := txnRepo.Get(ctx, ports.SeededUserID, "txn-2"); err != nil {
		t.Fatalf("Get(txn-2): %v", err)
	}

	// Both records are now committed, pointing at their transaction.
	gotRec1, err := recordRepo.Get(ctx, ports.SeededUserID, "record-1")
	if err != nil {
		t.Fatalf("Get(record-1): %v", err)
	}
	if gotRec1.Status() != importing.ImportRecordStatusCommitted {
		t.Errorf("record-1 Status() = %q, want %q", gotRec1.Status(), importing.ImportRecordStatusCommitted)
	}
	if id, ok := gotRec1.TransactionID(); !ok || id != "txn-1" {
		t.Errorf("record-1 TransactionID() = (%q, %v), want (%q, true)", id, ok, "txn-1")
	}

	// The batch itself is committed.
	gotBatch, err := NewImportBatchRepository(db).Get(ctx, ports.SeededUserID, batch.ID())
	if err != nil {
		t.Fatalf("Get(batch): %v", err)
	}
	if gotBatch.Status() != importing.ImportBatchStatusCommitted {
		t.Errorf("batch Status() = %q, want %q", gotBatch.Status(), importing.ImportBatchStatusCommitted)
	}
}

// TestImportCommitRepository_Commit_AllOrNothing is this issue's central
// atomicity assertion: ADR-0008's "one database transaction: all or
// nothing, never half an import." The second of two transactions in one
// Commit call is engineered to fail (a primary-key collision with a
// transaction that already exists) — if Commit only wrote row by row
// instead of atomically, the first transaction, and the first record's
// committed status, would survive this failure. Neither may.
func TestImportCommitRepository_Commit_AllOrNothing(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")
	// seedImportBatch already created "acc-1" as the batch's target
	// account; only the category is missing.
	seedCategoryOnly(t, db, ports.SeededUserID, "cat-1")

	// A transaction already occupying the ID the second commit-produced
	// transaction will collide with.
	seedTransaction(t, db, ports.SeededUserID, "txn-collide", "acc-1", "cat-1")

	recordRepo := NewImportRecordRepository(db)
	rec1 := readyImportRecord(t, "record-1", ports.SeededUserID, batch.ID(), "acc-1", "cat-1", -450, 0)
	rec2 := readyImportRecord(t, "record-2", ports.SeededUserID, batch.ID(), "acc-1", "cat-1", -1200, 1)
	if err := recordRepo.CreateBatch(ctx, ports.SeededUserID, []importing.ImportRecord{rec1, rec2}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	txn1, committed1 := commitTransactionFor(t, ports.SeededUserID, rec1, "txn-1")
	// txn2 reuses the already-seeded "txn-collide" ID, so its INSERT fails
	// on a primary-key violation partway through Commit's loop, after
	// txn1 has already been inserted.
	txn2, committed2 := commitTransactionFor(t, ports.SeededUserID, rec2, "txn-collide")

	reviewed, err := batch.MarkReviewed()
	if err != nil {
		t.Fatalf("MarkReviewed: %v", err)
	}
	committedBatch, err := reviewed.MarkCommitted()
	if err != nil {
		t.Fatalf("MarkCommitted: %v", err)
	}

	repo := NewImportCommitRepository(db)
	err = repo.Commit(ctx, ports.SeededUserID, committedBatch,
		[]importing.ImportRecord{committed1, committed2}, []ledger.Transaction{txn1, txn2})
	if err == nil {
		t.Fatal("Commit with a colliding transaction ID: want an error, got nil")
	}

	// txn-1 must not have survived, even though its own INSERT succeeded
	// before the failure.
	if _, _, err := NewTransactionRepository(db).Get(ctx, ports.SeededUserID, "txn-1"); err == nil {
		t.Error("Get(txn-1) after a failed Commit: want an error (nothing partial persists), got nil")
	}

	// record-1 must still be "ready", not "committed" — the whole call
	// rolled back, including the import_record update that would have
	// followed the (failed) transaction inserts.
	gotRec1, err := recordRepo.Get(ctx, ports.SeededUserID, "record-1")
	if err != nil {
		t.Fatalf("Get(record-1): %v", err)
	}
	if gotRec1.Status() != importing.ImportRecordStatusReady {
		t.Errorf("record-1 Status() after a failed Commit = %q, want %q (unchanged)", gotRec1.Status(), importing.ImportRecordStatusReady)
	}

	// The batch itself must still be "staged" — MarkCommitted only ever
	// happened to the in-memory copy this test built, never persisted.
	gotBatch, err := NewImportBatchRepository(db).Get(ctx, ports.SeededUserID, batch.ID())
	if err != nil {
		t.Fatalf("Get(batch): %v", err)
	}
	if gotBatch.Status() != importing.ImportBatchStatusStaged {
		t.Errorf("batch Status() after a failed Commit = %q, want %q (unchanged)", gotBatch.Status(), importing.ImportBatchStatusStaged)
	}
}

func TestImportCommitRepository_Commit_RejectsMismatchedActor(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")
	// seedImportBatch already created "acc-1" as the batch's target
	// account; only the category is missing.
	seedCategoryOnly(t, db, ports.SeededUserID, "cat-1")

	recordRepo := NewImportRecordRepository(db)
	rec := readyImportRecord(t, "record-1", ports.SeededUserID, batch.ID(), "acc-1", "cat-1", -450, 0)
	if err := recordRepo.CreateBatch(ctx, ports.SeededUserID, []importing.ImportRecord{rec}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}
	txn, committedRec := commitTransactionFor(t, ports.SeededUserID, rec, "txn-1")

	reviewed, err := batch.MarkReviewed()
	if err != nil {
		t.Fatalf("MarkReviewed: %v", err)
	}
	committedBatch, err := reviewed.MarkCommitted()
	if err != nil {
		t.Fatalf("MarkCommitted: %v", err)
	}

	repo := NewImportCommitRepository(db)
	err = repo.Commit(ctx, otherUserID, committedBatch, []importing.ImportRecord{committedRec}, []ledger.Transaction{txn})
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("Commit(mismatched actor) error = %v, want *errs.Error with code NotAllowed", err)
	}

	if _, _, err := NewTransactionRepository(db).Get(ctx, ports.SeededUserID, "txn-1"); err == nil {
		t.Error("Get(txn-1) after a rejected Commit: want an error, got nil")
	}
}

// seedCommittedBatch stages, marks ready, and commits a two-record batch
// via the real repository stack, returning the batch and the IDs of the
// transactions it produced — the fixture every Rollback test below builds
// on.
func seedCommittedBatch(t *testing.T, db *DB, userID, batchID, accountID string) (importing.ImportBatch, []string) {
	t.Helper()
	ctx := context.Background()
	batch := seedImportBatch(t, db, userID, batchID, accountID)
	seedCategoryOnly(t, db, userID, batchID+"-cat")

	recordRepo := NewImportRecordRepository(db)
	rec1 := readyImportRecord(t, batchID+"-record-1", userID, batch.ID(), accountID, batchID+"-cat", -450, 0)
	rec2 := readyImportRecord(t, batchID+"-record-2", userID, batch.ID(), accountID, batchID+"-cat", -900, 1)
	if err := recordRepo.CreateBatch(ctx, userID, []importing.ImportRecord{rec1, rec2}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	txn1, committed1 := commitTransactionFor(t, userID, rec1, batchID+"-txn-1")
	txn2, committed2 := commitTransactionFor(t, userID, rec2, batchID+"-txn-2")

	reviewed, err := batch.MarkReviewed()
	if err != nil {
		t.Fatalf("MarkReviewed: %v", err)
	}
	committedBatch, err := reviewed.MarkCommitted()
	if err != nil {
		t.Fatalf("MarkCommitted: %v", err)
	}

	if err := NewImportCommitRepository(db).Commit(ctx, userID, committedBatch,
		[]importing.ImportRecord{committed1, committed2}, []ledger.Transaction{txn1, txn2}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return committedBatch, []string{batchID + "-txn-1", batchID + "-txn-2"}
}

func TestImportCommitRepository_Rollback_SoftDeletesOnlyBatchTransactions(t *testing.T) {
	db, clk := newTestDB(t)
	ctx := context.Background()
	committedBatch, txnIDs := seedCommittedBatch(t, db, ports.SeededUserID, "batch-a", "acc-a")
	// A second, unrelated committed batch's transaction must survive
	// batch-a's rollback untouched.
	_, otherTxnIDs := seedCommittedBatch(t, db, ports.SeededUserID, "batch-b", "acc-b")

	rolledBack, err := committedBatch.MarkRolledBack()
	if err != nil {
		t.Fatalf("MarkRolledBack: %v", err)
	}

	repo := NewImportCommitRepository(db)
	if err := repo.Rollback(ctx, ports.SeededUserID, rolledBack, txnIDs, clk.Now()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	txnRepo := NewTransactionRepository(db)
	for _, id := range txnIDs {
		if _, _, err := txnRepo.Get(ctx, ports.SeededUserID, id); err == nil {
			t.Errorf("Get(%s) after Rollback: want an error (soft-deleted), got nil", id)
		}
	}
	for _, id := range otherTxnIDs {
		if _, _, err := txnRepo.Get(ctx, ports.SeededUserID, id); err != nil {
			t.Errorf("Get(%s) after a different batch's Rollback: want success, got %v", id, err)
		}
	}

	gotBatch, err := NewImportBatchRepository(db).Get(ctx, ports.SeededUserID, committedBatch.ID())
	if err != nil {
		t.Fatalf("Get(batch): %v", err)
	}
	if gotBatch.Status() != importing.ImportBatchStatusRolledBack {
		t.Errorf("batch Status() = %q, want %q", gotBatch.Status(), importing.ImportBatchStatusRolledBack)
	}
}

// TestImportCommitRepository_Rollback_RepeatedIsNoop exercises Rollback's
// own idempotency at the storage layer (ADR-0008: "a batch remains
// rollback-able after the fact" never means a second call may error out
// on rows it already deleted) — independent of app.RollbackImportBatch's
// state-machine guard, which is what actually prevents a real second
// rollback call from reaching this repository in production.
func TestImportCommitRepository_Rollback_RepeatedIsNoop(t *testing.T) {
	db, clk := newTestDB(t)
	ctx := context.Background()
	committedBatch, txnIDs := seedCommittedBatch(t, db, ports.SeededUserID, "batch-a", "acc-a")

	rolledBack, err := committedBatch.MarkRolledBack()
	if err != nil {
		t.Fatalf("MarkRolledBack: %v", err)
	}

	repo := NewImportCommitRepository(db)
	if err := repo.Rollback(ctx, ports.SeededUserID, rolledBack, txnIDs, clk.Now()); err != nil {
		t.Fatalf("Rollback (first): %v", err)
	}
	if err := repo.Rollback(ctx, ports.SeededUserID, rolledBack, txnIDs, clk.Now()); err != nil {
		t.Fatalf("Rollback (second, repeated): %v, want success (a no-op over already-deleted rows)", err)
	}
}

func TestImportCommitRepository_Rollback_RejectsMismatchedActor(t *testing.T) {
	db, clk := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	committedBatch, txnIDs := seedCommittedBatch(t, db, ports.SeededUserID, "batch-a", "acc-a")

	rolledBack, err := committedBatch.MarkRolledBack()
	if err != nil {
		t.Fatalf("MarkRolledBack: %v", err)
	}

	repo := NewImportCommitRepository(db)
	err = repo.Rollback(ctx, otherUserID, rolledBack, txnIDs, clk.Now())
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("Rollback(mismatched actor) error = %v, want *errs.Error with code NotAllowed", err)
	}

	// Nothing must have been soft-deleted.
	if _, _, err := NewTransactionRepository(db).Get(ctx, ports.SeededUserID, txnIDs[0]); err != nil {
		t.Errorf("Get(%s) after a rejected Rollback: want success, got %v", txnIDs[0], err)
	}
}
