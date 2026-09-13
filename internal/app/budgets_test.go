package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain/budgeting"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func mustCreateCategory(t *testing.T, svc *app.Service, name string) string {
	t.Helper()
	result, err := svc.CreateCategory(context.Background(), app.CreateCategoryCommand{
		ActorID: testActorID,
		Name:    name,
		Kind:    "expense",
	})
	if err != nil {
		t.Fatalf("CreateCategory(%q): %v", name, err)
	}
	return result.Category.ID()
}

func TestCreateBudget(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	groceries := mustCreateCategory(t, svc, "Groceries")

	result, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{
		ActorID:  testActorID,
		Name:     "Monthly plan",
		Currency: "USD",
		StartsOn: "2026-01-01",
		Lines: []app.BudgetLineInput{
			{CategoryRef: groceries, Amount: "500.00"},
		},
	})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}
	if result.Budget.Name() != "Monthly plan" {
		t.Errorf("Name = %q, want %q", result.Budget.Name(), "Monthly plan")
	}
	if result.Budget.Currency() != "USD" {
		t.Errorf("Currency = %q, want USD", result.Budget.Currency())
	}
	if result.Budget.PeriodType() != budgeting.PeriodTypeMonthly {
		t.Errorf("PeriodType = %q, want monthly", result.Budget.PeriodType())
	}
	if result.Budget.Archived() {
		t.Error("a freshly created budget should not be archived")
	}
	lines := result.Budget.Lines()
	if len(lines) != 1 {
		t.Fatalf("len(Lines) = %d, want 1", len(lines))
	}
	if lines[0].AmountMinor() != 50000 {
		t.Errorf("AmountMinor = %d, want 50000", lines[0].AmountMinor())
	}
	if lines[0].CategoryID() != groceries {
		t.Errorf("CategoryID = %q, want %q", lines[0].CategoryID(), groceries)
	}
}

func TestCreateBudget_DefaultsStartsOnToToday(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	result, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{
		ActorID:  testActorID,
		Name:     "No explicit start",
		Currency: "USD",
	})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}
	if result.Budget.StartsOn().String() != "2026-03-03" {
		t.Errorf("StartsOn = %s, want 2026-03-03", result.Budget.StartsOn())
	}
}

func TestCreateBudget_RejectsEmptyName(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	_, err := svc.CreateBudget(context.Background(), app.CreateBudgetCommand{ActorID: testActorID, Currency: "USD"})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCreateBudget_RejectsUnknownCurrency(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	_, err := svc.CreateBudget(context.Background(), app.CreateBudgetCommand{
		ActorID: testActorID, Name: "Plan", Currency: "NOTREAL",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCreateBudget_RejectsDuplicateCategoryLines(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	groceries := mustCreateCategory(t, svc, "Groceries")

	_, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{
		ActorID: testActorID, Name: "Plan", Currency: "USD",
		Lines: []app.BudgetLineInput{
			{CategoryRef: groceries, Amount: "100.00"},
			{CategoryRef: groceries, Amount: "50.00"},
		},
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCreateBudget_RejectsUnknownCategory(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	_, err := svc.CreateBudget(context.Background(), app.CreateBudgetCommand{
		ActorID: testActorID, Name: "Plan", Currency: "USD",
		Lines: []app.BudgetLineInput{{CategoryRef: "does-not-exist", Amount: "10.00"}},
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestCreateBudget_RejectsNonPositiveLineAmount(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	groceries := mustCreateCategory(t, svc, "Groceries")
	_, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{
		ActorID: testActorID, Name: "Plan", Currency: "USD",
		Lines: []app.BudgetLineInput{{CategoryRef: groceries, Amount: "0.00"}},
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestUpdateBudget(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	created, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{ActorID: testActorID, Name: "Old name", Currency: "USD"})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}

	updated, err := svc.UpdateBudget(ctx, app.UpdateBudgetCommand{
		ActorID:  testActorID,
		BudgetID: created.Budget.ID(),
		Name:     "New name",
		StartsOn: "2026-02-01",
	})
	if err != nil {
		t.Fatalf("UpdateBudget: %v", err)
	}
	if updated.Budget.Name() != "New name" {
		t.Errorf("Name = %q, want %q", updated.Budget.Name(), "New name")
	}
	if updated.Budget.StartsOn().String() != "2026-02-01" {
		t.Errorf("StartsOn = %s, want 2026-02-01", updated.Budget.StartsOn())
	}
	if updated.Budget.Currency() != "USD" {
		t.Error("UpdateBudget must not change currency")
	}
}

func TestUpdateBudget_UnknownBudgetIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	_, err := svc.UpdateBudget(context.Background(), app.UpdateBudgetCommand{
		ActorID: testActorID, BudgetID: "does-not-exist", Name: "X",
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestArchiveBudget(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	groceries := mustCreateCategory(t, svc, "Groceries")
	created, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{
		ActorID: testActorID, Name: "Plan", Currency: "USD",
		Lines: []app.BudgetLineInput{{CategoryRef: groceries, Amount: "100.00"}},
	})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}

	archived, err := svc.ArchiveBudget(ctx, app.ArchiveBudgetCommand{ActorID: testActorID, BudgetID: created.Budget.ID()})
	if err != nil {
		t.Fatalf("ArchiveBudget: %v", err)
	}
	if !archived.Budget.Archived() {
		t.Fatal("budget should be archived")
	}
	if archivedAt, ok := archived.Budget.ArchivedAt(); !ok || archivedAt.String() != "2026-06-15" {
		t.Errorf("ArchivedAt = %v, %v, want 2026-06-15, true", archivedAt, ok)
	}

	// Archiving twice is idempotent and doesn't move the archive date.
	archivedAgain, err := svc.ArchiveBudget(ctx, app.ArchiveBudgetCommand{ActorID: testActorID, BudgetID: created.Budget.ID()})
	if err != nil {
		t.Fatalf("ArchiveBudget (again): %v", err)
	}
	if got, _ := archivedAgain.Budget.ArchivedAt(); got.String() != "2026-06-15" {
		t.Errorf("re-archiving moved ArchivedAt to %v", got)
	}

	// An archived budget's lines and own record stay fully queryable —
	// #243's actuals/history reads depend on this.
	fetched, err := svc.GetBudget(ctx, app.GetBudgetQuery{ActorID: testActorID, BudgetID: created.Budget.ID()})
	if err != nil {
		t.Fatalf("GetBudget after archive: %v", err)
	}
	if len(fetched.Budget.Lines()) != 1 {
		t.Errorf("an archived budget's lines must remain queryable, got %d lines", len(fetched.Budget.Lines()))
	}
	listed, err := svc.ListBudgets(ctx, app.ListBudgetsQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListBudgets after archive: %v", err)
	}
	if len(listed.Budgets) != 1 {
		t.Errorf("ListBudgets must still return an archived budget, got %d", len(listed.Budgets))
	}
}

func TestAddBudgetLine(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	groceries := mustCreateCategory(t, svc, "Groceries")
	rent := mustCreateCategory(t, svc, "Rent")
	created, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{
		ActorID: testActorID, Name: "Plan", Currency: "USD",
		Lines: []app.BudgetLineInput{{CategoryRef: groceries, Amount: "100.00"}},
	})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}

	updated, err := svc.AddBudgetLine(ctx, app.AddBudgetLineCommand{
		ActorID: testActorID, BudgetID: created.Budget.ID(), CategoryRef: rent, Amount: "1200.00",
	})
	if err != nil {
		t.Fatalf("AddBudgetLine: %v", err)
	}
	if len(updated.Budget.Lines()) != 2 {
		t.Fatalf("len(Lines) = %d, want 2", len(updated.Budget.Lines()))
	}
}

func TestAddBudgetLine_RejectsDuplicateCategory(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	groceries := mustCreateCategory(t, svc, "Groceries")
	created, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{
		ActorID: testActorID, Name: "Plan", Currency: "USD",
		Lines: []app.BudgetLineInput{{CategoryRef: groceries, Amount: "100.00"}},
	})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}

	_, err = svc.AddBudgetLine(ctx, app.AddBudgetLineCommand{
		ActorID: testActorID, BudgetID: created.Budget.ID(), CategoryRef: groceries, Amount: "50.00",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestUpdateBudgetLine(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	groceries := mustCreateCategory(t, svc, "Groceries")
	created, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{
		ActorID: testActorID, Name: "Plan", Currency: "USD",
		Lines: []app.BudgetLineInput{{CategoryRef: groceries, Amount: "100.00"}},
	})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}
	lineID := created.Budget.Lines()[0].ID()

	updated, err := svc.UpdateBudgetLine(ctx, app.UpdateBudgetLineCommand{
		ActorID: testActorID, BudgetID: created.Budget.ID(), LineID: lineID, Amount: "250.00", Rollover: true,
	})
	if err != nil {
		t.Fatalf("UpdateBudgetLine: %v", err)
	}
	got := updated.Budget.Lines()[0]
	if got.AmountMinor() != 25000 {
		t.Errorf("AmountMinor = %d, want 25000", got.AmountMinor())
	}
	if !got.Rollover() {
		t.Error("Rollover should be true")
	}
	if got.CategoryID() != groceries {
		t.Error("UpdateBudgetLine must not change the line's category")
	}
}

func TestUpdateBudgetLine_UnknownLineIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	created, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{ActorID: testActorID, Name: "Plan", Currency: "USD"})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}
	_, err = svc.UpdateBudgetLine(ctx, app.UpdateBudgetLineCommand{
		ActorID: testActorID, BudgetID: created.Budget.ID(), LineID: "does-not-exist", Amount: "10.00",
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestRemoveBudgetLine(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	groceries := mustCreateCategory(t, svc, "Groceries")
	created, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{
		ActorID: testActorID, Name: "Plan", Currency: "USD",
		Lines: []app.BudgetLineInput{{CategoryRef: groceries, Amount: "100.00"}},
	})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}
	lineID := created.Budget.Lines()[0].ID()

	updated, err := svc.RemoveBudgetLine(ctx, app.RemoveBudgetLineCommand{
		ActorID: testActorID, BudgetID: created.Budget.ID(), LineID: lineID,
	})
	if err != nil {
		t.Fatalf("RemoveBudgetLine: %v", err)
	}
	if len(updated.Budget.Lines()) != 0 {
		t.Errorf("len(Lines) = %d, want 0 — removing a budget's last line must be allowed", len(updated.Budget.Lines()))
	}
}

func TestRemoveBudgetLine_UnknownLineIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	created, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{ActorID: testActorID, Name: "Plan", Currency: "USD"})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}
	_, err = svc.RemoveBudgetLine(ctx, app.RemoveBudgetLineCommand{
		ActorID: testActorID, BudgetID: created.Budget.ID(), LineID: "does-not-exist",
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestGetBudget_AnotherActorsBudgetIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	created, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{ActorID: testActorID, Name: "Plan", Currency: "USD"})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}
	_, err = svc.GetBudget(ctx, app.GetBudgetQuery{ActorID: "someone-else", BudgetID: created.Budget.ID()})
	wantErrCode(t, err, errs.NotFound)
}

func TestListBudgets(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	if _, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{ActorID: testActorID, Name: "A", Currency: "USD"}); err != nil {
		t.Fatalf("CreateBudget A: %v", err)
	}
	if _, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{ActorID: testActorID, Name: "B", Currency: "USD"}); err != nil {
		t.Fatalf("CreateBudget B: %v", err)
	}
	result, err := svc.ListBudgets(ctx, app.ListBudgetsQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListBudgets: %v", err)
	}
	if len(result.Budgets) != 2 {
		t.Fatalf("len(Budgets) = %d, want 2", len(result.Budgets))
	}
	// Most recently created first.
	if result.Budgets[0].Name() != "B" {
		t.Errorf("Budgets[0].Name = %q, want %q (most recent first)", result.Budgets[0].Name(), "B")
	}
}
