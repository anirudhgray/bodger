package normalize_test

import (
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func mustDate(t *testing.T, year int, month time.Month, day int) domain.Date {
	t.Helper()
	d, err := domain.NewDate(year, month, day)
	if err != nil {
		t.Fatalf("domain.NewDate(%d, %d, %d) unexpected error: %v", year, month, day, err)
	}
	return d
}

// TestDateOf_MonthBoundaryUnderHostileClock is issue #5's "done when"
// centrepiece: a frozen clock at 2026-07-31T18:45:00Z is 2026-08-01 in
// Asia/Kolkata (UTC+5:30) — the whole reason DateOf resolves "today" in
// the user's timezone via the injected clock rather than the host's.
func TestDateOf_MonthBoundaryUnderHostileClock(t *testing.T) {
	frozenAt := time.Date(2026, time.July, 31, 18, 45, 0, 0, time.UTC)
	clk := clock.NewFrozen(frozenAt)

	got, err := normalize.DateOf("today", clk, "Asia/Kolkata")
	if err != nil {
		t.Fatalf("DateOf(\"today\", ...) unexpected error: %v", err)
	}
	want := mustDate(t, 2026, time.August, 1)
	if !got.Equal(want) {
		t.Errorf("DateOf(\"today\") under frozen clock %s in Asia/Kolkata = %s, want %s", frozenAt, got, want)
	}
}

func TestDateOf_EmptyStringMeansToday(t *testing.T) {
	frozenAt := time.Date(2026, time.July, 31, 18, 45, 0, 0, time.UTC)
	clk := clock.NewFrozen(frozenAt)

	got, err := normalize.DateOf("", clk, "Asia/Kolkata")
	if err != nil {
		t.Fatalf("DateOf(\"\", ...) unexpected error: %v", err)
	}
	want := mustDate(t, 2026, time.August, 1)
	if !got.Equal(want) {
		t.Errorf("DateOf(\"\") = %s, want %s", got, want)
	}
}

func TestDateOf_Yesterday(t *testing.T) {
	// Same hostile instant: "yesterday" in Asia/Kolkata from 2026-08-01
	// local is 2026-07-31, not the UTC-side 2026-07-30.
	frozenAt := time.Date(2026, time.July, 31, 18, 45, 0, 0, time.UTC)
	clk := clock.NewFrozen(frozenAt)

	got, err := normalize.DateOf("Yesterday", clk, "Asia/Kolkata")
	if err != nil {
		t.Fatalf("DateOf(\"Yesterday\", ...) unexpected error: %v", err)
	}
	want := mustDate(t, 2026, time.July, 31)
	if !got.Equal(want) {
		t.Errorf("DateOf(\"Yesterday\") = %s, want %s", got, want)
	}
}

func TestDateOf_ISOAndLocaleFormats(t *testing.T) {
	clk := clock.NewFrozen(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))

	tests := []struct {
		name string
		raw  string
		want domain.Date
	}{
		{name: "ISO 8601", raw: "2026-08-14", want: mustDate(t, 2026, time.August, 14)},
		{name: "day/month/year", raw: "14/08/2026", want: mustDate(t, 2026, time.August, 14)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalize.DateOf(tt.raw, clk, "UTC")
			if err != nil {
				t.Fatalf("DateOf(%q, ...) unexpected error: %v", tt.raw, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("DateOf(%q) = %s, want %s", tt.raw, got, tt.want)
			}
		})
	}
}

func TestDateOf_RejectsUnrecognisedInput(t *testing.T) {
	clk := clock.NewFrozen(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))

	tests := []string{"next tuesday", "2026/08/14", "not a date", "2026-13-01"}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			_, err := normalize.DateOf(raw, clk, "UTC")
			if err == nil {
				t.Fatalf("DateOf(%q, ...) returned no error, want a named error", raw)
			}
			var appErr *errs.Error
			if !errors.As(err, &appErr) {
				t.Fatalf("error is not a *errs.Error: %v", err)
			}
			if appErr.Code != errs.InvalidInput {
				t.Errorf("error code = %s, want %s", appErr.Code, errs.InvalidInput)
			}
		})
	}
}

func TestDateOf_UnknownTimezone(t *testing.T) {
	clk := clock.NewFrozen(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	_, err := normalize.DateOf("today", clk, "Nowhere/Imaginary")
	if err == nil {
		t.Fatal("DateOf with an unknown timezone returned no error")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not a *errs.Error: %v", err)
	}
	if appErr.FieldPath != "timezone" {
		t.Errorf("FieldPath = %q, want %q", appErr.FieldPath, "timezone")
	}
}
