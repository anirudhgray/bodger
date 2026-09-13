package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/budgeting"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func mustBudgetLine(t *testing.T, budgetID, categoryID string, amountMinor int64) budgeting.BudgetLine {
	t.Helper()
	l, err := budgeting.NewBudgetLine("line-"+categoryID, budgetID, categoryID, amountMinor, false)
	if err != nil {
		t.Fatalf("budgeting.NewBudgetLine: %v", err)
	}
	return l
}

func mustBudget(t *testing.T, id, userID string, lines []budgeting.BudgetLine) budgeting.Budget {
	t.Helper()
	b, err := budgeting.NewBudget(id, userID, id+"-name", budgeting.PeriodTypeMonthly, "USD",
		mustDate(t, 2026, time.January, 1), lines)
	if err != nil {
		t.Fatalf("budgeting.NewBudget: %v", err)
	}
	return b
}

// seedCategoryForBudget creates a category satisfying budget_lines'
// category_id foreign key.
func seedCategoryForBudget(t *testing.T, db *DB, userID, categoryID string) {
	t.Helper()
	repo := NewCategoryRepository(db)
	cat := mustCategory(t, categoryID, userID, nil, categoryID+"-name", ledger.CategoryKindExpense)
	if err := repo.Create(context.Background(), userID, cat); err != nil {
		t.Fatalf("seed category %q: %v", categoryID, err)
	}
}

func TestBudgetRepository_CreateGet(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedCategoryForBudget(t, db, ports.SeededUserID, "cat-groceries")

	lines := []budgeting.BudgetLine{mustBudgetLine(t, "b1", "cat-groceries", 50000)}
	budget := mustBudget(t, "b1", ports.SeededUserID, lines)

	repo := NewBudgetRepository(db)
	if err := repo.Create(ctx, ports.SeededUserID, budget); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "b1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != budget.ID() || got.UserID() != budget.UserID() || got.Name() != budget.Name() ||
		got.PeriodType() != budget.PeriodType() || got.Currency() != budget.Currency() || got.StartsOn() != budget.StartsOn() {
		t.Errorf("Get() = %+v, want a round trip of %+v", got, budget)
	}
	if got.Archived() {
		t.Error("a freshly created budget must not be archived")
	}
	gotLines := got.Lines()
	if len(gotLines) != 1 || gotLines[0].CategoryID() != "cat-groceries" || gotLines[0].AmountMinor() != 50000 {
		t.Errorf("Get().Lines() = %+v, want the one seeded line", gotLines)
	}
}

func TestBudgetRepository_Create_RejectsMismatchedActor(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	budget := mustBudget(t, "b1", ports.SeededUserID, nil)

	repo := NewBudgetRepository(db)
	err := repo.Create(ctx, otherUserID, budget)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("Create(mismatched actor) error = %v, want *errs.Error with code NotAllowed", err)
	}
}

func TestBudgetRepository_Get_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewBudgetRepository(db)
	_, err := repo.Get(context.Background(), ports.SeededUserID, "no-such-budget")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Get(unknown) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestBudgetRepository_Get_CrossUserIsolation(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)

	budget := mustBudget(t, "b1", ports.SeededUserID, nil)
	repo := NewBudgetRepository(db)
	if err := repo.Create(ctx, ports.SeededUserID, budget); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := repo.Get(ctx, otherUserID, "b1")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Get(other user's budget) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestBudgetRepository_List(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)

	repo := NewBudgetRepository(db)
	if err := repo.Create(ctx, ports.SeededUserID, mustBudget(t, "budget-a", ports.SeededUserID, nil)); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, mustBudget(t, "budget-b", ports.SeededUserID, nil)); err != nil {
		t.Fatalf("Create b: %v", err)
	}
	if err := repo.Create(ctx, otherUserID, mustBudget(t, "budget-other", otherUserID, nil)); err != nil {
		t.Fatalf("Create other: %v", err)
	}

	got, err := repo.List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d budgets, want 2 (never another user's)", len(got))
	}
	// Every insert lands under the same frozen-clock instant, so the tie
	// is broken by id DESC — the most recently *created* (highest id in
	// insertion order here) budget comes first, the same convention
	// TestImportBatchRepository_List documents.
	if got[0].ID() != "budget-b" || got[1].ID() != "budget-a" {
		t.Errorf("List order = [%s, %s], want [budget-b, budget-a]", got[0].ID(), got[1].ID())
	}
}

func TestBudgetRepository_Update(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedCategoryForBudget(t, db, ports.SeededUserID, "cat-groceries")
	seedCategoryForBudget(t, db, ports.SeededUserID, "cat-rent")

	repo := NewBudgetRepository(db)
	budget := mustBudget(t, "b1", ports.SeededUserID, []budgeting.BudgetLine{mustBudgetLine(t, "b1", "cat-groceries", 50000)})
	if err := repo.Create(ctx, ports.SeededUserID, budget); err != nil {
		t.Fatalf("Create: %v", err)
	}

	archivedOn := mustDate(t, 2026, time.June, 1)
	updated, err := budgeting.NewBudget(budget.ID(), budget.UserID(), "renamed", budget.PeriodType(), budget.Currency(), budget.StartsOn(),
		[]budgeting.BudgetLine{mustBudgetLine(t, "b1", "cat-rent", 120000)},
		budgeting.WithArchivedAt(archivedOn))
	if err != nil {
		t.Fatalf("NewBudget(updated): %v", err)
	}

	if err := repo.Update(ctx, ports.SeededUserID, updated); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "b1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name() != "renamed" {
		t.Errorf("Name() after Update = %q, want %q", got.Name(), "renamed")
	}
	if archived, ok := got.ArchivedAt(); !ok || archived != archivedOn {
		t.Errorf("ArchivedAt() after Update = (%v, %v), want (%v, true)", archived, ok, archivedOn)
	}
	lines := got.Lines()
	if len(lines) != 1 || lines[0].CategoryID() != "cat-rent" || lines[0].AmountMinor() != 120000 {
		t.Errorf("Lines() after Update = %+v, want the replaced line only", lines)
	}
}

func TestBudgetRepository_Update_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewBudgetRepository(db)
	budget := mustBudget(t, "no-such-budget", ports.SeededUserID, nil)
	err := repo.Update(context.Background(), ports.SeededUserID, budget)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Update(unknown) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestBudgetRepository_Update_RejectsMismatchedActor(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)

	repo := NewBudgetRepository(db)
	budget := mustBudget(t, "b1", ports.SeededUserID, nil)
	if err := repo.Create(ctx, ports.SeededUserID, budget); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := repo.Update(ctx, otherUserID, budget)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("Update(mismatched actor) error = %v, want *errs.Error with code NotAllowed", err)
	}
}
