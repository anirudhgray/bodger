package importing_test

import (
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/domain/importing"
)

func TestNewDuplicateMatch(t *testing.T) {
	t.Parallel()

	t.Run("starts unresolved", func(t *testing.T) {
		t.Parallel()
		m, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		if m.Resolved() {
			t.Error("Resolved() = true, want false for a freshly-constructed match")
		}
		if m.Resolution() != importing.DuplicateResolutionPending {
			t.Errorf("Resolution() = %q, want %q", m.Resolution(), importing.DuplicateResolutionPending)
		}
		if m.Tier() != importing.DuplicateMatchTierSuspected {
			t.Errorf("Tier() = %q, want %q", m.Tier(), importing.DuplicateMatchTierSuspected)
		}
		if m.MatchedTransactionID() != "txn-1" {
			t.Errorf("MatchedTransactionID() = %q, want %q", m.MatchedTransactionID(), "txn-1")
		}
	})

	t.Run("accepts both tiers", func(t *testing.T) {
		t.Parallel()
		for _, tier := range []importing.DuplicateMatchTier{
			importing.DuplicateMatchTierExact,
			importing.DuplicateMatchTierSuspected,
		} {
			tier := tier
			t.Run(string(tier), func(t *testing.T) {
				t.Parallel()
				if _, err := importing.NewDuplicateMatch(tier, "txn-1"); err != nil {
					t.Fatalf("NewDuplicateMatch(%q, ...) = %v, want success", tier, err)
				}
			})
		}
	})

	t.Run("rejects an unknown tier", func(t *testing.T) {
		t.Parallel()
		_, err := importing.NewDuplicateMatch(importing.DuplicateMatchTier("bogus"), "txn-1")
		if !errors.Is(err, importing.ErrDuplicateMatchInvalidTier) {
			t.Fatalf("NewDuplicateMatch(bad tier) error = %v, want ErrDuplicateMatchInvalidTier", err)
		}
	})

	t.Run("rejects an empty matched transaction id", func(t *testing.T) {
		t.Parallel()
		_, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierExact, "")
		if !errors.Is(err, importing.ErrDuplicateMatchEmptyTransactionID) {
			t.Fatalf("NewDuplicateMatch(empty transaction id) error = %v, want ErrDuplicateMatchEmptyTransactionID", err)
		}
	})
}

// TestDuplicateMatch_Resolve covers ADR-0008's rule that only a tier-2
// suspected duplicate is ever surfaced for the user to resolve — a tier-1
// exact match is skipped automatically and has no decision to record.
func TestDuplicateMatch_Resolve(t *testing.T) {
	t.Parallel()

	t.Run("a suspected match can be confirmed", func(t *testing.T) {
		t.Parallel()
		m, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		resolved, err := m.Resolve(importing.DuplicateResolutionConfirmed)
		if err != nil {
			t.Fatalf("Resolve(confirmed) = %v, want success", err)
		}
		if !resolved.Resolved() {
			t.Error("Resolved() = false after Resolve, want true")
		}
		if resolved.Resolution() != importing.DuplicateResolutionConfirmed {
			t.Errorf("Resolution() = %q, want %q", resolved.Resolution(), importing.DuplicateResolutionConfirmed)
		}
		// m itself must be unchanged (copy-transform, not mutation).
		if m.Resolved() {
			t.Error("original match Resolved() = true after Resolve, want unchanged false")
		}
	})

	t.Run("a suspected match can be dismissed", func(t *testing.T) {
		t.Parallel()
		m, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		resolved, err := m.Resolve(importing.DuplicateResolutionDismissed)
		if err != nil {
			t.Fatalf("Resolve(dismissed) = %v, want success", err)
		}
		if resolved.Resolution() != importing.DuplicateResolutionDismissed {
			t.Errorf("Resolution() = %q, want %q", resolved.Resolution(), importing.DuplicateResolutionDismissed)
		}
	})

	t.Run("an exact match cannot be resolved", func(t *testing.T) {
		t.Parallel()
		m, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierExact, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		if _, err := m.Resolve(importing.DuplicateResolutionConfirmed); !errors.Is(err, importing.ErrDuplicateMatchExactNotResolvable) {
			t.Fatalf("Resolve(...) on an exact match error = %v, want ErrDuplicateMatchExactNotResolvable", err)
		}
	})

	t.Run("rejects resolving back to pending", func(t *testing.T) {
		t.Parallel()
		m, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		if _, err := m.Resolve(importing.DuplicateResolutionPending); !errors.Is(err, importing.ErrDuplicateMatchInvalidResolution) {
			t.Fatalf("Resolve(pending) error = %v, want ErrDuplicateMatchInvalidResolution", err)
		}
	})

	t.Run("rejects an unknown resolution", func(t *testing.T) {
		t.Parallel()
		m, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		if _, err := m.Resolve(importing.DuplicateResolution("bogus")); !errors.Is(err, importing.ErrDuplicateMatchInvalidResolution) {
			t.Fatalf("Resolve(bogus) error = %v, want ErrDuplicateMatchInvalidResolution", err)
		}
	})
}
