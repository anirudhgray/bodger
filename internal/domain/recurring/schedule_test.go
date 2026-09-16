package recurring_test

import (
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
)

func mustDate(t *testing.T, y int, m time.Month, d int) domain.Date {
	t.Helper()
	date, err := domain.NewDate(y, m, d)
	if err != nil {
		t.Fatalf("domain.NewDate: %v", err)
	}
	return date
}

// parseTestDate turns an ISO 8601 literal into a domain.Date, so a table
// of expectations can be written in the same form assertDates compares in.
func parseTestDate(t *testing.T, s string) domain.Date {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return mustDate(t, parsed.Year(), parsed.Month(), parsed.Day())
}

func mustMonthly(t *testing.T, interval, dayOfMonth int) recurring.Schedule {
	t.Helper()
	s, err := recurring.NewMonthlySchedule(interval, dayOfMonth)
	if err != nil {
		t.Fatalf("NewMonthlySchedule(%d, %d): %v", interval, dayOfMonth, err)
	}
	return s
}

func mustWeekly(t *testing.T, interval int, weekday time.Weekday) recurring.Schedule {
	t.Helper()
	s, err := recurring.NewWeeklySchedule(interval, weekday)
	if err != nil {
		t.Fatalf("NewWeeklySchedule(%d, %v): %v", interval, weekday, err)
	}
	return s
}

func mustYearly(t *testing.T, interval int, month time.Month, dayOfMonth int) recurring.Schedule {
	t.Helper()
	s, err := recurring.NewYearlySchedule(interval, month, dayOfMonth)
	if err != nil {
		t.Fatalf("NewYearlySchedule(%d, %v, %d): %v", interval, month, dayOfMonth, err)
	}
	return s
}

// ruleWith builds a minimal active rule around a schedule, for the
// occurrence-evaluation tests below — evaluation is only reachable through
// a rule, since clipping to starts_on/ends_on is the rule's job.
func ruleWith(t *testing.T, s recurring.Schedule, startsOn domain.Date, opts ...recurring.RecurringRuleOption) recurring.RecurringRule {
	t.Helper()
	r, err := recurring.NewRecurringRule("rule-1", "user-1", "acc-1", "cat-1", 50000, "Rent", s, startsOn, opts...)
	if err != nil {
		t.Fatalf("NewRecurringRule: %v", err)
	}
	return r
}

func datesToStrings(dates []domain.Date) []string {
	out := make([]string, len(dates))
	for i, d := range dates {
		out[i] = d.String()
	}
	return out
}

func assertDates(t *testing.T, got []domain.Date, want []string) {
	t.Helper()
	gotStrings := datesToStrings(got)
	if len(gotStrings) != len(want) {
		t.Fatalf("occurrences = %v, want %v", gotStrings, want)
	}
	for i := range want {
		if gotStrings[i] != want[i] {
			t.Fatalf("occurrences = %v, want %v", gotStrings, want)
		}
	}
}

func TestScheduleConstructors_Validation(t *testing.T) {
	tests := []struct {
		name    string
		build   func() (recurring.Schedule, error)
		wantErr error
	}{
		{
			name:    "weekly interval below 1",
			build:   func() (recurring.Schedule, error) { return recurring.NewWeeklySchedule(0, time.Monday) },
			wantErr: recurring.ErrScheduleIntervalNotPositive,
		},
		{
			name:    "weekly weekday out of range",
			build:   func() (recurring.Schedule, error) { return recurring.NewWeeklySchedule(1, time.Weekday(7)) },
			wantErr: recurring.ErrScheduleInvalidWeekday,
		},
		{
			name:    "monthly day 0",
			build:   func() (recurring.Schedule, error) { return recurring.NewMonthlySchedule(1, 0) },
			wantErr: recurring.ErrScheduleInvalidDayOfMonth,
		},
		{
			name:    "monthly day 32",
			build:   func() (recurring.Schedule, error) { return recurring.NewMonthlySchedule(1, 32) },
			wantErr: recurring.ErrScheduleInvalidDayOfMonth,
		},
		{
			name:    "monthly negative interval",
			build:   func() (recurring.Schedule, error) { return recurring.NewMonthlySchedule(-1, 15) },
			wantErr: recurring.ErrScheduleIntervalNotPositive,
		},
		{
			name:    "yearly month out of range",
			build:   func() (recurring.Schedule, error) { return recurring.NewYearlySchedule(1, time.Month(13), 1) },
			wantErr: recurring.ErrScheduleInvalidMonth,
		},
		{
			name: "reconstruction with an unknown frequency",
			build: func() (recurring.Schedule, error) {
				return recurring.NewSchedule(recurring.Frequency("daily"), 1, time.Monday, time.January, 1)
			},
			wantErr: recurring.ErrScheduleInvalidFrequency,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.build(); !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestNewMonthlySchedule_AcceptsShortMonthDays is the constructor half of
// ADR-0014's clamping decision: 29, 30, and 31 are legitimate days to
// declare, because a month too short for them clamps rather than being
// rejected up front.
func TestNewMonthlySchedule_AcceptsShortMonthDays(t *testing.T) {
	for _, day := range []int{29, 30, 31} {
		s := mustMonthly(t, 1, day)
		if got, ok := s.DayOfMonth(); !ok || got != day {
			t.Errorf("DayOfMonth() = (%d, %v), want (%d, true)", got, ok, day)
		}
	}
}

func TestSchedule_Accessors(t *testing.T) {
	weekly := mustWeekly(t, 2, time.Friday)
	if weekly.Frequency() != recurring.FrequencyWeekly || weekly.Interval() != 2 {
		t.Errorf("weekly schedule = %v/%d, want weekly/2", weekly.Frequency(), weekly.Interval())
	}
	if wd, ok := weekly.Weekday(); !ok || wd != time.Friday {
		t.Errorf("Weekday() = (%v, %v), want (Friday, true)", wd, ok)
	}
	if _, ok := weekly.DayOfMonth(); ok {
		t.Error("a weekly schedule must not report a day of month")
	}
	if _, ok := weekly.Month(); ok {
		t.Error("a weekly schedule must not report a month")
	}

	monthly := mustMonthly(t, 3, 15)
	if _, ok := monthly.Weekday(); ok {
		t.Error("a monthly schedule must not report a weekday")
	}
	if _, ok := monthly.Month(); ok {
		t.Error("a monthly schedule must not report a month")
	}

	yearly := mustYearly(t, 1, time.April, 6)
	if m, ok := yearly.Month(); !ok || m != time.April {
		t.Errorf("Month() = (%v, %v), want (April, true)", m, ok)
	}
	if _, ok := yearly.Weekday(); ok {
		t.Error("a yearly schedule must not report a weekday")
	}

	var zero recurring.Schedule
	if zero.Valid() {
		t.Error("a zero Schedule must not report itself as valid")
	}
}

// TestSchedule_String renders ADR-0014's subset as canonical RRULE text —
// a one-way, derived rendering, never the stored form.
func TestSchedule_String(t *testing.T) {
	tests := []struct {
		schedule recurring.Schedule
		want     string
	}{
		{mustWeekly(t, 1, time.Monday), "FREQ=WEEKLY;INTERVAL=1;BYDAY=MO"},
		{mustWeekly(t, 2, time.Sunday), "FREQ=WEEKLY;INTERVAL=2;BYDAY=SU"},
		{mustMonthly(t, 1, 31), "FREQ=MONTHLY;INTERVAL=1;BYMONTHDAY=31"},
		{mustMonthly(t, 3, 1), "FREQ=MONTHLY;INTERVAL=3;BYMONTHDAY=1"},
		{mustYearly(t, 1, time.February, 29), "FREQ=YEARLY;INTERVAL=1;BYMONTH=2;BYMONTHDAY=29"},
	}
	for _, tt := range tests {
		if got := tt.schedule.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
	var zero recurring.Schedule
	if got := zero.String(); got != "" {
		t.Errorf("zero Schedule String() = %q, want an empty string", got)
	}
}

func TestNewSchedule_RoundTripsEveryFrequency(t *testing.T) {
	tests := []recurring.Schedule{
		mustWeekly(t, 2, time.Wednesday),
		mustMonthly(t, 1, 31),
		mustYearly(t, 4, time.February, 29),
	}
	for _, want := range tests {
		weekday, _ := want.Weekday()
		month, _ := want.Month()
		day, _ := want.DayOfMonth()

		got, err := recurring.NewSchedule(want.Frequency(), want.Interval(), weekday, month, day)
		if err != nil {
			t.Fatalf("NewSchedule(%s): %v", want.String(), err)
		}
		if got.String() != want.String() {
			t.Errorf("NewSchedule round trip = %q, want %q", got.String(), want.String())
		}
	}
}

// TestMonthlySchedule_ClampsShortMonths is ADR-0014's central edge case: a
// monthly-on-the-31st rule fires on 28 February (29 in a leap year) and 30
// April, rather than skipping those months the way RFC 5545's own
// BYMONTHDAY semantics would.
func TestMonthlySchedule_ClampsShortMonths(t *testing.T) {
	rule := ruleWith(t, mustMonthly(t, 1, 31), mustDate(t, 2026, time.January, 31))

	got := rule.OccurrencesBetween(mustDate(t, 2026, time.January, 1), mustDate(t, 2026, time.June, 30))
	assertDates(t, got, []string{
		"2026-01-31",
		"2026-02-28",
		"2026-03-31",
		"2026-04-30",
		"2026-05-31",
		"2026-06-30",
	})
}

// TestMonthlySchedule_ClampsLeapFebruary proves the clamp reads the real
// month length rather than assuming 28.
func TestMonthlySchedule_ClampsLeapFebruary(t *testing.T) {
	rule := ruleWith(t, mustMonthly(t, 1, 31), mustDate(t, 2028, time.January, 1))

	got := rule.OccurrencesBetween(mustDate(t, 2028, time.February, 1), mustDate(t, 2028, time.February, 29))
	assertDates(t, got, []string{"2028-02-29"})
}

// TestMonthlySchedule_ClampDoesNotDriftTheAnchor is the other half of the
// clamping decision: February's clamped 28th must not become the rule's
// new day-of-month. March returns to the 31st, and a year of firings still
// lands on the last day of every month rather than walking backwards.
func TestMonthlySchedule_ClampDoesNotDriftTheAnchor(t *testing.T) {
	rule := ruleWith(t, mustMonthly(t, 1, 31), mustDate(t, 2026, time.January, 31))

	got := rule.OccurrencesBetween(mustDate(t, 2026, time.January, 1), mustDate(t, 2026, time.December, 31))
	assertDates(t, got, []string{
		"2026-01-31", "2026-02-28", "2026-03-31", "2026-04-30",
		"2026-05-31", "2026-06-30", "2026-07-31", "2026-08-31",
		"2026-09-30", "2026-10-31", "2026-11-30", "2026-12-31",
	})

	if declared, _ := rule.Schedule().DayOfMonth(); declared != 31 {
		t.Errorf("the rule's declared day of month = %d after evaluation, want an unchanged 31", declared)
	}
}

// TestMonthlySchedule_ClampNeverDuplicatesADate guards the failure mode a
// previous-firing-based implementation would have: two consecutive months
// resolving to the same date.
func TestMonthlySchedule_ClampNeverDuplicatesADate(t *testing.T) {
	for _, day := range []int{29, 30, 31} {
		rule := ruleWith(t, mustMonthly(t, 1, day), mustDate(t, 2024, time.January, 1))
		got := rule.OccurrencesBetween(mustDate(t, 2024, time.January, 1), mustDate(t, 2029, time.December, 31))

		seen := map[string]bool{}
		for i, d := range got {
			if seen[d.String()] {
				t.Fatalf("day %d: duplicate occurrence %s", day, d)
			}
			seen[d.String()] = true
			if i > 0 && !got[i-1].Before(d) {
				t.Fatalf("day %d: occurrences are not strictly ascending around %s", day, d)
			}
		}
		if len(got) != 72 { // six years, one per month, none skipped
			t.Errorf("day %d: got %d occurrences over six years, want 72 (no month skipped)", day, len(got))
		}
	}
}

func TestMonthlySchedule_Interval(t *testing.T) {
	rule := ruleWith(t, mustMonthly(t, 3, 15), mustDate(t, 2026, time.January, 15))

	got := rule.OccurrencesBetween(mustDate(t, 2026, time.January, 1), mustDate(t, 2026, time.December, 31))
	assertDates(t, got, []string{"2026-01-15", "2026-04-15", "2026-07-15", "2026-10-15"})
}

// TestMonthlySchedule_AnchorSkipsAStartMonthAlreadyPast checks the first
// firing lands in the next month of the interval cycle when the declared
// day has already passed in starts_on's own month.
func TestMonthlySchedule_AnchorSkipsAStartMonthAlreadyPast(t *testing.T) {
	rule := ruleWith(t, mustMonthly(t, 1, 5), mustDate(t, 2026, time.January, 20))

	got := rule.OccurrencesBetween(mustDate(t, 2026, time.January, 1), mustDate(t, 2026, time.March, 31))
	assertDates(t, got, []string{"2026-02-05", "2026-03-05"})
}

func TestWeeklySchedule(t *testing.T) {
	// 2026-08-31 is a Monday.
	rule := ruleWith(t, mustWeekly(t, 1, time.Monday), mustDate(t, 2026, time.August, 31))

	got := rule.OccurrencesBetween(mustDate(t, 2026, time.August, 1), mustDate(t, 2026, time.September, 30))
	assertDates(t, got, []string{"2026-08-31", "2026-09-07", "2026-09-14", "2026-09-21", "2026-09-28"})
}

func TestWeeklySchedule_StartsOnADifferentWeekdayAndInterval(t *testing.T) {
	// Starting on a Monday with a Friday schedule: the first firing is the
	// following Friday, then every other week.
	rule := ruleWith(t, mustWeekly(t, 2, time.Friday), mustDate(t, 2026, time.August, 31))

	got := rule.OccurrencesBetween(mustDate(t, 2026, time.August, 1), mustDate(t, 2026, time.October, 15))
	assertDates(t, got, []string{"2026-09-04", "2026-09-18", "2026-10-02"})
}

// TestWeeklySchedule_UnaffectedByDSTTransitions is ADR-0014's timezone
// claim made concrete: occurrence dates are calendar dates with no
// instant, so a week that contains a spring-forward or fall-back
// transition is still exactly seven days. These windows span the 2026 US
// (8 March) and EU (25 October) transitions.
func TestWeeklySchedule_UnaffectedByDSTTransitions(t *testing.T) {
	spring := ruleWith(t, mustWeekly(t, 1, time.Sunday), mustDate(t, 2026, time.February, 22))
	assertDates(t, spring.OccurrencesBetween(mustDate(t, 2026, time.February, 22), mustDate(t, 2026, time.March, 22)), []string{
		"2026-02-22", "2026-03-01", "2026-03-08", "2026-03-15", "2026-03-22",
	})

	autumn := ruleWith(t, mustWeekly(t, 1, time.Sunday), mustDate(t, 2026, time.October, 11))
	assertDates(t, autumn.OccurrencesBetween(mustDate(t, 2026, time.October, 11), mustDate(t, 2026, time.November, 8)), []string{
		"2026-10-11", "2026-10-18", "2026-10-25", "2026-11-01", "2026-11-08",
	})
}

// TestSchedule_EvaluationIgnoresTheHostTimezone is the other half of the
// same claim: the computation reads no ambient location, so temporarily
// pointing time.Local somewhere with a half-hour offset and a DST rule
// changes nothing. CI already runs under TZ=UTC; this asserts the result
// doesn't depend on that.
func TestSchedule_EvaluationIgnoresTheHostTimezone(t *testing.T) {
	rule := ruleWith(t, mustMonthly(t, 1, 31), mustDate(t, 2026, time.January, 31))
	from, to := mustDate(t, 2026, time.January, 1), mustDate(t, 2026, time.April, 30)
	want := datesToStrings(rule.OccurrencesBetween(from, to))

	for _, name := range []string{"Asia/Kolkata", "America/New_York", "Pacific/Auckland"} {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Skipf("timezone database unavailable for %s: %v", name, err)
		}
		original := time.Local
		time.Local = loc
		got := datesToStrings(rule.OccurrencesBetween(from, to))
		time.Local = original

		if len(got) != len(want) {
			t.Fatalf("under %s: occurrences = %v, want %v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("under %s: occurrences = %v, want %v", name, got, want)
			}
		}
	}
}

func TestYearlySchedule(t *testing.T) {
	rule := ruleWith(t, mustYearly(t, 1, time.April, 6), mustDate(t, 2026, time.January, 1))

	got := rule.OccurrencesBetween(mustDate(t, 2026, time.January, 1), mustDate(t, 2029, time.December, 31))
	assertDates(t, got, []string{"2026-04-06", "2027-04-06", "2028-04-06", "2029-04-06"})
}

// TestYearlySchedule_ClampsLeapDay applies the same clamping rule to a
// 29 February anniversary: it fires on the 28th in a non-leap year, and
// the declared day still returns on the next leap year.
func TestYearlySchedule_ClampsLeapDay(t *testing.T) {
	rule := ruleWith(t, mustYearly(t, 1, time.February, 29), mustDate(t, 2024, time.January, 1))

	got := rule.OccurrencesBetween(mustDate(t, 2024, time.January, 1), mustDate(t, 2028, time.December, 31))
	assertDates(t, got, []string{"2024-02-29", "2025-02-28", "2026-02-28", "2027-02-28", "2028-02-29"})
}

func TestYearlySchedule_Interval(t *testing.T) {
	rule := ruleWith(t, mustYearly(t, 2, time.July, 1), mustDate(t, 2026, time.July, 1))

	got := rule.OccurrencesBetween(mustDate(t, 2026, time.January, 1), mustDate(t, 2032, time.December, 31))
	assertDates(t, got, []string{"2026-07-01", "2028-07-01", "2030-07-01", "2032-07-01"})
}
