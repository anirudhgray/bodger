package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
)

// ProviderRate is one dated rate row as an FxRateProvider actually
// returned it. Date is the provider's own answer to "what date is this
// rate for" — never the date a caller asked for. ADR-0012 documents why
// those two can differ: a provider may walk a requested date back to the
// nearest one it has data for, and a batch/range request can come back
// with several rows at several distinct dates in a single response. A
// caller (issue #135) is expected to persist Date alongside Rate, not
// assume it matches whatever date it requested.
type ProviderRate struct {
	Rate fx.Rate
	Date domain.Date
}

// FxRateProvider fetches exchange rates from an external source
// (ADR-0004, ADR-0012). It is never a hard dependency for reads: bodger
// persists every rate it fetches, and every historical read is served
// from that storage (issue #130) — this interface is called only from an
// explicit, user-initiated fetch path (issue #135), never from inline
// conversion logic on a read path.
//
// Defined here, in the layer that consumes it (ADR-0007: the repository/
// port boundary is what keeps this reversible) — internal/adapters/
// fxprovider implements it for Frankfurter, and the application layer
// depends only on this interface, never on a concrete adapter.
type FxRateProvider interface {
	// FetchRate returns the rate the provider has for base/quote nearest
	// to (at or before) date, in the provider's own dated answer — see
	// ProviderRate. It never returns a row dated after the requested
	// date.
	//
	// It returns a *errs.Error with code NotFound if the provider has no
	// data for this pair at or before date (both currencies are valid,
	// but there's no coverage — e.g. a date before the pair's history, or
	// in the future). It returns InvalidInput if base or quote is not a
	// currency code the provider recognizes, or the request was
	// otherwise malformed. It returns Unavailable if the provider itself
	// could not be reached or answered with a server error (network
	// failure, timeout, DNS failure, 5xx).
	FetchRate(ctx context.Context, base, quote string, date domain.Date) (ProviderRate, error)

	// FetchRange returns every rate row the provider has for base/quote
	// with a date between from and to, inclusive, fetched as the
	// provider's own time-series/range primitive — one request per
	// base/quote pair, never a loop over individual dates (ADR-0012). The
	// response can and often does carry several distinct dates; every row
	// the provider returns is surfaced here, none collapsed, averaged, or
	// silently dropped. It never returns a row dated after to.
	//
	// Same error codes as FetchRate: NotFound if the provider has no data
	// anywhere in the range, InvalidInput for an unrecognized currency or
	// malformed request, Unavailable if the provider could not be
	// reached.
	FetchRange(ctx context.Context, base, quote string, from, to domain.Date) ([]ProviderRate, error)

	// Name identifies which provider produced a fetched rate -- FetchFxRates
	// (issue #135) stores this verbatim as FxRateRepository's `source`
	// column alongside every row it writes. It lives on the port rather
	// than being hardcoded in the application layer because ADR-0007 keeps
	// internal/app depending only on this interface, never on a concrete
	// adapters/fxprovider package -- "which provider this rate came from"
	// is exactly the kind of adapter-specific fact the port boundary
	// exists to carry across without leaking the adapter itself upward.
	Name() string
}
