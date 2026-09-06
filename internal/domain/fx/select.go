package fx

import (
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
)

// DefaultStalenessWindowDays is ADR-0004's "missing rates degrade loudly"
// default: a rate up to 7 days older than the requested date is usable
// (flagged stale); older than that, lookup fails rather than silently
// using a stale or absent rate. Callers may pass a different window (a
// future per-instance setting), but this is the default absent one.
const DefaultStalenessWindowDays = 7

// RateCandidate is one stored rate available for selection: the calendar
// date it was recorded for, the rate itself, and which provider it came
// from. Base/quote filtering happens before SelectRate is called — the app
// layer's repository query (a separate issue) is expected to have already
// restricted candidates to a single currency pair; SelectRate itself is
// agnostic to what Base/Quote its candidates carry.
type RateCandidate struct {
	Date   domain.Date
	Rate   Rate
	Source string
}

// Selection is what SelectRate found: the chosen rate, the calendar date
// it was actually recorded for (which may be earlier than the date that
// was asked for), which provider it came from, and whether it was an
// exact match or a stale within-window substitute. ADR-0004 requires every
// converted figure to carry this provenance, so Selection carries the same
// fields the candidate it was chosen from carried.
type Selection struct {
	Rate   Rate
	Date   domain.Date
	Source string
	Stale  bool
}

// SelectRate implements ADR-0004's rate lookup order: an exact match on
// want first; failing that, the most recent candidate strictly earlier
// than want but no more than windowDays days earlier, flagged Stale;
// failing that, ErrNoRateWithinWindow. It never looks forward in time —
// "no silent 1.0, no nearest-neighbour search into the future" — and never
// returns a rate older than the window, however sparse the candidates are.
//
// windowDays must not be negative (ErrInvalidStalenessWindow); 0 means
// "exact match only". See DefaultStalenessWindowDays for ADR-0004's
// default of 7.
//
// This is pure domain logic: no I/O, no clock. It takes whatever
// candidates the caller already has in memory (the app layer's
// repository-backed lookup, a separate issue, is what will assemble that
// slice from fx_rate rows) and picks among them.
func SelectRate(candidates []RateCandidate, want domain.Date, windowDays int) (Selection, error) {
	if windowDays < 0 {
		return Selection{}, ErrInvalidStalenessWindow
	}

	var best *RateCandidate
	for i := range candidates {
		c := candidates[i]
		if c.Date.Equal(want) {
			return Selection{Rate: c.Rate, Date: c.Date, Source: c.Source, Stale: false}, nil
		}
		if !c.Date.Before(want) {
			// Later than the requested date: never a candidate — ADR-0004
			// forbids "nearest-neighbour search into the future".
			continue
		}
		if daysBetween(c.Date, want) > windowDays {
			continue
		}
		if best == nil || c.Date.After(best.Date) {
			best = &c
		}
	}

	if best == nil {
		return Selection{}, ErrNoRateWithinWindow
	}
	return Selection{Rate: best.Rate, Date: best.Date, Source: best.Source, Stale: true}, nil
}

// daysBetween returns the number of calendar days between earlier and
// later (later - earlier), assuming later is not before earlier. domain.Date
// carries no timezone or clock, so this converts both to a noon-UTC
// time.Time purely to get calendar-day arithmetic for free from the
// standard library — no wall-clock time is read.
func daysBetween(earlier, later domain.Date) int {
	e := time.Date(earlier.Year(), earlier.Month(), earlier.Day(), 12, 0, 0, 0, time.UTC)
	l := time.Date(later.Year(), later.Month(), later.Day(), 12, 0, 0, 0, time.UTC)
	return int(l.Sub(e).Hours() / 24)
}
