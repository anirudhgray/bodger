package idgen

import (
	"fmt"
	"sync"
)

// Sequence is a deterministic Generator for tests. Each call to NewID
// returns "<prefix>-000001", "<prefix>-000002", and so on in call order, so
// a test can assert against exact IDs instead of "some UUID". Safe for
// concurrent use.
type Sequence struct {
	mu     sync.Mutex
	prefix string
	n      uint64
}

// NewSequence constructs a Sequence generator. prefix may be empty.
func NewSequence(prefix string) *Sequence {
	return &Sequence{prefix: prefix}
}

// NewID returns the next ID in the sequence.
func (s *Sequence) NewID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	if s.prefix == "" {
		return fmt.Sprintf("%06d", s.n)
	}
	return fmt.Sprintf("%s-%06d", s.prefix, s.n)
}

// Reset returns the sequence to its initial state, so the next call to
// NewID again returns "<prefix>-000001". Useful between subtests that each
// want their own predictable IDs from a shared Sequence.
func (s *Sequence) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n = 0
}
