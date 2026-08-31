package money_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/anirudhgray/bodger/internal/domain/money"
)

func TestFormatAcrossExponentClasses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		amountMinor int64
		currency    string
		want        string
	}{
		// The issue's own acceptance test: ¥1500 round-trips as ¥1500,
		// not ¥15 — the exponent must come from currency data (0 for
		// JPY), not an assumed 2.
		{"yen: exponent 0", 1500, "JPY", "¥1500"},
		{"yen: small amount", 3, "JPY", "¥3"},
		{"won: exponent 0", 25000, "KRW", "₩25000"},

		{"dollars: exponent 2", 150000, "USD", "$1500.00"},
		{"rupees: exponent 2", 150000, "INR", "₹1500.00"},
		{"euros: exponent 2, small amount", 5, "EUR", "€0.05"},

		{"dinars: exponent 3", 1500000, "BHD", "BD1500.000"},
		{"dinars: exponent 3, small amount", 1, "KWD", "KD0.001"},

		{"negative: sign before symbol", -150000, "USD", "-$1500.00"},
		{"zero", 0, "USD", "$0.00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := mustMoney(t, tc.amountMinor, tc.currency)
			got, err := money.Format(m)
			if err != nil {
				t.Fatalf("Format(%s) = %v, want success", m, err)
			}
			if got != tc.want {
				t.Errorf("Format(%d minor, %s) = %q, want %q", tc.amountMinor, tc.currency, got, tc.want)
			}
		})
	}
}

func TestParseAcrossExponentClasses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		input, currency string
		wantMinor       int64
	}{
		{"yen: whole number, no decimal point", "1500", "JPY", 1500},
		{"yen: negative", "-1500", "JPY", -1500},

		{"dollars: exact cents", "1500.00", "USD", 150000},
		{"dollars: whole number, no decimal point", "1500", "USD", 150000},
		{"dollars: one fractional digit is padded", "12.5", "USD", 1250},
		{"dollars: negative", "-12.50", "USD", -1250},

		{"dinars: exact fils", "1500.000", "BHD", 1500000},
		{"dinars: whole number, no decimal point", "1500", "BHD", 1500000},
		{"dinars: fewer fractional digits are padded", "1.5", "KWD", 1500},

		{"zero", "0", "USD", 0},
		{"zero with decimals", "0.00", "USD", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, err := money.Parse(tc.input, tc.currency)
			if err != nil {
				t.Fatalf("Parse(%q, %s) = %v, want success", tc.input, tc.currency, err)
			}
			if m.AmountMinor() != tc.wantMinor {
				t.Errorf("Parse(%q, %s) = %d minor, want %d", tc.input, tc.currency, m.AmountMinor(), tc.wantMinor)
			}
		})
	}
}

func TestParseRejectsFloatPrecisionBeyondCurrency(t *testing.T) {
	t.Parallel()

	// "Float input" here means precision the currency's minor unit can't
	// represent: any fractional digits at all for a zero-exponent
	// currency (JPY has no sub-yen unit), or more fractional digits than
	// a currency's exponent allows. Accepting either would mean silently
	// rounding, which this package never does.
	cases := []struct {
		name            string
		input, currency string
	}{
		{"yen has no fractional unit at all", "1500.5", "JPY"},
		{"yen rejects even .00", "1500.00", "JPY"},
		{"dollars allow at most 2 decimal digits", "12.505", "USD"},
		{"dinars allow at most 3 decimal digits", "1.5000", "BHD"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := money.Parse(tc.input, tc.currency)
			if !errors.Is(err, money.ErrPrecisionExceedsCurrency) {
				t.Fatalf("Parse(%q, %s) error = %v, want ErrPrecisionExceedsCurrency", tc.input, tc.currency, err)
			}
		})
	}
}

func TestParseRejectsGroupedSeparatorAmbiguity(t *testing.T) {
	t.Parallel()

	// A "," could mean a thousands grouping separator (US/UK style,
	// "1,500.00") or a decimal point (much of Europe, "1.500,00" reversed
	// with "." grouping and "," decimal). Rather than guess the locale,
	// any comma is rejected outright with a named error.
	cases := []string{
		"1,500.00",
		"1,500",
		"12,50",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := money.Parse(input, "USD")
			if !errors.Is(err, money.ErrAmbiguousSeparator) {
				t.Fatalf("Parse(%q) error = %v, want ErrAmbiguousSeparator", input, err)
			}
		})
	}
}

func TestParseRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",
		"-",
		".",
		"12.",
		".50",
		"1e10",
		"abc",
		"$12.50",
		"12.50.00",
		"1 500",
		"++12",
		"12-50",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := money.Parse(input, "USD")
			if !errors.Is(err, money.ErrInvalidAmountFormat) {
				t.Fatalf("Parse(%q) error = %v, want ErrInvalidAmountFormat", input, err)
			}
		})
	}
}

func TestParseRejectsUnknownCurrency(t *testing.T) {
	t.Parallel()

	_, err := money.Parse("100.00", "ZZZ")
	if !errors.Is(err, money.ErrUnknownCurrency) {
		t.Fatalf("Parse(_, ZZZ) error = %v, want ErrUnknownCurrency", err)
	}
}

func TestFormatParseRoundTrip(t *testing.T) {
	t.Parallel()

	// Format() prefixes a symbol; Parse() takes a plain number. Stripping
	// the symbol back off and re-parsing must recover the exact original
	// value for every exponent class - this is the concrete form of the
	// issue's "¥1500 round-trips as ¥1500, not ¥15" requirement.
	cases := []struct {
		amountMinor int64
		currency    string
	}{
		{1500, "JPY"},
		{150000, "USD"},
		{1500000, "BHD"},
		{-150000, "USD"},
		{0, "USD"},
	}

	for _, tc := range cases {
		m := mustMoney(t, tc.amountMinor, tc.currency)
		formatted, err := money.Format(m)
		if err != nil {
			t.Fatalf("Format(%s) = %v, want success", m, err)
		}
		c, _ := money.LookupCurrency(tc.currency)
		negative := strings.HasPrefix(formatted, "-")
		unsigned := strings.TrimPrefix(formatted, "-")
		numeric := strings.TrimPrefix(unsigned, c.Symbol)
		if negative {
			numeric = "-" + numeric
		}

		reparsed, err := money.Parse(numeric, tc.currency)
		if err != nil {
			t.Fatalf("Parse(%q, %s) = %v, want success", numeric, tc.currency, err)
		}
		if !reparsed.Equal(m) {
			t.Errorf("round trip via Format/Parse: got %s, want %s (formatted was %q)", reparsed, m, formatted)
		}
	}
}
