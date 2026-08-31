package money_test

import (
	"encoding/json"
	"errors"
	"testing"

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

func TestNewMoney(t *testing.T) {
	t.Parallel()

	t.Run("accepts a known currency", func(t *testing.T) {
		t.Parallel()
		m, err := money.NewMoney(1500, "JPY")
		if err != nil {
			t.Fatalf("NewMoney(1500, JPY) = %v, want success", err)
		}
		if m.AmountMinor() != 1500 || m.Currency() != "JPY" {
			t.Errorf("NewMoney(1500, JPY) = %+v", m)
		}
	})

	t.Run("rejects an unknown currency", func(t *testing.T) {
		t.Parallel()
		_, err := money.NewMoney(100, "ZZZ")
		if !errors.Is(err, money.ErrUnknownCurrency) {
			t.Fatalf("NewMoney(100, ZZZ) error = %v, want ErrUnknownCurrency", err)
		}
	})

	t.Run("rejects an empty currency", func(t *testing.T) {
		t.Parallel()
		_, err := money.NewMoney(100, "")
		if !errors.Is(err, money.ErrUnknownCurrency) {
			t.Fatalf("NewMoney(100, \"\") error = %v, want ErrUnknownCurrency", err)
		}
	})
}

func TestAdd(t *testing.T) {
	t.Parallel()

	t.Run("same currency sums the minor units", func(t *testing.T) {
		t.Parallel()
		sum, err := mustMoney(t, 1000, "USD").Add(mustMoney(t, 250, "USD"))
		if err != nil {
			t.Fatalf("Add() = %v, want success", err)
		}
		if !sum.Equal(mustMoney(t, 1250, "USD")) {
			t.Errorf("Add() = %s, want 12.50 USD", sum)
		}
	})

	t.Run("mismatched currency is an error, not a number", func(t *testing.T) {
		t.Parallel()
		// The issue's own acceptance test: adding INR to USD returns an
		// error rather than a number.
		_, err := mustMoney(t, 100000, "INR").Add(mustMoney(t, 100, "USD"))
		if !errors.Is(err, money.ErrCurrencyMismatch) {
			t.Fatalf("Add(INR, USD) error = %v, want ErrCurrencyMismatch", err)
		}
	})
}

func TestSubtract(t *testing.T) {
	t.Parallel()

	t.Run("same currency", func(t *testing.T) {
		t.Parallel()
		diff, err := mustMoney(t, 1000, "USD").Subtract(mustMoney(t, 400, "USD"))
		if err != nil {
			t.Fatalf("Subtract() = %v, want success", err)
		}
		if !diff.Equal(mustMoney(t, 600, "USD")) {
			t.Errorf("Subtract() = %s, want 6.00 USD", diff)
		}
	})

	t.Run("mismatched currency", func(t *testing.T) {
		t.Parallel()
		_, err := mustMoney(t, 1000, "EUR").Subtract(mustMoney(t, 100, "GBP"))
		if !errors.Is(err, money.ErrCurrencyMismatch) {
			t.Fatalf("Subtract(EUR, GBP) error = %v, want ErrCurrencyMismatch", err)
		}
	})
}

func TestNegate(t *testing.T) {
	t.Parallel()

	m := mustMoney(t, 500, "USD")
	neg := m.Negate()
	if neg.AmountMinor() != -500 || neg.Currency() != "USD" {
		t.Errorf("Negate() = %+v, want -500 USD", neg)
	}
	if neg.Negate().AmountMinor() != 500 {
		t.Errorf("Negate().Negate() did not round-trip")
	}
}

func TestAbs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input int64
		want  int64
	}{
		{"positive stays positive", 500, 500},
		{"negative becomes positive", -500, 500},
		{"zero stays zero", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mustMoney(t, tc.input, "USD").Abs()
			if got.AmountMinor() != tc.want {
				t.Errorf("Abs(%d) = %d, want %d", tc.input, got.AmountMinor(), tc.want)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	t.Parallel()

	t.Run("orders same-currency amounts", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			a, b int64
			want int
		}{
			{100, 200, -1},
			{200, 100, 1},
			{100, 100, 0},
		}
		for _, tc := range cases {
			got, err := mustMoney(t, tc.a, "USD").Compare(mustMoney(t, tc.b, "USD"))
			if err != nil {
				t.Fatalf("Compare(%d, %d) = %v, want success", tc.a, tc.b, err)
			}
			if got != tc.want {
				t.Errorf("Compare(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		}
	})

	t.Run("mismatched currency has no order", func(t *testing.T) {
		t.Parallel()
		_, err := mustMoney(t, 100, "USD").Compare(mustMoney(t, 100, "EUR"))
		if !errors.Is(err, money.ErrCurrencyMismatch) {
			t.Fatalf("Compare(USD, EUR) error = %v, want ErrCurrencyMismatch", err)
		}
	})
}

func TestEqual(t *testing.T) {
	t.Parallel()

	if !mustMoney(t, 100, "USD").Equal(mustMoney(t, 100, "USD")) {
		t.Errorf("Equal: same amount and currency should be equal")
	}
	if mustMoney(t, 100, "USD").Equal(mustMoney(t, 200, "USD")) {
		t.Errorf("Equal: different amounts should not be equal")
	}
	if mustMoney(t, 100, "USD").Equal(mustMoney(t, 100, "EUR")) {
		t.Errorf("Equal: different currencies should not be equal, never coerced")
	}
}

func TestSum(t *testing.T) {
	t.Parallel()

	t.Run("sums several values", func(t *testing.T) {
		t.Parallel()
		total, err := money.Sum(
			mustMoney(t, 100, "USD"),
			mustMoney(t, 200, "USD"),
			mustMoney(t, 300, "USD"),
		)
		if err != nil {
			t.Fatalf("Sum() = %v, want success", err)
		}
		if !total.Equal(mustMoney(t, 600, "USD")) {
			t.Errorf("Sum() = %s, want 6.00 USD", total)
		}
	})

	t.Run("empty is an error", func(t *testing.T) {
		t.Parallel()
		_, err := money.Sum()
		if !errors.Is(err, money.ErrEmptySum) {
			t.Fatalf("Sum() error = %v, want ErrEmptySum", err)
		}
	})

	t.Run("mismatched currency is an error", func(t *testing.T) {
		t.Parallel()
		_, err := money.Sum(mustMoney(t, 100, "USD"), mustMoney(t, 100, "EUR"))
		if !errors.Is(err, money.ErrCurrencyMismatch) {
			t.Fatalf("Sum(USD, EUR) error = %v, want ErrCurrencyMismatch", err)
		}
	})
}

func TestMoneyJSONRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		amountMinor int64
		currency    string
		wantJSON    string
	}{
		{"yen has no decimal places", 1500, "JPY", `{"amount":"1500","currency":"JPY","exponent":0}`},
		{"dollars have two decimal places", 150000, "USD", `{"amount":"1500.00","currency":"USD","exponent":2}`},
		{"dinars have three decimal places", 1500000, "BHD", `{"amount":"1500.000","currency":"BHD","exponent":3}`},
		{"negative amount", -150000, "USD", `{"amount":"-1500.00","currency":"USD","exponent":2}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := mustMoney(t, tc.amountMinor, tc.currency)

			got, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("json.Marshal() = %v, want success", err)
			}
			if string(got) != tc.wantJSON {
				t.Errorf("json.Marshal() = %s, want %s", got, tc.wantJSON)
			}

			var back money.Money
			if err := json.Unmarshal(got, &back); err != nil {
				t.Fatalf("json.Unmarshal() = %v, want success", err)
			}
			if !back.Equal(m) {
				t.Errorf("round trip = %s, want %s", back, m)
			}
		})
	}
}

func TestMoneyUnmarshalJSONRejectsUnknownCurrency(t *testing.T) {
	t.Parallel()

	var m money.Money
	err := json.Unmarshal([]byte(`{"amount":"100.00","currency":"ZZZ","exponent":2}`), &m)
	if !errors.Is(err, money.ErrUnknownCurrency) {
		t.Fatalf("Unmarshal(unknown currency) error = %v, want ErrUnknownCurrency", err)
	}
}

func TestMoneyString(t *testing.T) {
	t.Parallel()

	if got, want := mustMoney(t, 150000, "USD").String(), "1500.00 USD"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
