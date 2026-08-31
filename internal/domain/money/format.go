package money

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// plainDecimal matches a signed, ungrouped decimal number: an optional
// leading "-", one or more digits, and an optional "." followed by one or
// more digits. Anything with a grouping separator, exponent notation, or
// stray characters is rejected before this even runs (see Parse).
var plainDecimal = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// AmountString renders m's amount as a plain decimal string using its
// currency's minor-unit exponent — e.g. "1500.00" for $15.00, "1500" for
// ¥1500, "1500.000" for a 3-decimal currency. No symbol, no grouping; this
// is what MarshalJSON uses for the wire "amount" field, and it round-trips
// exactly through Parse.
func (m Money) AmountString() string {
	exponent := 2
	if c, ok := LookupCurrency(m.currency); ok {
		exponent = c.MinorUnitExponent
	}
	return formatAmount(m.amountMinor, exponent)
}

// Format renders m for display: its currency's symbol followed by
// AmountString, with a leading "-" for negative amounts placed before the
// symbol (e.g. "-$50.00", not "$-50.00"). It returns ErrUnknownCurrency if
// m's currency isn't in the reference data — this should never happen for
// a Money built via NewMoney, but Format doesn't assume that.
func Format(m Money) (string, error) {
	c, ok := LookupCurrency(m.currency)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownCurrency, m.currency)
	}
	amount := formatAmount(m.amountMinor, c.MinorUnitExponent)
	if strings.HasPrefix(amount, "-") {
		return "-" + c.Symbol + amount[1:], nil
	}
	return c.Symbol + amount, nil
}

// formatAmount renders minor (a count of a currency's minor units) as a
// plain decimal string with exactly exponent fractional digits (none, and
// no ".", if exponent is 0).
func formatAmount(minor int64, exponent int) string {
	negative := minor < 0

	// Avoid overflow negating math.MinInt64 by working in uint64.
	var abs uint64
	if negative {
		abs = uint64(-(minor + 1)) + 1
	} else {
		abs = uint64(minor)
	}

	divisor := pow10(exponent)
	whole := abs / divisor
	frac := abs % divisor

	var sb strings.Builder
	if negative {
		sb.WriteByte('-')
	}
	sb.WriteString(strconv.FormatUint(whole, 10))
	if exponent > 0 {
		fracStr := strconv.FormatUint(frac, 10)
		sb.WriteByte('.')
		sb.WriteString(strings.Repeat("0", exponent-len(fracStr)))
		sb.WriteString(fracStr)
	}
	return sb.String()
}

// Parse parses s as a plain decimal amount in currency and returns the
// resulting Money. s must have no grouping separator, no exponent
// notation, and no more fractional digits than currency's minor-unit
// exponent allows (a zero-exponent currency such as JPY allows none at
// all) — anything else is rejected with a named error rather than
// silently rounded or guessed at.
func Parse(s string, currency string) (Money, error) {
	c, ok := LookupCurrency(currency)
	if !ok {
		return Money{}, fmt.Errorf("%w: %q", ErrUnknownCurrency, currency)
	}
	minor, err := parseAmount(s, c.MinorUnitExponent)
	if err != nil {
		return Money{}, err
	}
	return NewMoney(minor, currency)
}

// parseAmount parses s (a plain decimal string) into a count of minor
// units at the given exponent.
func parseAmount(s string, exponent int) (int64, error) {
	if strings.ContainsRune(s, ',') {
		return 0, fmt.Errorf("%w: %q uses a grouping separator; enter a plain number without thousands separators", ErrAmbiguousSeparator, s)
	}
	if !plainDecimal.MatchString(s) {
		return 0, fmt.Errorf("%w: %q is not a plain decimal amount", ErrInvalidAmountFormat, s)
	}

	negative := strings.HasPrefix(s, "-")
	unsigned := strings.TrimPrefix(s, "-")

	wholePart, fracPart, hasFrac := strings.Cut(unsigned, ".")
	if !hasFrac {
		fracPart = ""
	}
	if len(fracPart) > exponent {
		return 0, fmt.Errorf("%w: %q has more decimal digits than this currency's %d allow", ErrPrecisionExceedsCurrency, s, exponent)
	}
	fracPart += strings.Repeat("0", exponent-len(fracPart))

	whole, err := strconv.ParseUint(wholePart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q: %w", ErrInvalidAmountFormat, s, err)
	}

	var frac uint64
	if exponent > 0 {
		frac, err = strconv.ParseUint(fracPart, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: %q: %w", ErrInvalidAmountFormat, s, err)
		}
	}

	minor := whole*pow10(exponent) + frac
	result := int64(minor)
	if negative {
		result = -result
	}
	return result, nil
}

// pow10 returns 10^n for the small, non-negative exponents currency data
// actually uses (0, 2, or 3 in practice).
func pow10(n int) uint64 {
	result := uint64(1)
	for range n {
		result *= 10
	}
	return result
}
