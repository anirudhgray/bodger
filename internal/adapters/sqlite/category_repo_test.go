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

func mustCategory(t *testing.T, id, userID string, parentID *string, name string, kind ledger.CategoryKind) ledger.Category {
	t.Helper()
	c, err := ledger.NewCategory(id, userID, parentID, name, kind, 0, nil)
	if err != nil {
		t.Fatalf("ledger.NewCategory: %v", err)
	}
	return c
}

func TestCategoryRepository_CreateGetList(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewCategoryRepository(db)
	ctx := context.Background()

	food := mustCategory(t, "cat-food", ports.SeededUserID, nil, "Food", ledger.CategoryKindExpense)
	if err := repo.Create(ctx, ports.SeededUserID, food); err != nil {
		t.Fatalf("Create top-level: %v", err)
	}

	parentID := "cat-food"
	groceries := mustCategory(t, "cat-groceries", ports.SeededUserID, &parentID, "Groceries", ledger.CategoryKindExpense)
	if err := repo.Create(ctx, ports.SeededUserID, groceries); err != nil {
		t.Fatalf("Create child: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "cat-groceries")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gotParent, ok := got.ParentID()
	if !ok || gotParent != "cat-food" {
		t.Errorf("Get(cat-groceries).ParentID() = (%q, %v), want (cat-food, true)", gotParent, ok)
	}

	list, err := repo.List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("List = %d categories, want 2", len(list))
	}
}

func TestCategoryRepository_List_FiltersByActor(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewCategoryRepository(db)
	ctx := context.Background()

	seedOtherUser(t, db, otherUserID)

	mine := mustCategory(t, "cat-mine", ports.SeededUserID, nil, "Mine", ledger.CategoryKindExpense)
	theirs := mustCategory(t, "cat-theirs", otherUserID, nil, "Theirs", ledger.CategoryKindExpense)
	if err := repo.Create(ctx, ports.SeededUserID, mine); err != nil {
		t.Fatalf("Create mine: %v", err)
	}
	if err := repo.Create(ctx, otherUserID, theirs); err != nil {
		t.Fatalf("Create theirs: %v", err)
	}

	list, err := repo.List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID() != "cat-mine" {
		t.Errorf("List(seeded user) = %+v, want only cat-mine", list)
	}
}

func TestCategoryRepository_Create_DuplicateSiblingNameConflicts(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewCategoryRepository(db)
	ctx := context.Background()

	a := mustCategory(t, "cat-a", ports.SeededUserID, nil, "Food", ledger.CategoryKindExpense)
	b := mustCategory(t, "cat-b", ports.SeededUserID, nil, "Food", ledger.CategoryKindExpense)

	if err := repo.Create(ctx, ports.SeededUserID, a); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	err := repo.Create(ctx, ports.SeededUserID, b)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.Conflict {
		t.Errorf("Create duplicate top-level sibling name: err = %v, want errs.Conflict", err)
	}
}

func TestCategoryRepository_Create_SameNameDifferentParentsAllowed(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewCategoryRepository(db)
	ctx := context.Background()

	parent1 := mustCategory(t, "cat-p1", ports.SeededUserID, nil, "Home", ledger.CategoryKindExpense)
	parent2 := mustCategory(t, "cat-p2", ports.SeededUserID, nil, "Work", ledger.CategoryKindExpense)
	if err := repo.Create(ctx, ports.SeededUserID, parent1); err != nil {
		t.Fatalf("Create parent1: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, parent2); err != nil {
		t.Fatalf("Create parent2: %v", err)
	}

	p1, p2 := "cat-p1", "cat-p2"
	child1 := mustCategory(t, "cat-c1", ports.SeededUserID, &p1, "Supplies", ledger.CategoryKindExpense)
	child2 := mustCategory(t, "cat-c2", ports.SeededUserID, &p2, "Supplies", ledger.CategoryKindExpense)
	if err := repo.Create(ctx, ports.SeededUserID, child1); err != nil {
		t.Errorf("Create child1 (Supplies under Home): %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, child2); err != nil {
		t.Errorf("Create child2 (Supplies under Work): %v", err)
	}
}

// TestCategoryRepository_Update_RejectsDeepCycle covers the cycle NewCategory
// itself can't catch (it only rejects a category being its own immediate
// parent): A -> B -> C, then trying to set A's parent to C.
func TestCategoryRepository_Update_RejectsDeepCycle(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewCategoryRepository(db)
	ctx := context.Background()

	a := mustCategory(t, "cat-a", ports.SeededUserID, nil, "A", ledger.CategoryKindExpense)
	if err := repo.Create(ctx, ports.SeededUserID, a); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	aID := "cat-a"
	b := mustCategory(t, "cat-b", ports.SeededUserID, &aID, "B", ledger.CategoryKindExpense)
	if err := repo.Create(ctx, ports.SeededUserID, b); err != nil {
		t.Fatalf("Create b: %v", err)
	}
	bID := "cat-b"
	c := mustCategory(t, "cat-c", ports.SeededUserID, &bID, "C", ledger.CategoryKindExpense)
	if err := repo.Create(ctx, ports.SeededUserID, c); err != nil {
		t.Fatalf("Create c: %v", err)
	}

	cID := "cat-c"
	aWithCycle := mustCategory(t, "cat-a", ports.SeededUserID, &cID, "A", ledger.CategoryKindExpense)
	err := repo.Update(ctx, ports.SeededUserID, aWithCycle)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.InvalidInput {
		t.Errorf("Update creating a cycle: err = %v, want errs.InvalidInput", err)
	}
}

// TestCategoryRepository_Update_ArchivedAtAndSortOrderRoundTrip covers the
// two fields issue #22 added: sort_order is a plain int, and archived_at
// round-trips through the nullable-date convention the rest of this
// package uses for optional dates.
func TestCategoryRepository_Update_ArchivedAtAndSortOrderRoundTrip(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewCategoryRepository(db)
	ctx := context.Background()

	food := mustCategory(t, "cat-food", ports.SeededUserID, nil, "Food", ledger.CategoryKindExpense)
	if err := repo.Create(ctx, ports.SeededUserID, food); err != nil {
		t.Fatalf("Create: %v", err)
	}

	archivedOn, err := domain.NewDate(2026, time.March, 3)
	if err != nil {
		t.Fatalf("domain.NewDate: %v", err)
	}
	archived, err := ledger.NewCategory("cat-food", ports.SeededUserID, nil, "Food", ledger.CategoryKindExpense, 9, &archivedOn)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if err := repo.Update(ctx, ports.SeededUserID, archived); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "cat-food")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gotArchivedAt, isArchived := got.ArchivedAt()
	if got.SortOrder() != 9 || !isArchived || !gotArchivedAt.Equal(archivedOn) {
		t.Errorf("Get after Update = sortOrder=%d archivedAt=%s/%v, want sortOrder=9 archivedAt=2026-03-03/true",
			got.SortOrder(), gotArchivedAt, isArchived)
	}
}

func TestCategoryRepository_Get_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewCategoryRepository(db)

	_, err := repo.Get(context.Background(), ports.SeededUserID, "does-not-exist")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("Get(missing): err = %v, want errs.NotFound", err)
	}
}
