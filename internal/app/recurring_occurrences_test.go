package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func mustCreateWeeklyRule(t *testing.T, svc *app.Service, actorID, account, category, startsOn string) string {
	t.Helper()
	result, err := svc.CreateRecurringRule(context.Background(), app.CreateRecurringRuleCommand{
		ActorID: actorID, AccountRef: account, CategoryRef: category,
		Amount: "10.00", Description: "Weekly", StartsOn: startsOn,
		Schedule: app.RecurringScheduleInput{Frequency: "weekly", Interval: 1, Weekday: time.Monday},
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	return result.Rule.ID()
}

func mustCreateMonthlyRule(t *testing.T, svc *app.Service, actorID, account, category, startsOn string, dayOfMonth int) string {
	t.Helper()
	result, err := svc.CreateRecurringRule(context.Background(), app.CreateRecurringRuleCommand{
		ActorID: actorID, AccountRef: account, CategoryRef: category,
		Amount: "100.00", Description: "Monthly", StartsOn: startsOn,
		Schedule: app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: dayOfMonth},
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	return result.Rule.ID()
}

func listOccurrences(t *testing.T, svc *app.Service, actorID string, filter ports.ScheduledOccurrenceFilter) []recurring.ScheduledOccurrence {
	t.Helper()
	out, err := svc.ScheduledOccurrences.List(context.Background(), actorID, filter)
	if err != nil {
		t.Fatalf("ScheduledOccurrences.List: %v", err)
	}
	return out
}

func TestGenerateOccurrences_Weekly(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	ruleID := mustCreateWeeklyRule(t, svc, testActorID, account, rent, "2026-01-05") // a Monday

	result, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: ruleID})
	if err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}
	// A weekly rule over a ~12-month horizon produces roughly 52 firings;
	// assert the shape (every date a Monday, within the window, pending,
	// strictly ascending) rather than an exact count that would be brittle
	// against the horizon's own exact-day arithmetic.
	if len(result.Created) < 50 || len(result.Created) > 54 {
		t.Fatalf("len(Created) = %d, want ~52 Mondays across a 12-month horizon", len(result.Created))
	}
	var prev string
	for _, o := range result.Created {
		d := o.OccurrenceDate()
		if d.String() < "2026-01-05" || d.String() > "2027-02-05" {
			t.Errorf("occurrence date %s outside expected window", d)
		}
		if got := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC).Weekday(); got != time.Monday {
			t.Errorf("occurrence date %s is a %s, want Monday", d, got)
		}
		if o.Status() != recurring.OccurrenceStatusPending {
			t.Errorf("newly generated occurrence should be pending, got %s", o.Status())
		}
		if prev != "" && d.String() <= prev {
			t.Errorf("occurrence dates not strictly ascending: %s then %s", prev, d)
		}
		prev = d.String()
	}

	stored := listOccurrences(t, svc, testActorID, ports.ScheduledOccurrenceFilter{RuleID: ruleID})
	if len(stored) != len(result.Created) {
		t.Errorf("stored count = %d, want %d", len(stored), len(result.Created))
	}
}

func TestGenerateOccurrences_RespectsEndsOn(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	created, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID: testActorID, AccountRef: account, CategoryRef: rent,
		Amount: "100.00", Description: "Bounded", StartsOn: "2026-01-01", EndsOn: "2026-03-01",
		Schedule: app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}

	result, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: created.Rule.ID()})
	if err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}
	if len(result.Created) != 3 {
		t.Fatalf("len(Created) = %d, want 3 (Jan, Feb, Mar)", len(result.Created))
	}
	last := result.Created[len(result.Created)-1].OccurrenceDate()
	if last.String() != "2026-03-01" {
		t.Errorf("last occurrence = %s, want 2026-03-01 (ends_on)", last)
	}
}

func TestGenerateOccurrences_ArchivedRuleGeneratesNothing(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	ruleID := mustCreateMonthlyRule(t, svc, testActorID, account, rent, "2026-01-01", 1)

	if _, err := svc.ArchiveRecurringRule(ctx, app.ArchiveRecurringRuleCommand{ActorID: testActorID, RuleID: ruleID}); err != nil {
		t.Fatalf("ArchiveRecurringRule: %v", err)
	}

	// Explicit single-rule request against an archived rule.
	result, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: ruleID})
	if err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}
	if len(result.Created) != 0 {
		t.Errorf("archived rule created %d occurrences, want 0", len(result.Created))
	}

	// The all-rules path must also skip it.
	all, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID})
	if err != nil {
		t.Fatalf("GenerateOccurrences (all): %v", err)
	}
	if len(all.Created) != 0 {
		t.Errorf("all-rules generation created %d occurrences for an archived rule, want 0", len(all.Created))
	}
}

func TestGenerateOccurrences_Idempotent(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	ruleID := mustCreateMonthlyRule(t, svc, testActorID, account, rent, "2026-01-01", 1)

	first, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: ruleID})
	if err != nil {
		t.Fatalf("GenerateOccurrences (first): %v", err)
	}
	if len(first.Created) == 0 {
		t.Fatal("expected the first generation to create occurrences")
	}

	second, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: ruleID})
	if err != nil {
		t.Fatalf("GenerateOccurrences (second): %v", err)
	}
	if len(second.Created) != 0 {
		t.Errorf("re-running generation over an overlapping window created %d new occurrences, want 0", len(second.Created))
	}

	stored := listOccurrences(t, svc, testActorID, ports.ScheduledOccurrenceFilter{RuleID: ruleID})
	if len(stored) != len(first.Created) {
		t.Errorf("stored count after re-running = %d, want %d (no duplicates)", len(stored), len(first.Created))
	}
}

func TestGenerateOccurrences_AllActiveRulesForActor(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	activeA := mustCreateMonthlyRule(t, svc, testActorID, account, rent, "2026-01-01", 1)
	activeB := mustCreateMonthlyRule(t, svc, testActorID, account, rent, "2026-01-01", 15)
	archived := mustCreateMonthlyRule(t, svc, testActorID, account, rent, "2026-01-01", 20)
	if _, err := svc.ArchiveRecurringRule(ctx, app.ArchiveRecurringRuleCommand{ActorID: testActorID, RuleID: archived}); err != nil {
		t.Fatalf("ArchiveRecurringRule: %v", err)
	}

	result, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID})
	if err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}
	byRule := map[string]int{}
	for _, o := range result.Created {
		byRule[o.RuleID()]++
	}
	if byRule[activeA] == 0 || byRule[activeB] == 0 {
		t.Errorf("both active rules should have generated occurrences, got %v", byRule)
	}
	if byRule[archived] != 0 {
		t.Errorf("archived rule generated %d occurrences, want 0", byRule[archived])
	}
}

func TestGenerateOccurrences_ActorScoping(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	_ = mustCreateMonthlyRule(t, svc, testActorID, account, rent, "2026-01-01", 1)

	otherAccount := mustCreateAccount(t, svc, "someone-else", "Their Checking")
	otherCategory, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: "someone-else", Name: "Rent", Kind: "expense"})
	if err != nil {
		t.Fatalf("CreateCategory (other actor): %v", err)
	}
	otherRule := mustCreateMonthlyRule(t, svc, "someone-else", otherAccount, otherCategory.Category.ID(), "2026-01-01", 1)

	result, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID})
	if err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}
	for _, o := range result.Created {
		if o.RuleID() == otherRule {
			t.Fatal("generating for testActorID must never touch another actor's rule")
		}
	}
}

// occurrenceByDate finds the occurrence in occurrences falling on date
// (ISO 8601), failing the test if none matches — shared by
// recurring_rules_test.go's schedule-change/archive-cancellation tests to
// pick out a specific generated occurrence to mutate or inspect.
func occurrenceByDate(t *testing.T, occurrences []recurring.ScheduledOccurrence, date string) recurring.ScheduledOccurrence {
	t.Helper()
	for _, o := range occurrences {
		if o.OccurrenceDate().String() == date {
			return o
		}
	}
	t.Fatalf("no occurrence found for date %s among %d occurrences", date, len(occurrences))
	return recurring.ScheduledOccurrence{}
}

func TestGenerateOccurrences_UnknownRuleIDIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	_, err := svc.GenerateOccurrences(context.Background(), app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: "does-not-exist"})
	wantErrCode(t, err, errs.NotFound)
}

func TestListScheduledOccurrences_FiltersByStatus(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	ruleID := mustCreateWeeklyRule(t, svc, testActorID, account, rent, "2026-01-05")

	genResult, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: ruleID})
	if err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}
	if len(genResult.Created) == 0 {
		t.Fatalf("expected at least one generated occurrence")
	}
	first := occurrenceByDate(t, genResult.Created, "2026-01-05")

	if _, err := svc.MaterialiseOccurrence(ctx, app.MaterialiseOccurrenceCommand{ActorID: testActorID, OccurrenceID: first.ID()}); err != nil {
		t.Fatalf("MaterialiseOccurrence: %v", err)
	}

	all, err := svc.ListScheduledOccurrences(ctx, app.ListScheduledOccurrencesQuery{ActorID: testActorID, RuleID: ruleID})
	if err != nil {
		t.Fatalf("ListScheduledOccurrences (all): %v", err)
	}
	if len(all.Occurrences) != len(genResult.Created) {
		t.Fatalf("all: got %d occurrences, want %d", len(all.Occurrences), len(genResult.Created))
	}

	materialised, err := svc.ListScheduledOccurrences(ctx, app.ListScheduledOccurrencesQuery{
		ActorID: testActorID, RuleID: ruleID, Status: "materialised",
	})
	if err != nil {
		t.Fatalf("ListScheduledOccurrences (materialised): %v", err)
	}
	if len(materialised.Occurrences) != 1 || materialised.Occurrences[0].ID() != first.ID() {
		t.Fatalf("materialised: got %+v, want exactly the materialised occurrence", materialised.Occurrences)
	}

	pending, err := svc.ListScheduledOccurrences(ctx, app.ListScheduledOccurrencesQuery{
		ActorID: testActorID, RuleID: ruleID, Status: "pending",
	})
	if err != nil {
		t.Fatalf("ListScheduledOccurrences (pending): %v", err)
	}
	if len(pending.Occurrences) != len(genResult.Created)-1 {
		t.Fatalf("pending: got %d, want %d", len(pending.Occurrences), len(genResult.Created)-1)
	}
	for _, o := range pending.Occurrences {
		if o.ID() == first.ID() {
			t.Fatalf("pending: unexpectedly includes the materialised occurrence")
		}
	}
}

func TestListScheduledOccurrences_DateRange(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	ruleID := mustCreateWeeklyRule(t, svc, testActorID, account, rent, "2026-01-05")

	if _, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: ruleID}); err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}

	result, err := svc.ListScheduledOccurrences(ctx, app.ListScheduledOccurrencesQuery{
		ActorID: testActorID, RuleID: ruleID, FromDate: "2026-01-05", ToDate: "2026-01-12",
	})
	if err != nil {
		t.Fatalf("ListScheduledOccurrences: %v", err)
	}
	if len(result.Occurrences) != 2 {
		t.Fatalf("got %d occurrences in range, want 2 (2026-01-05 and 2026-01-12)", len(result.Occurrences))
	}
}

func TestListScheduledOccurrences_UnknownRuleIDIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	_, err := svc.ListScheduledOccurrences(context.Background(), app.ListScheduledOccurrencesQuery{ActorID: testActorID, RuleID: "does-not-exist"})
	wantErrCode(t, err, errs.NotFound)
}

func TestListScheduledOccurrences_InvalidStatusIsInvalidInput(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	_, err := svc.ListScheduledOccurrences(context.Background(), app.ListScheduledOccurrencesQuery{ActorID: testActorID, Status: "cancelled"})
	wantErrCode(t, err, errs.InvalidInput)
}
