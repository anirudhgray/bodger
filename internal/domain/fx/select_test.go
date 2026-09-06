package fx_test

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
)

func mustDate(t *testing.T, year int, month time.Month, day int) domain.Date {
	t.Helper()
	d, err := domain.NewDate(year, month, day)
	if err != nil {
		t.Fatalf("NewDate(%d, %s, %d) = %v, want success", year, month, day, err)
	}
	return d
}

func mustRate(t *testing.T, value string) fx.Rate {
	t.Helper()
	r, err := fx.NewRate("USD", "INR", decimal.RequireFromString(value))
	if err != nil {
		t.Fatalf("NewRate(%s) = %v, want success", value, err)
	}
	return r
}

func TestSelectRate(t *testing.T) {
	t.Parallel()

	aug10 := mustDate(t, 2026, time.August, 10)
	aug12 := mustDate(t, 2026, time.August, 12)
	aug14 := mustDate(t, 2026, time.August, 14)

	t.Run("an exact match wins even when a closer-but-wrong-date candidate exists", func(t *testing.T) {
		t.Parallel()
		candidates := []fx.RateCandidate{
			{Date: aug12, Rate: mustRate(t, "83.00")},
			{Date: aug14, Rate: mustRate(t, "84.00")},
		}
		got, err := fx.SelectRate(candidates, aug14, fx.DefaultStalenessWindowDays)
		if err != nil {
			t.Fatalf("SelectRate() = %v, want success", err)
		}
		if got.Stale {
			t.Errorf("Stale = true, want false for an exact match")
		}
		if !got.Date.Equal(aug14) || !got.Rate.Value().Equal(decimal.RequireFromString("84.00")) {
			t.Errorf("got %+v, want the exact aug14 candidate", got)
		}
	})

	t.Run("falls back to the nearest earlier rate within the window, flagged stale", func(t *testing.T) {
		t.Parallel()
		candidates := []fx.RateCandidate{
			{Date: aug10, Rate: mustRate(t, "82.00")},
			{Date: aug12, Rate: mustRate(t, "83.00")},
		}
		got, err := fx.SelectRate(candidates, aug14, fx.DefaultStalenessWindowDays)
		if err != nil {
			t.Fatalf("SelectRate() = %v, want success", err)
		}
		if !got.Stale {
			t.Errorf("Stale = false, want true for a within-window fallback")
		}
		if !got.Date.Equal(aug12) || !got.Rate.Value().Equal(decimal.RequireFromString("83.00")) {
			t.Errorf("got %+v, want the nearest earlier candidate (aug12)", got)
		}
	})

	t.Run("never looks forward in time", func(t *testing.T) {
		t.Parallel()
		future := mustDate(t, 2026, time.August, 20)
		candidates := []fx.RateCandidate{
			{Date: aug12, Rate: mustRate(t, "83.00")},
			{Date: future, Rate: mustRate(t, "99.00")},
		}
		got, err := fx.SelectRate(candidates, aug14, fx.DefaultStalenessWindowDays)
		if err != nil {
			t.Fatalf("SelectRate() = %v, want success", err)
		}
		if got.Date.Equal(future) {
			t.Fatalf("selected the future candidate, want the earlier one only")
		}
		if !got.Date.Equal(aug12) {
			t.Errorf("Date = %s, want aug12", got.Date)
		}
	})

	t.Run("fails when nothing is within the window", func(t *testing.T) {
		t.Parallel()
		tooOld := mustDate(t, 2026, time.July, 1)
		candidates := []fx.RateCandidate{
			{Date: tooOld, Rate: mustRate(t, "80.00")},
		}
		if _, err := fx.SelectRate(candidates, aug14, fx.DefaultStalenessWindowDays); !errors.Is(err, fx.ErrNoRateWithinWindow) {
			t.Fatalf("SelectRate() error = %v, want ErrNoRateWithinWindow", err)
		}
	})

	t.Run("fails with no candidates at all", func(t *testing.T) {
		t.Parallel()
		if _, err := fx.SelectRate(nil, aug14, fx.DefaultStalenessWindowDays); !errors.Is(err, fx.ErrNoRateWithinWindow) {
			t.Fatalf("SelectRate(nil) error = %v, want ErrNoRateWithinWindow", err)
		}
	})

	t.Run("respects the exact boundary of the staleness window", func(t *testing.T) {
		t.Parallel()
		sevenDaysBack := mustDate(t, 2026, time.August, 7) // exactly 7 days before aug14
		eightDaysBack := mustDate(t, 2026, time.August, 6) // 8 days before aug14

		withinWindow := []fx.RateCandidate{{Date: sevenDaysBack, Rate: mustRate(t, "81.00")}}
		got, err := fx.SelectRate(withinWindow, aug14, fx.DefaultStalenessWindowDays)
		if err != nil {
			t.Fatalf("SelectRate(exactly 7 days back) = %v, want success", err)
		}
		if !got.Date.Equal(sevenDaysBack) {
			t.Errorf("Date = %s, want %s", got.Date, sevenDaysBack)
		}

		outsideWindow := []fx.RateCandidate{{Date: eightDaysBack, Rate: mustRate(t, "80.50")}}
		if _, err := fx.SelectRate(outsideWindow, aug14, fx.DefaultStalenessWindowDays); !errors.Is(err, fx.ErrNoRateWithinWindow) {
			t.Fatalf("SelectRate(8 days back) error = %v, want ErrNoRateWithinWindow", err)
		}
	})

	t.Run("rejects a negative staleness window", func(t *testing.T) {
		t.Parallel()
		if _, err := fx.SelectRate(nil, aug14, -1); !errors.Is(err, fx.ErrInvalidStalenessWindow) {
			t.Fatalf("SelectRate(window=-1) error = %v, want ErrInvalidStalenessWindow", err)
		}
	})

	t.Run("a window of zero means exact match only", func(t *testing.T) {
		t.Parallel()
		candidates := []fx.RateCandidate{{Date: aug12, Rate: mustRate(t, "83.00")}}
		if _, err := fx.SelectRate(candidates, aug14, 0); !errors.Is(err, fx.ErrNoRateWithinWindow) {
			t.Fatalf("SelectRate(window=0, no exact match) error = %v, want ErrNoRateWithinWindow", err)
		}

		exact := []fx.RateCandidate{{Date: aug14, Rate: mustRate(t, "84.00")}}
		got, err := fx.SelectRate(exact, aug14, 0)
		if err != nil {
			t.Fatalf("SelectRate(window=0, exact match) = %v, want success", err)
		}
		if got.Stale {
			t.Errorf("Stale = true, want false for an exact match")
		}
	})
}
