package importing_test

import (
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/domain/importing"
)

func TestNewOccurrenceMatch(t *testing.T) {
	t.Parallel()

	t.Run("starts unresolved", func(t *testing.T) {
		t.Parallel()
		m, err := importing.NewOccurrenceMatch("occ-1")
		if err != nil {
			t.Fatalf("NewOccurrenceMatch(...) = %v, want success", err)
		}
		if m.Resolved() {
			t.Error("Resolved() = true, want false for a freshly-constructed match")
		}
		if m.Resolution() != importing.OccurrenceMatchResolutionPending {
			t.Errorf("Resolution() = %q, want %q", m.Resolution(), importing.OccurrenceMatchResolutionPending)
		}
		if m.OccurrenceID() != "occ-1" {
			t.Errorf("OccurrenceID() = %q, want %q", m.OccurrenceID(), "occ-1")
		}
	})

	t.Run("rejects an empty occurrence id", func(t *testing.T) {
		t.Parallel()
		_, err := importing.NewOccurrenceMatch("")
		if !errors.Is(err, importing.ErrOccurrenceMatchEmptyOccurrenceID) {
			t.Fatalf("NewOccurrenceMatch(\"\") error = %v, want ErrOccurrenceMatchEmptyOccurrenceID", err)
		}
	})
}

// TestOccurrenceMatch_Resolve covers issue #309's two occurrence-match
// resolution outcomes — materialize and dismiss — plus the invalid inputs
// Resolve must reject.
func TestOccurrenceMatch_Resolve(t *testing.T) {
	t.Parallel()

	t.Run("can be resolved to materialized", func(t *testing.T) {
		t.Parallel()
		m, err := importing.NewOccurrenceMatch("occ-1")
		if err != nil {
			t.Fatalf("NewOccurrenceMatch(...) = %v, want success", err)
		}
		resolved, err := m.Resolve(importing.OccurrenceMatchResolutionMaterialized)
		if err != nil {
			t.Fatalf("Resolve(materialized) = %v, want success", err)
		}
		if !resolved.Resolved() {
			t.Error("Resolved() = false after Resolve, want true")
		}
		if resolved.Resolution() != importing.OccurrenceMatchResolutionMaterialized {
			t.Errorf("Resolution() = %q, want %q", resolved.Resolution(), importing.OccurrenceMatchResolutionMaterialized)
		}
		// m itself must be unchanged (copy-transform, not mutation).
		if m.Resolved() {
			t.Error("original match Resolved() = true after Resolve, want unchanged false")
		}
	})

	t.Run("can be resolved to dismissed", func(t *testing.T) {
		t.Parallel()
		m, err := importing.NewOccurrenceMatch("occ-1")
		if err != nil {
			t.Fatalf("NewOccurrenceMatch(...) = %v, want success", err)
		}
		resolved, err := m.Resolve(importing.OccurrenceMatchResolutionDismissed)
		if err != nil {
			t.Fatalf("Resolve(dismissed) = %v, want success", err)
		}
		if resolved.Resolution() != importing.OccurrenceMatchResolutionDismissed {
			t.Errorf("Resolution() = %q, want %q", resolved.Resolution(), importing.OccurrenceMatchResolutionDismissed)
		}
	})

	t.Run("rejects resolving back to pending", func(t *testing.T) {
		t.Parallel()
		m, err := importing.NewOccurrenceMatch("occ-1")
		if err != nil {
			t.Fatalf("NewOccurrenceMatch(...) = %v, want success", err)
		}
		if _, err := m.Resolve(importing.OccurrenceMatchResolutionPending); !errors.Is(err, importing.ErrOccurrenceMatchInvalidResolution) {
			t.Fatalf("Resolve(pending) error = %v, want ErrOccurrenceMatchInvalidResolution", err)
		}
	})

	t.Run("rejects an unknown resolution", func(t *testing.T) {
		t.Parallel()
		m, err := importing.NewOccurrenceMatch("occ-1")
		if err != nil {
			t.Fatalf("NewOccurrenceMatch(...) = %v, want success", err)
		}
		if _, err := m.Resolve(importing.OccurrenceMatchResolution("bogus")); !errors.Is(err, importing.ErrOccurrenceMatchInvalidResolution) {
			t.Fatalf("Resolve(bogus) error = %v, want ErrOccurrenceMatchInvalidResolution", err)
		}
	})
}
