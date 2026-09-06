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
}
