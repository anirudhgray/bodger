package fxprovider_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/adapters/fxprovider"
)

// TestFetchRate_DecodesRateWithoutFloat64RoundTrip guards ADR-0012's
// "rates arrive as JSON numbers; decode them as text... never through
// float64, not even transiently." 1234567.123456789012 has nineteen
// significant digits -- more than a float64's ~15-17 digit mantissa can
// hold -- so if the wire number were ever decoded into a float64 before
// becoming a decimal.Decimal, the value that survives would not be this
// exact string. Only a json.Number (or an equivalent text-preserving
// decode) reproduces it exactly.
func TestFetchRate_DecodesRateWithoutFloat64RoundTrip(t *testing.T) {
	const wantRate = "1234567.123456789012"

	requested := mustDate(t, 2026, time.January, 15)
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"date":"2026-01-15","base":"EUR","quote":"USD","rate":`+wantRate+`}`), nil
	})
	p := fxprovider.New("http://example.invalid", client)

	got, err := p.FetchRate(context.Background(), "EUR", "USD", requested)
	if err != nil {
		t.Fatalf("FetchRate: %v", err)
	}
	if got.Rate.Value().String() != wantRate {
		t.Errorf("Rate = %s, want %s exactly (precision lost -- likely round-tripped through float64)",
			got.Rate.Value(), wantRate)
	}
}
