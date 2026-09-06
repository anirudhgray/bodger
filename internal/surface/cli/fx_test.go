package cli_test

import (
	"context"
	"fmt"
	"strings"
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
// tests exercising `fx rates fetch` without ever touching the network —
// the real adapter (internal/adapters/fxprovider.Frankfurter) always makes
// a real HTTP call, which a unit test must never depend on.
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

type fxFetchedRow struct {
	Pair   string `json:"pair"`
	Rate   string `json:"rate"`
	Date   string `json:"date"`
	Source string `json:"source"`
}

type fxFetchResult struct {
	ReportingCurrency string         `json:"reporting_currency"`
	Fetched           []fxFetchedRow `json:"fetched"`
}

// TestFxRatesFetch_DefaultsToInUsePairs checks issue #135's "no --pair
// flags fetches every currency pair actually in use" default path: an
// account in a currency other than the (unset, so instance-default)
// reporting currency is enough to put a pair in play.
func TestFxRatesFetch_DefaultsToInUsePairs(t *testing.T) {
	provider := newFakeFxProvider()
	factory := newTestFactoryWithFxProvider(t, mustFrozen(t), provider)
	mustRun(t, factory, "accounts", "add", "Wallet", "--type", "cash", "--currency", "INR")

	today := mustFxTestDate(t, 2026, time.August, 14) // matches frozenInstant's date
	provider.setRate(t, "INR", "USD", "0.0115", today)

	var got fxFetchResult
	decodeData(t, mustRun(t, factory, "fx", "rates", "fetch", "--json"), &got)

	if got.ReportingCurrency != "USD" {
		t.Errorf("reporting_currency = %q, want USD (the instance default, unset by this actor)", got.ReportingCurrency)
	}
	if len(got.Fetched) != 1 {
		t.Fatalf("fetched = %+v, want exactly one row", got.Fetched)
	}
	row := got.Fetched[0]
	if row.Pair != "INR/USD" || row.Rate != "0.0115" || row.Date != "2026-08-14" || row.Source != "fake-provider" {
		t.Errorf("fetched[0] = %+v, want INR/USD 0.0115 on 2026-08-14 from fake-provider", row)
	}
}

// TestFxRatesFetch_PairFilter checks that --pair restricts the fetch to
// just the named base currencies, even when other in-use pairs exist.
func TestFxRatesFetch_PairFilter(t *testing.T) {
	provider := newFakeFxProvider()
	factory := newTestFactoryWithFxProvider(t, mustFrozen(t), provider)
	mustRun(t, factory, "accounts", "add", "Wallet", "--type", "cash", "--currency", "INR")
	mustRun(t, factory, "accounts", "add", "Euro Account", "--type", "bank", "--currency", "EUR")

	today := mustFxTestDate(t, 2026, time.August, 14)
	provider.setRate(t, "INR", "USD", "0.0115", today)
	provider.setRate(t, "EUR", "USD", "1.08", today)

	var got fxFetchResult
	decodeData(t, mustRun(t, factory, "fx", "rates", "fetch", "--pair", "INR", "--json"), &got)

	if len(got.Fetched) != 1 || got.Fetched[0].Pair != "INR/USD" {
		t.Errorf("fetched = %+v, want just INR/USD", got.Fetched)
	}
}

// TestFxRatesFetch_Backfill checks --from/--to's historical range fetch:
// FetchFxRates delegates to FxProvider.FetchRange, which can (and here
// does) return several distinct dates in one response.
func TestFxRatesFetch_Backfill(t *testing.T) {
	provider := newFakeFxProvider()
	factory := newTestFactoryWithFxProvider(t, mustFrozen(t), provider)
	mustRun(t, factory, "accounts", "add", "Wallet", "--type", "cash", "--currency", "INR")

	provider.setRate(t, "INR", "USD", "0.0110", mustFxTestDate(t, 2026, time.August, 1))
	provider.setRate(t, "INR", "USD", "0.0112", mustFxTestDate(t, 2026, time.August, 2))
	provider.setRate(t, "INR", "USD", "0.0115", mustFxTestDate(t, 2026, time.August, 3))

	var got fxFetchResult
	decodeData(t, mustRun(t, factory, "fx", "rates", "fetch", "--pair", "INR",
		"--from", "2026-08-01", "--to", "2026-08-03", "--json"), &got)

	if len(got.Fetched) != 3 {
		t.Fatalf("fetched = %+v, want 3 rows", got.Fetched)
	}
	wantDates := []string{"2026-08-01", "2026-08-02", "2026-08-03"}
	for i, want := range wantDates {
		if got.Fetched[i].Date != want {
			t.Errorf("fetched[%d].Date = %q, want %q", i, got.Fetched[i].Date, want)
		}
	}
}

// TestFxRatesFetch_BackfillRequiresBothFromAndTo checks that this
// package doesn't duplicate FetchFxRates' own both-or-neither validation —
// a lone --from surfaces exactly the app layer's InvalidInput.
func TestFxRatesFetch_BackfillRequiresBothFromAndTo(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	_, _, err := run(t, factory, "fx", "rates", "fetch", "--from", "2026-08-01")
	wantErrCode(t, err, errs.InvalidInput)
}

// TestFxRatesFetch_NothingToFetch checks the empty-state message when
// there are no in-use currency pairs to fetch (a fresh instance, or one
// whose only accounts are already in the reporting currency).
func TestFxRatesFetch_NothingToFetch(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	stdout := mustRun(t, factory, "fx", "rates", "fetch")
	if !strings.Contains(stdout, "Nothing to fetch") {
		t.Errorf("stdout = %q, want a friendly empty-state message", stdout)
	}
}

type fxRateResult struct {
	Rate       string `json:"rate"`
	RateDate   string `json:"rate_date"`
	RateSource string `json:"rate_source"`
	Stale      bool   `json:"stale"`
	Policy     string `json:"policy"`
	Amount     string `json:"amount"`
	Converted  string `json:"converted"`
}

// TestFxRatesList_ResolvesEachPolicy exercises all three ADR-0004 policies
// against the same stored data: transaction_date and pinned both resolve
// to the (historical) date they're given, current resolves to today
// (mustFrozen's date) regardless, and a pinned date past the stored rate
// but within the default 7-day staleness window comes back flagged Stale
// with the earlier rate's own date, not the requested one.
func TestFxRatesList_ResolvesEachPolicy(t *testing.T) {
	provider := newFakeFxProvider()
	factory := newTestFactoryWithFxProvider(t, mustFrozen(t), provider)
	mustRun(t, factory, "accounts", "add", "Wallet", "--type", "cash", "--currency", "INR")

	provider.setRate(t, "INR", "USD", "0.0115", mustFxTestDate(t, 2026, time.August, 1))
	provider.setRate(t, "INR", "USD", "0.0125", mustFxTestDate(t, 2026, time.August, 14)) // today

	mustRun(t, factory, "fx", "rates", "fetch", "--pair", "INR", "--from", "2026-08-01", "--to", "2026-08-01")
	mustRun(t, factory, "fx", "rates", "fetch", "--pair", "INR") // stores today's rate

	t.Run("transaction_date", func(t *testing.T) {
		var got fxRateResult
		decodeData(t, mustRun(t, factory, "fx", "rates", "list", "--from", "INR", "--to", "USD",
			"--policy", "transaction_date", "--transaction-date", "2026-08-01", "--json"), &got)
		if got.Rate != "0.0115" || got.RateDate != "2026-08-01" || got.Stale || got.Policy != "transaction_date" {
			t.Errorf("got %+v", got)
		}
		if got.RateSource != "fake-provider" {
			t.Errorf("rate_source = %q, want fake-provider", got.RateSource)
		}
	})

	t.Run("current", func(t *testing.T) {
		var got fxRateResult
		decodeData(t, mustRun(t, factory, "fx", "rates", "list", "--from", "INR", "--to", "USD",
			"--policy", "current", "--json"), &got)
		if got.Rate != "0.0125" || got.RateDate != "2026-08-14" || got.Stale || got.Policy != "current" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("pinned, exact date", func(t *testing.T) {
		var got fxRateResult
		decodeData(t, mustRun(t, factory, "fx", "rates", "list", "--from", "INR", "--to", "USD",
			"--policy", "pinned", "--pinned-date", "2026-08-01", "--json"), &got)
		if got.Rate != "0.0115" || got.RateDate != "2026-08-01" || got.Stale {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("pinned, past a stale nearest-earlier rate", func(t *testing.T) {
		var got fxRateResult
		decodeData(t, mustRun(t, factory, "fx", "rates", "list", "--from", "INR", "--to", "USD",
			"--policy", "pinned", "--pinned-date", "2026-08-05", "--json"), &got)
		if got.Rate != "0.0115" || got.RateDate != "2026-08-01" || !got.Stale {
			t.Errorf("got %+v, want the 08-01 rate flagged stale", got)
		}
	})
}

// TestFxRatesList_WithAmountConverts checks --amount forwards through
// ListFxRatesQuery.AmountRaw (a raw string, since this package can't
// construct a money.Money itself) and that the result's converted figure
// is rendered alongside the rate.
func TestFxRatesList_WithAmountConverts(t *testing.T) {
	provider := newFakeFxProvider()
	factory := newTestFactoryWithFxProvider(t, mustFrozen(t), provider)
	mustRun(t, factory, "accounts", "add", "Wallet", "--type", "cash", "--currency", "INR")
	provider.setRate(t, "INR", "USD", "0.0125", mustFxTestDate(t, 2026, time.August, 14)) // today
	mustRun(t, factory, "fx", "rates", "fetch", "--pair", "INR")

	var got fxRateResult
	decodeData(t, mustRun(t, factory, "fx", "rates", "list", "--from", "INR", "--to", "USD",
		"--policy", "current", "--amount", "1000", "--json"), &got)
	if got.Amount != "1000.00 INR" {
		t.Errorf("amount = %q, want 1000.00 INR", got.Amount)
	}
	if got.Converted != "12.50 USD" {
		t.Errorf("converted = %q, want 12.50 USD", got.Converted)
	}
}

// TestFxRatesList_InvalidPolicyForwardsAppError checks this package
// doesn't hardcode its own policy enum — an unrecognized --policy value
// surfaces exactly ConvertAmount's own InvalidInput, field "policy".
func TestFxRatesList_InvalidPolicyForwardsAppError(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	_, _, err := run(t, factory, "fx", "rates", "list", "--from", "INR", "--to", "USD", "--policy", "yesterday")
	wantErrCode(t, err, errs.InvalidInput)
}
