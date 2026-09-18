package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/recurring"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func mustMonthlySchedule(t *testing.T, interval, dayOfMonth int) recurring.Schedule {
	t.Helper()
	s, err := recurring.NewMonthlySchedule(interval, dayOfMonth)
	if err != nil {
		t.Fatalf("recurring.NewMonthlySchedule: %v", err)
	}
	return s
}

func mustRule(t *testing.T, id, userID, accountID, categoryID string, schedule recurring.Schedule, opts ...recurring.RecurringRuleOption) recurring.RecurringRule {
	t.Helper()
	r, err := recurring.NewRecurringRule(id, userID, accountID, categoryID, 50000, id+"-description",
		schedule, mustDate(t, 2026, time.January, 1), opts...)
	if err != nil {
		t.Fatalf("recurring.NewRecurringRule: %v", err)
	}
	return r
}

func TestRecurringRuleRepository_CreateGet(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-rent")

	rule := mustRule(t, "r1", ports.SeededUserID, "acc-1", "cat-rent", mustMonthlySchedule(t, 1, 31))

	repo := NewRecurringRuleRepository(db)
	if err := repo.Create(ctx, ports.SeededUserID, rule); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "r1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != rule.ID() || got.UserID() != rule.UserID() || got.AccountID() != rule.AccountID() ||
		got.CategoryID() != rule.CategoryID() || got.AmountMinor() != rule.AmountMinor() ||
		got.Description() != rule.Description() || got.StartsOn() != rule.StartsOn() {
		t.Errorf("Get() = %+v, want a round trip of %+v", got, rule)
	}
	if got.Schedule().String() != rule.Schedule().String() {
		t.Errorf("Get().Schedule() = %q, want %q", got.Schedule().String(), rule.Schedule().String())
	}
	if _, ok := got.EndsOn(); ok {
		t.Error("a rule created without an end date must read back without one")
	}
	if got.Archived() {
		t.Error("a freshly created rule must not be archived")
	}
}

// TestRecurringRuleRepository_ScheduleRoundTrip covers all three
// frequencies through the typed-column mapping ADR-0014 chose over a
// stored RRULE string: what goes in as a Schedule comes back as the same
// Schedule.
func TestRecurringRuleRepository_ScheduleRoundTrip(t *testing.T) {
	weekly, err := recurring.NewWeeklySchedule(2, time.Friday)
	if err != nil {
		t.Fatalf("NewWeeklySchedule: %v", err)
	}
	yearly, err := recurring.NewYearlySchedule(1, time.February, 29)
	if err != nil {
		t.Fatalf("NewYearlySchedule: %v", err)
	}

	tests := []struct {
		id       string
		schedule recurring.Schedule
	}{
		{"r-weekly", weekly},
		{"r-monthly", mustMonthlySchedule(t, 3, 31)},
		{"r-yearly", yearly},
	}

	db, _ := newTestDB(t)
	ctx := context.Background()
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-rent")
	repo := NewRecurringRuleRepository(db)

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			rule := mustRule(t, tt.id, ports.SeededUserID, "acc-1", "cat-rent", tt.schedule)
			if err := repo.Create(ctx, ports.SeededUserID, rule); err != nil {
				t.Fatalf("Create: %v", err)
			}

			got, err := repo.Get(ctx, ports.SeededUserID, tt.id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Schedule().String() != tt.schedule.String() {
				t.Errorf("Schedule() = %q, want %q", got.Schedule().String(), tt.schedule.String())
			}
			if got.Schedule().Frequency() != tt.schedule.Frequency() || got.Schedule().Interval() != tt.schedule.Interval() {
				t.Errorf("Schedule() = %+v, want %+v", got.Schedule(), tt.schedule)
			}
		})
	}
}

func TestRecurringRuleRepository_EndsOnAndArchivedAtRoundTrip(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-rent")

	endsOn := mustDate(t, 2026, time.December, 31)
	archivedOn := mustDate(t, 2026, time.June, 1)
	rule := mustRule(t, "r1", ports.SeededUserID, "acc-1", "cat-rent", mustMonthlySchedule(t, 1, 15),
		recurring.WithEndsOn(endsOn), recurring.WithArchivedAt(archivedOn))

	repo := NewRecurringRuleRepository(db)
	if err := repo.Create(ctx, ports.SeededUserID, rule); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "r1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotEnds, ok := got.EndsOn(); !ok || gotEnds != endsOn {
		t.Errorf("EndsOn() = (%v, %v), want (%v, true)", gotEnds, ok, endsOn)
	}
	if gotArchived, ok := got.ArchivedAt(); !ok || gotArchived != archivedOn {
		t.Errorf("ArchivedAt() = (%v, %v), want (%v, true)", gotArchived, ok, archivedOn)
	}
}

func TestRecurringRuleRepository_Create_RejectsMismatchedActor(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-rent")

	rule := mustRule(t, "r1", ports.SeededUserID, "acc-1", "cat-rent", mustMonthlySchedule(t, 1, 15))

	repo := NewRecurringRuleRepository(db)
	err := repo.Create(ctx, otherUserID, rule)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("Create(mismatched actor) error = %v, want *errs.Error with code NotAllowed", err)
	}
}

func TestRecurringRuleRepository_Get_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewRecurringRuleRepository(db)
	_, err := repo.Get(context.Background(), ports.SeededUserID, "no-such-rule")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Get(unknown) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestRecurringRuleRepository_Get_CrossUserIsolation(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-rent")

	repo := NewRecurringRuleRepository(db)
	if err := repo.Create(ctx, ports.SeededUserID, mustRule(t, "r1", ports.SeededUserID, "acc-1", "cat-rent", mustMonthlySchedule(t, 1, 15))); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := repo.Get(ctx, otherUserID, "r1")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Get(other user's rule) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestRecurringRuleRepository_List(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-rent")
	seedAccountAndCategory(t, db, otherUserID, "acc-other", "cat-other")

	repo := NewRecurringRuleRepository(db)
	schedule := mustMonthlySchedule(t, 1, 15)
	if err := repo.Create(ctx, ports.SeededUserID, mustRule(t, "rule-a", ports.SeededUserID, "acc-1", "cat-rent", schedule)); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, mustRule(t, "rule-b", ports.SeededUserID, "acc-1", "cat-rent", schedule)); err != nil {
		t.Fatalf("Create b: %v", err)
	}
	if err := repo.Create(ctx, otherUserID, mustRule(t, "rule-other", otherUserID, "acc-other", "cat-other", schedule)); err != nil {
		t.Fatalf("Create other: %v", err)
	}

	got, err := repo.List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d rules, want 2 (never another user's)", len(got))
	}
	// Every insert lands under the same frozen-clock instant, so the tie
	// is broken by id DESC — the same convention TestBudgetRepository_List
	// documents.
	if got[0].ID() != "rule-b" || got[1].ID() != "rule-a" {
		t.Errorf("List order = [%s, %s], want [rule-b, rule-a]", got[0].ID(), got[1].ID())
	}
}

func TestRecurringRuleRepository_Update(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-rent")
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-2", "cat-gym")

	repo := NewRecurringRuleRepository(db)
	rule := mustRule(t, "r1", ports.SeededUserID, "acc-1", "cat-rent", mustMonthlySchedule(t, 1, 31))
	if err := repo.Create(ctx, ports.SeededUserID, rule); err != nil {
		t.Fatalf("Create: %v", err)
	}

	weekly, err := recurring.NewWeeklySchedule(1, time.Monday)
	if err != nil {
		t.Fatalf("NewWeeklySchedule: %v", err)
	}
	updated, err := recurring.NewRecurringRule(rule.ID(), rule.UserID(), "acc-2", "cat-gym", 12500, "Gym membership",
		weekly, rule.StartsOn(), recurring.WithEndsOn(mustDate(t, 2027, time.January, 1)))
	if err != nil {
		t.Fatalf("NewRecurringRule(updated): %v", err)
	}

	if err := repo.Update(ctx, ports.SeededUserID, updated); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "r1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccountID() != "acc-2" || got.CategoryID() != "cat-gym" ||
		got.AmountMinor() != 12500 || got.Description() != "Gym membership" {
		t.Errorf("Get() after Update = %+v, want the updated fields", got)
	}
	if got.Schedule().String() != weekly.String() {
		t.Errorf("Schedule() after Update = %q, want %q", got.Schedule().String(), weekly.String())
	}
	// The weekly schedule's columns replace the monthly one's wholesale —
	// day_of_month must not survive as a stale leftover, or the schema's
	// own frequency CHECK would have rejected the write.
	if _, ok := got.Schedule().DayOfMonth(); ok {
		t.Error("a rule updated from monthly to weekly still reports a day of month")
	}
}

func TestRecurringRuleRepository_Update_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewRecurringRuleRepository(db)
	rule := mustRule(t, "no-such-rule", ports.SeededUserID, "acc-1", "cat-rent", mustMonthlySchedule(t, 1, 15))
	err := repo.Update(context.Background(), ports.SeededUserID, rule)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Update(unknown) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestRecurringRuleRepository_Update_RejectsMismatchedActor(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-rent")

	repo := NewRecurringRuleRepository(db)
	rule := mustRule(t, "r1", ports.SeededUserID, "acc-1", "cat-rent", mustMonthlySchedule(t, 1, 15))
	if err := repo.Create(ctx, ports.SeededUserID, rule); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := repo.Update(ctx, otherUserID, rule)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("Update(mismatched actor) error = %v, want *errs.Error with code NotAllowed", err)
	}
}

// TestRecurringRuleRepository_Create_RejectsUnknownReferences proves the
// foreign keys are live: a rule against an account or category that
// doesn't exist is InvalidInput, not a silently dangling row.
func TestRecurringRuleRepository_Create_RejectsUnknownReferences(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-rent")

	repo := NewRecurringRuleRepository(db)
	tests := []struct {
		name       string
		accountID  string
		categoryID string
	}{
		{"unknown account", "no-such-account", "cat-rent"},
		{"unknown category", "acc-1", "no-such-category"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := mustRule(t, "r-"+tt.name, ports.SeededUserID, tt.accountID, tt.categoryID, mustMonthlySchedule(t, 1, 15))
			err := repo.Create(ctx, ports.SeededUserID, rule)
			var e *errs.Error
			if !errors.As(err, &e) || e.Code != errs.InvalidInput {
				t.Fatalf("Create(%s) error = %v, want *errs.Error with code InvalidInput", tt.name, err)
			}
		})
	}
}
