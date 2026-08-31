// Package clock provides the injected time source for bodger.
//
// ADR-0005 ("Time is injected, always") requires that nothing outside this
// package call time.Now(): the application layer holds one Clock, wires the
// real implementation in production, and wires a frozen implementation in
// tests, so that "what does today mean" is a deterministic, testable
// question rather than whatever instant happened to be current when a test
// ran.
//
// This file is the only file in the repository permitted to call
// time.Now(). See docs/decisions/0005-shared-application-layer.md.
package clock

import "time"

// Clock is a port: something that can report the current instant. The
// application layer depends on this interface, never on time.Now()
// directly, so a frozen implementation can stand in for tests.
type Clock interface {
	// Now returns the current instant in UTC. Callers resolve it into the
	// user's timezone themselves (see internal/platform/config for where
	// that timezone comes from); the clock has no opinion on timezone.
	Now() time.Time
}

// Real is a Clock backed by the system clock. It is the only type in the
// repository that calls time.Now().
type Real struct{}

// Now returns time.Now(), normalised to UTC so every caller starts from the
// same reference frame regardless of the host's local timezone (the process
// runs under TZ=UTC anyway — see the Makefile and docs/contributing.md).
func (Real) Now() time.Time {
	return time.Now().UTC()
}

// New constructs a Clock backed by the system clock.
func New() Clock {
	return Real{}
}
