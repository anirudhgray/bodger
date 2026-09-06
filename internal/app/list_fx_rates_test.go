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

	result, err := svc.ListFxRates(ctx, app.ListFxRatesQuery{
		From:   "INR",
		To:     "USD",
		Policy: app.PolicyCurrent,
		Amount: "100000.00", // parsed against From, same as any other user-typed amount
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

// TestListFxRates_RejectsUnparsableAmount replaces a now-impossible case:
// Amount used to be a *money.Money, so a caller could hand in one whose
// own currency disagreed with From. Now that Amount is a raw string
// parsed against From (like every other user-typed amount in this
// package), that mismatch can't be expressed any more -- what can still go
// wrong is the string itself failing to parse, which is what this checks.
func TestListFxRates_RejectsUnparsableAmount(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.ListFxRates(ctx, app.ListFxRatesQuery{
		From: "INR", To: "USD", Policy: app.PolicyCurrent, Amount: "not-a-number",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestListFxRates_SameCurrencyIsIdentityWithNoLookup(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	result, err := svc.ListFxRates(ctx, app.ListFxRatesQuery{
		From: "USD", To: "USD", Policy: app.PolicyCurrent, Amount: "5.00",
	})
	if err != nil {
		t.Fatalf("ListFxRates: %v", err)
	}
	if !result.Rate.IsIdentity() {
		t.Errorf("Rate = %s, want identity", result.Rate)
	}
	want := mustMoney(t, 500, "USD")
	if result.Converted == nil || !result.Converted.Amount.Equal(want) {
		t.Errorf("Converted = %+v, want an identity conversion of %s", result.Converted, want)
	}
}
