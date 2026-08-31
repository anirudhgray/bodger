package normalize_test

import (
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestAmount_EquivalentForms(t *testing.T) {
	// "1,800.50" (US-style grouping), "₹1800.50" (currency symbol), and
	// "1800.50" (plain) must all produce the identical minor-unit integer
	// — issue #5's "done when" list, first bullet.
	inputs := []string{"1,800.50", "₹1800.50", "1800.50"}

	var want int64 = -1
	for _, raw := range inputs {
		got, err := normalize.Amount(raw, "INR")
		if err != nil {
			t.Fatalf("Amount(%q, INR) unexpected error: %v", raw, err)
		}
		if want == -1 {
			want = got
			continue
		}
		if got != want {
			t.Errorf("Amount(%q, INR) = %d, want %d (to match the other equivalent forms)", raw, got, want)
		}
	}
	if want != 180050 {
		t.Errorf("Amount(...) = %d, want 180050", want)
	}
}

func TestAmount_RejectsMalformedAndOverPreciseInput(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		currency string
	}{
		{
			name:     "two decimal points",
			raw:      "1800.5.0",
			currency: "USD",
		},
		{
			name: "more fractional digits than the currency allows " +
				"(what a naive fmt.Sprintf(\"%f\", 1800.50) would produce)",
			raw:      "1800.500000",
			currency: "USD",
		},
		{
			name:     "zero-exponent currency given fractional digits",
			raw:      "1500.00",
			currency: "JPY",
		},
		{
			name:     "letters that aren't a recognised symbol or code",
			raw:      "abc",
			currency: "USD",
		},
		{
			name:     "empty string",
			raw:      "",
			currency: "USD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalize.Amount(tt.raw, tt.currency)
			if err == nil {
				t.Fatalf("Amount(%q, %s) returned no error, want a named error", tt.raw, tt.currency)
			}
			var appErr *errs.Error
			if !errors.As(err, &appErr) {
				t.Fatalf("Amount(%q, %s) error is not a *errs.Error: %v", tt.raw, tt.currency, err)
			}
			if appErr.Code != errs.InvalidInput {
				t.Errorf("Amount(%q, %s) error code = %s, want %s", tt.raw, tt.currency, appErr.Code, errs.InvalidInput)
			}
		})
	}
}

func TestAmount_UnknownCurrency(t *testing.T) {
	_, err := normalize.Amount("100.00", "XXX")
	if err == nil {
		t.Fatal("Amount with an unknown currency returned no error")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not a *errs.Error: %v", err)
	}
	if appErr.Code != errs.InvalidInput {
		t.Errorf("error code = %s, want %s", appErr.Code, errs.InvalidInput)
	}
	if appErr.FieldPath != "currency" {
		t.Errorf("FieldPath = %q, want %q", appErr.FieldPath, "currency")
	}
}

func TestAmount_NegativeAndZeroExponent(t *testing.T) {
	got, err := normalize.Amount("-500", "JPY")
	if err != nil {
		t.Fatalf("Amount(-500, JPY) unexpected error: %v", err)
	}
	if got != -500 {
		t.Errorf("Amount(-500, JPY) = %d, want -500", got)
	}
}
