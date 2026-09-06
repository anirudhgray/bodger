package http_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// fakeFxProvider is a deterministic, in-memory ports.FxRateProvider for
// tests exercising POST /api/v1/fx/rates/fetch without ever touching the
// network — the real adapter (internal/adapters/fxprovider.Frankfurter)
// always makes a real HTTP call, which a unit test must never depend on.
// This package can't import internal/surface/cli's own fakeFxProvider
// (test helpers aren't shared across packages in this codebase), so this
// is a parallel copy of the same fake internal/surface/cli/fx_test.go
// defines.
type fakeFxProvider struct {
	mu    sync.Mutex
	name  string
	rates map[string]map[string]fx.Rate // "base/quote" -> date string -> rate
}

func newFakeFxProvider() *fakeFxProvider {
	return &fakeFxProvider{name: "fake-provider", rates: map[string]map[string]fx.Rate{}}
}

func (p *fakeFxProvider) Name() string { return p.name }

func (p *fakeFxProvider) setRate(t *testing.T, base, quote, value string, date domain.Date) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	rate, err := fx.NewRate(base, quote, decimal.RequireFromString(value))
	if err != nil {
		t.Fatalf("fx.NewRate(%s, %s, %s): %v", base, quote, value, err)
	}
	key := base + "/" + quote
	if p.rates[key] == nil {
		p.rates[key] = map[string]fx.Rate{}
	}
	p.rates[key][date.String()] = rate
}

func (p *fakeFxProvider) FetchRate(_ context.Context, base, quote string, date domain.Date) (ports.ProviderRate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rate, ok := p.rates[base+"/"+quote][date.String()]
	if !ok {
		return ports.ProviderRate{}, errs.New(errs.NotFound).Explain("no fake rate for %s/%s on %s", base, quote, date)
	}
	return ports.ProviderRate{Rate: rate, Date: date}, nil
}

func (p *fakeFxProvider) FetchRange(_ context.Context, base, quote string, from, to domain.Date) ([]ports.ProviderRate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []ports.ProviderRate
	for dateStr, rate := range p.rates[base+"/"+quote] {
		d, err := parseYMD(dateStr)
		if err != nil {
			return nil, err
		}
		if d.Before(from) || d.After(to) {
			continue
		}
		out = append(out, ports.ProviderRate{Rate: rate, Date: d})
	}
	if len(out) == 0 {
		return nil, errs.New(errs.NotFound).Explain("no fake rate data for %s/%s between %s and %s", base, quote, from, to)
	}
	sortProviderRates(out)
	return out, nil
}

var _ ports.FxRateProvider = (*fakeFxProvider)(nil)

// parseYMD parses a domain.Date.String() value ("YYYY-MM-DD") back into a
// domain.Date, for indexing fakeFxProvider's map keys.
func parseYMD(s string) (domain.Date, error) {
	var y, m, d int
	if _, err := fmt.Sscanf(s, "%d-%d-%d", &y, &m, &d); err != nil {
		return domain.Date{}, err
	}
	return domain.NewDate(y, time.Month(m), d)
}

func sortProviderRates(rows []ports.ProviderRate) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].Date.Before(rows[j-1].Date); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

func mustFxTestDate(t *testing.T, year int, month time.Month, day int) domain.Date {
	t.Helper()
	d, err := domain.NewDate(year, month, day)
	if err != nil {
		t.Fatalf("domain.NewDate(%d, %s, %d): %v", year, month, day, err)
	}
	return d
}

// TestFxRatesFetch_DefaultsToInUsePairs checks POST
// /api/v1/fx/rates/fetch's "no pairs fetches every currency pair actually
// in use" default path, mirroring
// internal/surface/cli/fx_test.go's TestFxRatesFetch_DefaultsToInUsePairs.
func TestFxRatesFetch_DefaultsToInUsePairs(t *testing.T) {
	frozenAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	provider := newFakeFxProvider()
	srv := newTestServerWithFxProvider(t, frozenAt, "UTC", provider)

	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Wallet", "type": "cash", "currency": "INR"})

	today := mustFxTestDate(t, 2026, time.August, 14)
	provider.setRate(t, "INR", "USD", "0.0115", today)

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/fx/rates/fetch", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %+v", status, decoded)
	}
	data := dataOf(t, decoded)
	if data["reporting_currency"] != "USD" {
		t.Errorf("reporting_currency = %v, want USD (the instance default, unset by this actor)", data["reporting_currency"])
	}
	fetched, _ := data["fetched"].([]any)
	if len(fetched) != 1 {
		t.Fatalf("fetched = %+v, want exactly one row", data["fetched"])
	}
	row := fetched[0].(map[string]any)
	if row["pair"] != "INR/USD" || row["rate"] != "0.0115" || row["date"] != "2026-08-14" || row["source"] != "fake-provider" {
		t.Errorf("fetched[0] = %+v, want INR/USD 0.0115 on 2026-08-14 from fake-provider", row)
	}
}

// TestFxRatesFetch_PairFilter checks that "pairs" restricts the fetch to
// just the named base currencies, even when other in-use pairs exist.
func TestFxRatesFetch_PairFilter(t *testing.T) {
	frozenAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	provider := newFakeFxProvider()
	srv := newTestServerWithFxProvider(t, frozenAt, "UTC", provider)

	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Wallet", "type": "cash", "currency": "INR"})
	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Euro Account", "type": "bank", "currency": "EUR"})

	today := mustFxTestDate(t, 2026, time.August, 14)
	provider.setRate(t, "INR", "USD", "0.0115", today)
	provider.setRate(t, "EUR", "USD", "1.08", today)

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/fx/rates/fetch", map[string]any{"pairs": []string{"INR"}})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %+v", status, decoded)
	}
	fetched, _ := dataOf(t, decoded)["fetched"].([]any)
	if len(fetched) != 1 || fetched[0].(map[string]any)["pair"] != "INR/USD" {
		t.Errorf("fetched = %+v, want just INR/USD", fetched)
	}
}

// TestFxRatesFetch_Backfill checks the "from"/"to" historical range fetch:
// FetchFxRates delegates to FxProvider.FetchRange, which can (and here
// does) return several distinct dates in one response.
func TestFxRatesFetch_Backfill(t *testing.T) {
	frozenAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	provider := newFakeFxProvider()
	srv := newTestServerWithFxProvider(t, frozenAt, "UTC", provider)

	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Wallet", "type": "cash", "currency": "INR"})

	provider.setRate(t, "INR", "USD", "0.0110", mustFxTestDate(t, 2026, time.August, 1))
	provider.setRate(t, "INR", "USD", "0.0112", mustFxTestDate(t, 2026, time.August, 2))
	provider.setRate(t, "INR", "USD", "0.0115", mustFxTestDate(t, 2026, time.August, 3))

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/fx/rates/fetch", map[string]any{
		"pairs": []string{"INR"}, "from": "2026-08-01", "to": "2026-08-03",
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %+v", status, decoded)
	}
	fetched, _ := dataOf(t, decoded)["fetched"].([]any)
	if len(fetched) != 3 {
		t.Fatalf("fetched = %+v, want 3 rows", fetched)
	}
}

// TestFxRatesFetch_BackfillRequiresBothFromAndTo checks that this package
// doesn't duplicate FetchFxRates' own both-or-neither validation — a lone
// "from" surfaces exactly the app layer's InvalidInput, as a 422.
func TestFxRatesFetch_BackfillRequiresBothFromAndTo(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/fx/rates/fetch", map[string]any{"from": "2026-08-01"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %+v", status, decoded)
	}
	if code := errorCodeOf(t, decoded); code != string(errs.InvalidInput) {
		t.Errorf("error code = %q, want invalid_input", code)
	}
}

// TestFxRatesList_ResolvesEachPolicyAndAmount exercises GET
// /api/v1/fx/rates for all three ADR-0004 policies plus the "amount"
// conversion path, and a stale-rate case — the HTTP-surface equivalent of
// internal/surface/cli/fx_test.go's TestFxRatesList_ResolvesEachPolicy and
// TestFxRatesList_WithAmountConverts.
func TestFxRatesList_ResolvesEachPolicyAndAmount(t *testing.T) {
	frozenAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	provider := newFakeFxProvider()
	srv := newTestServerWithFxProvider(t, frozenAt, "UTC", provider)

	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Wallet", "type": "cash", "currency": "INR"})

	provider.setRate(t, "INR", "USD", "0.0115", mustFxTestDate(t, 2026, time.August, 1))
	provider.setRate(t, "INR", "USD", "0.0125", mustFxTestDate(t, 2026, time.August, 14)) // today

	do(t, srv, http.MethodPost, "/api/v1/fx/rates/fetch", map[string]any{
		"pairs": []string{"INR"}, "from": "2026-08-01", "to": "2026-08-01",
	})
	do(t, srv, http.MethodPost, "/api/v1/fx/rates/fetch", map[string]any{"pairs": []string{"INR"}}) // stores today's rate

	t.Run("transaction_date", func(t *testing.T) {
		status, decoded := do(t, srv, http.MethodGet,
			"/api/v1/fx/rates?from=INR&to=USD&policy=transaction_date&transaction_date=2026-08-01", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200: %+v", status, decoded)
		}
		data := dataOf(t, decoded)
		if data["rate"] != "0.0115" || data["rate_date"] != "2026-08-01" || data["stale"] != false || data["policy"] != "transaction_date" {
			t.Errorf("got %+v", data)
		}
		if data["rate_source"] != "fake-provider" {
			t.Errorf("rate_source = %v, want fake-provider", data["rate_source"])
		}
	})

	t.Run("current", func(t *testing.T) {
		_, decoded := do(t, srv, http.MethodGet, "/api/v1/fx/rates?from=INR&to=USD&policy=current", nil)
		data := dataOf(t, decoded)
		if data["rate"] != "0.0125" || data["rate_date"] != "2026-08-14" || data["stale"] != false || data["policy"] != "current" {
			t.Errorf("got %+v", data)
		}
	})

	t.Run("pinned, exact date", func(t *testing.T) {
		_, decoded := do(t, srv, http.MethodGet, "/api/v1/fx/rates?from=INR&to=USD&policy=pinned&pinned_date=2026-08-01", nil)
		data := dataOf(t, decoded)
		if data["rate"] != "0.0115" || data["rate_date"] != "2026-08-01" || data["stale"] != false {
			t.Errorf("got %+v", data)
		}
	})

	t.Run("pinned, past a stale nearest-earlier rate", func(t *testing.T) {
		_, decoded := do(t, srv, http.MethodGet, "/api/v1/fx/rates?from=INR&to=USD&policy=pinned&pinned_date=2026-08-05", nil)
		data := dataOf(t, decoded)
		if data["rate"] != "0.0115" || data["rate_date"] != "2026-08-01" || data["stale"] != true {
			t.Errorf("got %+v, want the 08-01 rate flagged stale", data)
		}
	})

	t.Run("amount converts", func(t *testing.T) {
		_, decoded := do(t, srv, http.MethodGet, "/api/v1/fx/rates?from=INR&to=USD&policy=current&amount=1000", nil)
		data := dataOf(t, decoded)
		if data["amount"] != "1000.00 INR" {
			t.Errorf("amount = %v, want 1000.00 INR", data["amount"])
		}
		if data["converted"] != "12.50 USD" {
			t.Errorf("converted = %v, want 12.50 USD", data["converted"])
		}
	})
}

// TestFxRatesList_NoRateWithinWindowIs404 checks that a request for a rate
// with nothing stored within the staleness window surfaces the app
// layer's NotFound as a 404, not a 422 or a silent success.
func TestFxRatesList_NoRateWithinWindowIs404(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")

	status, decoded := do(t, srv, http.MethodGet, "/api/v1/fx/rates?from=INR&to=USD&policy=current", nil)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %+v", status, decoded)
	}
	if code := errorCodeOf(t, decoded); code != string(errs.NotFound) {
		t.Errorf("error code = %q, want not_found", code)
	}
}

// TestFxRatesList_InvalidPolicyIs422 checks this package doesn't hardcode
// its own policy enum — an unrecognized policy value surfaces exactly
// ConvertAmount's own InvalidInput, field "policy".
func TestFxRatesList_InvalidPolicyIs422(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")

	status, decoded := do(t, srv, http.MethodGet, "/api/v1/fx/rates?from=INR&to=USD&policy=yesterday", nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %+v", status, decoded)
	}
	if code := errorCodeOf(t, decoded); code != string(errs.InvalidInput) {
		t.Errorf("error code = %q, want invalid_input", code)
	}
}
