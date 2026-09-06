package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// mustAppDate is mustDate's app-package-local equivalent (the sqlite
// package's own mustDate isn't visible here) -- a domain.Date built from
// plain year/month/day, for asserting against FetchFxRates' stored rows.
func mustAppDate(t *testing.T, year int, month time.Month, day int) domain.Date {
	t.Helper()
	d, err := domain.NewDate(year, month, day)
	if err != nil {
		t.Fatalf("domain.NewDate(%d, %s, %d): %v", year, month, day, err)
	}
	return d
}

func mustProviderRate(t *testing.T, base, quote, value string, date domain.Date) ports.ProviderRate {
	t.Helper()
	rate, err := fx.NewRate(base, quote, decimal.RequireFromString(value))
	if err != nil {
		t.Fatalf("fx.NewRate(%s, %s, %s): %v", base, quote, value, err)
	}
	return ports.ProviderRate{Rate: rate, Date: date}
}

func TestFetchFxRates_DefaultsToEveryInUsePairAtCurrentDate(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	provider := svc.FxProvider.(*memFxProvider)

	if err := svc.SetReportingCurrency(ctx, testActorID, "USD"); err != nil {
		t.Fatalf("SetReportingCurrency: %v", err)
	}
	mustAccountFixture(t, svc, "HDFC", "bank", "INR")

	today := mustAppDate(t, 2026, time.August, 20)
	provider.setRate("INR", "USD", mustProviderRate(t, "INR", "USD", "0.0115", today))

	result, err := svc.FetchFxRates(ctx, app.FetchFxRatesCommand{ActorID: testActorID})
	if err != nil {
		t.Fatalf("FetchFxRates: %v", err)
	}
	if result.ReportingCurrency != "USD" {
		t.Errorf("ReportingCurrency = %q, want USD", result.ReportingCurrency)
	}
	if len(result.Fetched) != 1 {
		t.Fatalf("len(Fetched) = %d, want 1", len(result.Fetched))
	}
	fetched := result.Fetched[0]
	if fetched.Rate.Base() != "INR" || fetched.Rate.Quote() != "USD" {
		t.Errorf("Fetched[0].Rate = %s, want INR/USD", fetched.Rate)
	}
	if !fetched.Date.Equal(today) {
		t.Errorf("Fetched[0].Date = %s, want %s", fetched.Date, today)
	}
	if fetched.Source != "mem-provider" {
		t.Errorf("Fetched[0].Source = %q, want mem-provider", fetched.Source)
	}

	// It must actually be stored, readable back through FxRates.Lookup.
	sel, err := svc.FxRates.Lookup(ctx, "INR", "USD", today, 0)
	if err != nil {
		t.Fatalf("FxRates.Lookup after FetchFxRates: %v", err)
	}
	if sel.Rate.Value().String() != "0.0115" {
		t.Errorf("stored rate = %s, want 0.0115", sel.Rate.Value())
	}

	// One FetchRate call for the one in-use pair -- never FetchRange when
	// no backfill range was requested.
	if len(provider.calls) != 1 {
		t.Fatalf("len(provider.calls) = %d, want 1", len(provider.calls))
	}
	if provider.calls[0].IsRangeCall {
		t.Errorf("provider.calls[0] used FetchRange, want FetchRate for a current-date fetch")
	}
}

func TestFetchFxRates_ExplicitPairsRestrictTheFetch(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	provider := svc.FxProvider.(*memFxProvider)

	if err := svc.SetReportingCurrency(ctx, testActorID, "USD"); err != nil {
		t.Fatalf("SetReportingCurrency: %v", err)
	}
	mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	mustAccountFixture(t, svc, "Deutsche", "bank", "EUR")

	today := mustAppDate(t, 2026, time.August, 20)
	provider.setRate("EUR", "USD", mustProviderRate(t, "EUR", "USD", "1.08", today))
	// INR/USD deliberately has no canned response -- if it were fetched,
	// the provider fake would error and fail this test.

	result, err := svc.FetchFxRates(ctx, app.FetchFxRatesCommand{ActorID: testActorID, Pairs: []string{"EUR"}})
	if err != nil {
		t.Fatalf("FetchFxRates: %v", err)
	}
	if len(result.Fetched) != 1 || result.Fetched[0].Rate.Base() != "EUR" {
		t.Fatalf("Fetched = %+v, want exactly one EUR/USD row", result.Fetched)
	}
	if len(provider.calls) != 1 || provider.calls[0].Base != "EUR" {
		t.Fatalf("provider.calls = %+v, want exactly one call for EUR", provider.calls)
	}
}

func TestFetchFxRates_PairEqualToReportingCurrencyIsSkipped(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	provider := svc.FxProvider.(*memFxProvider)

	if err := svc.SetReportingCurrency(ctx, testActorID, "USD"); err != nil {
		t.Fatalf("SetReportingCurrency: %v", err)
	}

	result, err := svc.FetchFxRates(ctx, app.FetchFxRatesCommand{ActorID: testActorID, Pairs: []string{"USD"}})
	if err != nil {
		t.Fatalf("FetchFxRates: %v", err)
	}
	if len(result.Fetched) != 0 {
		t.Errorf("Fetched = %+v, want none (USD against itself is a no-op)", result.Fetched)
	}
	if len(provider.calls) != 0 {
		t.Errorf("provider.calls = %+v, want none", provider.calls)
	}
}

func TestFetchFxRates_RejectsUnknownPairCurrency(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.FetchFxRates(ctx, app.FetchFxRatesCommand{ActorID: testActorID, Pairs: []string{"ZZZ"}})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestFetchFxRates_BackfillRangeDelegatesToFetchRange(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	provider := svc.FxProvider.(*memFxProvider)

	if err := svc.SetReportingCurrency(ctx, testActorID, "USD"); err != nil {
		t.Fatalf("SetReportingCurrency: %v", err)
	}

	aug10 := mustAppDate(t, 2026, time.August, 10)
	aug11 := mustAppDate(t, 2026, time.August, 11)
	provider.setRange("INR", "USD", []ports.ProviderRate{
		mustProviderRate(t, "INR", "USD", "0.0114", aug10),
		mustProviderRate(t, "INR", "USD", "0.0115", aug11),
	})

	result, err := svc.FetchFxRates(ctx, app.FetchFxRatesCommand{
		ActorID: testActorID, Pairs: []string{"INR"}, From: "2026-08-10", To: "2026-08-11",
	})
	if err != nil {
		t.Fatalf("FetchFxRates: %v", err)
	}
	if len(result.Fetched) != 2 {
		t.Fatalf("len(Fetched) = %d, want 2 (both dates in the range)", len(result.Fetched))
	}

	if len(provider.calls) != 1 || !provider.calls[0].IsRangeCall {
		t.Fatalf("provider.calls = %+v, want exactly one FetchRange call, never a loop per date", provider.calls)
	}
	if !provider.calls[0].From.Equal(aug10) || !provider.calls[0].To.Equal(aug11) {
		t.Errorf("provider.calls[0] From/To = %s/%s, want %s/%s", provider.calls[0].From, provider.calls[0].To, aug10, aug11)
	}

	// Both dates must actually be stored.
	selAug10, err := svc.FxRates.Lookup(ctx, "INR", "USD", aug10, 0)
	if err != nil {
		t.Fatalf("Lookup(aug10): %v", err)
	}
	if selAug10.Rate.Value().String() != "0.0114" {
		t.Errorf("stored rate for aug10 = %s, want 0.0114", selAug10.Rate.Value())
	}
	selAug11, err := svc.FxRates.Lookup(ctx, "INR", "USD", aug11, 0)
	if err != nil {
		t.Fatalf("Lookup(aug11): %v", err)
	}
	if selAug11.Rate.Value().String() != "0.0115" {
		t.Errorf("stored rate for aug11 = %s, want 0.0115", selAug11.Rate.Value())
	}
}

func TestFetchFxRates_RejectsPartialRange(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.FetchFxRates(ctx, app.FetchFxRatesCommand{ActorID: testActorID, From: "2026-08-10"})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestFetchFxRates_RejectsToBeforeFrom(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.FetchFxRates(ctx, app.FetchFxRatesCommand{ActorID: testActorID, From: "2026-08-11", To: "2026-08-10"})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestFetchFxRates_ProviderFailureStoresNothing(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	provider := svc.FxProvider.(*memFxProvider)

	if err := svc.SetReportingCurrency(ctx, testActorID, "USD"); err != nil {
		t.Fatalf("SetReportingCurrency: %v", err)
	}
	mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	mustAccountFixture(t, svc, "Deutsche", "bank", "EUR")

	today := mustAppDate(t, 2026, time.August, 20)
	// EUR/USD succeeds; INR/USD fails -- nothing should be stored for
	// either pair, since the write is one all-or-nothing StoreBatch call
	// after every pair has already been fetched.
	provider.setRate("EUR", "USD", mustProviderRate(t, "EUR", "USD", "1.08", today))
	provider.setError("INR", "USD", errs.New(errs.Unavailable).Explain("provider down"))

	_, err := svc.FetchFxRates(ctx, app.FetchFxRatesCommand{ActorID: testActorID})
	wantErrCode(t, err, errs.Unavailable)

	if _, err := svc.FxRates.Lookup(ctx, "EUR", "USD", today, 0); err == nil {
		t.Errorf("EUR/USD was stored despite the overall fetch failing")
	}
}

func TestFetchFxRates_RequiresActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.FetchFxRates(context.Background(), app.FetchFxRatesCommand{})
	wantErrCode(t, err, errs.InvalidInput)
}
