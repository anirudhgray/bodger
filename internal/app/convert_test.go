package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func mustMoney(t *testing.T, amountMinor int64, currency string) money.Money {
	t.Helper()
	m, err := money.NewMoney(amountMinor, currency)
	if err != nil {
		t.Fatalf("NewMoney(%d, %q) = %v, want success", amountMinor, currency, err)
	}
	return m
}

func TestConvertAmount_SameCurrencyIsIdentityWithNoLookup(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	amount := mustMoney(t, 150000, "USD")
	result, err := svc.ConvertAmount(ctx, app.ConvertAmountQuery{
		Amount: amount,
		To:     "USD",
		Policy: app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("ConvertAmount: %v", err)
	}
	if !result.Amount.Equal(amount) {
		t.Errorf("Amount = %s, want unchanged %s", result.Amount, amount)
	}
	if !result.ConvertedFrom.Equal(amount) {
		t.Errorf("ConvertedFrom = %s, want %s", result.ConvertedFrom, amount)
	}
	if !result.Rate.IsIdentity() {
		t.Errorf("Rate = %s, want identity", result.Rate)
	}
	if result.RateSource != "" {
		t.Errorf("RateSource = %q, want empty for an identity conversion", result.RateSource)
	}
	if result.Stale {
		t.Errorf("Stale = true, want false")
	}
	if result.RateDate.String() != "2026-08-20" {
		t.Errorf("RateDate = %s, want 2026-08-20 (today, per PolicyCurrent)", result.RateDate)
	}
	if result.Policy != app.PolicyCurrent {
		t.Errorf("Policy = %s, want %s", result.Policy, app.PolicyCurrent)
	}
}

func TestConvertAmount_TransactionDatePolicyUsesTheGivenDate(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	seedRate(t, svc, "INR", "USD", "0.0115", "2026-08-14", "ecb")

	amount := mustMoney(t, 10000000, "INR") // 100000.00 INR
	result, err := svc.ConvertAmount(ctx, app.ConvertAmountQuery{
		Amount:          amount,
		To:              "USD",
		Policy:          app.PolicyTransactionDate,
		TransactionDate: "2026-08-14",
	})
	if err != nil {
		t.Fatalf("ConvertAmount: %v", err)
	}
	if got := result.Amount.AmountMinor(); got != 115000 {
		t.Errorf("Amount.AmountMinor() = %d, want 115000 (1150.00 USD)", got)
	}
	if result.RateDate.String() != "2026-08-14" {
		t.Errorf("RateDate = %s, want 2026-08-14", result.RateDate)
	}
	if result.RateSource != "ecb" {
		t.Errorf("RateSource = %q, want ecb", result.RateSource)
	}
	if result.Stale {
		t.Errorf("Stale = true, want false for an exact match")
	}
}

func TestConvertAmount_PinnedPolicyUsesTheGivenDate(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	seedRate(t, svc, "INR", "USD", "0.0120", "2026-01-01", "ecb")

	amount := mustMoney(t, 10000000, "INR")
	result, err := svc.ConvertAmount(ctx, app.ConvertAmountQuery{
		Amount:     amount,
		To:         "USD",
		Policy:     app.PolicyPinned,
		PinnedDate: "2026-01-01",
	})
	if err != nil {
		t.Fatalf("ConvertAmount: %v", err)
	}
	if got := result.Amount.AmountMinor(); got != 120000 {
		t.Errorf("Amount.AmountMinor() = %d, want 120000 (1200.00 USD)", got)
	}
	if result.RateDate.String() != "2026-01-01" {
		t.Errorf("RateDate = %s, want 2026-01-01", result.RateDate)
	}
}

func TestConvertAmount_CurrentPolicyResolvesTodayViaTheFakeClock(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	seedRate(t, svc, "INR", "USD", "0.0130", "2026-08-20", "ecb")

	amount := mustMoney(t, 10000000, "INR")
	result, err := svc.ConvertAmount(ctx, app.ConvertAmountQuery{
		Amount: amount,
		To:     "USD",
		Policy: app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("ConvertAmount: %v", err)
	}
	if result.RateDate.String() != "2026-08-20" {
		t.Errorf("RateDate = %s, want 2026-08-20 (frozen clock's today)", result.RateDate)
	}
	if got := result.Amount.AmountMinor(); got != 130000 {
		t.Errorf("Amount.AmountMinor() = %d, want 130000 (1300.00 USD)", got)
	}
}

func TestConvertAmount_StaleButWithinWindowIsFlagged(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	// 3 days before the requested date -- within the default 7-day window.
	seedRate(t, svc, "INR", "USD", "0.0115", "2026-08-17", "ecb")

	amount := mustMoney(t, 10000000, "INR")
	result, err := svc.ConvertAmount(ctx, app.ConvertAmountQuery{
		Amount:          amount,
		To:              "USD",
		Policy:          app.PolicyTransactionDate,
		TransactionDate: "2026-08-20",
	})
	if err != nil {
		t.Fatalf("ConvertAmount: %v", err)
	}
	if !result.Stale {
		t.Errorf("Stale = false, want true for a within-window substitute")
	}
	if result.RateDate.String() != "2026-08-17" {
		t.Errorf("RateDate = %s, want 2026-08-17 (the substitute rate's own date)", result.RateDate)
	}
}

func TestConvertAmount_MissingRateFailsLoudly(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	amount := mustMoney(t, 10000000, "INR")
	_, err := svc.ConvertAmount(ctx, app.ConvertAmountQuery{
		Amount:          amount,
		To:              "USD",
		Policy:          app.PolicyTransactionDate,
		TransactionDate: "2026-08-14",
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestConvertAmount_RejectsUnknownTargetCurrency(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	amount := mustMoney(t, 100, "USD")
	_, err := svc.ConvertAmount(ctx, app.ConvertAmountQuery{Amount: amount, To: "ZZZ", Policy: app.PolicyCurrent})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestConvertAmount_RejectsUnknownPolicy(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	amount := mustMoney(t, 100, "USD")
	_, err := svc.ConvertAmount(ctx, app.ConvertAmountQuery{Amount: amount, To: "EUR", Policy: "made_up"})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestConvertAmount_RequiresTransactionDateForThatPolicy(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	amount := mustMoney(t, 100, "USD")
	_, err := svc.ConvertAmount(ctx, app.ConvertAmountQuery{Amount: amount, To: "EUR", Policy: app.PolicyTransactionDate})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestConvertAmount_RequiresPinnedDateForThatPolicy(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	amount := mustMoney(t, 100, "USD")
	_, err := svc.ConvertAmount(ctx, app.ConvertAmountQuery{Amount: amount, To: "EUR", Policy: app.PolicyPinned})
	wantErrCode(t, err, errs.InvalidInput)
}

// seedRate stores a rate directly through svc.FxRates -- the same
// repository ConvertAmount reads through -- so these tests exercise
// ConvertAmount's own date-resolution and Lookup-wiring logic rather than
// re-testing fx.SelectRate itself (already covered in internal/domain/fx).
func seedRate(t *testing.T, svc *app.Service, base, quote, value, date, source string) {
	t.Helper()
	rate, err := fx.NewRate(base, quote, decimal.RequireFromString(value))
	if err != nil {
		t.Fatalf("NewRate(%s, %s, %s) = %v, want success", base, quote, value, err)
	}
	parsed, err := time.Parse(time.DateOnly, date)
	if err != nil {
		t.Fatalf("parse date %q: %v", date, err)
	}
	d, err := domain.NewDate(parsed.Year(), parsed.Month(), parsed.Day())
	if err != nil {
		t.Fatalf("NewDate(%q): %v", date, err)
	}
	if err := svc.FxRates.Store(context.Background(), rate, d, source); err != nil {
		t.Fatalf("FxRates.Store: %v", err)
	}
}
