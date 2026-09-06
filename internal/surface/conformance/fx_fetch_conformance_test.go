// This file extends this package's conformance coverage to
// POST /api/v1/fx/rates/fetch against `bodger fx rates fetch`, for issue
// #165's direct base/quote fetch. Like balance_conformance_test.go, this
// is a separate, parallel test function rather than another
// conformanceCase row — a fetch response is a list-shaped
// {reporting_currency, fetched[]} result compared as a whole, not a
// single-transaction body, so it doesn't fit conformanceCase's
// purpose-built shape either.
package conformance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// fakeConformanceFxProvider is a minimal, deterministic
// ports.FxRateProvider — this package's fetch conformance case needs
// FetchFxRates to actually resolve a canned rate, unlike
// balance_conformance_test.go's seedFxRate, which bypasses the provider
// entirely by writing straight to FxRates.
type fakeConformanceFxProvider struct {
	rates map[string]fx.Rate // "base/quote" -> rate, always dated `date` below
	date  domain.Date
}

func (p *fakeConformanceFxProvider) Name() string { return "fake-conformance-provider" }

func (p *fakeConformanceFxProvider) FetchRate(_ context.Context, base, quote string, date domain.Date) (ports.ProviderRate, error) {
	rate, ok := p.rates[base+"/"+quote]
	if !ok {
		return ports.ProviderRate{}, errs.New(errs.NotFound).Explain("no fake rate for %s/%s", base, quote)
	}
	return ports.ProviderRate{Rate: rate, Date: date}, nil
}

func (p *fakeConformanceFxProvider) FetchRange(ctx context.Context, base, quote string, from, _ domain.Date) ([]ports.ProviderRate, error) {
	rate, err := p.FetchRate(ctx, base, quote, from)
	if err != nil {
		return nil, err
	}
	return []ports.ProviderRate{rate}, nil
}

var _ ports.FxRateProvider = (*fakeConformanceFxProvider)(nil)

func mustConformanceRate(t *testing.T, base, quote, value string) fx.Rate {
	t.Helper()
	rate, err := fx.NewRate(base, quote, decimal.RequireFromString(value))
	if err != nil {
		t.Fatalf("fx.NewRate(%s, %s, %s): %v", base, quote, value, err)
	}
	return rate
}

// TestFxRatesFetchConformance drives `bodger fx rates fetch --pair --quote
// --json` and `POST /api/v1/fx/rates/fetch` with an explicit,
// non-reporting quote currency against the same seeded INR account and
// fake provider, and asserts the two surfaces' reporting_currency/fetched
// shapes agree field for field — including that both surfaces also land
// the reporting-quoted quote/reporting row in the same call (issue #165).
func TestFxRatesFetchConformance(t *testing.T) {
	today := mustHarnessDate(t, 2026, time.August, 1) // matches hostileInstant's actorTimezone date
	provider := &fakeConformanceFxProvider{
		date: today,
		rates: map[string]fx.Rate{
			"INR/EUR": mustConformanceRate(t, "INR", "EUR", "0.0106"),
			"EUR/USD": mustConformanceRate(t, "EUR", "USD", "1.08"),
		},
	}
	h := newHarnessWithFxProvider(t, provider)
	h.seed() // HDFC Savings/INR, among others

	cliOut, cliErr := h.runCLI("fx", "rates", "fetch", "--pair", "INR", "--quote", "EUR", "--json")
	if cliErr != nil {
		t.Fatalf("CLI: unexpected error: %v (output: %s)", cliErr, cliOut)
	}
	httpStatus, httpDecoded := h.runHTTP("POST", "/api/v1/fx/rates/fetch", map[string]any{
		"pairs": []string{"INR"}, "quote": "EUR",
	})
	if httpStatus >= 300 {
		t.Fatalf("HTTP: unexpected error status %d: %v", httpStatus, httpDecoded)
	}

	cliResult := decodeFxFetchEnvelope(t, cliOut)
	httpResult := fxFetchResultFromHTTP(t, h.httpData(httpDecoded))

	cliJSON, _ := json.MarshalIndent(normalizeFxFetch(cliResult), "", "  ")
	httpJSON, _ := json.MarshalIndent(normalizeFxFetch(httpResult), "", "  ")
	if string(cliJSON) != string(httpJSON) {
		t.Errorf("CLI and HTTP disagree on the fetch result:\nCLI:\n%s\nHTTP:\n%s", cliJSON, httpJSON)
	}

	pairs := map[string]bool{}
	for _, f := range cliResult.Fetched {
		pairs[f.Pair] = true
	}
	if len(cliResult.Fetched) != 2 || !pairs["INR/EUR"] || !pairs["EUR/USD"] {
		t.Fatalf("Fetched = %+v, want exactly INR/EUR (requested) and EUR/USD (reporting-quoted)", cliResult.Fetched)
	}
}

type fxFetchedRowView struct {
	Pair   string `json:"pair"`
	Rate   string `json:"rate"`
	Date   string `json:"date"`
	Source string `json:"source"`
}

type fxFetchResultView struct {
	ReportingCurrency string             `json:"reporting_currency"`
	Fetched           []fxFetchedRowView `json:"fetched"`
}

// decodeFxFetchEnvelope decodes runCLI's stdout — {"data": {...}} on
// success, per internal/surface/cli's own --json envelope.
func decodeFxFetchEnvelope(t *testing.T, output string) fxFetchResultView {
	t.Helper()
	var envelope struct {
		Data fxFetchResultView `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode CLI --json output: %v (output: %s)", err, output)
	}
	return envelope.Data
}

// fxFetchResultFromHTTP re-marshals runHTTP's already-generic
// map[string]any shape into fxFetchResultView, so it's directly
// comparable to decodeFxFetchEnvelope's CLI-side result.
func fxFetchResultFromHTTP(t *testing.T, data map[string]any) fxFetchResultView {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal HTTP data: %v", err)
	}
	var v fxFetchResultView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal HTTP data into fxFetchResultView: %v (data: %s)", err, raw)
	}
	return v
}

// normalizeFxFetch keys Fetched by its pair rather than list order, so a
// harmless difference in enumeration order between the two surfaces never
// causes a false mismatch.
func normalizeFxFetch(v fxFetchResultView) map[string]any {
	fetched := map[string]fxFetchedRowView{}
	for _, f := range v.Fetched {
		fetched[f.Pair] = f
	}
	return map[string]any{"reporting_currency": v.ReportingCurrency, "fetched": fetched}
}
