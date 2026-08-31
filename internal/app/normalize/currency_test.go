package normalize_test

import (
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestCurrency_Precedence(t *testing.T) {
	tests := []struct {
		name                           string
		entry, account, user, instance string
		want                           string
	}{
		{
			name:  "entry wins over every other level",
			entry: "EUR", account: "USD", user: "GBP", instance: "INR",
			want: "EUR",
		},
		{
			name:  "entry empty: account wins over user and instance",
			entry: "", account: "USD", user: "GBP", instance: "INR",
			want: "USD",
		},
		{
			name:  "entry and account empty: user wins over instance",
			entry: "", account: "", user: "GBP", instance: "INR",
			want: "GBP",
		},
		{
			name:  "only instance set: the fall-through case",
			entry: "", account: "", user: "", instance: "INR",
			want: "INR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalize.Currency(tt.entry, tt.account, tt.user, tt.instance)
			if err != nil {
				t.Fatalf("Currency(...) unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Currency(%q, %q, %q, %q) = %q, want %q", tt.entry, tt.account, tt.user, tt.instance, got, tt.want)
			}
		})
	}
}

func TestCurrency_AllLevelsEmpty(t *testing.T) {
	_, err := normalize.Currency("", "", "", "")
	if err == nil {
		t.Fatal("Currency(\"\",\"\",\"\",\"\") returned no error, want a named error")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not a *errs.Error: %v", err)
	}
	if appErr.Code != errs.InvalidInput {
		t.Errorf("error code = %s, want %s", appErr.Code, errs.InvalidInput)
	}
}

func TestCurrency_UnknownCode(t *testing.T) {
	_, err := normalize.Currency("NOTREAL", "", "", "USD")
	if err == nil {
		t.Fatal("Currency with an unknown entry-level code returned no error")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not a *errs.Error: %v", err)
	}
	if appErr.FieldPath != "currency" {
		t.Errorf("FieldPath = %q, want %q", appErr.FieldPath, "currency")
	}
}
