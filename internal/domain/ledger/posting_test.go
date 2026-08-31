package ledger_test

import (
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
)

func TestNewPosting(t *testing.T) {
	t.Parallel()

	t.Run("accepts a posting with no category", func(t *testing.T) {
		t.Parallel()
		p, err := ledger.NewPosting("post-1", "acc-1", mustMoney(t, -80000, "INR"), nil, 0)
		if err != nil {
			t.Fatalf("NewPosting() = %v, want success", err)
		}
		if _, ok := p.CategoryID(); ok {
			t.Errorf("CategoryID() ok = true, want false")
		}
		if p.Currency() != "INR" {
			t.Errorf("Currency() = %s, want INR", p.Currency())
		}
	})

	t.Run("accepts a posting with a category", func(t *testing.T) {
		t.Parallel()
		catID := "cat-groceries"
		p, err := ledger.NewPosting("post-1", "acc-1", mustMoney(t, -80000, "INR"), &catID, 0)
		if err != nil {
			t.Fatalf("NewPosting() = %v, want success", err)
		}
		got, ok := p.CategoryID()
		if !ok || got != catID {
			t.Errorf("CategoryID() = (%s, %v), want (%s, true)", got, ok, catID)
		}
	})

	t.Run("rejects an empty id", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewPosting("", "acc-1", mustMoney(t, -100, "USD"), nil, 0)
		if !errors.Is(err, ledger.ErrPostingEmptyID) {
			t.Fatalf("NewPosting(empty id) error = %v, want ErrPostingEmptyID", err)
		}
	})

	t.Run("rejects an empty account id", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewPosting("post-1", "", mustMoney(t, -100, "USD"), nil, 0)
		if !errors.Is(err, ledger.ErrPostingEmptyAccountID) {
			t.Fatalf("NewPosting(empty account id) error = %v, want ErrPostingEmptyAccountID", err)
		}
	})

	t.Run("rejects a non-nil but empty category id", func(t *testing.T) {
		t.Parallel()
		empty := ""
		_, err := ledger.NewPosting("post-1", "acc-1", mustMoney(t, -100, "USD"), &empty, 0)
		if !errors.Is(err, ledger.ErrPostingEmptyCategoryID) {
			t.Fatalf("NewPosting(empty category id) error = %v, want ErrPostingEmptyCategoryID", err)
		}
	})

	t.Run("sort order round-trips", func(t *testing.T) {
		t.Parallel()
		p, err := ledger.NewPosting("post-1", "acc-1", mustMoney(t, -100, "USD"), nil, 3)
		if err != nil {
			t.Fatalf("NewPosting() = %v, want success", err)
		}
		if p.SortOrder() != 3 {
			t.Errorf("SortOrder() = %d, want 3", p.SortOrder())
		}
	})
}
