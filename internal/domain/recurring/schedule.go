// Package recurring holds the pure financial model for data-model.md §11's
// recurring activity: a RecurringRule is a template and a
// ScheduledOccurrence is a projection of one of its firings. Neither is
// money that has moved, and neither can contribute to a balance —
// ADR-0014 records why that is structural here rather than conventional
// (no account, no currency, no Money, and no path to a ledger.Posting:
// this package deliberately does not import internal/domain/ledger).
//
// No I/O, no clock, no database — the same ADR-0005 boundary
// internal/domain/ledger, internal/domain/budgeting, and
// internal/domain/importing hold to. Every evaluation method below takes
// the window it should evaluate as explicit domain.Date arguments;
// resolving what "today" is belongs to internal/app, which holds the
// injected clock and the actor's timezone.
package recurring

import (
	"fmt"
	"strings"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
)

// Frequency is the cycle a Schedule repeats on — ADR-0014's supported
// RFC 5545 FREQ subset, and nothing else. DAILY and the sub-day
// frequencies are deliberately absent; so are multi-value BY* parts,
// BYSETPOS, and ordinal weekdays.
type Frequency string

const (
	// FrequencyWeekly fires on one weekday, every Interval weeks.
	FrequencyWeekly Frequency = "weekly"

	// FrequencyMonthly fires on one day of the month, every Interval
	// months, clamping to the month's last day when it is shorter than
	// the declared day (ADR-0014).
	FrequencyMonthly Frequency = "monthly"

	// FrequencyYearly fires on one month-and-day pair, every Interval
	// years, with the same clamping rule for 29 February.
	FrequencyYearly Frequency = "yearly"
)

var validFrequencies = map[Frequency]bool{
	FrequencyWeekly:  true,
	FrequencyMonthly: true,
	FrequencyYearly:  true,
}

// Schedule is the structured form of ADR-0014's RRULE subset. Its fields
// are unexported and only exactly the ones its frequency needs are ever
// populated: the only way to produce one is through NewWeeklySchedule,
// NewMonthlySchedule, or NewYearlySchedule, each of which validates its
// own arguments. A raw RRULE string was rejected precisely because it can
// hold grammar no evaluator here can compute — see ADR-0014.
type Schedule struct {
	frequency  Frequency
	interval   int
	weekday    time.Weekday
	dayOfMonth int
	month      time.Month
}

// NewWeeklySchedule constructs a schedule firing on weekday every interval
// weeks. interval must be at least 1 (1 = every week, 2 = fortnightly).
func NewWeeklySchedule(interval int, weekday time.Weekday) (Schedule, error) {
	if err := validateInterval(interval); err != nil {
		return Schedule{}, err
	}
	if weekday < time.Sunday || weekday > time.Saturday {
		return Schedule{}, fmt.Errorf("%w: %d", ErrScheduleInvalidWeekday, int(weekday))
	}
	return Schedule{frequency: FrequencyWeekly, interval: interval, weekday: weekday}, nil
}

// NewMonthlySchedule constructs a schedule firing on dayOfMonth every
// interval months. dayOfMonth may be 29, 30, or 31: a month too short for
// it clamps to its own last day rather than skipping, and the declared day
// is never rewritten by that clamp (ADR-0014).
func NewMonthlySchedule(interval, dayOfMonth int) (Schedule, error) {
	if err := validateInterval(interval); err != nil {
		return Schedule{}, err
	}
	if err := validateDayOfMonth(dayOfMonth); err != nil {
		return Schedule{}, err
	}
	return Schedule{frequency: FrequencyMonthly, interval: interval, dayOfMonth: dayOfMonth}, nil
}

// NewYearlySchedule constructs a schedule firing on month/dayOfMonth every
// interval years. 29 February is accepted and clamps to the 28th in a
// non-leap year, the same rule NewMonthlySchedule's short months follow.
func NewYearlySchedule(interval int, month time.Month, dayOfMonth int) (Schedule, error) {
	if err := validateInterval(interval); err != nil {
		return Schedule{}, err
	}
	if month < time.January || month > time.December {
		return Schedule{}, fmt.Errorf("%w: %d", ErrScheduleInvalidMonth, int(month))
	}
	if err := validateDayOfMonth(dayOfMonth); err != nil {
		return Schedule{}, err
	}
	return Schedule{frequency: FrequencyYearly, interval: interval, month: month, dayOfMonth: dayOfMonth}, nil
}

// NewSchedule reconstructs a Schedule from already-persisted column
// values, dispatching on frequency to whichever constructor owns it. This
// is the sqlite adapter's entry point — the same restore-not-re-derive
// shape budgeting.WithArchivedAt and importing.WithStatus exist for — and
// it rejects a frequency outside the supported subset rather than
// producing a schedule nothing can evaluate.
func NewSchedule(frequency Frequency, interval int, weekday time.Weekday, month time.Month, dayOfMonth int) (Schedule, error) {
	switch frequency {
	case FrequencyWeekly:
		return NewWeeklySchedule(interval, weekday)
	case FrequencyMonthly:
		return NewMonthlySchedule(interval, dayOfMonth)
	case FrequencyYearly:
		return NewYearlySchedule(interval, month, dayOfMonth)
	default:
		return Schedule{}, fmt.Errorf("%w: %q", ErrScheduleInvalidFrequency, frequency)
	}
}

func validateInterval(interval int) error {
	if interval < 1 {
		return fmt.Errorf("%w: %d", ErrScheduleIntervalNotPositive, interval)
	}
	return nil
}

func validateDayOfMonth(day int) error {
	if day < 1 || day > 31 {
		return fmt.Errorf("%w: %d", ErrScheduleInvalidDayOfMonth, day)
	}
	return nil
}

// Frequency returns the cycle the schedule repeats on.
func (s Schedule) Frequency() Frequency { return s.frequency }

// Interval returns how many of the frequency's units separate two
// consecutive firings — 1 for every week/month/year, 2 for fortnightly,
// 3 for quarterly, and so on.
func (s Schedule) Interval() int { return s.interval }

// Weekday returns the weekday a weekly schedule fires on, and false for
// any other frequency.
func (s Schedule) Weekday() (time.Weekday, bool) {
	if s.frequency != FrequencyWeekly {
		return 0, false
	}
	return s.weekday, true
}

// DayOfMonth returns the day a monthly or yearly schedule fires on — the
// declared day, never a clamped one — and false for a weekly schedule.
func (s Schedule) DayOfMonth() (int, bool) {
	if s.frequency != FrequencyMonthly && s.frequency != FrequencyYearly {
		return 0, false
	}
	return s.dayOfMonth, true
}

// Month returns the month a yearly schedule fires in, and false for any
// other frequency.
func (s Schedule) Month() (time.Month, bool) {
	if s.frequency != FrequencyYearly {
		return 0, false
	}
	return s.month, true
}

// Valid reports whether s was produced by one of this package's
// constructors rather than being a zero value.
func (s Schedule) Valid() bool {
	return validFrequencies[s.frequency] && s.interval >= 1
}

// String renders the schedule as the canonical RFC 5545 RRULE text for
// ADR-0014's subset, e.g. "FREQ=MONTHLY;INTERVAL=1;BYMONTHDAY=31". This
// is a one-way, derived rendering for display and debugging: the
// structured fields are canonical, and bodger's own month-end clamping
// deliberately differs from what an RFC 5545 implementation would do with
// this same text (ADR-0014).
func (s Schedule) String() string {
	if !s.Valid() {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "FREQ=%s;INTERVAL=%d", strings.ToUpper(string(s.frequency)), s.interval)
	switch s.frequency {
	case FrequencyWeekly:
		fmt.Fprintf(&b, ";BYDAY=%s", rfcWeekdays[s.weekday])
	case FrequencyMonthly:
		fmt.Fprintf(&b, ";BYMONTHDAY=%d", s.dayOfMonth)
	case FrequencyYearly:
		fmt.Fprintf(&b, ";BYMONTH=%d;BYMONTHDAY=%d", int(s.month), s.dayOfMonth)
	}
	return b.String()
}

// rfcWeekdays maps Go's weekdays onto RFC 5545's two-letter BYDAY codes.
var rfcWeekdays = map[time.Weekday]string{
	time.Sunday:    "SU",
	time.Monday:    "MO",
	time.Tuesday:   "TU",
	time.Wednesday: "WE",
	time.Thursday:  "TH",
	time.Friday:    "FR",
	time.Saturday:  "SA",
}

// occurrenceAt returns the n-th (0-based) firing of s for a rule anchored
// at startsOn. Every firing is computed from the anchor rather than from
// its predecessor, which is what keeps a clamped short month from becoming
// the new anchor and walking the rule backwards through the calendar
// (ADR-0014).
//
// All arithmetic runs against time.UTC, used purely as a calendar: these
// are dates with no time and no zone (data-model.md §9), so there is no
// instant for a DST transition to shift and no wall-clock hour that can
// happen twice or not at all.
func (s Schedule) occurrenceAt(startsOn domain.Date, n int) domain.Date {
	switch s.frequency {
	case FrequencyWeekly:
		return s.weeklyOccurrenceAt(startsOn, n)
	case FrequencyMonthly:
		year, month := s.monthlyAnchor(startsOn)
		return clampedDate(addMonths(year, month, n*s.interval), s.dayOfMonth)
	case FrequencyYearly:
		year := s.yearlyAnchor(startsOn)
		return clampedDate(yearMonth{year: year + n*s.interval, month: s.month}, s.dayOfMonth)
	default:
		return domain.Date{}
	}
}

// weeklyOccurrenceAt walks forward from startsOn to the first date falling
// on the schedule's weekday, then adds whole interval-weeks.
func (s Schedule) weeklyOccurrenceAt(startsOn domain.Date, n int) domain.Date {
	t := toTime(startsOn)
	offset := (int(s.weekday) - int(t.Weekday()) + 7) % 7
	return fromTime(t.AddDate(0, 0, offset+7*s.interval*n))
}

// monthlyAnchor returns the year and month of a monthly schedule's first
// firing: startsOn's own month when that month's (possibly clamped) day
// still falls on or after startsOn, and otherwise the next month in the
// interval cycle.
func (s Schedule) monthlyAnchor(startsOn domain.Date) (int, time.Month) {
	ym := yearMonth{year: startsOn.Year(), month: startsOn.Month()}
	if clampedDate(ym, s.dayOfMonth).Before(startsOn) {
		ym = addMonths(ym.year, ym.month, s.interval)
	}
	return ym.year, ym.month
}

// yearlyAnchor returns the year of a yearly schedule's first firing, by
// the same rule monthlyAnchor applies to months.
func (s Schedule) yearlyAnchor(startsOn domain.Date) int {
	year := startsOn.Year()
	if clampedDate(yearMonth{year: year, month: s.month}, s.dayOfMonth).Before(startsOn) {
		year += s.interval
	}
	return year
}

// occurrencesBetween returns every firing of s in [from, to] inclusive,
// for a rule anchored at startsOn, in ascending order.
//
// It walks from the first firing rather than jumping to the window's own
// start: the caller bounds the window (ADR-0014's generation horizon), and
// a firing count measured in the hundreds is not worth the extra
// per-frequency arithmetic a direct seek would need.
func (s Schedule) occurrencesBetween(startsOn, from, to domain.Date) []domain.Date {
	if !s.Valid() || to.Before(from) {
		return nil
	}
	var out []domain.Date
	for n := 0; ; n++ {
		d := s.occurrenceAt(startsOn, n)
		if d.After(to) {
			return out
		}
		if !d.Before(from) {
			out = append(out, d)
		}
	}
}

// yearMonth is a year and month with no day — the intermediate a monthly
// or yearly firing is computed in before its day is clamped onto it.
type yearMonth struct {
	year  int
	month time.Month
}

// addMonths advances a year/month pair by months, normalising the rollover
// through time.Date on the 1st, where no day-of-month clamping can occur.
func addMonths(year int, month time.Month, months int) yearMonth {
	t := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, months, 0)
	return yearMonth{year: t.Year(), month: t.Month()}
}

// clampedDate places day within ym, clamping to the month's own last day
// when it is shorter — ADR-0014's "28 February for a monthly-on-the-31st
// rule", never a skip and never a roll forward into the next month.
func clampedDate(ym yearMonth, day int) domain.Date {
	if last := daysInMonth(ym.year, ym.month); day > last {
		day = last
	}
	d, err := domain.NewDate(ym.year, ym.month, day)
	if err != nil {
		// Unreachable: ym.month comes from time.Date normalisation and
		// day has just been clamped into the month's real length.
		return domain.Date{}
	}
	return d
}

// daysInMonth returns the number of days in the given month, using the
// standard "day 0 of the next month" trick so leap years need no special
// case.
func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// toTime lifts a domain.Date into a time.Time at midnight UTC, purely for
// calendar arithmetic. The zone is always UTC and the time is always
// midnight, so nothing here can observe a DST transition.
func toTime(d domain.Date) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}

// fromTime is toTime's inverse, dropping back to a zoneless calendar date.
func fromTime(t time.Time) domain.Date {
	d, err := domain.NewDate(t.Year(), t.Month(), t.Day())
	if err != nil {
		// Unreachable: t came from time.Date, which only ever produces
		// real calendar dates.
		return domain.Date{}
	}
	return d
}
