package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func mustMoney(t *testing.T, minor int64, currency string) money.Money {
	t.Helper()
	m, err := money.NewMoney(minor, currency)
	if err != nil {
		t.Fatalf("money.NewMoney: %v", err)
	}
	return m
}

func mustAccount(t *testing.T, id, userID, name string) ledger.Account {
	t.Helper()
	a, err := ledger.NewAccount(id, userID, name, ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, nil, 0, nil)
	if err != nil {
		t.Fatalf("ledger.NewAccount: %v", err)
	}
	return a
}

// otherUserID is a second, non-seeded user for cross-user isolation tests.
// It doesn't need a row in the users table: nothing here exercises the
// foreign key from accounts.user_id (that constraint's job is a different
// test's concern), only that queries scoped to one actor never surface
// another's rows.
const otherUserID = "11111111-1111-1111-1111-111111111111"

func TestAccountRepository_CreateGetList(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAccountRepository(db)
	ctx := context.Background()

	acc := mustAccount(t, "acc-1", ports.SeededUserID, "HDFC Savings")
	if err := repo.Create(ctx, ports.SeededUserID, acc); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "acc-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name() != "HDFC Savings" || got.UserID() != ports.SeededUserID {
		t.Errorf("Get returned %+v", got)
	}

	list, err := repo.List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID() != "acc-1" {
		t.Errorf("List = %+v, want one account acc-1", list)
	}
}

func TestAccountRepository_Create_RejectsActorMismatch(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAccountRepository(db)
	ctx := context.Background()

	acc := mustAccount(t, "acc-1", ports.SeededUserID, "HDFC Savings")
	err := repo.Create(ctx, otherUserID, acc)
	if err == nil {
		t.Fatal("Create with mismatched actor: want error, got nil")
	}
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Errorf("Create with mismatched actor: err = %v, want errs.NotAllowed", err)
	}
}

// TestAccountRepository_List_FiltersByActor proves the user_id predicate is
// really applied: two users' accounts exist in the same database, and each
// actor must only ever see their own. If a future change dropped the
// WHERE user_id = ? clause, this test would start failing (both accounts
// would appear for both actors).
func TestAccountRepository_List_FiltersByActor(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAccountRepository(db)
	ctx := context.Background()

	seedOtherUser(t, db, otherUserID)

	mine := mustAccount(t, "acc-mine", ports.SeededUserID, "My Account")
	theirs := mustAccount(t, "acc-theirs", otherUserID, "Their Account")
	if err := repo.Create(ctx, ports.SeededUserID, mine); err != nil {
		t.Fatalf("Create mine: %v", err)
	}
	if err := repo.Create(ctx, otherUserID, theirs); err != nil {
		t.Fatalf("Create theirs: %v", err)
	}

	myList, err := repo.List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List(seeded user): %v", err)
	}
	if len(myList) != 1 || myList[0].ID() != "acc-mine" {
		t.Errorf("List(seeded user) = %+v, want only acc-mine", myList)
	}

	// Get must also refuse to cross the actor boundary, not just List.
	if _, err := repo.Get(ctx, ports.SeededUserID, "acc-theirs"); err == nil {
		t.Error("Get(seeded user, acc-theirs): want NotFound, got nil error")
	} else {
		var e *errs.Error
		if !errors.As(err, &e) || e.Code != errs.NotFound {
			t.Errorf("Get(seeded user, acc-theirs): err = %v, want errs.NotFound", err)
		}
	}
}

func TestAccountRepository_Get_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAccountRepository(db)

	_, err := repo.Get(context.Background(), ports.SeededUserID, "does-not-exist")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("Get(missing): err = %v, want errs.NotFound", err)
	}
}

func TestAccountRepository_Create_DuplicateNameConflicts(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAccountRepository(db)
	ctx := context.Background()

	acc1 := mustAccount(t, "acc-1", ports.SeededUserID, "Savings")
	acc2 := mustAccount(t, "acc-2", ports.SeededUserID, "Savings")

	if err := repo.Create(ctx, ports.SeededUserID, acc1); err != nil {
		t.Fatalf("Create acc1: %v", err)
	}
	err := repo.Create(ctx, ports.SeededUserID, acc2)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.Conflict {
		t.Errorf("Create duplicate name: err = %v, want errs.Conflict", err)
	}
}

func TestAccountRepository_Update(t *testing.T) {
	db, clk := newTestDB(t)
	repo := NewAccountRepository(db)
	ctx := context.Background()

	acc := mustAccount(t, "acc-1", ports.SeededUserID, "Savings")
	if err := repo.Create(ctx, ports.SeededUserID, acc); err != nil {
		t.Fatalf("Create: %v", err)
	}

	clk.Advance(time.Minute)
	archivedOn, err := domain.NewDate(2026, time.January, 15)
	if err != nil {
		t.Fatalf("domain.NewDate: %v", err)
	}
	institution := "HDFC Bank"
	renamed, err := ledger.NewAccount("acc-1", ports.SeededUserID, "Renamed", ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, &institution, 4, &archivedOn)
	if err != nil {
		t.Fatalf("NewAccount: %v", err)
	}
	if err := repo.Update(ctx, ports.SeededUserID, renamed); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "acc-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gotInstitution, hasInstitution := got.Institution()
	gotArchivedAt, isArchived := got.ArchivedAt()
	if got.Name() != "Renamed" || !got.Archived() || !hasInstitution || gotInstitution != institution ||
		!isArchived || !gotArchivedAt.Equal(archivedOn) || got.SortOrder() != 4 {
		t.Errorf("Get after Update = %+v (institution=%q/%v, archivedAt=%s/%v, sortOrder=%d), want Renamed/archived/HDFC Bank/2026-01-15/sortOrder=4",
			got, gotInstitution, hasInstitution, gotArchivedAt, isArchived, got.SortOrder())
	}
}

func TestAccountRepository_Update_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAccountRepository(db)

	acc := mustAccount(t, "does-not-exist", ports.SeededUserID, "Ghost")
	err := repo.Update(context.Background(), ports.SeededUserID, acc)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("Update(missing): err = %v, want errs.NotFound", err)
	}
}
