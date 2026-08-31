package money

import "errors"

// Named errors returned by this package. Callers should match on these with
// errors.Is; the wrapped text carries the offending value for humans, the
// sentinel carries the identity for code.
var (
	// ErrUnknownCurrency is returned when a currency code has no matching
	// entry in the seeded reference data.
	ErrUnknownCurrency = errors.New("money: unknown currency code")

	// ErrCurrencyMismatch is returned by any arithmetic or comparison
	// operation given two Money values in different currencies. Arithmetic
	// never coerces between currencies.
	ErrCurrencyMismatch = errors.New("money: currency mismatch")

	// ErrEmptySum is returned by Sum when called with no values: there is
	// no currency to attach to the result.
	ErrEmptySum = errors.New("money: cannot sum zero values")

	// ErrInvalidAmountFormat is returned when a string handed to Parse
	// isn't a plain, unsigned-or-signed decimal number.
	ErrInvalidAmountFormat = errors.New("money: invalid amount format")

	// ErrAmbiguousSeparator is returned when a string handed to Parse
	// contains a grouping separator (e.g. "1,500.00"). Different locales
	// use "," and "." for opposite purposes, so a grouped amount is
	// rejected rather than guessed at.
	ErrAmbiguousSeparator = errors.New("money: amount contains an ambiguous grouping separator")

	// ErrPrecisionExceedsCurrency is returned when a string handed to
	// Parse carries more fractional digits than its currency's minor-unit
	// exponent allows (including any fractional digits at all for a
	// zero-exponent currency such as JPY). Accepting it would mean
	// silently rounding, which this package never does.
	ErrPrecisionExceedsCurrency = errors.New("money: amount precision exceeds currency's minor-unit exponent")
)
