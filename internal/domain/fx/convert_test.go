package fx_test

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain/fx"
)

func TestConvert(t *testing.T) {
	t.Parallel()

	t.Run("matches ADR-0004's worked example", func(t *testing.T) {
		t.Parallel()
		amount := mustMoney(t, 10000000, "INR") // 100000.00 INR
		rate, err := fx.NewRate("INR", "USD", decimal.RequireFromString("0.0115"))
		if err != nil {
			t.Fatalf("NewRate() = %v, want success", err)
		}
		got, err := fx.Convert(amount, "USD", rate)
		if err != nil {
			t.Fatalf("Convert() = %v, want success", err)
		}
		if got.Currency() != "USD" {
			t.Fatalf("Currency() = %s, want USD", got.Currency())
		}
		if got.AmountMinor() != 115000 { // 1150.00 USD
			t.Errorf("AmountMinor() = %d, want 115000 (1150.00 USD)", got.AmountMinor())
		}
	})

	t.Run("handles differing minor-unit exponents", func(t *testing.T) {
		t.Parallel()
		// 11000 JPY (exponent 0) at 0.7 INR per JPY -> 7700.00 INR.
		amount := mustMoney(t, 11000, "JPY")
		rate, err := fx.NewRate("JPY", "INR", decimal.RequireFromString("0.7"))
		if err != nil {
			t.Fatalf("NewRate() = %v, want success", err)
		}
		got, err := fx.Convert(amount, "INR", rate)
		if err != nil {
			t.Fatalf("Convert() = %v, want success", err)
		}
		if got.AmountMinor() != 770000 {
			t.Errorf("AmountMinor() = %d, want 770000 (7700.00 INR)", got.AmountMinor())
		}
	})

	t.Run("preserves sign", func(t *testing.T) {
		t.Parallel()
		amount := mustMoney(t, -10000000, "INR")
		rate, err := fx.NewRate("INR", "USD", decimal.RequireFromString("0.0115"))
		if err != nil {
			t.Fatalf("NewRate() = %v, want success", err)
		}
		got, err := fx.Convert(amount, "USD", rate)
		if err != nil {
			t.Fatalf("Convert() = %v, want success", err)
		}
		if got.AmountMinor() != -115000 {
			t.Errorf("AmountMinor() = %d, want -115000", got.AmountMinor())
		}
	})

	t.Run("rounds half away from zero at the target currency's minor-unit scale", func(t *testing.T) {
		t.Parallel()
		// 100.00 USD * 83.456789 = 8345.6789 INR -> rounds to 8345.68.
		amount := mustMoney(t, 10000, "USD")
		rate, err := fx.NewRate("USD", "INR", decimal.RequireFromString("83.456789"))
		if err != nil {
			t.Fatalf("NewRate() = %v, want success", err)
		}
		got, err := fx.Convert(amount, "INR", rate)
		if err != nil {
			t.Fatalf("Convert() = %v, want success", err)
		}
		if got.AmountMinor() != 834568 {
			t.Errorf("AmountMinor() = %d, want 834568 (8345.68 INR)", got.AmountMinor())
		}
	})

	t.Run("rounds a tie away from zero", func(t *testing.T) {
		t.Parallel()
		// 1.00 USD * 0.125 = 0.125 -> shifted to 12.5 minor units -> rounds
		// to 13, not banker's-rounding's 12.
		amount := mustMoney(t, 100, "USD")
		rate, err := fx.NewRate("USD", "EUR", decimal.RequireFromString("0.125"))
		if err != nil {
			t.Fatalf("NewRate() = %v, want success", err)
		}
		got, err := fx.Convert(amount, "EUR", rate)
		if err != nil {
			t.Fatalf("Convert() = %v, want success", err)
		}
		if got.AmountMinor() != 13 {
			t.Errorf("AmountMinor() = %d, want 13 (rounded away from zero)", got.AmountMinor())
		}
	})

	t.Run("supports identity conversion", func(t *testing.T) {
		t.Parallel()
		amount := mustMoney(t, 5000, "USD")
		rate, err := fx.IdentityRate("USD")
		if err != nil {
			t.Fatalf("IdentityRate() = %v, want success", err)
		}
		got, err := fx.Convert(amount, "USD", rate)
		if err != nil {
			t.Fatalf("Convert() = %v, want success", err)
		}
		if !got.Equal(amount) {
			t.Errorf("Convert(identity) = %s, want unchanged %s", got, amount)
		}
	})

	t.Run("rejects a rate whose base doesn't match the amount's currency", func(t *testing.T) {
		t.Parallel()
		amount := mustMoney(t, 1000, "USD")
		rate, err := fx.NewRate("INR", "USD", decimal.RequireFromString("0.0115"))
		if err != nil {
			t.Fatalf("NewRate() = %v, want success", err)
		}
		if _, err := fx.Convert(amount, "USD", rate); !errors.Is(err, fx.ErrRateCurrencyMismatch) {
			t.Fatalf("Convert() error = %v, want ErrRateCurrencyMismatch", err)
		}
	})

	t.Run("rejects a rate whose quote doesn't match the target currency", func(t *testing.T) {
		t.Parallel()
		amount := mustMoney(t, 1000, "INR")
		rate, err := fx.NewRate("INR", "USD", decimal.RequireFromString("0.0115"))
		if err != nil {
			t.Fatalf("NewRate() = %v, want success", err)
		}
		if _, err := fx.Convert(amount, "EUR", rate); !errors.Is(err, fx.ErrRateCurrencyMismatch) {
			t.Fatalf("Convert() error = %v, want ErrRateCurrencyMismatch", err)
		}
	})
}
