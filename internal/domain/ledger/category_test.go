package ledger_test

import (
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
)

func TestNewCategory(t *testing.T) {
	t.Parallel()

	t.Run("accepts expense and income", func(t *testing.T) {
		t.Parallel()
		for _, k := range []ledger.CategoryKind{ledger.CategoryKindExpense, ledger.CategoryKindIncome} {
			c, err := ledger.NewCategory("cat-1", "user-1", nil, "Groceries", k, 0, nil)
			if err != nil {
				t.Fatalf("NewCategory(kind=%s) = %v, want success", k, err)
			}
			if c.Kind() != k {
				t.Errorf("Kind() = %s, want %s", c.Kind(), k)
			}
		}
	})

	t.Run("rejects an unknown kind", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewCategory("cat-1", "user-1", nil, "Groceries", ledger.CategoryKind("transfer"), 0, nil)
		if !errors.Is(err, ledger.ErrCategoryInvalidKind) {
			t.Fatalf("NewCategory(bad kind) error = %v, want ErrCategoryInvalidKind", err)
		}
	})

	t.Run("rejects an empty id", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewCategory("", "user-1", nil, "Groceries", ledger.CategoryKindExpense, 0, nil)
		if !errors.Is(err, ledger.ErrCategoryEmptyID) {
			t.Fatalf("NewCategory(empty id) error = %v, want ErrCategoryEmptyID", err)
		}
	})

	t.Run("rejects an empty user id", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewCategory("cat-1", "", nil, "Groceries", ledger.CategoryKindExpense, 0, nil)
		if !errors.Is(err, ledger.ErrCategoryEmptyUserID) {
			t.Fatalf("NewCategory(empty user id) error = %v, want ErrCategoryEmptyUserID", err)
		}
	})

	t.Run("rejects an empty name", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewCategory("cat-1", "user-1", nil, "", ledger.CategoryKindExpense, 0, nil)
		if !errors.Is(err, ledger.ErrCategoryEmptyName) {
			t.Fatalf("NewCategory(empty name) error = %v, want ErrCategoryEmptyName", err)
		}
	})

	t.Run("top-level category has no parent", func(t *testing.T) {
		t.Parallel()
		c, err := ledger.NewCategory("cat-1", "user-1", nil, "Food", ledger.CategoryKindExpense, 0, nil)
		if err != nil {
			t.Fatalf("NewCategory() = %v, want success", err)
		}
		if _, ok := c.ParentID(); ok {
			t.Errorf("ParentID() ok = true, want false for a top-level category")
		}
	})

	t.Run("accepts a distinct parent", func(t *testing.T) {
		t.Parallel()
		parent := "cat-food"
		c, err := ledger.NewCategory("cat-groceries", "user-1", &parent, "Groceries", ledger.CategoryKindExpense, 0, nil)
		if err != nil {
			t.Fatalf("NewCategory() = %v, want success", err)
		}
		got, ok := c.ParentID()
		if !ok || got != parent {
			t.Errorf("ParentID() = (%s, %v), want (%s, true)", got, ok, parent)
		}
		if c.ID() != "cat-groceries" || c.UserID() != "user-1" || c.Name() != "Groceries" {
			t.Errorf("ID()/UserID()/Name() = %q/%q/%q, want cat-groceries/user-1/Groceries", c.ID(), c.UserID(), c.Name())
		}
	})

	t.Run("rejects being its own parent", func(t *testing.T) {
		t.Parallel()
		self := "cat-1"
		_, err := ledger.NewCategory("cat-1", "user-1", &self, "Food", ledger.CategoryKindExpense, 0, nil)
		if !errors.Is(err, ledger.ErrCategorySelfParent) {
			t.Fatalf("NewCategory(self parent) error = %v, want ErrCategorySelfParent", err)
		}
	})

	t.Run("archived_at is optional and drives Archived", func(t *testing.T) {
		t.Parallel()

		c, err := ledger.NewCategory("cat-1", "user-1", nil, "Food", ledger.CategoryKindExpense, 0, nil)
		if err != nil {
			t.Fatalf("NewCategory() = %v, want success", err)
		}
		if c.Archived() {
			t.Errorf("Archived() = true, want false when archivedAt is nil")
		}
		if _, ok := c.ArchivedAt(); ok {
			t.Errorf("ArchivedAt() ok = true, want false when nil was passed")
		}

		d := mustDate(t, 2026, 1, 1)
		c, err = ledger.NewCategory("cat-1", "user-1", nil, "Food", ledger.CategoryKindExpense, 0, &d)
		if err != nil {
			t.Fatalf("NewCategory() = %v, want success", err)
		}
		if !c.Archived() {
			t.Errorf("Archived() = false, want true when archivedAt is set")
		}
		got, ok := c.ArchivedAt()
		if !ok || !got.Equal(d) {
			t.Errorf("ArchivedAt() = (%s, %v), want (%s, true)", got, ok, d)
		}
	})

	t.Run("a future archived_at is accepted", func(t *testing.T) {
		// Same rationale as Account: this package has no clock
		// (ADR-0005), so it can't reject a future date against "now".
		t.Parallel()
		future := mustDate(t, 2999, 1, 1)
		c, err := ledger.NewCategory("cat-1", "user-1", nil, "Food", ledger.CategoryKindExpense, 0, &future)
		if err != nil {
			t.Fatalf("NewCategory(future archivedAt) = %v, want success", err)
		}
		if !c.Archived() {
			t.Errorf("Archived() = false, want true")
		}
	})

	t.Run("sort_order round-trips, including negative values", func(t *testing.T) {
		t.Parallel()
		c, err := ledger.NewCategory("cat-1", "user-1", nil, "Food", ledger.CategoryKindExpense, -2, nil)
		if err != nil {
			t.Fatalf("NewCategory() = %v, want success", err)
		}
		if c.SortOrder() != -2 {
			t.Errorf("SortOrder() = %d, want -2", c.SortOrder())
		}
	})
}
