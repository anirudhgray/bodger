package normalize_test

import (
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestText_TrimsAndCollapsesWhitespace(t *testing.T) {
	got, err := normalize.Text("  Groceries   at   the   market  \t\n", 0)
	if err != nil {
		t.Fatalf("Text(...) unexpected error: %v", err)
	}
	want := "Groceries at the market"
	if got != want {
		t.Errorf("Text(...) = %q, want %q", got, want)
	}
}

func TestText_NFCNormalisation(t *testing.T) {
	// precomposed is "café" (one code point for the accented e);
	// decomposed is "café" spelled as "e" followed by a combining acute
	// accent (U+0301) — two different byte sequences a person can produce
	// from the same keystrokes depending on their input method. NFC must
	// fold them to the same normalised string.
	precomposed := "café"
	decomposed := "café"

	if precomposed == decomposed {
		t.Fatal("test setup bug: precomposed and decomposed should differ before normalisation")
	}

	got1, err := normalize.Text(precomposed, 0)
	if err != nil {
		t.Fatalf("Text(precomposed) unexpected error: %v", err)
	}
	got2, err := normalize.Text(decomposed, 0)
	if err != nil {
		t.Fatalf("Text(decomposed) unexpected error: %v", err)
	}
	if got1 != got2 {
		t.Errorf("Text(precomposed) = %q, Text(decomposed) = %q, want them equal after NFC normalisation", got1, got2)
	}
	if got1 != precomposed {
		t.Errorf("Text(precomposed) = %q, want %q (already-NFC input round-trips unchanged)", got1, precomposed)
	}
}

func TestText_LengthCap(t *testing.T) {
	_, err := normalize.Text("this is way too long", 5)
	if err == nil {
		t.Fatal("Text with input over maxLen returned no error")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not a *errs.Error: %v", err)
	}
	if appErr.Code != errs.InvalidInput {
		t.Errorf("error code = %s, want %s", appErr.Code, errs.InvalidInput)
	}
}

func TestText_ZeroMaxLenMeansNoCap(t *testing.T) {
	long := "this description happens to be longer than five characters"
	got, err := normalize.Text(long, 0)
	if err != nil {
		t.Fatalf("Text(..., 0) unexpected error: %v", err)
	}
	if got != long {
		t.Errorf("Text(..., 0) = %q, want %q unchanged", got, long)
	}
}
