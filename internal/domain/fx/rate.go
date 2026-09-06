// Package fx holds the pure exchange-rate model: the Rate value object and
// the nearest-earlier-within-staleness-window selection rule ADR-0004
// describes. Like internal/domain/money, this is domain code — no I/O, no
// clock, no dependency on anything project-local outside internal/domain
// and its subpackages.
//
// Money never needed a fractional type: it's an integer count of minor
// units. A rate genuinely is fractional (data-model.md §8:
// decimal(24,12)), so this package is the first user of a decimal library
// in this codebase (github.com/shopspring/decimal) rather than a float —
// the same "never a float, anywhere" discipline Money holds, just for a
// value that can't be an integer.
package fx

import (
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain/money"
)

// maxRateDecimalPlaces is data-model.md §8's fx_rate.rate column:
// decimal(24,12), stored as a decimal string, never a float.
const maxRateDecimalPlaces = 12

// Rate is an exchange rate between two ISO 4217 currency codes: how many
// units of Quote equal one unit of Base. A Rate of 0.0115 with
// Base()=="INR" and Quote()=="USD" means 1 INR buys 0.0115 USD.
//
// Its fields are unexported: the only way to produce a Rate is NewRate,
// IdentityRate, or DeriveImpliedRate — all of which validate the
// currencies and the value. There is no exported way to construct one from
// a struct literal outside this package — see nocompile/rate_construct.go
// for the manually verified proof.
type Rate struct {
	base  string
	quote string
	value decimal.Decimal
}

// NewRate constructs a Rate, rejecting an unknown currency code (per
// money's seeded reference data), a non-positive value, or a value with
// more than 12 fractional digits (data-model.md §8's decimal(24,12)).
//
// base and quote may be equal — that's a legitimate 1:1 rate (see
// IdentityRate) — NewRate doesn't special-case it.
func NewRate(base, quote string, value decimal.Decimal) (Rate, error) {
	if base == "" || quote == "" {
		return Rate{}, ErrRateEmptyCurrency
	}
	if _, ok := money.LookupCurrency(base); !ok {
		return Rate{}, fmt.Errorf("%w: %q", ErrRateUnknownCurrency, base)
	}
	if _, ok := money.LookupCurrency(quote); !ok {
		return Rate{}, fmt.Errorf("%w: %q", ErrRateUnknownCurrency, quote)
	}
	if !value.IsPositive() {
		return Rate{}, fmt.Errorf("%w: %s", ErrRateNotPositive, value.String())
	}
	// decimal's Exponent is the power of 10 the coefficient is scaled by
	// (e.g. 0.0115 has Exponent() == -4); more than 12 fractional digits
	// means Exponent() < -12.
	if value.Exponent() < -maxRateDecimalPlaces {
		return Rate{}, fmt.Errorf("%w: %s", ErrRatePrecisionExceeded, value.String())
	}
	return Rate{base: base, quote: quote, value: value}, nil
}

// IdentityRate returns the trivial 1:1 rate for a single currency —
// NewTransfer's answer for a same-currency transfer, where there is no
// exchange to derive but a uniform Rate is still useful to callers that
// always want one (rather than a same-currency special case downstream).
func IdentityRate(currency string) (Rate, error) {
	return NewRate(currency, currency, decimal.NewFromInt(1))
}

// DeriveImpliedRate computes the rate implied by a cross-currency
// transfer's two legs — data-model.md §5/§8 and ADR-0004's "Cross-currency
// transfers record both legs": both amounts are authoritative, and the
// rate between them is the derived quantity, not the other way round.
//
// base is the outflow leg (the money that left an account) and quote is
// the inflow leg (the money that arrived) — matching ADR-0004's example
// (₹20,000 out, $230 in implies a rate of 0.0115 USD per INR). Both
// amounts are treated as magnitudes; their signs are the caller's concern,
// not this function's.
//
// The two legs may have different minor-unit exponents (e.g. JPY's 0
// against INR's 2); DeriveImpliedRate converts each to its currency's
// major-unit decimal amount before dividing, so the result is a rate
// between major units (yen, rupees), not minor units (paise counted
// against whole yen).
func DeriveImpliedRate(base, quote money.Money) (Rate, error) {
	if base.Currency() == quote.Currency() {
		return Rate{}, ErrRateSameCurrency
	}
	baseCur, ok := money.LookupCurrency(base.Currency())
	if !ok {
		return Rate{}, fmt.Errorf("%w: %q", ErrRateUnknownCurrency, base.Currency())
	}
	quoteCur, ok := money.LookupCurrency(quote.Currency())
	if !ok {
		return Rate{}, fmt.Errorf("%w: %q", ErrRateUnknownCurrency, quote.Currency())
	}

	baseMajor := decimal.New(base.Abs().AmountMinor(), int32(-baseCur.MinorUnitExponent))
	if baseMajor.IsZero() {
		return Rate{}, ErrRateZeroBase
	}
	quoteMajor := decimal.New(quote.Abs().AmountMinor(), int32(-quoteCur.MinorUnitExponent))

	// DivRound at exactly the column's scale so the result always
	// satisfies NewRate's precision check below, rather than depending on
	// decimal's default (16-digit) division precision.
	value := quoteMajor.DivRound(baseMajor, maxRateDecimalPlaces)
	return NewRate(base.Currency(), quote.Currency(), value)
}

// Base returns the rate's base currency code: one unit of this currency
// converts to Value() units of Quote().
func (r Rate) Base() string { return r.base }

// Quote returns the rate's quote currency code.
func (r Rate) Quote() string { return r.quote }

// Value returns the rate as a decimal: how many units of Quote() equal one
// unit of Base().
func (r Rate) Value() decimal.Decimal { return r.value }

// IsIdentity reports whether r is a trivial 1:1 rate between a currency
// and itself — the shape IdentityRate produces, and the shape a
// same-currency NewTransfer returns.
func (r Rate) IsIdentity() bool {
	return r.base == r.quote && r.value.Equal(decimal.NewFromInt(1))
}

// String renders the rate's value as a fixed-point decimal string at
// data-model.md §8's scale (12 fractional digits) — the form fx_rate.rate
// and transaction.fx_rate_used are stored as, never a float.
func (r Rate) String() string {
	return r.value.StringFixed(maxRateDecimalPlaces)
}
