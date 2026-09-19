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

func mustCreateAccount(t *testing.T, svc *app.Service, actorID, name string) string {
	t.Helper()
	result, err := svc.CreateAccount(context.Background(), app.CreateAccountCommand{
		ActorID: actorID, Name: name, Kind: "bank", Currency: "USD",
	})
	if err != nil {
		t.Fatalf("CreateAccount(%q): %v", name, err)
	}
	return result.Account.ID()
}

func TestCreateRecurringRule_Weekly(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	result, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  account,
		CategoryRef: rent,
		Amount:      "50.00",
		Description: "Weekly allowance",
		Schedule: app.RecurringScheduleInput{
			Frequency: "weekly",
			Interval:  1,
			Weekday:   time.Monday,
		},
		StartsOn: "2026-01-05",
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	if result.Rule.AccountID() != account {
		t.Errorf("AccountID = %q, want %q", result.Rule.AccountID(), account)
	}
	if result.Rule.CategoryID() != rent {
		t.Errorf("CategoryID = %q, want %q", result.Rule.CategoryID(), rent)
	}
	if result.Rule.AmountMinor() != 5000 {
		t.Errorf("AmountMinor = %d, want 5000", result.Rule.AmountMinor())
	}
	if result.Rule.Schedule().Frequency() != recurring.FrequencyWeekly {
		t.Errorf("Frequency = %q, want weekly", result.Rule.Schedule().Frequency())
	}
	if result.Rule.Archived() {
		t.Error("a freshly created rule should not be archived")
	}
	if _, ok := result.Rule.EndsOn(); ok {
		t.Error("EndsOn should be unset when not supplied")
	}
}

func TestCreateRecurringRule_Monthly(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	result, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  account,
		CategoryRef: rent,
		Amount:      "1200.00",
		Description: "Rent",
		Schedule: app.RecurringScheduleInput{
			Frequency:  "monthly",
			Interval:   1,
			DayOfMonth: 31,
		},
		StartsOn: "2026-01-31",
		EndsOn:   "2026-12-31",
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	if day, ok := result.Rule.Schedule().DayOfMonth(); !ok || day != 31 {
		t.Errorf("DayOfMonth = %d, %v, want 31, true", day, ok)
	}
	endsOn, ok := result.Rule.EndsOn()
	if !ok || endsOn.String() != "2026-12-31" {
		t.Errorf("EndsOn = %v, %v, want 2026-12-31, true", endsOn, ok)
	}
}

func TestCreateRecurringRule_Yearly(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	gift := mustCreateCategory(t, svc, "Gifts")

	result, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  account,
		CategoryRef: gift,
		Amount:      "75.00",
		Description: "Birthday gift",
		Schedule: app.RecurringScheduleInput{
			Frequency:  "yearly",
			Interval:   1,
			Month:      time.March,
			DayOfMonth: 15,
		},
		StartsOn: "2026-03-15",
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	if month, ok := result.Rule.Schedule().Month(); !ok || month != time.March {
		t.Errorf("Month = %v, %v, want March, true", month, ok)
	}
}

func TestCreateRecurringRule_DefaultsStartsOnToToday(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	result, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  account,
		CategoryRef: rent,
		Amount:      "10.00",
		Description: "No explicit start",
		Schedule:    app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	if result.Rule.StartsOn().String() != "2026-03-03" {
		t.Errorf("StartsOn = %s, want 2026-03-03", result.Rule.StartsOn())
	}
}

func TestCreateRecurringRule_RejectsUnknownFrequency(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	_, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  account,
		CategoryRef: rent,
		Amount:      "10.00",
		Description: "Bad frequency",
		Schedule:    app.RecurringScheduleInput{Frequency: "daily", Interval: 1},
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCreateRecurringRule_RejectsInvalidDayOfMonth(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	_, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  account,
		CategoryRef: rent,
		Amount:      "10.00",
		Description: "Bad day",
		Schedule:    app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 32},
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCreateRecurringRule_RejectsNonPositiveAmount(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	_, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  account,
		CategoryRef: rent,
		Amount:      "0.00",
		Description: "Zero amount",
		Schedule:    app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCreateRecurringRule_RejectsEndsOnBeforeStartsOn(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	_, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  account,
		CategoryRef: rent,
		Amount:      "10.00",
		Description: "Backwards range",
		Schedule:    app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
		StartsOn:    "2026-06-01",
		EndsOn:      "2026-01-01",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCreateRecurringRule_RejectsUnknownAccount(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	rent := mustCreateCategory(t, svc, "Rent")

	_, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  "does-not-exist",
		CategoryRef: rent,
		Amount:      "10.00",
		Description: "No account",
		Schedule:    app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestCreateRecurringRule_AnotherActorsAccountIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	// "someone-else" has no accounts of their own, so testActorID's
	// account ref can't resolve against their candidate list — the app
	// layer's authorisation decision falls out of resolveOwnedAccount
	// itself (ADR-0006), with no separate ownership check to remember.
	_, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     "someone-else",
		AccountRef:  account,
		CategoryRef: rent,
		Amount:      "10.00",
		Description: "Wrong actor",
		Schedule:    app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestUpdateRecurringRule(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	created, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  account,
		CategoryRef: rent,
		Amount:      "100.00",
		Description: "Old description",
		Schedule:    app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
		StartsOn:    "2026-01-01",
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}

	updated, err := svc.UpdateRecurringRule(ctx, app.UpdateRecurringRuleCommand{
		ActorID:     testActorID,
		RuleID:      created.Rule.ID(),
		Amount:      "150.00",
		Description: "New description",
		Schedule:    app.RecurringScheduleInput{Frequency: "weekly", Interval: 2, Weekday: time.Friday},
		EndsOn:      "2026-12-31",
	})
	if err != nil {
		t.Fatalf("UpdateRecurringRule: %v", err)
	}
	if updated.Rule.AmountMinor() != 15000 {
		t.Errorf("AmountMinor = %d, want 15000", updated.Rule.AmountMinor())
	}
	if updated.Rule.Description() != "New description" {
		t.Errorf("Description = %q, want %q", updated.Rule.Description(), "New description")
	}
	if updated.Rule.Schedule().Frequency() != recurring.FrequencyWeekly {
		t.Errorf("Frequency = %q, want weekly", updated.Rule.Schedule().Frequency())
	}
	if endsOn, ok := updated.Rule.EndsOn(); !ok || endsOn.String() != "2026-12-31" {
		t.Errorf("EndsOn = %v, %v, want 2026-12-31, true", endsOn, ok)
	}
	// AccountRef, CategoryRef, and StartsOn are fixed at creation.
	if updated.Rule.AccountID() != account {
		t.Error("UpdateRecurringRule must not change the account")
	}
	if updated.Rule.CategoryID() != rent {
		t.Error("UpdateRecurringRule must not change the category")
	}
	if updated.Rule.StartsOn().String() != "2026-01-01" {
		t.Error("UpdateRecurringRule must not change starts_on")
	}
}

func TestUpdateRecurringRule_ClearsEndsOnWhenOmitted(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	created, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  account,
		CategoryRef: rent,
		Amount:      "100.00",
		Description: "Bounded",
		Schedule:    app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
		StartsOn:    "2026-01-01",
		EndsOn:      "2026-06-01",
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}

	updated, err := svc.UpdateRecurringRule(ctx, app.UpdateRecurringRuleCommand{
		ActorID:     testActorID,
		RuleID:      created.Rule.ID(),
		Amount:      "100.00",
		Description: "Now unbounded",
		Schedule:    app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	if err != nil {
		t.Fatalf("UpdateRecurringRule: %v", err)
	}
	if _, ok := updated.Rule.EndsOn(); ok {
		t.Error("omitting ends_on on update should clear it to unbounded")
	}
}

func TestUpdateRecurringRule_UnknownRuleIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	_, err := svc.UpdateRecurringRule(context.Background(), app.UpdateRecurringRuleCommand{
		ActorID: testActorID, RuleID: "does-not-exist", Amount: "10.00", Description: "X",
		Schedule: app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestArchiveRecurringRule(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	created, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  account,
		CategoryRef: rent,
		Amount:      "100.00",
		Description: "Rent",
		Schedule:    app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}

	archived, err := svc.ArchiveRecurringRule(ctx, app.ArchiveRecurringRuleCommand{ActorID: testActorID, RuleID: created.Rule.ID()})
	if err != nil {
		t.Fatalf("ArchiveRecurringRule: %v", err)
	}
	if !archived.Rule.Archived() {
		t.Fatal("rule should be archived")
	}
	if archivedAt, ok := archived.Rule.ArchivedAt(); !ok || archivedAt.String() != "2026-06-15" {
		t.Errorf("ArchivedAt = %v, %v, want 2026-06-15, true", archivedAt, ok)
	}

	// Archiving twice is idempotent and doesn't move the archive date.
	archivedAgain, err := svc.ArchiveRecurringRule(ctx, app.ArchiveRecurringRuleCommand{ActorID: testActorID, RuleID: created.Rule.ID()})
	if err != nil {
		t.Fatalf("ArchiveRecurringRule (again): %v", err)
	}
	if got, _ := archivedAgain.Rule.ArchivedAt(); got.String() != "2026-06-15" {
		t.Errorf("re-archiving moved ArchivedAt to %v", got)
	}

	// An archived rule stays fully queryable.
	fetched, err := svc.GetRecurringRule(ctx, app.GetRecurringRuleQuery{ActorID: testActorID, RuleID: created.Rule.ID()})
	if err != nil {
		t.Fatalf("GetRecurringRule after archive: %v", err)
	}
	if !fetched.Rule.Archived() {
		t.Error("archived rule should stay archived when re-fetched")
	}
	listed, err := svc.ListRecurringRules(ctx, app.ListRecurringRulesQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListRecurringRules after archive: %v", err)
	}
	if len(listed.Rules) != 1 {
		t.Errorf("ListRecurringRules must still return an archived rule, got %d", len(listed.Rules))
	}
}

func TestGetRecurringRule_AnotherActorsRuleIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	created, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID:     testActorID,
		AccountRef:  account,
		CategoryRef: rent,
		Amount:      "10.00",
		Description: "Rent",
		Schedule:    app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	_, err = svc.GetRecurringRule(ctx, app.GetRecurringRuleQuery{ActorID: "someone-else", RuleID: created.Rule.ID()})
	wantErrCode(t, err, errs.NotFound)
}

func TestListRecurringRules(t *testing.T) {
	svc := newTestService(t, time.Now(), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	sched := app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1}

	if _, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID: testActorID, AccountRef: account, CategoryRef: rent, Amount: "10.00", Description: "A", Schedule: sched,
	}); err != nil {
		t.Fatalf("CreateRecurringRule A: %v", err)
	}
	if _, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID: testActorID, AccountRef: account, CategoryRef: rent, Amount: "20.00", Description: "B", Schedule: sched,
	}); err != nil {
		t.Fatalf("CreateRecurringRule B: %v", err)
	}
	// A second actor's rules must never appear in the first actor's list.
	otherAccount := mustCreateAccount(t, svc, "someone-else", "Their Checking")
	otherCategory, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: "someone-else", Name: "Rent", Kind: "expense"})
	if err != nil {
		t.Fatalf("CreateCategory (other actor): %v", err)
	}
	if _, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID: "someone-else", AccountRef: otherAccount, CategoryRef: otherCategory.Category.ID(),
		Amount: "30.00", Description: "Not mine", Schedule: sched,
	}); err != nil {
		t.Fatalf("CreateRecurringRule (other actor): %v", err)
	}

	result, err := svc.ListRecurringRules(ctx, app.ListRecurringRulesQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListRecurringRules: %v", err)
	}
	if len(result.Rules) != 2 {
		t.Fatalf("len(Rules) = %d, want 2", len(result.Rules))
	}
	// Most recently created first.
	if result.Rules[0].Description() != "B" {
		t.Errorf("Rules[0].Description = %q, want %q (most recent first)", result.Rules[0].Description(), "B")
	}
}

// TestUpdateRecurringRule_ScheduleChangeRegeneratesFutureOccurrences is
// issue #278's core regression: editing a rule's schedule must discard and
// regenerate its pending *future* occurrences (on/after today) while
// leaving past-pending, materialised, and skipped occurrences exactly as
// they were (ADR-0014).
func TestUpdateRecurringRule_ScheduleChangeRegeneratesFutureOccurrences(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	created, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID: testActorID, AccountRef: account, CategoryRef: rent,
		Amount: "100.00", Description: "Rent", StartsOn: "2026-01-01",
		Schedule: app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	ruleID := created.Rule.ID()

	generated, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: ruleID})
	if err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}
	// StartsOn=2026-01-01 with "today"=2026-03-15 means Jan/Feb/Mar's
	// monthly-day-1 occurrences are all in the past relative to today.
	jan := occurrenceByDate(t, generated.Created, "2026-01-01")
	feb := occurrenceByDate(t, generated.Created, "2026-02-01")
	mar := occurrenceByDate(t, generated.Created, "2026-03-01")
	apr := occurrenceByDate(t, generated.Created, "2026-04-01") // a future one, must be discarded

	materialised, err := jan.MarkMaterialised("txn-1")
	if err != nil {
		t.Fatalf("MarkMaterialised: %v", err)
	}
	if err := svc.ScheduledOccurrences.Update(ctx, testActorID, materialised); err != nil {
		t.Fatalf("Update (materialise Jan): %v", err)
	}
	skipped, err := feb.MarkSkipped()
	if err != nil {
		t.Fatalf("MarkSkipped: %v", err)
	}
	if err := svc.ScheduledOccurrences.Update(ctx, testActorID, skipped); err != nil {
		t.Fatalf("Update (skip Feb): %v", err)
	}

	// Change the schedule to something with no overlapping dates.
	if _, err := svc.UpdateRecurringRule(ctx, app.UpdateRecurringRuleCommand{
		ActorID: testActorID, RuleID: ruleID,
		Amount: "100.00", Description: "Rent",
		Schedule: app.RecurringScheduleInput{Frequency: "weekly", Interval: 1, Weekday: time.Friday},
	}); err != nil {
		t.Fatalf("UpdateRecurringRule: %v", err)
	}

	after := listOccurrences(t, svc, testActorID, ports.ScheduledOccurrenceFilter{RuleID: ruleID})

	stillMaterialised := occurrenceByDate(t, after, "2026-01-01")
	if stillMaterialised.Status() != recurring.OccurrenceStatusMaterialised {
		t.Errorf("Jan occurrence status = %s, want materialised (must survive a schedule change)", stillMaterialised.Status())
	}
	if txID, ok := stillMaterialised.TransactionID(); !ok || txID != "txn-1" {
		t.Errorf("Jan occurrence TransactionID = %q, %v, want txn-1, true", txID, ok)
	}

	stillSkipped := occurrenceByDate(t, after, "2026-02-01")
	if stillSkipped.Status() != recurring.OccurrenceStatusSkipped {
		t.Errorf("Feb occurrence status = %s, want skipped (must survive a schedule change)", stillSkipped.Status())
	}

	stillPastPending := occurrenceByDate(t, after, "2026-03-01")
	if stillPastPending.ID() != mar.ID() || stillPastPending.Status() != recurring.OccurrenceStatusPending {
		t.Error("a past-pending occurrence (before today) must not be touched by a schedule change")
	}

	for _, o := range after {
		if o.ID() == apr.ID() {
			t.Error("a future pending occurrence under the old schedule must be discarded on a schedule change")
		}
	}

	var newFridayFound bool
	for _, o := range after {
		d := o.OccurrenceDate()
		wd := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC).Weekday()
		if wd == time.Friday && d.String() >= "2026-03-15" {
			newFridayFound = true
			if o.Status() != recurring.OccurrenceStatusPending {
				t.Errorf("newly regenerated Friday occurrence %s should be pending, got %s", d, o.Status())
			}
		}
	}
	if !newFridayFound {
		t.Error("expected at least one newly regenerated Friday occurrence on/after today")
	}
}

// TestUpdateRecurringRule_NoScheduleChangeLeavesOccurrencesUntouched proves
// an amount/description/ends_on-only edit never triggers occurrence
// regeneration.
func TestUpdateRecurringRule_NoScheduleChangeLeavesOccurrencesUntouched(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	created, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID: testActorID, AccountRef: account, CategoryRef: rent,
		Amount: "100.00", Description: "Rent", StartsOn: "2026-01-01",
		Schedule: app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	ruleID := created.Rule.ID()

	generated, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: ruleID})
	if err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}
	before := listOccurrences(t, svc, testActorID, ports.ScheduledOccurrenceFilter{RuleID: ruleID})
	if len(before) != len(generated.Created) {
		t.Fatalf("before count = %d, want %d", len(before), len(generated.Created))
	}

	if _, err := svc.UpdateRecurringRule(ctx, app.UpdateRecurringRuleCommand{
		ActorID: testActorID, RuleID: ruleID,
		Amount: "150.00", Description: "Rent (increased)",
		Schedule: app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	}); err != nil {
		t.Fatalf("UpdateRecurringRule: %v", err)
	}

	after := listOccurrences(t, svc, testActorID, ports.ScheduledOccurrenceFilter{RuleID: ruleID})
	if len(after) != len(before) {
		t.Fatalf("occurrence count changed from %d to %d after a non-schedule edit", len(before), len(after))
	}
	for i := range before {
		if before[i].ID() != after[i].ID() || before[i].OccurrenceDate() != after[i].OccurrenceDate() {
			t.Errorf("occurrence %d changed identity across a non-schedule edit: %v -> %v", i, before[i], after[i])
		}
	}
}

// TestArchiveRecurringRule_CancelsPendingOccurrences is issue #278's other
// hook into #277's CRUD: archiving a rule must remove its still-pending
// occurrences rather than leaving them dangling (ADR-0014/#278's scope).
func TestArchiveRecurringRule_CancelsPendingOccurrences(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")

	created, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID: testActorID, AccountRef: account, CategoryRef: rent,
		Amount: "100.00", Description: "Rent", StartsOn: "2026-01-01",
		Schedule: app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	ruleID := created.Rule.ID()

	generated, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: ruleID})
	if err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}
	if len(generated.Created) == 0 {
		t.Fatal("expected generation to produce at least one occurrence")
	}
	// Materialise one so we can prove it survives the archive-time cleanup.
	jan := occurrenceByDate(t, generated.Created, "2026-01-01")
	materialised, err := jan.MarkMaterialised("txn-1")
	if err != nil {
		t.Fatalf("MarkMaterialised: %v", err)
	}
	if err := svc.ScheduledOccurrences.Update(ctx, testActorID, materialised); err != nil {
		t.Fatalf("Update (materialise Jan): %v", err)
	}

	if _, err := svc.ArchiveRecurringRule(ctx, app.ArchiveRecurringRuleCommand{ActorID: testActorID, RuleID: ruleID}); err != nil {
		t.Fatalf("ArchiveRecurringRule: %v", err)
	}

	after := listOccurrences(t, svc, testActorID, ports.ScheduledOccurrenceFilter{RuleID: ruleID})
	if len(after) != 1 {
		t.Fatalf("after archive, len(occurrences) = %d, want 1 (only the materialised one survives)", len(after))
	}
	if after[0].Status() != recurring.OccurrenceStatusMaterialised {
		t.Errorf("surviving occurrence status = %s, want materialised", after[0].Status())
	}

	// Re-archiving (idempotent no-op) must not error and must not touch
	// the surviving materialised occurrence again.
	if _, err := svc.ArchiveRecurringRule(ctx, app.ArchiveRecurringRuleCommand{ActorID: testActorID, RuleID: ruleID}); err != nil {
		t.Fatalf("ArchiveRecurringRule (again): %v", err)
	}
	stillAfter := listOccurrences(t, svc, testActorID, ports.ScheduledOccurrenceFilter{RuleID: ruleID})
	if len(stillAfter) != 1 {
		t.Fatalf("after re-archiving, len(occurrences) = %d, want 1", len(stillAfter))
	}
}
