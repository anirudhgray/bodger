package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestAccountBalances_OpeningBalancePlusPostings(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	created, err := svc.CreateAccount(ctx, app.CreateAccountCommand{
		ActorID: testActorID, Name: "HDFC Savings", Kind: "bank", Currency: "INR",
		OpeningBalance: "50000.00", OpeningBalanceDate: "2026-01-01",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: created.Account.ID(), Amount: "800", Date: "2026-08-14", Description: "Groceries",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: created.Account.ID(), Amount: "1000", Date: "2026-08-01", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}

	result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{ActorID: testActorID, AsOf: "2026-08-20"})
	if err != nil {
		t.Fatalf("AccountBalances: %v", err)
	}
	if len(result.Balances) != 1 {
		t.Fatalf("len(Balances) = %d, want 1", len(result.Balances))
	}
	// 50000.00 + 1000.00 - 800.00 = 50200.00 -> 5020000 minor units.
	if got := result.Balances[0].Balance.AmountMinor(); got != 5020000 {
		t.Errorf("Balance = %d, want 5020000", got)
	}
}

func TestAccountBalances_ExcludesTransactionsAfterAsOf(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "10", Date: "2026-08-10", Description: "before",
	}); err != nil {
		t.Fatalf("RecordOutflow(before): %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "10", Date: "2026-08-20", Description: "after",
	}); err != nil {
		t.Fatalf("RecordOutflow(after): %v", err)
	}

	result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{ActorID: testActorID, AsOf: "2026-08-15"})
	if err != nil {
		t.Fatalf("AccountBalances: %v", err)
	}
	if got := balanceFor(t, result, acc.Account.ID()); got != -1000 {
		t.Errorf("Balance = %d, want -1000 (only the 'before' transaction)", got)
	}
}

func TestAccountBalances_TransferAffectsBothAccounts(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	savings := mustAccountFixture(t, svc, "Savings", "bank", "USD")
	checking := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	if _, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: savings.Account.ID(), ToAccountRef: checking.Account.ID(),
		Amount: "200", Date: "2026-08-05", Description: "Move",
	}); err != nil {
		t.Fatalf("RecordTransfer: %v", err)
	}

	result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{ActorID: testActorID, AsOf: "2026-08-20"})
	if err != nil {
		t.Fatalf("AccountBalances: %v", err)
	}
	if got := balanceFor(t, result, savings.Account.ID()); got != -20000 {
		t.Errorf("Savings balance = %d, want -20000", got)
	}
	if got := balanceFor(t, result, checking.Account.ID()); got != 20000 {
		t.Errorf("Checking balance = %d, want 20000", got)
	}
}

func TestAccountBalances_ExcludesSoftDeletedTransactions(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	created, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "50", Date: "2026-08-10", Description: "gone soon",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if _, err := svc.DeleteTransaction(ctx, app.DeleteTransactionCommand{ActorID: testActorID, TransactionRef: created.Transaction.ID()}); err != nil {
		t.Fatalf("DeleteTransaction: %v", err)
	}

	result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{ActorID: testActorID, AsOf: "2026-08-20"})
	if err != nil {
		t.Fatalf("AccountBalances: %v", err)
	}
	if got := balanceFor(t, result, acc.Account.ID()); got != 0 {
		t.Errorf("Balance = %d, want 0 (deleted transaction excluded)", got)
	}
}

func TestAccountBalances_EmptyAsOfMeansToday(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "10", Date: "2026-08-20", Description: "today",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("AccountBalances: %v", err)
	}
	if result.AsOf.String() != "2026-08-20" {
		t.Errorf("AsOf = %s, want 2026-08-20 (the frozen clock's today)", result.AsOf.String())
	}
	if got := balanceFor(t, result, acc.Account.ID()); got != -1000 {
		t.Errorf("Balance = %d, want -1000", got)
	}
}

func TestAccountBalances_RequiresActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.AccountBalances(context.Background(), app.AccountBalancesQuery{})
	wantErrCode(t, err, errs.InvalidInput)
}
