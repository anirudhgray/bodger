package ledger_test

import (
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
)

func mustAccount(t *testing.T, id string, openingMinor int64, currency string) ledger.Account {
	t.Helper()
	a, err := ledger.NewAccount(id, "user-1", id, ledger.AccountKindBank, mustMoney(t, openingMinor, currency), nil, nil, 0, nil)
	if err != nil {
		t.Fatalf("NewAccount() = %v, want success", err)
	}
	return a
}

func TestBalance(t *testing.T) {
	t.Parallel()

	t.Run("opening balance alone with no transactions", func(t *testing.T) {
		t.Parallel()
		account := mustAccount(t, "acc-1", 500000, "INR")
		got, err := ledger.Balance(account, mustDate(t, 2026, 12, 31), nil)
		if err != nil {
			t.Fatalf("Balance() = %v, want success", err)
		}
		if !got.Equal(mustMoney(t, 500000, "INR")) {
			t.Errorf("Balance() = %s, want 5000.00 INR", got)
		}
	})

	t.Run("sums postings on or before as_of, ignoring later ones", func(t *testing.T) {
		t.Parallel()
		account := mustAccount(t, "acc-1", 0, "USD")

		txOnDate, err := ledger.NewOutflow("tx-1", "user-1", mustDate(t, 2026, 8, 14), "Groceries",
			[]ledger.Posting{mustPosting(t, "post-1", "acc-1", -8000, "USD", nil)})
		if err != nil {
			t.Fatalf("NewOutflow() = %v, want success", err)
		}
		txBeforeDate, err := ledger.NewOutflow("tx-2", "user-1", mustDate(t, 2026, 8, 1), "Rent",
			[]ledger.Posting{mustPosting(t, "post-2", "acc-1", -100000, "USD", nil)})
		if err != nil {
			t.Fatalf("NewOutflow() = %v, want success", err)
		}
		txAfterDate, err := ledger.NewInflow("tx-3", "user-1", mustDate(t, 2026, 8, 15), "Salary",
			[]ledger.Posting{mustPosting(t, "post-3", "acc-1", 200000, "USD", nil)})
		if err != nil {
			t.Fatalf("NewInflow() = %v, want success", err)
		}

		got, err := ledger.Balance(account, mustDate(t, 2026, 8, 14), []ledger.Transaction{txOnDate, txBeforeDate, txAfterDate})
		if err != nil {
			t.Fatalf("Balance() = %v, want success", err)
		}
		// -80.00 (on the date) + -1000.00 (before) = -1080.00; the +2000.00
		// booked the next day must not be included.
		if want := mustMoney(t, -108000, "USD"); !got.Equal(want) {
			t.Errorf("Balance() = %s, want %s", got, want)
		}
	})

	t.Run("ignores soft-deleted transactions", func(t *testing.T) {
		t.Parallel()
		account := mustAccount(t, "acc-1", 0, "USD")

		tx, err := ledger.NewOutflow("tx-1", "user-1", mustDate(t, 2026, 8, 1), "Mistake",
			[]ledger.Posting{mustPosting(t, "post-1", "acc-1", -50000, "USD", nil)})
		if err != nil {
			t.Fatalf("NewOutflow() = %v, want success", err)
		}
		deleted := tx.Delete(time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))

		got, err := ledger.Balance(account, mustDate(t, 2026, 12, 31), []ledger.Transaction{deleted})
		if err != nil {
			t.Fatalf("Balance() = %v, want success", err)
		}
		if !got.IsZero() {
			t.Errorf("Balance() = %s, want 0.00 USD — deleted transactions must not contribute", got)
		}
	})

	t.Run("only counts postings on the requested account", func(t *testing.T) {
		t.Parallel()
		account := mustAccount(t, "acc-1", 0, "USD")

		tx, err := ledger.NewTransfer("tx-1", "user-1", mustDate(t, 2026, 8, 1), "Move money",
			[]ledger.Posting{
				mustPosting(t, "post-1", "acc-1", -50000, "USD", nil),
				mustPosting(t, "post-2", "acc-other", 50000, "USD", nil),
			})
		if err != nil {
			t.Fatalf("NewTransfer() = %v, want success", err)
		}

		got, err := ledger.Balance(account, mustDate(t, 2026, 12, 31), []ledger.Transaction{tx})
		if err != nil {
			t.Fatalf("Balance() = %v, want success", err)
		}
		if want := mustMoney(t, -50000, "USD"); !got.Equal(want) {
			t.Errorf("Balance() = %s, want %s (only acc-1's own leg)", got, want)
		}
	})
}
