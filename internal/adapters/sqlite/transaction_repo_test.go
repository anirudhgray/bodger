package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func mustDate(t *testing.T, year int, month time.Month, day int) domain.Date {
	t.Helper()
	d, err := domain.NewDate(year, month, day)
	if err != nil {
		t.Fatalf("domain.NewDate: %v", err)
	}
	return d
}

func mustPosting(t *testing.T, id, accountID string, minor int64, currency string, categoryID *string) ledger.Posting {
	t.Helper()
	p, err := ledger.NewPosting(id, accountID, mustMoney(t, minor, currency), categoryID, 0)
	if err != nil {
		t.Fatalf("ledger.NewPosting: %v", err)
	}
	return p
}

func mustTag(t *testing.T, raw string) ledger.Tag {
	t.Helper()
	tag, err := ledger.NewTag(raw)
	if err != nil {
		t.Fatalf("ledger.NewTag: %v", err)
	}
	return tag
}

// seedAccountAndCategory creates a bank account and an expense category for
// userID, satisfying the foreign keys postings need.
func seedAccountAndCategory(t *testing.T, db *DB, userID, accountID, categoryID string) {
	t.Helper()
	ctx := context.Background()
	accRepo := NewAccountRepository(db)
	acc := mustAccount(t, accountID, userID, accountID+"-name")
	if err := accRepo.Create(ctx, userID, acc); err != nil {
		t.Fatalf("seed account %q: %v", accountID, err)
	}
	catRepo := NewCategoryRepository(db)
	cat := mustCategory(t, categoryID, userID, nil, categoryID+"-name", ledger.CategoryKindExpense)
	if err := catRepo.Create(ctx, userID, cat); err != nil {
		t.Fatalf("seed category %q: %v", categoryID, err)
	}
}

func TestTransactionRepository_CreateGet(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	repo := NewTransactionRepository(db)
	ctx := context.Background()

	catID := "cat-1"
	p := mustPosting(t, "post-1", "acc-1", -80000, "INR", &catID)
	txn, err := ledger.NewOutflow("txn-1", ports.SeededUserID, mustDate(t, 2026, time.August, 14), "Groceries", []ledger.Posting{p})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}
	tags := []ledger.Tag{mustTag(t, "reimbursable")}

	if err := repo.Create(ctx, ports.SeededUserID, txn, tags); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, gotTags, err := repo.Get(ctx, ports.SeededUserID, "txn-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Description() != "Groceries" || got.Kind() != ledger.TransactionKindOutflow {
		t.Errorf("Get = %+v", got)
	}
	if len(got.Postings()) != 1 || got.Postings()[0].Amount().AmountMinor() != -80000 {
		t.Errorf("Get postings = %+v", got.Postings())
	}
	if len(gotTags) != 1 || gotTags[0].String() != "reimbursable" {
		t.Errorf("Get tags = %+v, want [reimbursable]", gotTags)
	}
}

func TestTransactionRepository_Create_Transfer(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-a", "cat-1")
	// Second account for the transfer's other leg.
	accRepo := NewAccountRepository(db)
	accB := mustAccount(t, "acc-b", ports.SeededUserID, "acc-b-name")
	if err := accRepo.Create(context.Background(), ports.SeededUserID, accB); err != nil {
		t.Fatalf("seed acc-b: %v", err)
	}
	repo := NewTransactionRepository(db)
	ctx := context.Background()

	out := mustPosting(t, "post-out", "acc-a", -200000, "INR", nil)
	in := mustPosting(t, "post-in", "acc-b", 200000, "INR", nil)
	txn, err := ledger.NewTransfer("txn-transfer", ports.SeededUserID, mustDate(t, 2026, time.August, 5), "Move money", []ledger.Posting{out, in})
	if err != nil {
		t.Fatalf("NewTransfer: %v", err)
	}

	if err := repo.Create(ctx, ports.SeededUserID, txn, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, _, err := repo.Get(ctx, ports.SeededUserID, "txn-transfer")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind() != ledger.TransactionKindTransfer || len(got.Postings()) != 2 {
		t.Errorf("Get transfer = %+v", got)
	}
}

func TestTransactionRepository_List_FiltersDeletedAndActor(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	seedOtherUser(t, db, otherUserID)
	seedAccountAndCategory(t, db, otherUserID, "acc-other", "cat-other")

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	catID := "cat-1"
	p1 := mustPosting(t, "post-1", "acc-1", -1000, "INR", &catID)
	txn1, err := ledger.NewOutflow("txn-1", ports.SeededUserID, mustDate(t, 2026, time.August, 1), "One", []ledger.Posting{p1})
	if err != nil {
		t.Fatalf("NewOutflow txn1: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txn1, nil); err != nil {
		t.Fatalf("Create txn1: %v", err)
	}

	p2 := mustPosting(t, "post-2", "acc-1", -2000, "INR", &catID)
	txn2, err := ledger.NewOutflow("txn-2", ports.SeededUserID, mustDate(t, 2026, time.August, 2), "Two (to be deleted)", []ledger.Posting{p2})
	if err != nil {
		t.Fatalf("NewOutflow txn2: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txn2, nil); err != nil {
		t.Fatalf("Create txn2: %v", err)
	}

	// Soft-delete txn2 via Update, per ports.TransactionRepository's
	// documented contract (Delete is Update with DeletedAt set).
	deleted := txn2.Delete(testClockInstant)
	if err := repo.Update(ctx, ports.SeededUserID, deleted, nil); err != nil {
		t.Fatalf("Update (soft delete) txn2: %v", err)
	}

	otherCatID := "cat-other"
	pOther := mustPosting(t, "post-other", "acc-other", -3000, "INR", &otherCatID)
	txnOther, err := ledger.NewOutflow("txn-other", otherUserID, mustDate(t, 2026, time.August, 3), "Other user's", []ledger.Posting{pOther})
	if err != nil {
		t.Fatalf("NewOutflow txnOther: %v", err)
	}
	if err := repo.Create(ctx, otherUserID, txnOther, nil); err != nil {
		t.Fatalf("Create txnOther: %v", err)
	}

	list, err := repo.List(ctx, ports.SeededUserID, ports.TransactionFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID() != "txn-1" {
		t.Errorf("List(seeded user) = %+v, want only txn-1 (txn-2 deleted, txn-other belongs to a different user)", list)
	}

	// Get must also refuse a soft-deleted transaction.
	if _, _, err := repo.Get(ctx, ports.SeededUserID, "txn-2"); err == nil {
		t.Error("Get(soft-deleted txn-2): want NotFound, got nil")
	} else {
		var e *errs.Error
		if !errors.As(err, &e) || e.Code != errs.NotFound {
			t.Errorf("Get(soft-deleted txn-2): err = %v, want errs.NotFound", err)
		}
	}
}

func TestTransactionRepository_List_FilterByAccountAndAsOf(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	accRepo := NewAccountRepository(db)
	acc2 := mustAccount(t, "acc-2", ports.SeededUserID, "acc-2-name")
	if err := accRepo.Create(context.Background(), ports.SeededUserID, acc2); err != nil {
		t.Fatalf("seed acc-2: %v", err)
	}

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	catID := "cat-1"

	early := mustPosting(t, "post-early", "acc-1", -1000, "INR", &catID)
	txnEarly, err := ledger.NewOutflow("txn-early", ports.SeededUserID, mustDate(t, 2026, time.August, 1), "Early on acc-1", []ledger.Posting{early})
	if err != nil {
		t.Fatalf("NewOutflow txnEarly: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnEarly, nil); err != nil {
		t.Fatalf("Create txnEarly: %v", err)
	}

	late := mustPosting(t, "post-late", "acc-1", -1000, "INR", &catID)
	txnLate, err := ledger.NewOutflow("txn-late", ports.SeededUserID, mustDate(t, 2026, time.August, 20), "Late on acc-1", []ledger.Posting{late})
	if err != nil {
		t.Fatalf("NewOutflow txnLate: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnLate, nil); err != nil {
		t.Fatalf("Create txnLate: %v", err)
	}

	onOtherAccount := mustPosting(t, "post-acc2", "acc-2", -1000, "INR", nil)
	txnOtherAccount, err := ledger.NewOutflow("txn-acc2", ports.SeededUserID, mustDate(t, 2026, time.August, 10), "On acc-2", []ledger.Posting{onOtherAccount})
	if err != nil {
		t.Fatalf("NewOutflow txnOtherAccount: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnOtherAccount, nil); err != nil {
		t.Fatalf("Create txnOtherAccount: %v", err)
	}

	asOf := mustDate(t, 2026, time.August, 15)
	list, err := repo.List(ctx, ports.SeededUserID, ports.TransactionFilter{AccountID: "acc-1", AsOf: &asOf})
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if len(list) != 1 || list[0].ID() != "txn-early" {
		t.Errorf("List(acc-1, asOf=2026-08-15) = %+v, want only txn-early", list)
	}
}

func TestTransactionRepository_Update_WritesRevision(t *testing.T) {
	db, clk := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	repo := NewTransactionRepository(db)
	ctx := context.Background()

	catID := "cat-1"
	p := mustPosting(t, "post-1", "acc-1", -80000, "INR", &catID)
	txn, err := ledger.NewOutflow("txn-1", ports.SeededUserID, mustDate(t, 2026, time.August, 14), "Groceries", []ledger.Posting{p})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txn, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	clk.Advance(time.Hour)
	editedPosting := mustPosting(t, "post-1", "acc-1", -90000, "INR", &catID)
	edited, err := ledger.NewOutflow("txn-1", ports.SeededUserID, mustDate(t, 2026, time.August, 14), "Groceries (corrected)", []ledger.Posting{editedPosting})
	if err != nil {
		t.Fatalf("NewOutflow edited: %v", err)
	}
	if err := repo.Update(ctx, ports.SeededUserID, edited, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _, err := repo.Get(ctx, ports.SeededUserID, "txn-1")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Description() != "Groceries (corrected)" || got.Postings()[0].Amount().AmountMinor() != -90000 {
		t.Errorf("Get after update = %+v", got)
	}

	var revisionCount int
	var previousState string
	err = db.write.QueryRowContext(ctx, `
		SELECT count(*), previous_state FROM transaction_revisions WHERE transaction_id = ?
	`, "txn-1").Scan(&revisionCount, &previousState)
	if err != nil {
		t.Fatalf("query transaction_revisions: %v", err)
	}
	if revisionCount != 1 {
		t.Errorf("revisionCount = %d, want 1", revisionCount)
	}
	if previousState == "" {
		t.Error("previous_state is empty, want the pre-edit snapshot")
	}
}

func TestTransactionRepository_Update_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	repo := NewTransactionRepository(db)

	p := mustPosting(t, "post-1", "acc-1", -1000, "INR", nil)
	txn, err := ledger.NewOutflow("does-not-exist", ports.SeededUserID, mustDate(t, 2026, time.August, 14), "Ghost", []ledger.Posting{p})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}

	err = repo.Update(context.Background(), ports.SeededUserID, txn, nil)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("Update(missing): err = %v, want errs.NotFound", err)
	}
}

func TestTransactionRepository_Create_RejectsActorMismatch(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	repo := NewTransactionRepository(db)

	p := mustPosting(t, "post-1", "acc-1", -1000, "INR", nil)
	txn, err := ledger.NewOutflow("txn-1", ports.SeededUserID, mustDate(t, 2026, time.August, 14), "Groceries", []ledger.Posting{p})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}

	err = repo.Create(context.Background(), otherUserID, txn, nil)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Errorf("Create with mismatched actor: err = %v, want errs.NotAllowed", err)
	}
}
