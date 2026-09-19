package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// materialisationFor builds the ledger.Transaction and already-transitioned
// ScheduledOccurrence a materialisation produces for occurrence, posting to
// whichever account/category seedRule created for ruleID — the same shape
// app.MaterialiseOccurrence hands RecurringMaterializationRepository.
func materialisationFor(t *testing.T, userID, ruleID string, occurrence recurring.ScheduledOccurrence, txnID string) (ledger.Transaction, recurring.ScheduledOccurrence) {
	t.Helper()
	categoryID := ruleID + "-category"
	posting := mustPosting(t, txnID+"-posting", ruleID+"-account", -50000, "USD", &categoryID)
	txn, err := ledger.NewOutflow(txnID, userID, occurrence.OccurrenceDate(), "Rent", []ledger.Posting{posting})
	if err != nil {
		t.Fatalf("ledger.NewOutflow: %v", err)
	}
	materialised, err := occurrence.MarkMaterialised(txnID)
	if err != nil {
		t.Fatalf("MarkMaterialised: %v", err)
	}
	return txn, materialised
}

func TestRecurringMaterializationRepository_Materialize_WritesTransactionAndOccurrence(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	occRepo := NewScheduledOccurrenceRepository(db)
	on := mustDate(t, 2026, time.January, 15)
	if err := occRepo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, on),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}
	pending, err := occRepo.Get(ctx, ports.SeededUserID, "occ-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	txn, materialised := materialisationFor(t, ports.SeededUserID, "r1", pending, "txn-1")

	repo := NewRecurringMaterializationRepository(db)
	if err := repo.Materialize(ctx, ports.SeededUserID, materialised, txn); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	gotTxn, _, err := NewTransactionRepository(db).Get(ctx, ports.SeededUserID, "txn-1")
	if err != nil {
		t.Fatalf("Get(txn-1): %v", err)
	}
	if gotTxn.BookedDate().String() != "2026-01-15" {
		t.Errorf("txn BookedDate() = %s, want the occurrence's own projected date 2026-01-15", gotTxn.BookedDate())
	}

	gotOcc, err := occRepo.Get(ctx, ports.SeededUserID, "occ-1")
	if err != nil {
		t.Fatalf("Get(occ-1): %v", err)
	}
	if gotOcc.Status() != recurring.OccurrenceStatusMaterialised {
		t.Errorf("Status() = %q, want %q", gotOcc.Status(), recurring.OccurrenceStatusMaterialised)
	}
	if id, ok := gotOcc.TransactionID(); !ok || id != "txn-1" {
		t.Errorf("TransactionID() = (%q, %v), want (%q, true)", id, ok, "txn-1")
	}
}

// TestRecurringMaterializationRepository_Materialize_IsAtomic engineers a
// transaction-ID collision so the INSERT fails after the ownership check
// passes but before the occurrence UPDATE runs — proving neither write
// survives without the other, the same all-or-nothing contract
// ImportCommitRepository.Commit has.
func TestRecurringMaterializationRepository_Materialize_IsAtomic(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")
	seedTransaction(t, db, ports.SeededUserID, "txn-collide", "r1-account", "r1-category")

	occRepo := NewScheduledOccurrenceRepository(db)
	on := mustDate(t, 2026, time.January, 15)
	if err := occRepo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, on),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}
	pending, err := occRepo.Get(ctx, ports.SeededUserID, "occ-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// "txn-collide" already exists, so this INSERT fails on a primary-key
	// violation.
	txn, materialised := materialisationFor(t, ports.SeededUserID, "r1", pending, "txn-collide")

	repo := NewRecurringMaterializationRepository(db)
	if err := repo.Materialize(ctx, ports.SeededUserID, materialised, txn); err == nil {
		t.Fatal("Materialize with a colliding transaction ID: want an error, got nil")
	}

	gotOcc, err := occRepo.Get(ctx, ports.SeededUserID, "occ-1")
	if err != nil {
		t.Fatalf("Get(occ-1) after a failed Materialize: %v", err)
	}
	if gotOcc.Status() != recurring.OccurrenceStatusPending {
		t.Errorf("Status() after a failed Materialize = %q, want %q (unchanged)", gotOcc.Status(), recurring.OccurrenceStatusPending)
	}
	if _, ok := gotOcc.TransactionID(); ok {
		t.Error("occurrence carries a transaction id after a failed Materialize, want none")
	}
}

func TestRecurringMaterializationRepository_Materialize_RejectsMismatchedTransactionActor(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	occRepo := NewScheduledOccurrenceRepository(db)
	on := mustDate(t, 2026, time.January, 15)
	if err := occRepo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, on),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}
	pending, err := occRepo.Get(ctx, ports.SeededUserID, "occ-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	txn, materialised := materialisationFor(t, otherUserID, "r1", pending, "txn-1")

	repo := NewRecurringMaterializationRepository(db)
	err = repo.Materialize(ctx, ports.SeededUserID, materialised, txn)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("Materialize(mismatched transaction actor) error = %v, want *errs.Error with code NotAllowed", err)
	}

	if _, _, err := NewTransactionRepository(db).Get(ctx, ports.SeededUserID, "txn-1"); err == nil {
		t.Error("Get(txn-1) after a rejected Materialize: want an error, got nil")
	}
}

func TestRecurringMaterializationRepository_Materialize_RejectsAnotherUsersOccurrence(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	occRepo := NewScheduledOccurrenceRepository(db)
	on := mustDate(t, 2026, time.January, 15)
	if err := occRepo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, on),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}
	pending, err := occRepo.Get(ctx, ports.SeededUserID, "occ-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// The transaction itself is otherUserID's own (so the actor check on
	// txn.UserID() passes), but the occurrence belongs to ports.SeededUserID's
	// rule — the second, rule-scoped ownership check must still catch this.
	txn, materialised := materialisationFor(t, otherUserID, "r1", pending, "txn-1")

	repo := NewRecurringMaterializationRepository(db)
	err = repo.Materialize(ctx, otherUserID, materialised, txn)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("Materialize(another user's occurrence) error = %v, want *errs.Error with code NotAllowed", err)
	}

	if _, _, err := NewTransactionRepository(db).Get(ctx, ports.SeededUserID, "txn-1"); err == nil {
		t.Error("Get(txn-1) after a rejected Materialize: want an error, got nil")
	}
}
