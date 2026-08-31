package money_test

import (
	"testing"

	"github.com/anirudhgray/bodger/internal/domain/money"
)

func TestLookupCurrency(t *testing.T) {
	t.Parallel()

	usd, ok := money.LookupCurrency("USD")
	if !ok {
		t.Fatalf("LookupCurrency(USD) not found")
	}
	if usd.Name != "US Dollar" || usd.Symbol != "$" || usd.MinorUnitExponent != 2 {
		t.Errorf("LookupCurrency(USD) = %+v, want name=US Dollar symbol=$ exponent=2", usd)
	}

	if _, ok := money.LookupCurrency("XXX"); ok {
		t.Errorf("LookupCurrency(XXX) = ok, want not found")
	}
}

func TestCurrenciesSeeded(t *testing.T) {
	t.Parallel()

	cs := money.Currencies()

	// The ~20 commonly used currencies, not all ~180 of ISO 4217.
	if len(cs) < 15 || len(cs) > 30 {
		t.Fatalf("Currencies() returned %d currencies, want roughly 20", len(cs))
	}

	for i := 1; i < len(cs); i++ {
		if cs[i-1].Code >= cs[i].Code {
			t.Fatalf("Currencies() not sorted by code: %q before %q", cs[i-1].Code, cs[i].Code)
		}
	}

	// All three exponent classes actually seen in practice must be present.
	haveExponent := map[int]bool{}
	for _, c := range cs {
		haveExponent[c.MinorUnitExponent] = true
		if c.Code == "" || c.Name == "" || c.Symbol == "" {
			t.Errorf("currency %+v has an empty field", c)
		}
	}
	for _, want := range []int{0, 2, 3} {
		if !haveExponent[want] {
			t.Errorf("no seeded currency has minor-unit exponent %d", want)
		}
	}
}

func TestCurrencyExponentClasses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		code     string
		exponent int
	}{
		{"JPY", 0},
		{"KRW", 0},
		{"USD", 2},
		{"INR", 2},
		{"EUR", 2},
		{"BHD", 3},
		{"KWD", 3},
	}
	for _, tc := range cases {
		c, ok := money.LookupCurrency(tc.code)
		if !ok {
			t.Fatalf("LookupCurrency(%s) not found", tc.code)
		}
		if c.MinorUnitExponent != tc.exponent {
			t.Errorf("LookupCurrency(%s).MinorUnitExponent = %d, want %d", tc.code, c.MinorUnitExponent, tc.exponent)
		}
	}
}
