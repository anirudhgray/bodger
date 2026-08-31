package normalize

import (
	"strings"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// localeLayouts are the non-ISO date layouts DateOf accepts, tried in
// order after the ISO layout fails to parse. bodger ships exactly one
// today: day/month/year, e.g. "14/08/2026" — the unambiguous layout
// ADR-0005 itself uses as the worked example for this function. A second
// locale format is deliberately not guessed at here; extending this list
// (or making it a per-instance config value) is a judgment call this
// package leaves for whenever a second locale is actually needed, rather
// than inventing untested formats now. See the issue #5 final report.
var localeLayouts = []string{
	"02/01/2006",
	"2/1/2006",
}

// DateOf resolves raw into a calendar Date, interpreting anything relative
// ("", "today", "yesterday") in tz (an IANA zone name) as measured by clk.
// This is the one function in this package — and one of the very few in
// the whole repository — that touches time, and it never calls
// time.Now(): clk is the only source of "now" (ADR-0005 "Time is injected,
// always").
//
// Resolution order:
//  1. Empty string, or "today" (case-insensitive): the current date in tz.
//  2. "yesterday" (case-insensitive): the day before that, in tz.
//  3. ISO 8601 ("2026-08-14").
//  4. One of localeLayouts.
//  5. Otherwise, a named error.
//
// An empty string resolving to "today" is deliberate: it's the same
// default a REST handler would otherwise be tempted to apply itself
// (ADR-0005's opening example), so it belongs here instead, where every
// surface gets it identically.
func DateOf(raw string, clk clock.Clock, tz string) (domain.Date, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return domain.Date{}, errs.New(errs.InvalidInput).
			Explain("%q is not a known time zone", tz).
			Field("timezone").
			Wrap(err)
	}

	trimmed := strings.TrimSpace(raw)
	switch strings.ToLower(trimmed) {
	case "", "today":
		return dateFromInstant(clk.Now(), loc, 0)
	case "yesterday":
		return dateFromInstant(clk.Now(), loc, -1)
	}

	if t, perr := time.Parse(time.DateOnly, trimmed); perr == nil {
		return newDate(t)
	}
	for _, layout := range localeLayouts {
		if t, perr := time.Parse(layout, trimmed); perr == nil {
			return newDate(t)
		}
	}

	return domain.Date{}, errs.New(errs.InvalidInput).
		Explain("%q isn't a recognized date", raw).
		Field("date")
}

// dateFromInstant converts now into tz's local calendar date, offset by
// offsetDays whole days (0 for "today", -1 for "yesterday"). AddDate
// operates on loc's wall-clock calendar fields, so this is correct across
// a DST transition, not just at a plain UTC offset.
func dateFromInstant(now time.Time, loc *time.Location, offsetDays int) (domain.Date, error) {
	local := now.In(loc).AddDate(0, 0, offsetDays)
	return newDate(local)
}

// newDate builds a domain.Date from t's calendar fields, wrapping
// domain.NewDate's error into an *errs.Error so every error leaving this
// package has the same shape.
func newDate(t time.Time) (domain.Date, error) {
	d, err := domain.NewDate(t.Year(), t.Month(), t.Day())
	if err != nil {
		return domain.Date{}, errs.New(errs.InvalidInput).
			Explain("%04d-%02d-%02d is not a real calendar date", t.Year(), int(t.Month()), t.Day()).
			Field("date").
			Wrap(err)
	}
	return d, nil
}
