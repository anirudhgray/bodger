package clock

import (
	"sync"
	"time"
)

// Frozen is a Clock for tests: it reports a fixed instant until explicitly
// moved. It never calls time.Now(), so it carries no dependency on wall-clock
// time and produces identical results wherever and whenever the test runs.
//
// Frozen is safe for concurrent use — app-layer tests that exercise
// concurrent requests can share one Frozen clock.
type Frozen struct {
	mu  sync.RWMutex
	now time.Time
}

// NewFrozen constructs a Frozen clock reporting at, and only at, until
// Set or Advance is called.
func NewFrozen(at time.Time) *Frozen {
	return &Frozen{now: at.UTC()}
}

// Now returns the frozen instant.
func (f *Frozen) Now() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.now
}

// Set moves the frozen clock to a new instant.
func (f *Frozen) Set(at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = at.UTC()
}

// Advance moves the frozen clock forward by d. A negative d moves it
// backward, which is occasionally useful for constructing "just before a
// boundary" fixtures.
func (f *Frozen) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
