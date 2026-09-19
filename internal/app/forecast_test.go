package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
)

func TestForecast_MonthlyRuleProjectsWithinRange(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	_, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	result, err := svc.Forecast(ctx, app.ForecastQuery{
		ActorID: testActorID, FromDate: "2026-01-01", ToDate: "2026-01-31",
		Options: app.AnalyticsOptions{ReportingCurrency: "USD", Policy: app.PolicyCurrent},
	})
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	if len(result.Points) != 1 {
		t.Fatalf("len(Points) = %d, want 1", len(result.Points))
	}
	p := result.Points[0]
	if p.ProjectedOutflow.AmountMinor() != 120000 {
		t.Errorf("ProjectedOutflow = %d, want 120000", p.ProjectedOutflow.AmountMinor())
	}
	if p.ProjectedInflow.AmountMinor() != 0 {
		t.Errorf("ProjectedInflow = %d, want 0", p.ProjectedInflow.AmountMinor())
	}
	if p.ProjectedNet.AmountMinor() != -120000 {
		t.Errorf("ProjectedNet = %d, want -120000", p.ProjectedNet.AmountMinor())
	}
	if len(result.Unconverted) != 0 {
		t.Errorf("Unconverted = %v, want empty", result.Unconverted)
	}
	_ = occ
}

func TestForecast_IncomeCategoryProjectsAsInflow(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	salary := mustCreateIncomeCategory(t, svc, "Salary")
	mustCreateRuleWithOccurrence(t, svc, account, salary, "5000.00", "Salary", "2026-01-01")

	result, err := svc.Forecast(ctx, app.ForecastQuery{
		ActorID: testActorID, FromDate: "2026-01-01", ToDate: "2026-01-31",
		Options: app.AnalyticsOptions{ReportingCurrency: "USD", Policy: app.PolicyCurrent},
	})
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	if len(result.Points) != 1 {
		t.Fatalf("len(Points) = %d, want 1", len(result.Points))
	}
	p := result.Points[0]
	if p.ProjectedInflow.AmountMinor() != 500000 {
		t.Errorf("ProjectedInflow = %d, want 500000", p.ProjectedInflow.AmountMinor())
	}
	if p.ProjectedOutflow.AmountMinor() != 0 {
		t.Errorf("ProjectedOutflow = %d, want 0", p.ProjectedOutflow.AmountMinor())
	}
}

func TestForecast_ZeroFillsPeriodsWithNoOccurrence(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	// A quarterly (interval=3) monthly schedule fires Jan 1 / Apr 1 / ...,
	// so Feb and Mar have no occurrence — a genuine gap, unlike
	// mustCreateRuleWithOccurrence's interval=1 fixture, which fires every
	// month across its 12-month generation horizon and would leave no gap
	// to zero-fill within this query's own Jan-Mar range.
	created, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID: testActorID, AccountRef: account, CategoryRef: rent,
		Amount: "1200.00", Description: "Rent", StartsOn: "2026-01-01",
		Schedule: app.RecurringScheduleInput{Frequency: "monthly", Interval: 3, DayOfMonth: 1},
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	if _, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: created.Rule.ID()}); err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}

	result, err := svc.Forecast(ctx, app.ForecastQuery{
		ActorID: testActorID, FromDate: "2026-01-01", ToDate: "2026-03-31",
		Options: app.AnalyticsOptions{ReportingCurrency: "USD", Policy: app.PolicyCurrent},
	})
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	if len(result.Points) != 3 {
		t.Fatalf("len(Points) = %d, want 3 (Jan/Feb/Mar zero-filled)", len(result.Points))
	}
	if result.Points[0].ProjectedOutflow.AmountMinor() != 120000 {
		t.Errorf("January ProjectedOutflow = %d, want 120000", result.Points[0].ProjectedOutflow.AmountMinor())
	}
	if result.Points[1].ProjectedOutflow.AmountMinor() != 0 {
		t.Errorf("February ProjectedOutflow = %d, want 0 (no occurrence that month)", result.Points[1].ProjectedOutflow.AmountMinor())
	}
	if result.Points[2].ProjectedOutflow.AmountMinor() != 0 {
		t.Errorf("March ProjectedOutflow = %d, want 0 (no occurrence that month)", result.Points[2].ProjectedOutflow.AmountMinor())
	}
}

func TestForecast_WeeklyGranularityBucketsByWeek(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	groceries := mustCreateCategory(t, svc, "Groceries")

	created, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID: testActorID, AccountRef: account, CategoryRef: groceries,
		Amount: "50.00", Description: "Groceries", StartsOn: "2026-01-05",
		Schedule: app.RecurringScheduleInput{Frequency: "weekly", Interval: 1, Weekday: time.Monday},
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	if _, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: created.Rule.ID()}); err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}

	result, err := svc.Forecast(ctx, app.ForecastQuery{
		ActorID: testActorID, FromDate: "2026-01-05", ToDate: "2026-01-18",
		Granularity: app.GranularityWeek,
		Options:     app.AnalyticsOptions{ReportingCurrency: "USD", Policy: app.PolicyCurrent},
	})
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	if len(result.Points) != 2 {
		t.Fatalf("len(Points) = %d, want 2 (two Mon-Sun weeks)", len(result.Points))
	}
	for _, p := range result.Points {
		if p.ProjectedOutflow.AmountMinor() != 5000 {
			t.Errorf("week %s..%s ProjectedOutflow = %d, want 5000", p.From, p.To, p.ProjectedOutflow.AmountMinor())
		}
	}
}

func TestForecast_UnconvertedOccurrenceReportedNotFailed(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	// No FX rate exists for USD->EUR, so converting into a different
	// reporting currency must report the occurrence as unconverted rather
	// than failing the whole query.
	result, err := svc.Forecast(ctx, app.ForecastQuery{
		ActorID: testActorID, FromDate: "2026-01-01", ToDate: "2026-01-31",
		Options: app.AnalyticsOptions{ReportingCurrency: "EUR", Policy: app.PolicyCurrent},
	})
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	if len(result.Unconverted) != 1 {
		t.Fatalf("len(Unconverted) = %d, want 1", len(result.Unconverted))
	}
	if result.Points[0].ProjectedOutflow.AmountMinor() != 0 {
		t.Errorf("ProjectedOutflow = %d, want 0 (unconverted, excluded from total)", result.Points[0].ProjectedOutflow.AmountMinor())
	}
}

func TestForecast_MaterialisedAndSkippedOccurrencesNeverAppear(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	_, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	if _, err := svc.MaterialiseOccurrence(ctx, app.MaterialiseOccurrenceCommand{ActorID: testActorID, OccurrenceID: occ.ID()}); err != nil {
		t.Fatalf("MaterialiseOccurrence: %v", err)
	}

	result, err := svc.Forecast(ctx, app.ForecastQuery{
		ActorID: testActorID, FromDate: "2026-01-01", ToDate: "2026-01-31",
		Options: app.AnalyticsOptions{ReportingCurrency: "USD", Policy: app.PolicyCurrent},
	})
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	if result.Points[0].ProjectedOutflow.AmountMinor() != 0 {
		t.Errorf("ProjectedOutflow = %d, want 0 (occurrence is materialised, not pending)", result.Points[0].ProjectedOutflow.AmountMinor())
	}
}

func TestForecast_ActorScoping(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	result, err := svc.Forecast(ctx, app.ForecastQuery{
		ActorID: "actor-2", FromDate: "2026-01-01", ToDate: "2026-01-31",
		Options: app.AnalyticsOptions{ReportingCurrency: "USD", Policy: app.PolicyCurrent},
	})
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	if result.Points[0].ProjectedOutflow.AmountMinor() != 0 {
		t.Errorf("ProjectedOutflow = %d, want 0 (another actor's rule must be invisible)", result.Points[0].ProjectedOutflow.AmountMinor())
	}
}

func TestForecast_RejectsToBeforeFrom(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.Forecast(ctx, app.ForecastQuery{
		ActorID: testActorID, FromDate: "2026-02-01", ToDate: "2026-01-01",
		Options: app.AnalyticsOptions{ReportingCurrency: "USD", Policy: app.PolicyCurrent},
	})
	if err == nil {
		t.Fatal("Forecast with to before from: want error, got nil")
	}
}
