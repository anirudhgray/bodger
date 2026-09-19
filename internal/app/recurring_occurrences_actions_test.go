package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// mustCreateIncomeCategory is mustCreateCategory's income-kind sibling, so
// materialisation direction tests can build an inflow-producing rule.
func mustCreateIncomeCategory(t *testing.T, svc *app.Service, name string) string {
	t.Helper()
	result, err := svc.CreateCategory(context.Background(), app.CreateCategoryCommand{
		ActorID: testActorID,
		Name:    name,
		Kind:    "income",
	})
	if err != nil {
		t.Fatalf("CreateCategory(%q): %v", name, err)
	}
	return result.Category.ID()
}

// mustCreateRuleWithOccurrence creates a monthly rule and generates its one
// occurrence on startsOn, returning both. A shared fixture builder for the
// materialise/skip tests below, which mostly differ only in category kind
// or in what happens to the rule/occurrence afterward.
func mustCreateRuleWithOccurrence(t *testing.T, svc *app.Service, accountID, categoryID, amount, description, startsOn string) (recurring.RecurringRule, recurring.ScheduledOccurrence) {
	t.Helper()
	ctx := context.Background()
	created, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
		ActorID: testActorID, AccountRef: accountID, CategoryRef: categoryID,
		Amount: amount, Description: description, StartsOn: startsOn,
		Schedule: app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	})
	if err != nil {
		t.Fatalf("CreateRecurringRule: %v", err)
	}
	generated, err := svc.GenerateOccurrences(ctx, app.GenerateOccurrencesCommand{ActorID: testActorID, RuleID: created.Rule.ID()})
	if err != nil {
		t.Fatalf("GenerateOccurrences: %v", err)
	}
	if len(generated.Created) == 0 {
		t.Fatalf("GenerateOccurrences produced no occurrences")
	}
	return created.Rule, generated.Created[0]
}

func TestMaterialiseOccurrence_Outflow(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	_, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	result, err := svc.MaterialiseOccurrence(context.Background(), app.MaterialiseOccurrenceCommand{
		ActorID: testActorID, OccurrenceID: occ.ID(),
	})
	if err != nil {
		t.Fatalf("MaterialiseOccurrence: %v", err)
	}
	if result.Transaction.Kind() != ledger.TransactionKindOutflow {
		t.Errorf("Transaction.Kind() = %q, want %q", result.Transaction.Kind(), ledger.TransactionKindOutflow)
	}
	postings := result.Transaction.Postings()
	if len(postings) != 1 || !postings[0].Amount().IsNegative() {
		t.Errorf("Postings() = %+v, want exactly one negative posting for an expense-category rule", postings)
	}
	if result.Occurrence.Status() != recurring.OccurrenceStatusMaterialised {
		t.Errorf("Occurrence.Status() = %q, want %q", result.Occurrence.Status(), recurring.OccurrenceStatusMaterialised)
	}
	if id, ok := result.Occurrence.TransactionID(); !ok || id != result.Transaction.ID() {
		t.Errorf("Occurrence.TransactionID() = (%q, %v), want (%q, true)", id, ok, result.Transaction.ID())
	}
}

func TestMaterialiseOccurrence_Inflow(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	salary := mustCreateIncomeCategory(t, svc, "Salary")
	_, occ := mustCreateRuleWithOccurrence(t, svc, account, salary, "5000.00", "Salary", "2026-01-01")

	result, err := svc.MaterialiseOccurrence(context.Background(), app.MaterialiseOccurrenceCommand{
		ActorID: testActorID, OccurrenceID: occ.ID(),
	})
	if err != nil {
		t.Fatalf("MaterialiseOccurrence: %v", err)
	}
	if result.Transaction.Kind() != ledger.TransactionKindInflow {
		t.Errorf("Transaction.Kind() = %q, want %q", result.Transaction.Kind(), ledger.TransactionKindInflow)
	}
	postings := result.Transaction.Postings()
	if len(postings) != 1 || postings[0].Amount().IsNegative() {
		t.Errorf("Postings() = %+v, want exactly one positive posting for an income-category rule", postings)
	}
}

// TestMaterialiseOccurrence_UsesOccurrenceDateNotToday freezes the clock
// well after the occurrence's own projected date, so a bug that booked the
// transaction "today" instead would be caught immediately.
func TestMaterialiseOccurrence_UsesOccurrenceDateNotToday(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC), "UTC")
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	_, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")
	if occ.OccurrenceDate().String() != "2026-01-01" {
		t.Fatalf("fixture occurrence date = %s, want 2026-01-01", occ.OccurrenceDate())
	}

	result, err := svc.MaterialiseOccurrence(context.Background(), app.MaterialiseOccurrenceCommand{
		ActorID: testActorID, OccurrenceID: occ.ID(),
	})
	if err != nil {
		t.Fatalf("MaterialiseOccurrence: %v", err)
	}
	if result.Transaction.BookedDate().String() != "2026-01-01" {
		t.Errorf("Transaction.BookedDate() = %s, want the occurrence's own projected date 2026-01-01, not today", result.Transaction.BookedDate())
	}
}

// TestMaterialiseOccurrence_UsesRulesCurrentFields proves an occurrence
// carries nothing frozen from generation time: editing the rule's amount
// and description after generating still changes what a later
// materialisation produces, since the occurrence itself has no copy of
// either field to fall back on.
func TestMaterialiseOccurrence_UsesRulesCurrentFields(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	rule, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	// Edit amount/description only — an unchanged schedule, so #278's
	// schedule-change regeneration never fires and this occurrence survives
	// untouched.
	if _, err := svc.UpdateRecurringRule(ctx, app.UpdateRecurringRuleCommand{
		ActorID: testActorID, RuleID: rule.ID(),
		Amount: "1500.00", Description: "Rent (increased)",
		Schedule: app.RecurringScheduleInput{Frequency: "monthly", Interval: 1, DayOfMonth: 1},
	}); err != nil {
		t.Fatalf("UpdateRecurringRule: %v", err)
	}

	result, err := svc.MaterialiseOccurrence(ctx, app.MaterialiseOccurrenceCommand{
		ActorID: testActorID, OccurrenceID: occ.ID(),
	})
	if err != nil {
		t.Fatalf("MaterialiseOccurrence: %v", err)
	}
	if result.Transaction.Description() != "Rent (increased)" {
		t.Errorf("Transaction.Description() = %q, want the rule's current description", result.Transaction.Description())
	}
	postings := result.Transaction.Postings()
	if len(postings) != 1 || postings[0].Amount().AmountMinor() != -150000 {
		t.Errorf("Postings() = %+v, want the rule's current amount (150000 minor units), not the original", postings)
	}
}

// TestMaterialiseOccurrence_SucceedsForArchivedRule proves materialisation
// doesn't refuse an archived rule's occurrence. In normal operation
// ArchiveRecurringRule (issue #278) cancels every still-pending occurrence
// at archive time, so this state has to be constructed directly at the
// repository level rather than through the app's own ArchiveRecurringRule
// call — this test is about MaterialiseOccurrence's own contract (issue
// #279's explicit "still valid" requirement), independent of whether
// today's archive flow happens to ever produce it.
func TestMaterialiseOccurrence_SucceedsForArchivedRule(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	rule, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	archived, err := recurring.NewRecurringRule(
		rule.ID(), testActorID, rule.AccountID(), rule.CategoryID(),
		rule.AmountMinor(), rule.Description(), rule.Schedule(), rule.StartsOn(),
		recurring.WithArchivedAt(rule.StartsOn()),
	)
	if err != nil {
		t.Fatalf("recurring.NewRecurringRule: %v", err)
	}
	if err := svc.RecurringRules.Update(ctx, testActorID, archived); err != nil {
		t.Fatalf("RecurringRules.Update: %v", err)
	}

	result, err := svc.MaterialiseOccurrence(ctx, app.MaterialiseOccurrenceCommand{
		ActorID: testActorID, OccurrenceID: occ.ID(),
	})
	if err != nil {
		t.Fatalf("MaterialiseOccurrence for an archived rule's occurrence: %v", err)
	}
	if result.Occurrence.Status() != recurring.OccurrenceStatusMaterialised {
		t.Errorf("Occurrence.Status() = %q, want %q", result.Occurrence.Status(), recurring.OccurrenceStatusMaterialised)
	}
}

func TestMaterialiseOccurrence_RejectsAlreadyMaterialised(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	_, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	if _, err := svc.MaterialiseOccurrence(ctx, app.MaterialiseOccurrenceCommand{ActorID: testActorID, OccurrenceID: occ.ID()}); err != nil {
		t.Fatalf("MaterialiseOccurrence (first): %v", err)
	}

	_, err := svc.MaterialiseOccurrence(ctx, app.MaterialiseOccurrenceCommand{ActorID: testActorID, OccurrenceID: occ.ID()})
	wantErrCode(t, err, errs.Conflict)
}

func TestMaterialiseOccurrence_RejectsAlreadySkipped(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	_, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	if _, err := svc.SkipOccurrence(ctx, app.SkipOccurrenceCommand{ActorID: testActorID, OccurrenceID: occ.ID()}); err != nil {
		t.Fatalf("SkipOccurrence: %v", err)
	}

	_, err := svc.MaterialiseOccurrence(ctx, app.MaterialiseOccurrenceCommand{ActorID: testActorID, OccurrenceID: occ.ID()})
	wantErrCode(t, err, errs.Conflict)
}

func TestMaterialiseOccurrence_AnotherActorsOccurrenceIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	_, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	_, err := svc.MaterialiseOccurrence(ctx, app.MaterialiseOccurrenceCommand{ActorID: "someone-else", OccurrenceID: occ.ID()})
	wantErrCode(t, err, errs.NotFound)
}

func TestSkipOccurrence(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	_, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	result, err := svc.SkipOccurrence(ctx, app.SkipOccurrenceCommand{ActorID: testActorID, OccurrenceID: occ.ID()})
	if err != nil {
		t.Fatalf("SkipOccurrence: %v", err)
	}
	if result.Occurrence.Status() != recurring.OccurrenceStatusSkipped {
		t.Errorf("Occurrence.Status() = %q, want %q", result.Occurrence.Status(), recurring.OccurrenceStatusSkipped)
	}
	if _, ok := result.Occurrence.TransactionID(); ok {
		t.Error("a skipped occurrence must not carry a transaction id")
	}
}

func TestSkipOccurrence_RejectsAlreadySkipped(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	_, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	if _, err := svc.SkipOccurrence(ctx, app.SkipOccurrenceCommand{ActorID: testActorID, OccurrenceID: occ.ID()}); err != nil {
		t.Fatalf("SkipOccurrence (first): %v", err)
	}
	_, err := svc.SkipOccurrence(ctx, app.SkipOccurrenceCommand{ActorID: testActorID, OccurrenceID: occ.ID()})
	wantErrCode(t, err, errs.Conflict)
}

func TestSkipOccurrence_RejectsAlreadyMaterialised(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	_, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	if _, err := svc.MaterialiseOccurrence(ctx, app.MaterialiseOccurrenceCommand{ActorID: testActorID, OccurrenceID: occ.ID()}); err != nil {
		t.Fatalf("MaterialiseOccurrence: %v", err)
	}
	_, err := svc.SkipOccurrence(ctx, app.SkipOccurrenceCommand{ActorID: testActorID, OccurrenceID: occ.ID()})
	wantErrCode(t, err, errs.Conflict)
}

func TestSkipOccurrence_AnotherActorsOccurrenceIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	account := mustCreateAccount(t, svc, testActorID, "Checking")
	rent := mustCreateCategory(t, svc, "Rent")
	_, occ := mustCreateRuleWithOccurrence(t, svc, account, rent, "1200.00", "Rent", "2026-01-01")

	_, err := svc.SkipOccurrence(ctx, app.SkipOccurrenceCommand{ActorID: "someone-else", OccurrenceID: occ.ID()})
	wantErrCode(t, err, errs.NotFound)
}
