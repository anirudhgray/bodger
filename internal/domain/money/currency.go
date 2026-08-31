package money

import "sort"

// Currency is reference data for one ISO 4217 currency: its display name,
// symbol, and how many decimal digits its minor unit has.
//
// This is a fixed, package-seeded set (see seedCurrencies below) — the
// commonly used currencies, not all ~180 of ISO 4217, and with no "enabled"
// flag: a currency nobody references simply doesn't appear. Persisting
// currency reference data so an instance can extend this set is out of
// scope here (issue #3).
type Currency struct {
	// Code is the three-letter ISO 4217 code, e.g. "USD".
	Code string
	// Name is the human-readable currency name, e.g. "US Dollar".
	Name string
	// Symbol is the display-only symbol, e.g. "$". Never used to decide
	// formatting precision — that's MinorUnitExponent.
	Symbol string
	// MinorUnitExponent is the number of digits in the currency's minor
	// unit: 0 for JPY, 2 for USD/INR, 3 for BHD/KWD.
	MinorUnitExponent int
}

// seedCurrencies is the ~20 commonly used currencies, covering all three
// minor-unit-exponent classes seen in practice (0, 2, 3).
var seedCurrencies = []Currency{
	{Code: "USD", Name: "US Dollar", Symbol: "$", MinorUnitExponent: 2},
	{Code: "EUR", Name: "Euro", Symbol: "€", MinorUnitExponent: 2},
	{Code: "GBP", Name: "British Pound", Symbol: "£", MinorUnitExponent: 2},
	{Code: "JPY", Name: "Japanese Yen", Symbol: "¥", MinorUnitExponent: 0},
	{Code: "INR", Name: "Indian Rupee", Symbol: "₹", MinorUnitExponent: 2},
	{Code: "AUD", Name: "Australian Dollar", Symbol: "$", MinorUnitExponent: 2},
	{Code: "CAD", Name: "Canadian Dollar", Symbol: "$", MinorUnitExponent: 2},
	{Code: "CHF", Name: "Swiss Franc", Symbol: "CHF", MinorUnitExponent: 2},
	{Code: "CNY", Name: "Chinese Yuan", Symbol: "¥", MinorUnitExponent: 2},
	{Code: "HKD", Name: "Hong Kong Dollar", Symbol: "$", MinorUnitExponent: 2},
	{Code: "SGD", Name: "Singapore Dollar", Symbol: "$", MinorUnitExponent: 2},
	{Code: "NZD", Name: "New Zealand Dollar", Symbol: "$", MinorUnitExponent: 2},
	{Code: "ZAR", Name: "South African Rand", Symbol: "R", MinorUnitExponent: 2},
	{Code: "SEK", Name: "Swedish Krona", Symbol: "kr", MinorUnitExponent: 2},
	{Code: "NOK", Name: "Norwegian Krone", Symbol: "kr", MinorUnitExponent: 2},
	{Code: "MXN", Name: "Mexican Peso", Symbol: "$", MinorUnitExponent: 2},
	{Code: "BRL", Name: "Brazilian Real", Symbol: "R$", MinorUnitExponent: 2},
	{Code: "KRW", Name: "South Korean Won", Symbol: "₩", MinorUnitExponent: 0},
	{Code: "BHD", Name: "Bahraini Dinar", Symbol: "BD", MinorUnitExponent: 3},
	{Code: "KWD", Name: "Kuwaiti Dinar", Symbol: "KD", MinorUnitExponent: 3},
}

// currencyRegistry indexes seedCurrencies by code for O(1) lookup. Built
// once at package init from the slice above, which stays the single source
// of truth for the seed data.
var currencyRegistry = func() map[string]Currency {
	reg := make(map[string]Currency, len(seedCurrencies))
	for _, c := range seedCurrencies {
		reg[c.Code] = c
	}
	return reg
}()

// LookupCurrency returns the reference data for code, and false if code
// isn't one of the seeded currencies.
func LookupCurrency(code string) (Currency, bool) {
	c, ok := currencyRegistry[code]
	return c, ok
}

// Currencies returns every seeded currency, sorted by code.
func Currencies() []Currency {
	out := make([]Currency, 0, len(seedCurrencies))
	out = append(out, seedCurrencies...)
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}
