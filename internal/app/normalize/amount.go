package normalize

import (
	"strings"

	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// Amount parses raw into signed minor units of currency (an ISO 4217 code
// that must already have been resolved — see Currency — since the
// exponent that turns a decimal string into an integer count of minor
// units comes from the currency's own reference data). It strips grouping
// separators ("1,800.50") and a leading or trailing occurrence of the
// currency's own symbol or code ("₹1800.50", "INR 1800.50"), then delegates
// the actual decimal-to-minor-units conversion to money.Parse.
//
// money.Parse already refuses more fractional digits than the currency's
// minor-unit exponent allows — the exact "rejects floats" behaviour this
// function needs, since a value that leaked out of a float64 (for example
// a naive fmt.Sprintf("%f", 1800.50), which renders as "1800.500000") is
// indistinguishable from a genuinely over-precise input, and both must be
// rejected rather than silently rounded. Reusing it here means the
// precision rule is defined and tested in exactly one place.
func Amount(raw, currencyCode string) (int64, error) {
	cur, ok := money.LookupCurrency(currencyCode)
	if !ok {
		return 0, errs.New(errs.InvalidInput).
			Explain("%q is not a known currency", currencyCode).
			Field("currency")
	}

	cleaned := strings.TrimSpace(raw)
	if cur.Symbol != "" {
		cleaned = strings.ReplaceAll(cleaned, cur.Symbol, "")
	}
	cleaned = strings.ReplaceAll(cleaned, cur.Code, "")
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.TrimSpace(cleaned)

	m, err := money.Parse(cleaned, currencyCode)
	if err != nil {
		return 0, errs.New(errs.InvalidInput).
			Explain("%q isn't a valid amount for %s", raw, cur.Code).
			Field("amount").
			Wrap(err)
	}
	return m.AmountMinor(), nil
}
