package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
)

func mustDate(t *testing.T, year int, month time.Month, day int) domain.Date {
	t.Helper()
	d, err := domain.NewDate(year, month, day)
	if err != nil {
		t.Fatalf("NewDate(%d, %v, %d) = %v, want success", year, month, day, err)
	}
	return d
}

func TestNewDateValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		year  int
		month time.Month
		day   int
	}{
		{"an ordinary date", 2026, time.August, 14},
		{"the first of the year", 2026, time.January, 1},
		{"the last of the year", 2026, time.December, 31},
		{"29 February in a leap year", 2024, time.February, 29},
		{"31 January", 2026, time.January, 31},
		{"30 April", 2026, time.April, 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, err := domain.NewDate(tc.year, tc.month, tc.day)
			if err != nil {
				t.Fatalf("NewDate(%d, %v, %d) = %v, want success", tc.year, tc.month, tc.day, err)
			}
			if d.Year() != tc.year || d.Month() != tc.month || d.Day() != tc.day {
				t.Errorf("NewDate(%d, %v, %d) = %+v", tc.year, tc.month, tc.day, d)
			}
		})
	}
}

func TestNewDateInvalid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		year  int
		month time.Month
		day   int
	}{
		// The issue's own acceptance tests.
		{"30 February doesn't exist", 2026, time.February, 30},
		{"month 13 doesn't exist", 2026, 13, 1},
		{"day 0 doesn't exist", 2026, time.August, 0},

		{"31 April doesn't exist", 2026, time.April, 31},
		{"29 February in a non-leap year", 2026, time.February, 29},
		{"month 0 doesn't exist", 2026, 0, 1},
		{"negative month", 2026, -1, 1},
		{"negative day", 2026, time.August, -1},
		{"day 32", 2026, time.January, 32},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewDate(tc.year, tc.month, tc.day)
			if !errors.Is(err, domain.ErrInvalidDate) {
				t.Fatalf("NewDate(%d, %v, %d) error = %v, want ErrInvalidDate", tc.year, tc.month, tc.day, err)
			}
		})
	}
}

func TestNewDateIsPure(t *testing.T) {
	t.Parallel()

	// Calling NewDate twice with the same integers must always produce
	// the same result - no clock, no timezone, no hidden state.
	d1 := mustDate(t, 2026, time.August, 14)
	d2 := mustDate(t, 2026, time.August, 14)
	if !d1.Equal(d2) {
		t.Errorf("NewDate is not pure: %v != %v", d1, d2)
	}
}

func TestDateComparison(t *testing.T) {
	t.Parallel()

	earlier := mustDate(t, 2026, time.August, 1)
	later := mustDate(t, 2026, time.August, 14)
	sameAsEarlier := mustDate(t, 2026, time.August, 1)

	if !earlier.Before(later) {
		t.Errorf("Before: %v should be before %v", earlier, later)
	}
	if later.Before(earlier) {
		t.Errorf("Before: %v should not be before %v", later, earlier)
	}
	if !later.After(earlier) {
		t.Errorf("After: %v should be after %v", later, earlier)
	}
	if earlier.After(later) {
		t.Errorf("After: %v should not be after %v", earlier, later)
	}
	if !earlier.Equal(sameAsEarlier) {
		t.Errorf("Equal: %v should equal %v", earlier, sameAsEarlier)
	}
	if earlier.Equal(later) {
		t.Errorf("Equal: %v should not equal %v", earlier, later)
	}

	// Year and month boundaries, not just day-of-month.
	endOfYear := mustDate(t, 2026, time.December, 31)
	startOfNextYear := mustDate(t, 2027, time.January, 1)
	if !endOfYear.Before(startOfNextYear) {
		t.Errorf("Before: %v should be before %v", endOfYear, startOfNextYear)
	}
}

func TestDateString(t *testing.T) {
	t.Parallel()

	if got, want := mustDate(t, 2026, time.August, 14).String(), "2026-08-14"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := mustDate(t, 2026, time.January, 1).String(), "2026-01-01"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
