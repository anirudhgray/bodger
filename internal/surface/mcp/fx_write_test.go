package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"

	"github.com/shopspring/decimal"
)

// fakeFxProvider is a deterministic, in-memory ports.FxRateProvider for
// tests exercising fetch_fx_rates without ever touching the network — the
// real adapter (internal/adapters/fxprovider.Frankfurter) always makes a
// real HTTP call, which a unit test must never depend on. This package
// can't import internal/surface/cli's or internal/surface/http's own
// fakeFxProvider (test helpers aren't shared across packages in this
// codebase), so this is a parallel copy of the same fake those packages'
// own fx_test.go files define.
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

// callFetchFxRatesTool is callTool (read_tools_test.go), specialized to
// fetch_fx_rates -- unlike every other tool this package tests, it needs
// a *app.Service wired to a fake, non-network FX provider rather than
// newTestService's own real (but harmless when unused) Frankfurter one.
func callFetchFxRatesTool(t *testing.T, svc *app.Service, args any) (json.RawMessage, *sdkmcp.CallToolResult) {
	t.Helper()
	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}

	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("json.Marshal(args): %v", err)
		}
		raw = b
	}

	result, err := d.Dispatch(context.Background(), "fetch_fx_rates", raw)
	if err != nil {
		t.Fatalf("Dispatch(fetch_fx_rates): %v", err)
	}
	text, ok := textContent(result)
	if !ok {
		t.Fatalf("Dispatch(fetch_fx_rates) content[0] wasn't text: %+v", result.Content)
	}
	return json.RawMessage(text), result
}

// TestFetchFxRates_DefaultsToInUsePairs checks fetch_fx_rates' "no pairs
// fetches every currency pair actually in use" default path, mirroring
// internal/surface/http/fx_test.go's and internal/surface/cli/fx_test.go's
// own TestFxRatesFetch_DefaultsToInUsePairs.
func TestFetchFxRates_DefaultsToInUsePairs(t *testing.T) {
	frozenAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	provider := newFakeFxProvider()
	svc, _ := newTestServiceWithFxProvider(t, frozenAt, provider)

	_, err := svc.CreateAccount(context.Background(), app.CreateAccountCommand{
		ActorID: ports.SeededUserID, Name: "Wallet", Kind: "cash", Currency: "INR",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	today := mustFxTestDate(t, 2026, time.August, 14)
	provider.setRate(t, "INR", "USD", "0.0115", today)

	raw, result := callFetchFxRatesTool(t, svc, nil)
	if result.IsError {
		t.Fatalf("fetch_fx_rates IsError = true: %s", raw)
	}

	var view fxFetchView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.ReportingCurrency != "USD" {
		t.Errorf("ReportingCurrency = %q, want USD (the instance default, unset by this actor)", view.ReportingCurrency)
	}
	if len(view.Fetched) != 1 {
		t.Fatalf("Fetched = %+v, want exactly one row", view.Fetched)
	}
	got := view.Fetched[0]
	if got.Pair != "INR/USD" || got.Rate != "0.0115" || got.Date != "2026-08-14" || got.Source != "fake-provider" {
		t.Errorf("Fetched[0] = %+v, want INR/USD 0.0115 on 2026-08-14 from fake-provider", got)
	}
}

// TestFetchFxRates_PairFilter checks that "pairs" restricts the fetch to
// just the named base currencies, even when other in-use pairs exist.
func TestFetchFxRates_PairFilter(t *testing.T) {
	frozenAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	provider := newFakeFxProvider()
	svc, _ := newTestServiceWithFxProvider(t, frozenAt, provider)

	ctx := context.Background()
	if _, err := svc.CreateAccount(ctx, app.CreateAccountCommand{
		ActorID: ports.SeededUserID, Name: "Wallet", Kind: "cash", Currency: "INR",
	}); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if _, err := svc.CreateAccount(ctx, app.CreateAccountCommand{
		ActorID: ports.SeededUserID, Name: "Euro Account", Kind: "bank", Currency: "EUR",
	}); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	today := mustFxTestDate(t, 2026, time.August, 14)
	provider.setRate(t, "INR", "USD", "0.0115", today)
	provider.setRate(t, "EUR", "USD", "1.08", today)

	raw, result := callFetchFxRatesTool(t, svc, map[string]any{"pairs": []string{"INR"}})
	if result.IsError {
		t.Fatalf("fetch_fx_rates IsError = true: %s", raw)
	}

	var view fxFetchView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(view.Fetched) != 1 || view.Fetched[0].Pair != "INR/USD" {
		t.Errorf("Fetched = %+v, want just INR/USD", view.Fetched)
	}
}

// TestFetchFxRates_BackfillRequiresBothFromAndTo checks that this package
// doesn't duplicate FetchFxRates' own both-or-neither validation for a
// backfill range — a lone "from" is rejected as InvalidInput, the
// validation-error path this write-tier tool's test needs (mirroring
// internal/surface/http/fx_test.go's own
// TestFxRatesFetch_BackfillRequiresBothFromAndTo).
func TestFetchFxRates_BackfillRequiresBothFromAndTo(t *testing.T) {
	frozenAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	provider := newFakeFxProvider()
	svc, _ := newTestServiceWithFxProvider(t, frozenAt, provider)

	raw, result := callFetchFxRatesTool(t, svc, map[string]any{"from": "2026-08-01"})
	if !result.IsError {
		t.Fatalf("fetch_fx_rates IsError = false, want true (from without to): %s", raw)
	}
}
