package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
)

// FxRateRepository persists exchange rates fetched from a provider
// (fx_rates, #128) and looks one up using fx.SelectRate's pure
// nearest-earlier-within-staleness-window rule (#129).
//
// Unlike every other repository in this package, fx_rates carries no
// actor/user scoping: a rate between two currencies on a given date is
// the same fact for every user of this bodger instance (ADR-0004), so
// none of these methods take an actorID.
type FxRateRepository interface {
	// Store upserts a single fetched rate for (rate.Base(), rate.Quote(),
	// date, source): storing again for the same key replaces the
	// previously stored rate rather than conflicting — re-fetching an
	// already-stored day is expected to converge on the provider's
	// current answer, not accumulate duplicate rows. It returns a
	// *errs.Error with code InvalidInput if rate has no base/quote
	// currency or source is empty.
	Store(ctx context.Context, rate fx.Rate, date domain.Date, source string) error

	// StoreBatch upserts many rows in one call, atomically: a date-range
	// backfill (a later issue) writes many (pair, date) rows at once, and
	// this is the one write path for that rather than N round trips
	// through Store. Semantically equivalent to calling Store for each
	// row in order, but on failure nothing in the batch is written — a
	// partially-applied backfill would be harder to reason about than an
	// all-or-nothing one.
	StoreBatch(ctx context.Context, rows []FxRateRow) error
}

// FxRateRow is one row for FxRateRepository.StoreBatch — the same fields
// Store takes, bundled so a caller writing many rows at once can pass
// them in a single call.
type FxRateRow struct {
	Rate   fx.Rate
	Date   domain.Date
	Source string
}
