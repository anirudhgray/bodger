package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func mustSubcategoryFixture(t *testing.T, svc *app.Service, name, kind, parentRef string) app.CategoryResult {
	t.Helper()
	result, err := svc.CreateCategory(context.Background(), app.CreateCategoryCommand{
		ActorID: testActorID, Name: name, Kind: kind, ParentRef: parentRef,
	})
	if err != nil {
		t.Fatalf("CreateCategory(%q): %v", name, err)
	}
	return result
}

func categoryBreakdownRow(t *testing.T, result app.CategoryBreakdownResult, name string) app.CategoryBreakdownRow {
	t.Helper()
	for _, row := range result.Rows {
		if row.Category != nil && row.Category.Name() == name {
			return row
		}
	}
	t.Fatalf("no CategoryBreakdown row found for category %q", name)
	return app.CategoryBreakdownRow{}
}

func defaultAnalyticsOptions() app.AnalyticsOptions {
	return app.AnalyticsOptions{ReportingCurrency: "USD", Policy: app.PolicyCurrent}
}

func TestCategoryBreakdown_RollsSubcategoriesIntoTopLevel(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	food := mustCategoryFixture(t, svc, "Food", "expense")
	groceries := mustSubcategoryFixture(t, svc, "Groceries", "expense", food.Category.ID())
	restaurants := mustSubcategoryFixture(t, svc, "Restaurants", "expense", food.Category.ID())

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), CategoryRef: groceries.Category.ID(),
		Amount: "50", Date: "2026-08-05", Description: "Groceries",
	}); err != nil {
		t.Fatalf("RecordOutflow(groceries): %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), CategoryRef: restaurants.Category.ID(),
		Amount: "30", Date: "2026-08-06", Description: "Dinner",
	}); err != nil {
		t.Fatalf("RecordOutflow(restaurants): %v", err)
	}

	result, err := svc.CategoryBreakdown(ctx, app.CategoryBreakdownQuery{ActorID: testActorID, Options: defaultAnalyticsOptions()})
	if err != nil {
		t.Fatalf("CategoryBreakdown: %v", err)
	}

	row := categoryBreakdownRow(t, result, "Food")
	if got := row.Spending.AmountMinor(); got != 8000 {
		t.Errorf("Food Spending = %d, want 8000 (50+30 rolled up from Groceries+Restaurants)", got)
	}
	if row.Category.ID() != food.Category.ID() {
		t.Errorf("row.Category = %q, want the top-level Food category %q", row.Category.ID(), food.Category.ID())
	}
}

func TestCategoryBreakdown_UncategorizedBucket(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "20", Date: "2026-08-05", Description: "Misc",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	result, err := svc.CategoryBreakdown(ctx, app.CategoryBreakdownQuery{ActorID: testActorID, Options: defaultAnalyticsOptions()})
	if err != nil {
		t.Fatalf("CategoryBreakdown: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(result.Rows))
	}
	if result.Rows[0].Category != nil {
		t.Errorf("Rows[0].Category = %+v, want nil (uncategorized)", result.Rows[0].Category)
	}
	if got := result.Rows[0].Spending.AmountMinor(); got != 2000 {
		t.Errorf("Spending = %d, want 2000", got)
	}
}

func TestCategoryBreakdown_ExcludesTransfers(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	savings := mustAccountFixture(t, svc, "Savings", "bank", "USD")
	checking := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: savings.Account.ID(), ToAccountRef: checking.Account.ID(),
		Amount: "500", Date: "2026-08-05", Description: "Move",
	}); err != nil {
		t.Fatalf("RecordTransfer: %v", err)
	}

	result, err := svc.CategoryBreakdown(ctx, app.CategoryBreakdownQuery{ActorID: testActorID, Options: defaultAnalyticsOptions()})
	if err != nil {
		t.Fatalf("CategoryBreakdown: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Errorf("Rows = %+v, want empty (a transfer contributes to no category)", result.Rows)
	}
}

func TestCategoryBreakdown_MultiCurrencyConvertsUnderPolicy(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	usdAcc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	inrAcc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	food := mustCategoryFixture(t, svc, "Food", "expense")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: usdAcc.Account.ID(), CategoryRef: food.Category.ID(),
		Amount: "10", Date: "2026-08-05", Description: "Coffee",
	}); err != nil {
		t.Fatalf("RecordOutflow(usd): %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: inrAcc.Account.ID(), CategoryRef: food.Category.ID(),
		Amount: "1000", Date: "2026-08-06", Description: "Lunch",
	}); err != nil {
		t.Fatalf("RecordOutflow(inr): %v", err)
	}
	seedRate(t, svc, "INR", "USD", "0.0115", "2026-08-20", "ecb")

	result, err := svc.CategoryBreakdown(ctx, app.CategoryBreakdownQuery{
		ActorID: testActorID,
		Options: app.AnalyticsOptions{ReportingCurrency: "USD", Policy: app.PolicyCurrent},
	})
	if err != nil {
		t.Fatalf("CategoryBreakdown: %v", err)
	}
	row := categoryBreakdownRow(t, result, "Food")
	// 10.00 USD + (1000.00 INR * 0.0115) = 10.00 + 11.50 = 21.50 USD -> 2150 minor units.
	if got := row.Spending.AmountMinor(); got != 2150 {
		t.Errorf("Food Spending = %d, want 2150 (10.00 USD + 11.50 USD converted from INR)", got)
	}
	if len(result.Unconverted) != 0 {
		t.Errorf("Unconverted = %+v, want empty", result.Unconverted)
	}
}

func TestCategoryBreakdown_MissingRateReportedNotOmitted(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	inrAcc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	food := mustCategoryFixture(t, svc, "Food", "expense")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: inrAcc.Account.ID(), CategoryRef: food.Category.ID(),
		Amount: "1000", Date: "2026-08-06", Description: "Lunch",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	// No INR/USD rate seeded.

	result, err := svc.CategoryBreakdown(ctx, app.CategoryBreakdownQuery{ActorID: testActorID, Options: defaultAnalyticsOptions()})
	if err != nil {
		t.Fatalf("CategoryBreakdown: %v, want success with the shortfall reported instead", err)
	}
	if len(result.Rows) != 0 {
		t.Errorf("Rows = %+v, want empty (the only posting couldn't be converted)", result.Rows)
	}
	if len(result.Unconverted) != 1 {
		t.Fatalf("len(Unconverted) = %d, want 1", len(result.Unconverted))
	}
	if result.Unconverted[0].Reason == "" {
		t.Errorf("Unconverted[0].Reason is empty, want a human-readable explanation")
	}
}

func TestCategoryBreakdown_InvalidReportingCurrency(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.CategoryBreakdown(context.Background(), app.CategoryBreakdownQuery{
		ActorID: testActorID,
		Options: app.AnalyticsOptions{ReportingCurrency: "not-a-currency", Policy: app.PolicyCurrent},
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCategoryBreakdown_RequiresActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.CategoryBreakdown(context.Background(), app.CategoryBreakdownQuery{Options: defaultAnalyticsOptions()})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCashFlow_GroupsByMonth(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "1000", Date: "2026-07-05", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow(july): %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "200", Date: "2026-08-05", Description: "Rent",
	}); err != nil {
		t.Fatalf("RecordOutflow(august): %v", err)
	}

	result, err := svc.CashFlow(ctx, app.CashFlowQuery{ActorID: testActorID, Options: defaultAnalyticsOptions()})
	if err != nil {
		t.Fatalf("CashFlow: %v", err)
	}
	if len(result.Points) != 2 {
		t.Fatalf("len(Points) = %d, want 2 (July and August)", len(result.Points))
	}
	if result.Points[0].From.String() != "2026-07-01" || result.Points[0].To.String() != "2026-07-31" {
		t.Errorf("Points[0] = %s..%s, want 2026-07-01..2026-07-31 (ascending)", result.Points[0].From, result.Points[0].To)
	}
	if got := result.Points[0].Inflow.AmountMinor(); got != 100000 {
		t.Errorf("July Inflow = %d, want 100000", got)
	}
	if result.Points[1].From.String() != "2026-08-01" || result.Points[1].To.String() != "2026-08-31" {
		t.Errorf("Points[1] = %s..%s, want 2026-08-01..2026-08-31", result.Points[1].From, result.Points[1].To)
	}
	if got := result.Points[1].Outflow.AmountMinor(); got != 20000 {
		t.Errorf("August Outflow = %d, want 20000", got)
	}
	if got := result.Points[1].Net.AmountMinor(); got != -20000 {
		t.Errorf("August Net = %d, want -20000", got)
	}
}

func TestCashFlow_ZeroFillsMonthsInBoundedRange(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "500", Date: "2026-06-01", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}

	result, err := svc.CashFlow(ctx, app.CashFlowQuery{
		ActorID: testActorID,
		Filter:  app.TransactionFilterInput{DateFrom: "2026-06-01", DateTo: "2026-08-31"},
		Options: defaultAnalyticsOptions(),
	})
	if err != nil {
		t.Fatalf("CashFlow: %v", err)
	}
	// June (data), July (no data -- must still appear, zeroed), August (no data).
	if len(result.Points) != 3 {
		t.Fatalf("len(Points) = %d, want 3 (June, July, August all present)", len(result.Points))
	}
	if result.Points[1].From.String() != "2026-07-01" || !result.Points[1].Inflow.IsZero() {
		t.Errorf("Points[1] = %+v, want a zeroed July point", result.Points[1])
	}
}

func TestCashFlow_ExcludesTransfers(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	savings := mustAccountFixture(t, svc, "Savings", "bank", "USD")
	checking := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: savings.Account.ID(), ToAccountRef: checking.Account.ID(),
		Amount: "500", Date: "2026-08-05", Description: "Move",
	}); err != nil {
		t.Fatalf("RecordTransfer: %v", err)
	}

	result, err := svc.CashFlow(ctx, app.CashFlowQuery{ActorID: testActorID, Options: defaultAnalyticsOptions()})
	if err != nil {
		t.Fatalf("CashFlow: %v", err)
	}
	if len(result.Points) != 0 {
		t.Errorf("Points = %+v, want empty (a transfer moves nothing in or out)", result.Points)
	}
}

func TestCashFlow_GranularityMonthDefaultMatchesEmptyString(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "1000", Date: "2026-07-05", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "200", Date: "2026-08-05", Description: "Rent",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	blank, err := svc.CashFlow(ctx, app.CashFlowQuery{ActorID: testActorID, Options: defaultAnalyticsOptions()})
	if err != nil {
		t.Fatalf("CashFlow (blank granularity): %v", err)
	}
	explicit, err := svc.CashFlow(ctx, app.CashFlowQuery{ActorID: testActorID, Options: defaultAnalyticsOptions(), Granularity: app.GranularityMonth})
	if err != nil {
		t.Fatalf("CashFlow (explicit month granularity): %v", err)
	}
	if len(blank.Points) != len(explicit.Points) {
		t.Fatalf("len(Points) blank=%d explicit=%d, want equal", len(blank.Points), len(explicit.Points))
	}
	for i := range blank.Points {
		b, e := blank.Points[i], explicit.Points[i]
		if b.From != e.From || b.To != e.To || b.Inflow != e.Inflow || b.Outflow != e.Outflow || b.Net != e.Net {
			t.Errorf("Points[%d] differ: blank=%+v explicit=%+v", i, b, e)
		}
	}
}

func TestCashFlow_WeekGranularityBuckets(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	// Wednesday of the week before (2026-08-10..2026-08-16) ...
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Date: "2026-08-12", Description: "A",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	// ... and Wednesday of the week containing "today" (2026-08-17..2026-08-23).
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "200", Date: "2026-08-19", Description: "B",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}

	result, err := svc.CashFlow(ctx, app.CashFlowQuery{ActorID: testActorID, Options: defaultAnalyticsOptions(), Granularity: app.GranularityWeek})
	if err != nil {
		t.Fatalf("CashFlow: %v", err)
	}
	if len(result.Points) != 2 {
		t.Fatalf("len(Points) = %d, want 2", len(result.Points))
	}
	if result.Points[0].From.String() != "2026-08-10" || result.Points[0].To.String() != "2026-08-16" {
		t.Errorf("Points[0] = %s..%s, want 2026-08-10..2026-08-16 (Monday-Sunday)", result.Points[0].From, result.Points[0].To)
	}
	if got := result.Points[0].Inflow.AmountMinor(); got != 10000 {
		t.Errorf("Points[0].Inflow = %d, want 10000", got)
	}
	if result.Points[1].From.String() != "2026-08-17" || result.Points[1].To.String() != "2026-08-23" {
		t.Errorf("Points[1] = %s..%s, want 2026-08-17..2026-08-23", result.Points[1].From, result.Points[1].To)
	}
}

func TestCashFlow_ZeroFillsWeeksInBoundedRange(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "500", Date: "2026-08-12", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}

	result, err := svc.CashFlow(ctx, app.CashFlowQuery{
		ActorID:     testActorID,
		Filter:      app.TransactionFilterInput{DateFrom: "2026-08-10", DateTo: "2026-08-23"},
		Options:     defaultAnalyticsOptions(),
		Granularity: app.GranularityWeek,
	})
	if err != nil {
		t.Fatalf("CashFlow: %v", err)
	}
	if len(result.Points) != 2 {
		t.Fatalf("len(Points) = %d, want 2 (both weeks present)", len(result.Points))
	}
	if !result.Points[1].Inflow.IsZero() {
		t.Errorf("Points[1] = %+v, want a zeroed second week", result.Points[1])
	}
}

func TestCashFlow_YearGranularityBuckets(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "1000", Date: "2025-12-15", Description: "A",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "2000", Date: "2026-01-05", Description: "B",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}

	result, err := svc.CashFlow(ctx, app.CashFlowQuery{ActorID: testActorID, Options: defaultAnalyticsOptions(), Granularity: app.GranularityYear})
	if err != nil {
		t.Fatalf("CashFlow: %v", err)
	}
	if len(result.Points) != 2 {
		t.Fatalf("len(Points) = %d, want 2 (2025 and 2026)", len(result.Points))
	}
	if result.Points[0].From.String() != "2025-01-01" || result.Points[0].To.String() != "2025-12-31" {
		t.Errorf("Points[0] = %s..%s, want 2025-01-01..2025-12-31", result.Points[0].From, result.Points[0].To)
	}
	if result.Points[1].From.String() != "2026-01-01" || result.Points[1].To.String() != "2026-12-31" {
		t.Errorf("Points[1] = %s..%s, want 2026-01-01..2026-12-31", result.Points[1].From, result.Points[1].To)
	}
}

func TestCashFlow_CustomGranularitySingleBucket(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Date: "2026-07-10", Description: "A",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "200", Date: "2026-08-05", Description: "B",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}

	result, err := svc.CashFlow(ctx, app.CashFlowQuery{
		ActorID:     testActorID,
		Filter:      app.TransactionFilterInput{DateFrom: "2026-07-01", DateTo: "2026-08-31"},
		Options:     defaultAnalyticsOptions(),
		Granularity: app.GranularityCustom,
	})
	if err != nil {
		t.Fatalf("CashFlow: %v", err)
	}
	if len(result.Points) != 1 {
		t.Fatalf("len(Points) = %d, want 1 (the whole range as one bucket)", len(result.Points))
	}
	if result.Points[0].From.String() != "2026-07-01" || result.Points[0].To.String() != "2026-08-31" {
		t.Errorf("Points[0] = %s..%s, want 2026-07-01..2026-08-31", result.Points[0].From, result.Points[0].To)
	}
	if got := result.Points[0].Inflow.AmountMinor(); got != 30000 {
		t.Errorf("Points[0].Inflow = %d, want 30000 (100+200 combined into one bucket)", got)
	}
}

func TestCashFlow_CustomGranularityRequiresBothBounds(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.CashFlow(context.Background(), app.CashFlowQuery{
		ActorID:     testActorID,
		Filter:      app.TransactionFilterInput{DateFrom: "2026-07-01"},
		Options:     defaultAnalyticsOptions(),
		Granularity: app.GranularityCustom,
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCashFlow_InvalidGranularity(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.CashFlow(context.Background(), app.CashFlowQuery{
		ActorID:     testActorID,
		Options:     defaultAnalyticsOptions(),
		Granularity: "fortnight",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestTrends_ComparesCurrentAndPreviousMonth(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Date: "2026-07-10", Description: "July rent",
	}); err != nil {
		t.Fatalf("RecordOutflow(july): %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "150", Date: "2026-08-10", Description: "August rent",
	}); err != nil {
		t.Fatalf("RecordOutflow(august): %v", err)
	}

	result, err := svc.Trends(ctx, app.TrendsQuery{ActorID: testActorID, Options: defaultAnalyticsOptions()})
	if err != nil {
		t.Fatalf("Trends: %v", err)
	}
	if result.Current.From.String() != "2026-08-01" || result.Current.To.String() != "2026-08-31" {
		t.Errorf("Current period = %s..%s, want 2026-08-01..2026-08-31", result.Current.From, result.Current.To)
	}
	if result.Previous.From.String() != "2026-07-01" || result.Previous.To.String() != "2026-07-31" {
		t.Errorf("Previous period = %s..%s, want 2026-07-01..2026-07-31", result.Previous.From, result.Previous.To)
	}
	if got := result.Current.Outflow.AmountMinor(); got != 15000 {
		t.Errorf("Current.Outflow = %d, want 15000", got)
	}
	if got := result.Previous.Outflow.AmountMinor(); got != 10000 {
		t.Errorf("Previous.Outflow = %d, want 10000", got)
	}
	if result.OutflowChangePct == nil {
		t.Fatalf("OutflowChangePct = nil, want a value")
	}
	// (150 - 100) / 100 * 100 = 50%.
	if got := *result.OutflowChangePct; got != 50 {
		t.Errorf("OutflowChangePct = %v, want 50", got)
	}
}

func TestTrends_NilChangePctWhenPreviousIsZero(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "150", Date: "2026-08-10", Description: "August rent",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	// No July transactions at all.

	result, err := svc.Trends(ctx, app.TrendsQuery{ActorID: testActorID, Options: defaultAnalyticsOptions()})
	if err != nil {
		t.Fatalf("Trends: %v", err)
	}
	if result.OutflowChangePct != nil {
		t.Errorf("OutflowChangePct = %v, want nil (previous outflow is zero)", *result.OutflowChangePct)
	}
}

func TestTrends_GranularityMonthDefaultMatchesEmptyString(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Date: "2026-07-10", Description: "July rent",
	}); err != nil {
		t.Fatalf("RecordOutflow(july): %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "150", Date: "2026-08-10", Description: "August rent",
	}); err != nil {
		t.Fatalf("RecordOutflow(august): %v", err)
	}

	blank, err := svc.Trends(ctx, app.TrendsQuery{ActorID: testActorID, Options: defaultAnalyticsOptions()})
	if err != nil {
		t.Fatalf("Trends (blank granularity): %v", err)
	}
	explicit, err := svc.Trends(ctx, app.TrendsQuery{ActorID: testActorID, Options: defaultAnalyticsOptions(), Granularity: app.GranularityMonth})
	if err != nil {
		t.Fatalf("Trends (explicit month granularity): %v", err)
	}
	if blank.Current != explicit.Current || blank.Previous != explicit.Previous {
		t.Errorf("Current/Previous differ: blank=%+v/%+v explicit=%+v/%+v", blank.Current, blank.Previous, explicit.Current, explicit.Previous)
	}
	if (blank.InflowChangePct == nil) != (explicit.InflowChangePct == nil) ||
		(blank.InflowChangePct != nil && *blank.InflowChangePct != *explicit.InflowChangePct) {
		t.Errorf("InflowChangePct differ: blank=%v explicit=%v", blank.InflowChangePct, explicit.InflowChangePct)
	}
}

func TestTrends_WeekGranularity(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	// Previous week: 2026-08-10..2026-08-16.
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Date: "2026-08-12", Description: "A",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	// Current week (containing 2026-08-20): 2026-08-17..2026-08-23.
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "150", Date: "2026-08-19", Description: "B",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	result, err := svc.Trends(ctx, app.TrendsQuery{ActorID: testActorID, Options: defaultAnalyticsOptions(), Granularity: app.GranularityWeek})
	if err != nil {
		t.Fatalf("Trends: %v", err)
	}
	if result.Current.From.String() != "2026-08-17" || result.Current.To.String() != "2026-08-23" {
		t.Errorf("Current = %s..%s, want 2026-08-17..2026-08-23", result.Current.From, result.Current.To)
	}
	if result.Previous.From.String() != "2026-08-10" || result.Previous.To.String() != "2026-08-16" {
		t.Errorf("Previous = %s..%s, want 2026-08-10..2026-08-16", result.Previous.From, result.Previous.To)
	}
	if got := result.Current.Outflow.AmountMinor(); got != 15000 {
		t.Errorf("Current.Outflow = %d, want 15000", got)
	}
	if got := result.Previous.Outflow.AmountMinor(); got != 10000 {
		t.Errorf("Previous.Outflow = %d, want 10000", got)
	}
}

func TestTrends_YearGranularity(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Date: "2025-12-15", Description: "A",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "150", Date: "2026-08-10", Description: "B",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	result, err := svc.Trends(ctx, app.TrendsQuery{ActorID: testActorID, Options: defaultAnalyticsOptions(), Granularity: app.GranularityYear})
	if err != nil {
		t.Fatalf("Trends: %v", err)
	}
	if result.Current.From.String() != "2026-01-01" || result.Current.To.String() != "2026-12-31" {
		t.Errorf("Current = %s..%s, want 2026-01-01..2026-12-31", result.Current.From, result.Current.To)
	}
	if result.Previous.From.String() != "2025-01-01" || result.Previous.To.String() != "2025-12-31" {
		t.Errorf("Previous = %s..%s, want 2025-01-01..2025-12-31", result.Previous.From, result.Previous.To)
	}
	if got := result.Current.Outflow.AmountMinor(); got != 15000 {
		t.Errorf("Current.Outflow = %d, want 15000", got)
	}
	if got := result.Previous.Outflow.AmountMinor(); got != 10000 {
		t.Errorf("Previous.Outflow = %d, want 10000", got)
	}
}

func TestTrends_CustomGranularityComparesEqualLengthPreviousPeriod(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	// Current: 2026-08-10..2026-08-20 (11 days). Previous: 2026-07-30..2026-08-09.
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Date: "2026-08-01", Description: "A",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "150", Date: "2026-08-15", Description: "B",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	result, err := svc.Trends(ctx, app.TrendsQuery{
		ActorID:     testActorID,
		Filter:      app.TransactionFilterInput{DateFrom: "2026-08-10", DateTo: "2026-08-20"},
		Options:     defaultAnalyticsOptions(),
		Granularity: app.GranularityCustom,
	})
	if err != nil {
		t.Fatalf("Trends: %v", err)
	}
	if result.Current.From.String() != "2026-08-10" || result.Current.To.String() != "2026-08-20" {
		t.Errorf("Current = %s..%s, want 2026-08-10..2026-08-20", result.Current.From, result.Current.To)
	}
	if result.Previous.From.String() != "2026-07-30" || result.Previous.To.String() != "2026-08-09" {
		t.Errorf("Previous = %s..%s, want 2026-07-30..2026-08-09 (same 11-day length)", result.Previous.From, result.Previous.To)
	}
	// The 2026-08-15 outflow (150) falls inside the current period; the
	// 2026-08-01 outflow (100) falls inside the previous period
	// (2026-07-30..2026-08-09 includes 2026-08-01).
	if got := result.Current.Outflow.AmountMinor(); got != 15000 {
		t.Errorf("Current.Outflow = %d, want 15000", got)
	}
	if got := result.Previous.Outflow.AmountMinor(); got != 10000 {
		t.Errorf("Previous.Outflow = %d, want 10000", got)
	}
}

func TestTrends_CustomGranularityRequiresBothBounds(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.Trends(context.Background(), app.TrendsQuery{
		ActorID:     testActorID,
		Filter:      app.TransactionFilterInput{DateFrom: "2026-08-10"},
		Options:     defaultAnalyticsOptions(),
		Granularity: app.GranularityCustom,
	})
	wantErrCode(t, err, errs.InvalidInput)

	_, err = svc.Trends(context.Background(), app.TrendsQuery{
		ActorID:     testActorID,
		Options:     defaultAnalyticsOptions(),
		Granularity: app.GranularityCustom,
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestTrends_InvalidGranularity(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.Trends(context.Background(), app.TrendsQuery{
		ActorID:     testActorID,
		Options:     defaultAnalyticsOptions(),
		Granularity: "fortnight",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestSavingsRate_ComputesIncomeMinusOutflowOverIncome(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "1000", Date: "2026-08-01", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "800", Date: "2026-08-10", Description: "Rent",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	result, err := svc.SavingsRate(ctx, app.SavingsRateQuery{
		ActorID: testActorID,
		Filter:  app.TransactionFilterInput{DateFrom: "2026-08-01", DateTo: "2026-08-31"},
		Options: defaultAnalyticsOptions(),
	})
	if err != nil {
		t.Fatalf("SavingsRate: %v", err)
	}
	if got := result.Income.AmountMinor(); got != 100000 {
		t.Errorf("Income = %d, want 100000", got)
	}
	if got := result.Outflow.AmountMinor(); got != 80000 {
		t.Errorf("Outflow = %d, want 80000", got)
	}
	if result.Rate == nil {
		t.Fatalf("Rate = nil, want a value")
	}
	// (1000 - 800) / 1000 = 0.2.
	if got := *result.Rate; got != 0.2 {
		t.Errorf("Rate = %v, want 0.2", got)
	}
}

func TestSavingsRate_NilRateWhenIncomeIsZero(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "800", Date: "2026-08-10", Description: "Rent",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	result, err := svc.SavingsRate(ctx, app.SavingsRateQuery{ActorID: testActorID, Options: defaultAnalyticsOptions()})
	if err != nil {
		t.Fatalf("SavingsRate: %v", err)
	}
	if result.Rate != nil {
		t.Errorf("Rate = %v, want nil (zero income)", *result.Rate)
	}
}

func TestAnalyticsOptions_InvalidPolicyFailsTheWholeQuery(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.SavingsRate(context.Background(), app.SavingsRateQuery{
		ActorID: testActorID,
		Options: app.AnalyticsOptions{ReportingCurrency: "USD", Policy: "not_a_real_policy"},
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestAnalyticsOptions_PinnedPolicyRequiresPinnedDate(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.SavingsRate(context.Background(), app.SavingsRateQuery{
		ActorID: testActorID,
		Options: app.AnalyticsOptions{ReportingCurrency: "USD", Policy: app.PolicyPinned},
	})
	wantErrCode(t, err, errs.InvalidInput)
}
