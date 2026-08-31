// Package idgen generates identifiers for domain entities behind an
// interface, so application-layer tests can substitute deterministic IDs
// instead of asserting against real UUIDs.
package idgen

import "github.com/google/uuid"

// Generator produces new, unique identifier strings.
type Generator interface {
	NewID() string
}

// UUID is a Generator backed by random (version 4) UUIDs.
type UUID struct{}

// NewID returns a new random UUID, formatted per RFC 4122
// ("xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx").
func (UUID) NewID() string {
	return uuid.NewString()
}

// New constructs a Generator backed by random UUIDs.
func New() Generator {
	return UUID{}
}
