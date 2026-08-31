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
			c, err := ledger.NewCategory("cat-1", "user-1", nil, "Groceries", k, false)
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
		_, err := ledger.NewCategory("cat-1", "user-1", nil, "Groceries", ledger.CategoryKind("transfer"), false)
		if !errors.Is(err, ledger.ErrCategoryInvalidKind) {
			t.Fatalf("NewCategory(bad kind) error = %v, want ErrCategoryInvalidKind", err)
		}
	})

	t.Run("rejects an empty id", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewCategory("", "user-1", nil, "Groceries", ledger.CategoryKindExpense, false)
		if !errors.Is(err, ledger.ErrCategoryEmptyID) {
			t.Fatalf("NewCategory(empty id) error = %v, want ErrCategoryEmptyID", err)
		}
	})

	t.Run("rejects an empty user id", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewCategory("cat-1", "", nil, "Groceries", ledger.CategoryKindExpense, false)
		if !errors.Is(err, ledger.ErrCategoryEmptyUserID) {
			t.Fatalf("NewCategory(empty user id) error = %v, want ErrCategoryEmptyUserID", err)
		}
	})

	t.Run("rejects an empty name", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewCategory("cat-1", "user-1", nil, "", ledger.CategoryKindExpense, false)
		if !errors.Is(err, ledger.ErrCategoryEmptyName) {
			t.Fatalf("NewCategory(empty name) error = %v, want ErrCategoryEmptyName", err)
		}
	})

	t.Run("top-level category has no parent", func(t *testing.T) {
		t.Parallel()
		c, err := ledger.NewCategory("cat-1", "user-1", nil, "Food", ledger.CategoryKindExpense, false)
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
		c, err := ledger.NewCategory("cat-groceries", "user-1", &parent, "Groceries", ledger.CategoryKindExpense, false)
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
		_, err := ledger.NewCategory("cat-1", "user-1", &self, "Food", ledger.CategoryKindExpense, false)
		if !errors.Is(err, ledger.ErrCategorySelfParent) {
			t.Fatalf("NewCategory(self parent) error = %v, want ErrCategorySelfParent", err)
		}
	})

	t.Run("archived flag round-trips", func(t *testing.T) {
		t.Parallel()
		c, err := ledger.NewCategory("cat-1", "user-1", nil, "Food", ledger.CategoryKindExpense, true)
		if err != nil {
			t.Fatalf("NewCategory() = %v, want success", err)
		}
		if !c.Archived() {
			t.Errorf("Archived() = false, want true")
		}
	})
}
