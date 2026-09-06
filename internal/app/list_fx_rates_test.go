package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestListFxRates_WithAmountReturnsConvertedFigure(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	seedRate(t, svc, "INR", "USD", "0.0115", "2026-08-20", "ecb")

	amount := mustMoney(t, 10000000, "INR") // 100000.00 INR
	result, err := svc.ListFxRates(ctx, app.ListFxRatesQuery{
		From:   "INR",
		To:     "USD",
		Policy: app.PolicyCurrent,
		Amount: &amount,
	})
	if err != nil {
		t.Fatalf("ListFxRates: %v", err)
	}
	if result.Converted == nil {
		t.Fatalf("Converted = nil, want a converted figure")
	}
	if got := result.Converted.Amount.AmountMinor(); got != 115000 {
		t.Errorf("Converted.Amount.AmountMinor() = %d, want 115000 (1150.00 USD)", got)
	}
	if result.RateSource != "ecb" {
		t.Errorf("RateSource = %q, want ecb", result.RateSource)
	}
	if result.RateDate.String() != "2026-08-20" {
		t.Errorf("RateDate = %s, want 2026-08-20", result.RateDate)
	}
	if result.Stale {
		t.Errorf("Stale = true, want false for an exact match")
	}
	if result.Policy != app.PolicyCurrent {
		t.Errorf("Policy = %s, want %s", result.Policy, app.PolicyCurrent)
	}
}

func TestListFxRates_WithoutAmountReturnsRateOnly(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	seedRate(t, svc, "INR", "USD", "0.0115", "2026-08-20", "ecb")

	result, err := svc.ListFxRates(ctx, app.ListFxRatesQuery{
		From:   "INR",
		To:     "USD",
		Policy: app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("ListFxRates: %v", err)
	}
	if result.Converted != nil {
		t.Errorf("Converted = %+v, want nil when no amount was given", result.Converted)
	}
	if result.RateSource != "ecb" {
		t.Errorf("RateSource = %q, want ecb", result.RateSource)
	}
	if result.Rate.Value().String() != "0.0115" {
		t.Errorf("Rate = %s, want 0.0115", result.Rate.Value())
	}
}

func TestListFxRates_NeverTouchesTheProvider(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	provider := svc.FxProvider.(*memFxProvider)

	seedRate(t, svc, "INR", "USD", "0.0115", "2026-08-20", "ecb")

	if _, err := svc.ListFxRates(ctx, app.ListFxRatesQuery{
		From: "INR", To: "USD", Policy: app.PolicyCurrent,
	}); err != nil {
		t.Fatalf("ListFxRates: %v", err)
	}
	if len(provider.calls) != 0 {
		t.Errorf("provider.calls = %+v, want none: ListFxRates must never call FxProvider", provider.calls)
	}
}

func TestListFxRates_MissingRateFailsLoudly(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.ListFxRates(ctx, app.ListFxRatesQuery{
		From: "INR", To: "USD", Policy: app.PolicyCurrent,
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestListFxRates_RejectsAmountCurrencyMismatch(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	amount := mustMoney(t, 100, "EUR")
	_, err := svc.ListFxRates(ctx, app.ListFxRatesQuery{
		From: "INR", To: "USD", Policy: app.PolicyCurrent, Amount: &amount,
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestListFxRates_SameCurrencyIsIdentityWithNoLookup(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	amount := mustMoney(t, 500, "USD")
	result, err := svc.ListFxRates(ctx, app.ListFxRatesQuery{
		From: "USD", To: "USD", Policy: app.PolicyCurrent, Amount: &amount,
	})
	if err != nil {
		t.Fatalf("ListFxRates: %v", err)
	}
	if !result.Rate.IsIdentity() {
		t.Errorf("Rate = %s, want identity", result.Rate)
	}
	if result.Converted == nil || !result.Converted.Amount.Equal(amount) {
		t.Errorf("Converted = %+v, want an identity conversion of %s", result.Converted, amount)
	}
}
