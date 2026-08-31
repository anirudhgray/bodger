package ledger_test

import (
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

func mustMoney(t *testing.T, amountMinor int64, currency string) money.Money {
	t.Helper()
	m, err := money.NewMoney(amountMinor, currency)
	if err != nil {
		t.Fatalf("NewMoney(%d, %q) = %v, want success", amountMinor, currency, err)
	}
	return m
}

func mustDate(t *testing.T, y int, m int, d int) domain.Date {
	t.Helper()
	date, err := domain.NewDate(y, time.Month(m), d)
	if err != nil {
		t.Fatalf("NewDate(%d, %d, %d) = %v, want success", y, m, d, err)
	}
	return date
}

func TestNewAccount(t *testing.T) {
	t.Parallel()

	t.Run("accepts every documented kind", func(t *testing.T) {
		t.Parallel()
		kinds := []ledger.AccountKind{
			ledger.AccountKindBank,
			ledger.AccountKindCash,
			ledger.AccountKindCreditCard,
			ledger.AccountKindWallet,
			ledger.AccountKindInvestment,
			ledger.AccountKindLoan,
			ledger.AccountKindOther,
		}
		for _, k := range kinds {
			k := k
			t.Run(string(k), func(t *testing.T) {
				t.Parallel()
				a, err := ledger.NewAccount("acc-1", "user-1", "Test Account", k, mustMoney(t, 0, "USD"), nil, nil, 0, nil)
				if err != nil {
					t.Fatalf("NewAccount(kind=%s) = %v, want success", k, err)
				}
				if a.Kind() != k {
					t.Errorf("Kind() = %s, want %s", a.Kind(), k)
				}
			})
		}
	})

	t.Run("rejects an unknown kind", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewAccount("acc-1", "user-1", "Test", ledger.AccountKind("crypto"), mustMoney(t, 0, "USD"), nil, nil, 0, nil)
		if !errors.Is(err, ledger.ErrAccountInvalidKind) {
			t.Fatalf("NewAccount(bad kind) error = %v, want ErrAccountInvalidKind", err)
		}
	})

	t.Run("rejects an empty id", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewAccount("", "user-1", "Test", ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, nil, 0, nil)
		if !errors.Is(err, ledger.ErrAccountEmptyID) {
			t.Fatalf("NewAccount(empty id) error = %v, want ErrAccountEmptyID", err)
		}
	})

	t.Run("rejects an empty user id", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewAccount("acc-1", "", "Test", ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, nil, 0, nil)
		if !errors.Is(err, ledger.ErrAccountEmptyUserID) {
			t.Fatalf("NewAccount(empty user id) error = %v, want ErrAccountEmptyUserID", err)
		}
	})

	t.Run("rejects an empty name", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewAccount("acc-1", "user-1", "", ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, nil, 0, nil)
		if !errors.Is(err, ledger.ErrAccountEmptyName) {
			t.Fatalf("NewAccount(empty name) error = %v, want ErrAccountEmptyName", err)
		}
	})

	t.Run("currency comes from the opening balance", func(t *testing.T) {
		t.Parallel()
		a, err := ledger.NewAccount("acc-1", "user-1", "HDFC Savings", ledger.AccountKindBank, mustMoney(t, 500000, "INR"), nil, nil, 0, nil)
		if err != nil {
			t.Fatalf("NewAccount() = %v, want success", err)
		}
		if a.Currency() != "INR" {
			t.Errorf("Currency() = %s, want INR", a.Currency())
		}
		if !a.OpeningBalance().Equal(mustMoney(t, 500000, "INR")) {
			t.Errorf("OpeningBalance() = %s, want 5000.00 INR", a.OpeningBalance())
		}
		if a.ID() != "acc-1" || a.UserID() != "user-1" || a.Name() != "HDFC Savings" {
			t.Errorf("ID()/UserID()/Name() = %q/%q/%q, want acc-1/user-1/HDFC Savings", a.ID(), a.UserID(), a.Name())
		}
	})

	t.Run("opening balance date is optional", func(t *testing.T) {
		t.Parallel()

		a, err := ledger.NewAccount("acc-1", "user-1", "Test", ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, nil, 0, nil)
		if err != nil {
			t.Fatalf("NewAccount() = %v, want success", err)
		}
		if _, ok := a.OpeningBalanceDate(); ok {
			t.Errorf("OpeningBalanceDate() ok = true, want false when nil was passed")
		}

		d := mustDate(t, 2026, 1, 1)
		a, err = ledger.NewAccount("acc-1", "user-1", "Test", ledger.AccountKindBank, mustMoney(t, 0, "USD"), &d, nil, 0, nil)
		if err != nil {
			t.Fatalf("NewAccount() = %v, want success", err)
		}
		got, ok := a.OpeningBalanceDate()
		if !ok || !got.Equal(d) {
			t.Errorf("OpeningBalanceDate() = (%s, %v), want (%s, true)", got, ok, d)
		}
	})

	t.Run("archived_at is optional and drives Archived", func(t *testing.T) {
		t.Parallel()

		a, err := ledger.NewAccount("acc-1", "user-1", "Test", ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, nil, 0, nil)
		if err != nil {
			t.Fatalf("NewAccount() = %v, want success", err)
		}
		if a.Archived() {
			t.Errorf("Archived() = true, want false when archivedAt is nil")
		}
		if _, ok := a.ArchivedAt(); ok {
			t.Errorf("ArchivedAt() ok = true, want false when nil was passed")
		}

		d := mustDate(t, 2026, 1, 1)
		a, err = ledger.NewAccount("acc-1", "user-1", "Test", ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, nil, 0, &d)
		if err != nil {
			t.Fatalf("NewAccount() = %v, want success", err)
		}
		if !a.Archived() {
			t.Errorf("Archived() = false, want true when archivedAt is set")
		}
		got, ok := a.ArchivedAt()
		if !ok || !got.Equal(d) {
			t.Errorf("ArchivedAt() = (%s, %v), want (%s, true)", got, ok, d)
		}
	})

	t.Run("a future archived_at is accepted", func(t *testing.T) {
		// This package has no clock (ADR-0005) and so cannot compare
		// against "now" to reject a future archive date. Whether that
		// should be disallowed is left to the application layer.
		t.Parallel()
		future := mustDate(t, 2999, 1, 1)
		a, err := ledger.NewAccount("acc-1", "user-1", "Test", ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, nil, 0, &future)
		if err != nil {
			t.Fatalf("NewAccount(future archivedAt) = %v, want success", err)
		}
		if !a.Archived() {
			t.Errorf("Archived() = false, want true")
		}
	})

	t.Run("sort_order round-trips, including negative values", func(t *testing.T) {
		t.Parallel()
		a, err := ledger.NewAccount("acc-1", "user-1", "Test", ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, nil, -3, nil)
		if err != nil {
			t.Fatalf("NewAccount() = %v, want success", err)
		}
		if a.SortOrder() != -3 {
			t.Errorf("SortOrder() = %d, want -3", a.SortOrder())
		}
	})

	t.Run("institution is optional", func(t *testing.T) {
		t.Parallel()

		a, err := ledger.NewAccount("acc-1", "user-1", "Test", ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, nil, 0, nil)
		if err != nil {
			t.Fatalf("NewAccount() = %v, want success", err)
		}
		if _, ok := a.Institution(); ok {
			t.Errorf("Institution() ok = true, want false when nil was passed")
		}

		inst := "HDFC Bank"
		a, err = ledger.NewAccount("acc-1", "user-1", "Test", ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, &inst, 0, nil)
		if err != nil {
			t.Fatalf("NewAccount() = %v, want success", err)
		}
		got, ok := a.Institution()
		if !ok || got != inst {
			t.Errorf("Institution() = (%q, %v), want (%q, true)", got, ok, inst)
		}
	})

	t.Run("rejects a non-nil empty institution", func(t *testing.T) {
		t.Parallel()
		empty := ""
		_, err := ledger.NewAccount("acc-1", "user-1", "Test", ledger.AccountKindBank, mustMoney(t, 0, "USD"), nil, &empty, 0, nil)
		if !errors.Is(err, ledger.ErrAccountEmptyInstitution) {
			t.Fatalf("NewAccount(empty institution) error = %v, want ErrAccountEmptyInstitution", err)
		}
	})
}

func TestAccountKindIsLiability(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind ledger.AccountKind
		want bool
	}{
		{ledger.AccountKindCreditCard, true},
		{ledger.AccountKindLoan, true},
		{ledger.AccountKindBank, false},
		{ledger.AccountKindCash, false},
		{ledger.AccountKindWallet, false},
		{ledger.AccountKindInvestment, false},
		{ledger.AccountKindOther, false},
	}
	for _, tc := range cases {
		if got := tc.kind.IsLiability(); got != tc.want {
			t.Errorf("%s.IsLiability() = %v, want %v", tc.kind, got, tc.want)
		}
	}
}
