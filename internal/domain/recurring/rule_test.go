package recurring_test

import (
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/recurring"
)

func TestNewRecurringRule(t *testing.T) {
	startsOn := mustDate(t, 2026, time.January, 1)
	schedule := mustMonthly(t, 1, 15)

	r, err := recurring.NewRecurringRule("r1", "user-1", "acc-1", "cat-1", 50000, "Rent", schedule, startsOn)
	if err != nil {
		t.Fatalf("NewRecurringRule: %v", err)
	}
	if r.ID() != "r1" || r.UserID() != "user-1" || r.AccountID() != "acc-1" || r.CategoryID() != "cat-1" ||
		r.AmountMinor() != 50000 || r.Description() != "Rent" || r.StartsOn() != startsOn {
		t.Errorf("NewRecurringRule() = %+v, unexpected field values", r)
	}
	if r.Schedule().String() != schedule.String() {
		t.Errorf("Schedule() = %q, want %q", r.Schedule().String(), schedule.String())
	}
	if _, ok := r.EndsOn(); ok {
		t.Error("a rule constructed without WithEndsOn must report no end date")
	}
	if r.Archived() {
		t.Error("a freshly constructed rule must not be archived")
	}
}

func TestNewRecurringRule_Validation(t *testing.T) {
	startsOn := mustDate(t, 2026, time.January, 1)
	schedule := mustMonthly(t, 1, 15)

	tests := []struct {
		name        string
		id          string
		userID      string
		accountID   string
		categoryID  string
		amountMinor int64
		description string
		schedule    recurring.Schedule
		wantErr     error
	}{
		{"empty id", "", "user-1", "acc-1", "cat-1", 1, "Rent", schedule, recurring.ErrRuleEmptyID},
		{"empty user id", "r1", "", "acc-1", "cat-1", 1, "Rent", schedule, recurring.ErrRuleEmptyUserID},
		{"empty account id", "r1", "user-1", "", "cat-1", 1, "Rent", schedule, recurring.ErrRuleEmptyAccountID},
		{"empty category id", "r1", "user-1", "acc-1", "", 1, "Rent", schedule, recurring.ErrRuleEmptyCategoryID},
		{"zero amount", "r1", "user-1", "acc-1", "cat-1", 0, "Rent", schedule, recurring.ErrRuleAmountNotPositive},
		{"negative amount", "r1", "user-1", "acc-1", "cat-1", -100, "Rent", schedule, recurring.ErrRuleAmountNotPositive},
		{"empty description", "r1", "user-1", "acc-1", "cat-1", 1, "", schedule, recurring.ErrRuleEmptyDescription},
		{"zero schedule", "r1", "user-1", "acc-1", "cat-1", 1, "Rent", recurring.Schedule{}, recurring.ErrRuleInvalidSchedule},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := recurring.NewRecurringRule(tt.id, tt.userID, tt.accountID, tt.categoryID,
				tt.amountMinor, tt.description, tt.schedule, startsOn)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("NewRecurringRule() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewRecurringRule_EndsOn(t *testing.T) {
	startsOn := mustDate(t, 2026, time.January, 1)
	schedule := mustMonthly(t, 1, 15)

	t.Run("rejects an end before the start", func(t *testing.T) {
		_, err := recurring.NewRecurringRule("r1", "user-1", "acc-1", "cat-1", 1, "Rent", schedule, startsOn,
			recurring.WithEndsOn(mustDate(t, 2025, time.December, 31)))
		if !errors.Is(err, recurring.ErrRuleEndsBeforeStart) {
			t.Errorf("NewRecurringRule() error = %v, want ErrRuleEndsBeforeStart", err)
		}
	})

	t.Run("accepts an end equal to the start", func(t *testing.T) {
		r, err := recurring.NewRecurringRule("r1", "user-1", "acc-1", "cat-1", 1, "Rent", schedule, startsOn,
			recurring.WithEndsOn(startsOn))
		if err != nil {
			t.Fatalf("NewRecurringRule: %v", err)
		}
		if got, ok := r.EndsOn(); !ok || got != startsOn {
			t.Errorf("EndsOn() = (%v, %v), want (%v, true)", got, ok, startsOn)
		}
	})
}

func TestRecurringRule_WithArchivedAt(t *testing.T) {
	startsOn := mustDate(t, 2026, time.January, 1)
	archivedOn := mustDate(t, 2026, time.June, 1)

	r, err := recurring.NewRecurringRule("r1", "user-1", "acc-1", "cat-1", 1, "Rent",
		mustMonthly(t, 1, 15), startsOn, recurring.WithArchivedAt(archivedOn))
	if err != nil {
		t.Fatalf("NewRecurringRule: %v", err)
	}
	if !r.Archived() {
		t.Error("Archived() = false, want true after WithArchivedAt")
	}
	if got, ok := r.ArchivedAt(); !ok || got != archivedOn {
		t.Errorf("ArchivedAt() = (%v, %v), want (%v, true)", got, ok, archivedOn)
	}
}

// TestRecurringRule_OccurrencesBetween_ClipsToStartsOn proves a window
// reaching back before the rule existed doesn't invent firings.
func TestRecurringRule_OccurrencesBetween_ClipsToStartsOn(t *testing.T) {
	rule := ruleWith(t, mustMonthly(t, 1, 15), mustDate(t, 2026, time.March, 1))

	got := rule.OccurrencesBetween(mustDate(t, 2025, time.January, 1), mustDate(t, 2026, time.May, 31))
	assertDates(t, got, []string{"2026-03-15", "2026-04-15", "2026-05-15"})
}

func TestRecurringRule_OccurrencesBetween_ClipsToEndsOn(t *testing.T) {
	rule := ruleWith(t, mustMonthly(t, 1, 15), mustDate(t, 2026, time.January, 1),
		recurring.WithEndsOn(mustDate(t, 2026, time.March, 20)))

	got := rule.OccurrencesBetween(mustDate(t, 2026, time.January, 1), mustDate(t, 2026, time.December, 31))
	assertDates(t, got, []string{"2026-01-15", "2026-02-15", "2026-03-15"})
}

// TestRecurringRule_OccurrencesBetween_ArchivedRuleFiresOnNothing is the
// invariant issue #278's generation use case depends on: an archived rule
// contributes no projections at all.
func TestRecurringRule_OccurrencesBetween_ArchivedRuleFiresOnNothing(t *testing.T) {
	rule := ruleWith(t, mustMonthly(t, 1, 15), mustDate(t, 2026, time.January, 1),
		recurring.WithArchivedAt(mustDate(t, 2026, time.February, 1)))

	if got := rule.OccurrencesBetween(mustDate(t, 2026, time.January, 1), mustDate(t, 2026, time.December, 31)); got != nil {
		t.Errorf("an archived rule produced %v, want no occurrences", datesToStrings(got))
	}
	if _, ok := rule.NextOccurrenceOnOrAfter(mustDate(t, 2026, time.January, 1)); ok {
		t.Error("an archived rule reported a next occurrence, want none")
	}
}

func TestRecurringRule_OccurrencesBetween_EmptyWindow(t *testing.T) {
	rule := ruleWith(t, mustMonthly(t, 1, 15), mustDate(t, 2026, time.January, 1))

	if got := rule.OccurrencesBetween(mustDate(t, 2026, time.March, 1), mustDate(t, 2026, time.February, 1)); got != nil {
		t.Errorf("an inverted window produced %v, want no occurrences", datesToStrings(got))
	}
	if got := rule.OccurrencesBetween(mustDate(t, 2026, time.March, 16), mustDate(t, 2026, time.April, 14)); got != nil {
		t.Errorf("a window containing no firing produced %v, want none", datesToStrings(got))
	}
}

func TestRecurringRule_NextOccurrenceOnOrAfter(t *testing.T) {
	rule := ruleWith(t, mustMonthly(t, 1, 31), mustDate(t, 2026, time.January, 1))

	tests := []struct {
		name string
		from string
		want string
	}{
		{"before the start", "2025-06-01", "2026-01-31"},
		{"exactly on a firing", "2026-01-31", "2026-01-31"},
		{"the day after a firing clamps into February", "2026-02-01", "2026-02-28"},
		{"on February's clamped firing", "2026-02-28", "2026-02-28"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from := parseTestDate(t, tt.from)
			got, ok := rule.NextOccurrenceOnOrAfter(from)
			if !ok {
				t.Fatalf("NextOccurrenceOnOrAfter(%s) reported none, want %s", tt.from, tt.want)
			}
			if got.String() != tt.want {
				t.Errorf("NextOccurrenceOnOrAfter(%s) = %s, want %s", tt.from, got, tt.want)
			}
		})
	}
}

func TestRecurringRule_NextOccurrenceOnOrAfter_PastTheEnd(t *testing.T) {
	rule := ruleWith(t, mustMonthly(t, 1, 15), mustDate(t, 2026, time.January, 1),
		recurring.WithEndsOn(mustDate(t, 2026, time.March, 31)))

	if _, ok := rule.NextOccurrenceOnOrAfter(mustDate(t, 2026, time.April, 1)); ok {
		t.Error("a rule past its end date reported a next occurrence, want none")
	}
}
