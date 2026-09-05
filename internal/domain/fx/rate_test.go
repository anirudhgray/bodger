package fx_test

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

func mustMoney(t *testing.T, amountMinor int64, currency string) money.Money {
	t.Helper()
	m, err := money.NewMoney(amountMinor, currency)
	if err != nil {
		t.Fatalf("NewMoney(%d, %q) = %v, want success", amountMinor, currency, err)
	}
	return m
}

func TestNewRate(t *testing.T) {
	t.Parallel()

	t.Run("accepts a positive value between two known currencies", func(t *testing.T) {
		t.Parallel()
		r, err := fx.NewRate("INR", "USD", decimal.RequireFromString("0.0115"))
		if err != nil {
			t.Fatalf("NewRate() = %v, want success", err)
		}
		if r.Base() != "INR" || r.Quote() != "USD" {
			t.Errorf("Base()/Quote() = %s/%s, want INR/USD", r.Base(), r.Quote())
		}
		if !r.Value().Equal(decimal.RequireFromString("0.0115")) {
			t.Errorf("Value() = %s, want 0.0115", r.Value())
		}
	})

	t.Run("allows base and quote to be equal", func(t *testing.T) {
		t.Parallel()
		r, err := fx.NewRate("USD", "USD", decimal.NewFromInt(1))
		if err != nil {
			t.Fatalf("NewRate(USD, USD, 1) = %v, want success", err)
		}
		if !r.IsIdentity() {
			t.Errorf("NewRate(USD, USD, 1).IsIdentity() = false, want true")
		}
	})

	t.Run("rejects an empty currency", func(t *testing.T) {
		t.Parallel()
		if _, err := fx.NewRate("", "USD", decimal.NewFromInt(1)); !errors.Is(err, fx.ErrRateEmptyCurrency) {
			t.Fatalf("NewRate(\"\", USD, ...) error = %v, want ErrRateEmptyCurrency", err)
		}
		if _, err := fx.NewRate("USD", "", decimal.NewFromInt(1)); !errors.Is(err, fx.ErrRateEmptyCurrency) {
			t.Fatalf("NewRate(USD, \"\", ...) error = %v, want ErrRateEmptyCurrency", err)
		}
	})

	t.Run("rejects an unknown currency", func(t *testing.T) {
		t.Parallel()
		if _, err := fx.NewRate("ZZZ", "USD", decimal.NewFromInt(1)); !errors.Is(err, fx.ErrRateUnknownCurrency) {
			t.Fatalf("NewRate(ZZZ, USD, ...) error = %v, want ErrRateUnknownCurrency", err)
		}
		if _, err := fx.NewRate("USD", "ZZZ", decimal.NewFromInt(1)); !errors.Is(err, fx.ErrRateUnknownCurrency) {
			t.Fatalf("NewRate(USD, ZZZ, ...) error = %v, want ErrRateUnknownCurrency", err)
		}
	})

	t.Run("rejects a zero or negative value", func(t *testing.T) {
		t.Parallel()
		if _, err := fx.NewRate("USD", "INR", decimal.Zero); !errors.Is(err, fx.ErrRateNotPositive) {
			t.Fatalf("NewRate(zero) error = %v, want ErrRateNotPositive", err)
		}
		if _, err := fx.NewRate("USD", "INR", decimal.NewFromInt(-1)); !errors.Is(err, fx.ErrRateNotPositive) {
			t.Fatalf("NewRate(negative) error = %v, want ErrRateNotPositive", err)
		}
	})

	t.Run("rejects more than 12 fractional digits", func(t *testing.T) {
		t.Parallel()
		ok := decimal.RequireFromString("1.123456789012") // exactly 12
		if _, err := fx.NewRate("USD", "INR", ok); err != nil {
			t.Fatalf("NewRate(12 fractional digits) = %v, want success", err)
		}
		tooMany := decimal.RequireFromString("1.1234567890123") // 13
		if _, err := fx.NewRate("USD", "INR", tooMany); !errors.Is(err, fx.ErrRatePrecisionExceeded) {
			t.Fatalf("NewRate(13 fractional digits) error = %v, want ErrRatePrecisionExceeded", err)
		}
	})
}

func TestIdentityRate(t *testing.T) {
	t.Parallel()

	r, err := fx.IdentityRate("INR")
	if err != nil {
		t.Fatalf("IdentityRate(INR) = %v, want success", err)
	}
	if r.Base() != "INR" || r.Quote() != "INR" {
		t.Errorf("Base()/Quote() = %s/%s, want INR/INR", r.Base(), r.Quote())
	}
	if !r.IsIdentity() {
		t.Errorf("IdentityRate(INR).IsIdentity() = false, want true")
	}
	if !r.Value().Equal(decimal.NewFromInt(1)) {
		t.Errorf("Value() = %s, want 1", r.Value())
	}

	if _, err := fx.IdentityRate("ZZZ"); !errors.Is(err, fx.ErrRateUnknownCurrency) {
		t.Fatalf("IdentityRate(ZZZ) error = %v, want ErrRateUnknownCurrency", err)
	}
}

func TestDeriveImpliedRate(t *testing.T) {
	t.Parallel()

	t.Run("derives the rate from two same-exponent currencies", func(t *testing.T) {
		t.Parallel()
		// ₹20,000 out, $230 in: 230/20000 = 0.0115 USD per INR.
		base := mustMoney(t, -2000000, "INR")
		quote := mustMoney(t, 23000, "USD")
		r, err := fx.DeriveImpliedRate(base, quote)
		if err != nil {
			t.Fatalf("DeriveImpliedRate() = %v, want success", err)
		}
		if r.Base() != "INR" || r.Quote() != "USD" {
			t.Fatalf("Base()/Quote() = %s/%s, want INR/USD", r.Base(), r.Quote())
		}
		want := decimal.RequireFromString("0.0115")
		if !r.Value().Equal(want) {
			t.Errorf("Value() = %s, want %s", r.Value(), want)
		}
	})

	t.Run("ignores sign — magnitudes only", func(t *testing.T) {
		t.Parallel()
		base := mustMoney(t, 2000000, "INR") // positive, unlike a real outflow
		quote := mustMoney(t, -23000, "USD") // negative, unlike a real inflow
		r, err := fx.DeriveImpliedRate(base, quote)
		if err != nil {
			t.Fatalf("DeriveImpliedRate() = %v, want success", err)
		}
		want := decimal.RequireFromString("0.0115")
		if !r.Value().Equal(want) {
			t.Errorf("Value() = %s, want %s", r.Value(), want)
		}
	})

	t.Run("handles differing minor-unit exponents", func(t *testing.T) {
		t.Parallel()
		// ¥11,000 out (JPY, exponent 0) for ₹7,700.00 in (INR, exponent
		// 2): 7700/11000 = 0.7 INR per JPY.
		base := mustMoney(t, -11000, "JPY")
		quote := mustMoney(t, 770000, "INR")
		r, err := fx.DeriveImpliedRate(base, quote)
		if err != nil {
			t.Fatalf("DeriveImpliedRate() = %v, want success", err)
		}
		if r.Base() != "JPY" || r.Quote() != "INR" {
			t.Fatalf("Base()/Quote() = %s/%s, want JPY/INR", r.Base(), r.Quote())
		}
		want := decimal.RequireFromString("0.7")
		if !r.Value().Equal(want) {
			t.Errorf("Value() = %s, want %s", r.Value(), want)
		}
	})

	t.Run("rejects the same currency on both legs", func(t *testing.T) {
		t.Parallel()
		base := mustMoney(t, -1000, "USD")
		quote := mustMoney(t, 1000, "USD")
		if _, err := fx.DeriveImpliedRate(base, quote); !errors.Is(err, fx.ErrRateSameCurrency) {
			t.Fatalf("DeriveImpliedRate(same currency) error = %v, want ErrRateSameCurrency", err)
		}
	})

	t.Run("rejects a zero base amount", func(t *testing.T) {
		t.Parallel()
		base := mustMoney(t, 0, "USD")
		quote := mustMoney(t, 1000, "INR")
		if _, err := fx.DeriveImpliedRate(base, quote); !errors.Is(err, fx.ErrRateZeroBase) {
			t.Fatalf("DeriveImpliedRate(zero base) error = %v, want ErrRateZeroBase", err)
		}
	})
}

func TestRateString(t *testing.T) {
	t.Parallel()
	r, err := fx.NewRate("INR", "USD", decimal.RequireFromString("0.0115"))
	if err != nil {
		t.Fatalf("NewRate() = %v, want success", err)
	}
	if got, want := r.String(), "0.011500000000"; got != want {
		t.Errorf("String() = %q, want %q (fixed at 12 fractional digits)", got, want)
	}
}
