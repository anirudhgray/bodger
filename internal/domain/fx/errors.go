package fx

import "errors"

// Named errors returned by this package. Callers should match on these
// with errors.Is; the wrapped text (added by the constructor that returns
// them) carries the offending value for humans, the sentinel carries the
// identity for code.
var (
	// ErrRateEmptyCurrency is returned when a Rate is constructed with an
	// empty base or quote currency code.
	ErrRateEmptyCurrency = errors.New("fx: rate currency must not be empty")

	// ErrRateUnknownCurrency is returned when a Rate's base or quote
	// currency code has no matching entry in money's seeded reference
	// data.
	ErrRateUnknownCurrency = errors.New("fx: unknown currency code")

	// ErrRateNotPositive is returned when a Rate is constructed with a
	// zero or negative value. An exchange rate of zero or less never
	// corresponds to any real conversion.
	ErrRateNotPositive = errors.New("fx: rate value must be positive")

	// ErrRatePrecisionExceeded is returned when a Rate's value carries
	// more than 12 fractional digits — data-model.md §8's
	// decimal(24,12) column. Accepting it would mean silently truncating
	// precision, which this package never does.
	ErrRatePrecisionExceeded = errors.New("fx: rate value has more than 12 fractional digits")

	// ErrRateSameCurrency is returned by DeriveImpliedRate when its two
	// amounts are already in the same currency — there is no exchange
	// rate to derive between a currency and itself. Callers with a
	// same-currency pair want IdentityRate instead.
	ErrRateSameCurrency = errors.New("fx: cannot derive an implied rate between one currency and itself")

	// ErrRateZeroBase is returned by DeriveImpliedRate when the base leg's
	// amount is zero — there is no rate to divide by zero into.
	ErrRateZeroBase = errors.New("fx: cannot derive an implied rate from a zero base amount")

	// ErrNoRateWithinWindow is returned by SelectRate when no candidate
	// matches the requested date exactly and none falls within the
	// staleness window either — ADR-0004's "missing rates degrade
	// loudly": no silent 1.0, no nearest-neighbour search into the
	// future, no dropping the row from the total.
	ErrNoRateWithinWindow = errors.New("fx: no rate available for the requested date within the staleness window")

	// ErrInvalidStalenessWindow is returned by SelectRate when given a
	// negative window. Zero is valid (exact-match-only); negative makes
	// no sense as a number of days to look back.
	ErrInvalidStalenessWindow = errors.New("fx: staleness window must not be negative")
)
