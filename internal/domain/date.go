// Package domain holds the pure financial model: value objects with no I/O,
// no clock, and no dependency on anything project-local outside this
// package tree (internal/domain and its subpackages, such as
// internal/domain/money).
package domain

import (
	"fmt"
	"time"
)

// Date is a calendar date with no time component and no timezone — see
// docs/data-model.md §9. It exists precisely because a naive time.Time
// silently changes meaning when attached to a clock or moved across
// timezones; Date can't do that because it never carries either.
//
// Its fields are unexported: the only way to produce a Date is NewDate,
// which validates the year/month/day. There is no exported way to
// construct one from a struct literal outside this package — see
// nocompile/date_construct.go for the manually verified proof.
type Date struct {
	year  int
	month time.Month
	day   int
}

// NewDate constructs a Date from already-known, unambiguous integers,
// rejecting anything that isn't a real calendar date (an out-of-range
// month, a day that doesn't exist in that month, day 0, and so on).
//
// NewDate is pure: no clock, no timezone, just integer validation. It is
// deliberately not the place that resolves "today", "yesterday", or a
// locale-format date string — that's ambiguous-input resolution, needs the
// application layer's injected clock, and lives in
// internal/app/normalize (see ADR-0005).
func NewDate(year int, month time.Month, day int) (Date, error) {
	// time.Date normalizes out-of-range components instead of erroring
	// (month 13 rolls into next year, day 30 in February rolls into
	// March) — so round-tripping through it and comparing back is the
	// standard way to detect an invalid calendar date.
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != month || t.Day() != day {
		return Date{}, fmt.Errorf("%w: %04d-%02d-%02d", ErrInvalidDate, year, int(month), day)
	}
	return Date{year: year, month: month, day: day}, nil
}

// Year returns the calendar year.
func (d Date) Year() int { return d.year }

// Month returns the calendar month.
func (d Date) Month() time.Month { return d.month }

// Day returns the day of the month.
func (d Date) Day() int { return d.day }

// Before reports whether d is strictly earlier than other.
func (d Date) Before(other Date) bool {
	return d.compare(other) < 0
}

// After reports whether d is strictly later than other.
func (d Date) After(other Date) bool {
	return d.compare(other) > 0
}

// Equal reports whether d and other are the same calendar date.
func (d Date) Equal(other Date) bool {
	return d.compare(other) == 0
}

func (d Date) compare(other Date) int {
	switch {
	case d.year != other.year:
		return d.year - other.year
	case d.month != other.month:
		return int(d.month) - int(other.month)
	default:
		return d.day - other.day
	}
}

// String returns the date in ISO 8601 form, e.g. "2026-08-14".
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.year, int(d.month), d.day)
}
