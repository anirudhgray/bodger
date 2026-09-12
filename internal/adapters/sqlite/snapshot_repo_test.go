package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func TestSnapshotRepository_Replace_WipesAndReloads(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedAccountAndCategory(t, db, ports.SeededUserID, "old-acc", "old-cat")

	oldCatID := "old-cat"
	oldPosting := mustPosting(t, "old-post", "old-acc", -50000, "USD", &oldCatID)
	oldTxn, err := ledger.NewOutflow("old-txn", ports.SeededUserID, mustDate(t, 2026, time.August, 1), "Old transaction", []ledger.Posting{oldPosting})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}
	if err := NewTransactionRepository(db).Create(ctx, ports.SeededUserID, oldTxn, nil); err != nil {
		t.Fatalf("seed old transaction: %v", err)
	}

	newAcc := mustAccount(t, "new-acc", ports.SeededUserID, "New Account")
	newCat := mustCategory(t, "new-cat", ports.SeededUserID, nil, "New Category", ledger.CategoryKindExpense)
	newCatID := "new-cat"
	newPosting := mustPosting(t, "new-post", "new-acc", -12300, "USD", &newCatID)
	newTxn, err := ledger.NewOutflow("new-txn", ports.SeededUserID, mustDate(t, 2026, time.August, 2), "New transaction", []ledger.Posting{newPosting})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}

	repo := NewSnapshotRepository(db)
	snapshot := ports.Snapshot{
		Accounts:   []ledger.Account{newAcc},
		Categories: []ledger.Category{newCat},
		Transactions: []ports.SnapshotTransaction{
			{Transaction: newTxn, Tags: []ledger.Tag{mustTag(t, "restored")}},
		},
	}
	if err := repo.Replace(ctx, ports.SeededUserID, snapshot); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	accounts, err := NewAccountRepository(db).List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List accounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID() != "new-acc" {
		t.Errorf("accounts after Replace = %+v, want exactly [new-acc]", accounts)
	}

	categories, err := NewCategoryRepository(db).List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List categories: %v", err)
	}
	if len(categories) != 1 || categories[0].ID() != "new-cat" {
		t.Errorf("categories after Replace = %+v, want exactly [new-cat]", categories)
	}

	if _, _, err := NewTransactionRepository(db).Get(ctx, ports.SeededUserID, "old-txn"); err == nil {
		t.Error("old-txn still exists after Replace, want it gone")
	}
	gotTxn, gotTags, err := NewTransactionRepository(db).Get(ctx, ports.SeededUserID, "new-txn")
	if err != nil {
		t.Fatalf("Get new-txn: %v", err)
	}
	if gotTxn.Description() != "New transaction" {
		t.Errorf("new-txn description = %q, want %q", gotTxn.Description(), "New transaction")
	}
	if len(gotTags) != 1 || gotTags[0].String() != "restored" {
		t.Errorf("new-txn tags = %+v, want [restored]", gotTags)
	}
}

func TestSnapshotRepository_Replace_RejectsActorMismatch(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	mismatched := mustAccount(t, "acc-1", otherUserID, "Not Mine")
	repo := NewSnapshotRepository(db)
	err := repo.Replace(ctx, ports.SeededUserID, ports.Snapshot{Accounts: []ledger.Account{mismatched}})
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Errorf("Replace with mismatched actor: err = %v, want errs.NotAllowed", err)
	}
}

// TestSnapshotRepository_Replace_PartialFailureLeavesExistingDataUntouched
// is issue #226's atomicity requirement, verified as a real test against
// the real database rather than by inspection: a snapshot that fails
// partway through its own writes (two accounts sharing a name, so the
// second INSERT hits the same UNIQUE(user_id, name) constraint
// AccountRepository.Create relies on) must roll back everything Replace
// had already done in that same call -- both the wipe of the actor's prior
// data and any of the new rows it had managed to insert before the
// failure -- leaving the actor's original accounts/categories/transactions
// exactly as they were.
func TestSnapshotRepository_Replace_PartialFailureLeavesExistingDataUntouched(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedAccountAndCategory(t, db, ports.SeededUserID, "keep-acc", "keep-cat")

	keepCatID := "keep-cat"
	keepPosting := mustPosting(t, "keep-post", "keep-acc", -7500, "USD", &keepCatID)
	keepTxn, err := ledger.NewOutflow("keep-txn", ports.SeededUserID, mustDate(t, 2026, time.August, 1), "Keep me", []ledger.Posting{keepPosting})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}
	if err := NewTransactionRepository(db).Create(ctx, ports.SeededUserID, keepTxn, nil); err != nil {
		t.Fatalf("seed keep-txn: %v", err)
	}

	// Two accounts with the same name: the first insert inside Replace's
	// transaction succeeds, the second hits accounts' UNIQUE(user_id, name)
	// constraint and fails the whole call.
	dup1 := mustAccount(t, "dup-1", ports.SeededUserID, "Duplicate Name")
	dup2 := mustAccount(t, "dup-2", ports.SeededUserID, "Duplicate Name")

	repo := NewSnapshotRepository(db)
	err = repo.Replace(ctx, ports.SeededUserID, ports.Snapshot{Accounts: []ledger.Account{dup1, dup2}})
	if err == nil {
		t.Fatal("Replace with a duplicate account name: want an error, got nil")
	}
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.Conflict {
		t.Errorf("Replace with a duplicate account name: err = %v, want errs.Conflict", err)
	}

	// Nothing from the failed snapshot should exist...
	if _, err := NewAccountRepository(db).Get(ctx, ports.SeededUserID, "dup-1"); err == nil {
		t.Error("dup-1 exists after a rolled-back Replace, want it absent")
	}
	if _, err := NewAccountRepository(db).Get(ctx, ports.SeededUserID, "dup-2"); err == nil {
		t.Error("dup-2 exists after a rolled-back Replace, want it absent")
	}

	// ...and everything from before the call should be untouched.
	accounts, err := NewAccountRepository(db).List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List accounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID() != "keep-acc" {
		t.Errorf("accounts after rolled-back Replace = %+v, want exactly [keep-acc] (untouched)", accounts)
	}
	categories, err := NewCategoryRepository(db).List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List categories: %v", err)
	}
	if len(categories) != 1 || categories[0].ID() != "keep-cat" {
		t.Errorf("categories after rolled-back Replace = %+v, want exactly [keep-cat] (untouched)", categories)
	}
	gotTxn, _, err := NewTransactionRepository(db).Get(ctx, ports.SeededUserID, "keep-txn")
	if err != nil {
		t.Fatalf("keep-txn is gone after a rolled-back Replace, want it untouched: %v", err)
	}
	if gotTxn.Description() != "Keep me" {
		t.Errorf("keep-txn description = %q, want %q (untouched)", gotTxn.Description(), "Keep me")
	}
}
