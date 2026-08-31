// Package money holds the Money value object and its Currency reference
// data. This is domain code: no I/O, no clock, no dependency on anything
// project-local outside this package.
//
// Money is never a float, anywhere — not in memory, not on the wire. An
// amount is always an integer count of a currency's minor units
// (amountMinor), paired with the currency's ISO 4217 code. The exponent
// that turns minor units into a display amount always comes from Currency
// reference data (see currency.go), never from an assumed 2.
package money

import "fmt"

// Money is a signed amount in one currency's minor units (paise, cents,
// fils). Its fields are unexported: the only way to produce a Money is
// NewMoney, which validates the currency code. There is no exported way to
// construct one from a struct literal outside this package — see
// nocompile/money_construct.go for the manually verified proof.
type Money struct {
	amountMinor int64
	currency    string
}

// NewMoney constructs a Money, rejecting any currency code that isn't in
// the seeded reference data (see Currency). It never inspects or rounds
// amountMinor — that's already an integer count of minor units by the time
// it reaches here.
func NewMoney(amountMinor int64, currency string) (Money, error) {
	if _, ok := LookupCurrency(currency); !ok {
		return Money{}, fmt.Errorf("%w: %q", ErrUnknownCurrency, currency)
	}
	return Money{amountMinor: amountMinor, currency: currency}, nil
}

// AmountMinor returns the signed amount in the currency's minor units.
func (m Money) AmountMinor() int64 { return m.amountMinor }

// Currency returns the ISO 4217 currency code.
func (m Money) Currency() string { return m.currency }

// IsZero reports whether the amount is exactly zero.
func (m Money) IsZero() bool { return m.amountMinor == 0 }

// IsNegative reports whether the amount is less than zero.
func (m Money) IsNegative() bool { return m.amountMinor < 0 }

// Add returns m + other. It returns ErrCurrencyMismatch if the two values
// are in different currencies — arithmetic never coerces.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency, other.currency)
	}
	return Money{amountMinor: m.amountMinor + other.amountMinor, currency: m.currency}, nil
}

// Subtract returns m - other. It returns ErrCurrencyMismatch if the two
// values are in different currencies.
func (m Money) Subtract(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency, other.currency)
	}
	return Money{amountMinor: m.amountMinor - other.amountMinor, currency: m.currency}, nil
}

// Negate returns -m, in the same currency.
func (m Money) Negate() Money {
	return Money{amountMinor: -m.amountMinor, currency: m.currency}
}

// Abs returns the absolute value of m, in the same currency.
func (m Money) Abs() Money {
	if m.amountMinor < 0 {
		return m.Negate()
	}
	return m
}

// Compare returns -1, 0, or 1 as m is less than, equal to, or greater than
// other. It returns ErrCurrencyMismatch if the two values are in different
// currencies — there is no total order across currencies.
func (m Money) Compare(other Money) (int, error) {
	if m.currency != other.currency {
		return 0, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency, other.currency)
	}
	switch {
	case m.amountMinor < other.amountMinor:
		return -1, nil
	case m.amountMinor > other.amountMinor:
		return 1, nil
	default:
		return 0, nil
	}
}

// Equal reports whether m and other have the same currency and amount. A
// mismatched currency is simply not equal — this doesn't coerce, it's a
// convenience wrapper that never needs to return an error.
func (m Money) Equal(other Money) bool {
	return m.currency == other.currency && m.amountMinor == other.amountMinor
}

// Sum adds every value in ms together. It returns ErrEmptySum if ms is
// empty (there'd be no currency to attach to the result), or
// ErrCurrencyMismatch on the first value whose currency doesn't match the
// first argument's.
func Sum(ms ...Money) (Money, error) {
	if len(ms) == 0 {
		return Money{}, ErrEmptySum
	}
	total := ms[0]
	for _, m := range ms[1:] {
		var err error
		total, err = total.Add(m)
		if err != nil {
			return Money{}, err
		}
	}
	return total, nil
}

// String returns a plain "amount CODE" representation, e.g. "1500.00 USD"
// or "1500 JPY". Intended for logs and test output, not end-user display —
// see Format for a symbol-prefixed display string.
func (m Money) String() string {
	return m.AmountString() + " " + m.currency
}
