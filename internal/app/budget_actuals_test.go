package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain"
)

func mustCreateBudget(t *testing.T, svc *app.Service, currency, startsOn string, lines ...app.BudgetLineInput) app.BudgetResult {
	t.Helper()
	result, err := svc.CreateBudget(context.Background(), app.CreateBudgetCommand{
		ActorID:  testActorID,
		Name:     "Test budget",
		Currency: currency,
		StartsOn: startsOn,
		Lines:    lines,
	})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}
	return result
}

func TestBudgetActuals_SumsSpendingWithinCategorySubtree(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	food := mustCategoryFixture(t, svc, "Food", "expense")
	groceries := mustSubcategoryFixture(t, svc, "Groceries", "expense", food.Category.ID())

	budget := mustCreateBudget(t, svc, "USD", "2026-08-01", app.BudgetLineInput{
		CategoryRef: food.Category.ID(), Amount: "500.00",
	})

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), CategoryRef: groceries.Category.ID(),
		Amount: "120.00", Date: "2026-08-05", Description: "Groceries",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	result, err := svc.BudgetActuals(ctx, app.BudgetActualsQuery{ActorID: testActorID, BudgetID: budget.Budget.ID(), Period: "2026-08-15"})
	if err != nil {
		t.Fatalf("BudgetActuals: %v", err)
	}
	if len(result.Lines) != 1 {
		t.Fatalf("len(Lines) = %d, want 1", len(result.Lines))
	}
	line := result.Lines[0]
	if got := line.Actual.AmountMinor(); got != 12000 {
		t.Errorf("Actual = %d, want 12000 (a subcategory posting rolled up into its parent's budget line)", got)
	}
	if got := line.Remaining.AmountMinor(); got != 38000 {
		t.Errorf("Remaining = %d, want 38000 (50000 budgeted - 12000 actual)", got)
	}
	if got := line.Utilisation; got != 0.24 {
		t.Errorf("Utilisation = %v, want 0.24 (12000/50000)", got)
	}
	if !result.From.Equal(mustDate(t, 2026, time.August, 1)) || !result.To.Equal(mustDate(t, 2026, time.August, 31)) {
		t.Errorf("From/To = %s..%s, want 2026-08-01..2026-08-31", result.From, result.To)
	}
}

func TestBudgetActuals_RefundReducesActual(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	food := mustCategoryFixture(t, svc, "Food", "expense")
	budget := mustCreateBudget(t, svc, "USD", "2026-08-01", app.BudgetLineInput{CategoryRef: food.Category.ID(), Amount: "500.00"})

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), CategoryRef: food.Category.ID(),
		Amount: "100.00", Date: "2026-08-05", Description: "Dinner",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), CategoryRef: food.Category.ID(),
		Amount: "30.00", Date: "2026-08-06", Description: "Refund",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}

	result, err := svc.BudgetActuals(ctx, app.BudgetActualsQuery{ActorID: testActorID, BudgetID: budget.Budget.ID(), Period: "2026-08-15"})
	if err != nil {
		t.Fatalf("BudgetActuals: %v", err)
	}
	if got := result.Lines[0].Actual.AmountMinor(); got != 7000 {
		t.Errorf("Actual = %d, want 7000 (10000 spend - 3000 refund)", got)
	}
}

// TestBudgetActuals_UnrelatedSiblingCategoryNotCounted proves the basic
// case: spending in a category outside a line's subtree doesn't count
// against it. It does not exercise categoryInSubtree's defensive filter
// specifically -- that guards against a split transaction (one
// transaction, multiple postings, multiple categories), which
// RecordOutflowCommand doesn't support creating yet (see its own doc
// comment), so there is no way to construct that scenario through the
// public Service API today.
func TestBudgetActuals_UnrelatedSiblingCategoryNotCounted(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	food := mustCategoryFixture(t, svc, "Food", "expense")
	transport := mustCategoryFixture(t, svc, "Transport", "expense")
	budget := mustCreateBudget(t, svc, "USD", "2026-08-01", app.BudgetLineInput{CategoryRef: food.Category.ID(), Amount: "500.00"})

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), CategoryRef: transport.Category.ID(),
		Amount: "80.00", Date: "2026-08-05", Description: "Bus pass",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	result, err := svc.BudgetActuals(ctx, app.BudgetActualsQuery{ActorID: testActorID, BudgetID: budget.Budget.ID(), Period: "2026-08-15"})
	if err != nil {
		t.Fatalf("BudgetActuals: %v", err)
	}
	if got := result.Lines[0].Actual.AmountMinor(); got != 0 {
		t.Errorf("Actual = %d, want 0 (Transport spending must not count against the Food line)", got)
	}
}

func TestBudgetActuals_CrossCurrencyConverts(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	inrAcc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	food := mustCategoryFixture(t, svc, "Food", "expense")
	budget := mustCreateBudget(t, svc, "USD", "2026-08-01", app.BudgetLineInput{CategoryRef: food.Category.ID(), Amount: "500.00"})

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: inrAcc.Account.ID(), CategoryRef: food.Category.ID(),
		Amount: "1000", Date: "2026-08-05", Description: "Lunch",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	seedRate(t, svc, "INR", "USD", "0.0115", "2026-08-05", "ecb")

	result, err := svc.BudgetActuals(ctx, app.BudgetActualsQuery{ActorID: testActorID, BudgetID: budget.Budget.ID(), Period: "2026-08-15"})
	if err != nil {
		t.Fatalf("BudgetActuals: %v", err)
	}
	if got := result.Lines[0].Actual.AmountMinor(); got != 1150 {
		t.Errorf("Actual = %d, want 1150 (1000 INR * 0.0115 = 11.50 USD)", got)
	}
	if len(result.Unconverted) != 0 {
		t.Errorf("Unconverted = %+v, want empty", result.Unconverted)
	}
}

func TestBudgetActuals_MissingRateReportedNotOmitted(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	inrAcc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	food := mustCategoryFixture(t, svc, "Food", "expense")
	budget := mustCreateBudget(t, svc, "USD", "2026-08-01", app.BudgetLineInput{CategoryRef: food.Category.ID(), Amount: "500.00"})

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: inrAcc.Account.ID(), CategoryRef: food.Category.ID(),
		Amount: "1000", Date: "2026-08-05", Description: "Lunch",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	// No INR/USD rate seeded.

	result, err := svc.BudgetActuals(ctx, app.BudgetActualsQuery{ActorID: testActorID, BudgetID: budget.Budget.ID(), Period: "2026-08-15"})
	if err != nil {
		t.Fatalf("BudgetActuals: %v, want success with the shortfall reported instead", err)
	}
	if got := result.Lines[0].Actual.AmountMinor(); got != 0 {
		t.Errorf("Actual = %d, want 0 (the only posting couldn't be converted)", got)
	}
	if len(result.Unconverted) != 1 {
		t.Fatalf("len(Unconverted) = %d, want 1", len(result.Unconverted))
	}
}

func TestBudgetActuals_NoActualsYet(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	food := mustCategoryFixture(t, svc, "Food", "expense")
	budget := mustCreateBudget(t, svc, "USD", "2026-08-01", app.BudgetLineInput{CategoryRef: food.Category.ID(), Amount: "500.00"})

	result, err := svc.BudgetActuals(ctx, app.BudgetActualsQuery{ActorID: testActorID, BudgetID: budget.Budget.ID(), Period: "2026-08-15"})
	if err != nil {
		t.Fatalf("BudgetActuals: %v", err)
	}
	line := result.Lines[0]
	if got := line.Actual.AmountMinor(); got != 0 {
		t.Errorf("Actual = %d, want 0", got)
	}
	if got := line.Remaining.AmountMinor(); got != 50000 {
		t.Errorf("Remaining = %d, want 50000 (full budgeted amount)", got)
	}
	if got := line.Utilisation; got != 0 {
		t.Errorf("Utilisation = %v, want 0", got)
	}
}

func TestBudgetActuals_RejectsPeriodBeforeBudgetStart(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	food := mustCategoryFixture(t, svc, "Food", "expense")
	budget := mustCreateBudget(t, svc, "USD", "2026-08-01", app.BudgetLineInput{CategoryRef: food.Category.ID(), Amount: "500.00"})

	if _, err := svc.BudgetActuals(ctx, app.BudgetActualsQuery{ActorID: testActorID, BudgetID: budget.Budget.ID(), Period: "2026-07-15"}); err == nil {
		t.Fatal("BudgetActuals with a period before the budget's StartsOn: want an error, got nil")
	}
}

func TestBudgetActuals_ArchivedBudgetHistoricalPeriodStillResolves(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	food := mustCategoryFixture(t, svc, "Food", "expense")
	budget := mustCreateBudget(t, svc, "USD", "2026-06-01", app.BudgetLineInput{CategoryRef: food.Category.ID(), Amount: "500.00"})

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), CategoryRef: food.Category.ID(),
		Amount: "50.00", Date: "2026-06-10", Description: "Groceries",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	if _, err := svc.ArchiveBudget(ctx, app.ArchiveBudgetCommand{ActorID: testActorID, BudgetID: budget.Budget.ID()}); err != nil {
		t.Fatalf("ArchiveBudget: %v", err)
	}

	result, err := svc.BudgetActuals(ctx, app.BudgetActualsQuery{ActorID: testActorID, BudgetID: budget.Budget.ID(), Period: "2026-06-15"})
	if err != nil {
		t.Fatalf("BudgetActuals on an archived budget's own historical period: %v", err)
	}
	if got := result.Lines[0].Actual.AmountMinor(); got != 5000 {
		t.Errorf("Actual = %d, want 5000 (archiving must not hide a budget's own history)", got)
	}
}

func TestBudgetHistory_ReturnsAscendingMonthsClampedToStart(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	food := mustCategoryFixture(t, svc, "Food", "expense")
	// Budget starts in July, so an August-anchored 6-month history should
	// only ever produce 2 periods (July, August) rather than erroring on
	// the other 4 months that don't exist for this budget yet.
	budget := mustCreateBudget(t, svc, "USD", "2026-07-01", app.BudgetLineInput{CategoryRef: food.Category.ID(), Amount: "500.00"})

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), CategoryRef: food.Category.ID(),
		Amount: "50.00", Date: "2026-07-10", Description: "Groceries",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), CategoryRef: food.Category.ID(),
		Amount: "70.00", Date: "2026-08-10", Description: "Groceries",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	result, err := svc.BudgetHistory(ctx, app.BudgetHistoryQuery{ActorID: testActorID, BudgetID: budget.Budget.ID(), Period: "2026-08-15"})
	if err != nil {
		t.Fatalf("BudgetHistory: %v", err)
	}
	if len(result.Periods) != 2 {
		t.Fatalf("len(Periods) = %d, want 2 (clamped to the budget's own StartsOn month)", len(result.Periods))
	}
	if got := result.Periods[0].Lines[0].Actual.AmountMinor(); got != 5000 {
		t.Errorf("Periods[0] (July) Actual = %d, want 5000", got)
	}
	if got := result.Periods[1].Lines[0].Actual.AmountMinor(); got != 7000 {
		t.Errorf("Periods[1] (August) Actual = %d, want 7000", got)
	}
	if !result.Periods[0].From.Before(result.Periods[1].From) {
		t.Error("Periods should be ascending (oldest first)")
	}
}

func mustDate(t *testing.T, year int, month time.Month, day int) domain.Date {
	t.Helper()
	dt, err := domain.NewDate(year, month, day)
	if err != nil {
		t.Fatalf("domain.NewDate(%d, %s, %d): %v", year, month, day, err)
	}
	return dt
}
