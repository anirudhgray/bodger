package idgen_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/anirudhgray/bodger/internal/platform/idgen"
)

func TestUUID_ReturnsUniqueParseableIDs(t *testing.T) {
	gen := idgen.New()

	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := gen.NewID()
		if _, err := uuid.Parse(id); err != nil {
			t.Fatalf("NewID() = %q, not a parseable UUID: %v", id, err)
		}
		if seen[id] {
			t.Fatalf("NewID() returned a duplicate: %q", id)
		}
		seen[id] = true
	}
}

func TestSequence_IsDeterministicAndOrdered(t *testing.T) {
	seq := idgen.NewSequence("acct")

	want := []string{"acct-000001", "acct-000002", "acct-000003"}
	for _, w := range want {
		if got := seq.NewID(); got != w {
			t.Errorf("NewID() = %q, want %q", got, w)
		}
	}
}

func TestSequence_EmptyPrefix(t *testing.T) {
	seq := idgen.NewSequence("")
	if got, want := seq.NewID(), "000001"; got != want {
		t.Errorf("NewID() = %q, want %q", got, want)
	}
}

func TestSequence_Reset(t *testing.T) {
	seq := idgen.NewSequence("x")
	seq.NewID()
	seq.NewID()
	seq.Reset()
	if got, want := seq.NewID(), "x-000001"; got != want {
		t.Errorf("after Reset, NewID() = %q, want %q", got, want)
	}
}

func TestSequence_ConcurrentUseProducesUniqueIDs(t *testing.T) {
	seq := idgen.NewSequence("c")
	const n = 200

	ids := make(chan string, n)
	for i := 0; i < n; i++ {
		go func() { ids <- seq.NewID() }()
	}

	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id := <-ids
		if seen[id] {
			t.Fatalf("concurrent NewID() returned a duplicate: %q", id)
		}
		seen[id] = true
	}
}
