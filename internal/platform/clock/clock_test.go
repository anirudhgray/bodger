package clock_test

import (
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/platform/clock"
)

// TestFrozen_ResolvesAcrossTimezones is the exact acceptance instant from
// issue #4 and ADR-0005: 2026-07-31T18:45:00Z is 2026-08-01 in Asia/Kolkata
// (UTC+5:30) and still 2026-07-31 in UTC. This is deliberately the instant
// the conformance suite (issue #9) runs at, because it is the instant most
// likely to expose a surface that resolved "today" in the wrong zone.
func TestFrozen_ResolvesAcrossTimezones(t *testing.T) {
	instant, err := time.Parse(time.RFC3339, "2026-07-31T18:45:00Z")
	if err != nil {
		t.Fatalf("parsing fixture instant: %v", err)
	}

	kolkata, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("loading Asia/Kolkata: %v", err)
	}

	frozen := clock.NewFrozen(instant)
	now := frozen.Now()

	if got := now.In(kolkata).Format("2006-01-02"); got != "2026-08-01" {
		t.Errorf("date in Asia/Kolkata = %q, want 2026-08-01", got)
	}
	if got := now.In(time.UTC).Format("2006-01-02"); got != "2026-07-31" {
		t.Errorf("date in UTC = %q, want 2026-07-31", got)
	}
}

func TestFrozen_DoesNotMoveOnItsOwn(t *testing.T) {
	instant := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	frozen := clock.NewFrozen(instant)

	first := frozen.Now()
	time.Sleep(2 * time.Millisecond)
	second := frozen.Now()

	if !first.Equal(second) {
		t.Errorf("Frozen.Now() moved on its own: %v != %v", first, second)
	}
	if !first.Equal(instant) {
		t.Errorf("Frozen.Now() = %v, want %v", first, instant)
	}
}

func TestFrozen_SetAndAdvance(t *testing.T) {
	frozen := clock.NewFrozen(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))

	frozen.Advance(24 * time.Hour)
	want := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	if got := frozen.Now(); !got.Equal(want) {
		t.Errorf("after Advance(24h): got %v, want %v", got, want)
	}

	newInstant := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	frozen.Set(newInstant)
	if got := frozen.Now(); !got.Equal(newInstant) {
		t.Errorf("after Set: got %v, want %v", got, newInstant)
	}
}

// TestReal_ReportsSystemTime is a light sanity check, not an exhaustive
// test of time.Now() — the whole point of clock.Real is that it is a thin,
// untested-by-design pass-through. It exists so app-layer wiring has a
// real Clock to reach for outside tests.
func TestReal_ReportsSystemTime(t *testing.T) {
	real := clock.New()

	before := time.Now().UTC()
	got := real.Now()
	after := time.Now().UTC()

	if got.Before(before) || got.After(after) {
		t.Errorf("Real.Now() = %v, want between %v and %v", got, before, after)
	}
	if got.Location() != time.UTC {
		t.Errorf("Real.Now() location = %v, want UTC", got.Location())
	}
}
