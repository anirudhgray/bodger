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

func accountBalanceFor(t *testing.T, result app.AccountBalancesResult, accountID string) app.AccountBalance {
	t.Helper()
	for _, b := range result.Balances {
		if b.Account.ID() == accountID {
			return b
		}
	}
	t.Fatalf("no balance found for account %q", accountID)
	return app.AccountBalance{}
}

func TestAccountBalances_NoTargetCurrencyMeansNoConversion(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{ActorID: testActorID, AsOf: "2026-08-20"})
	if err != nil {
		t.Fatalf("AccountBalances: %v", err)
	}
	ab := accountBalanceFor(t, result, acc.Account.ID())
	if ab.Converted != nil {
		t.Errorf("Converted = %+v, want nil when no TargetCurrency was requested", ab.Converted)
	}
	if len(result.Unconverted) != 0 {
		t.Errorf("Unconverted = %+v, want empty when no TargetCurrency was requested", result.Unconverted)
	}
}

func TestAccountBalances_ConvertsUnderCurrentPolicy(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100000", Date: "2026-08-01", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	seedRate(t, svc, "INR", "USD", "0.0115", "2026-08-20", "ecb")

	result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{
		ActorID: testActorID, AsOf: "2026-08-20", TargetCurrency: "USD", Policy: app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("AccountBalances: %v", err)
	}
	ab := accountBalanceFor(t, result, acc.Account.ID())
	if ab.Converted == nil {
		t.Fatalf("Converted = nil, want a converted figure")
	}
	if got := ab.Converted.Amount.AmountMinor(); got != 115000 {
		t.Errorf("Converted.Amount.AmountMinor() = %d, want 115000 (1150.00 USD)", got)
	}
	if !ab.Converted.ConvertedFrom.Equal(ab.Balance) {
		t.Errorf("Converted.ConvertedFrom = %s, want %s", ab.Converted.ConvertedFrom, ab.Balance)
	}
	if ab.Converted.RateSource != "ecb" {
		t.Errorf("Converted.RateSource = %q, want ecb", ab.Converted.RateSource)
	}
	if ab.Converted.Policy != app.PolicyCurrent {
		t.Errorf("Converted.Policy = %s, want %s", ab.Converted.Policy, app.PolicyCurrent)
	}
	if len(result.Unconverted) != 0 {
		t.Errorf("Unconverted = %+v, want empty", result.Unconverted)
	}
}

func TestAccountBalances_TransactionDatePolicyUsesAsOfsDate(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100000", Date: "2026-08-01", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	// A rate at AsOf (2026-08-14) and a different one at today (2026-08-20)
	// -- PolicyTransactionDate must use AsOf's, not today's.
	seedRate(t, svc, "INR", "USD", "0.0115", "2026-08-14", "ecb")
	seedRate(t, svc, "INR", "USD", "0.0200", "2026-08-20", "ecb")

	result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{
		ActorID: testActorID, AsOf: "2026-08-14", TargetCurrency: "USD", Policy: app.PolicyTransactionDate,
	})
	if err != nil {
		t.Fatalf("AccountBalances: %v", err)
	}
	ab := accountBalanceFor(t, result, acc.Account.ID())
	if ab.Converted == nil {
		t.Fatalf("Converted = nil, want a converted figure")
	}
	if got := ab.Converted.Amount.AmountMinor(); got != 115000 {
		t.Errorf("Converted.Amount.AmountMinor() = %d, want 115000 (1150.00 USD at AsOf's rate)", got)
	}
	if ab.Converted.RateDate.String() != "2026-08-14" {
		t.Errorf("Converted.RateDate = %s, want 2026-08-14 (AsOf, not today)", ab.Converted.RateDate)
	}
}

func TestAccountBalances_PinnedPolicyUsesPinnedDate(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100000", Date: "2026-08-01", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	seedRate(t, svc, "INR", "USD", "0.0120", "2026-01-01", "ecb")

	result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{
		ActorID: testActorID, AsOf: "2026-08-20", TargetCurrency: "USD", Policy: app.PolicyPinned, PinnedDate: "2026-01-01",
	})
	if err != nil {
		t.Fatalf("AccountBalances: %v", err)
	}
	ab := accountBalanceFor(t, result, acc.Account.ID())
	if ab.Converted == nil {
		t.Fatalf("Converted = nil, want a converted figure")
	}
	if got := ab.Converted.Amount.AmountMinor(); got != 120000 {
		t.Errorf("Converted.Amount.AmountMinor() = %d, want 120000 (1200.00 USD at the pinned rate)", got)
	}
}

func TestAccountBalances_SameCurrencyAccountNeedsNoRate(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "500", Date: "2026-08-01", Description: "Deposit",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}

	// No rate seeded at all -- a same-currency account must still convert
	// (as an identity) without needing one.
	result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{
		ActorID: testActorID, AsOf: "2026-08-20", TargetCurrency: "USD", Policy: app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("AccountBalances: %v", err)
	}
	ab := accountBalanceFor(t, result, acc.Account.ID())
	if ab.Converted == nil {
		t.Fatalf("Converted = nil, want an identity conversion")
	}
	if !ab.Converted.Rate.IsIdentity() {
		t.Errorf("Converted.Rate = %s, want identity", ab.Converted.Rate)
	}
	if len(result.Unconverted) != 0 {
		t.Errorf("Unconverted = %+v, want empty", result.Unconverted)
	}
}

func TestAccountBalances_MissingRateIsReportedNotOmitted(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	inrAcc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	usdAcc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: inrAcc.Account.ID(), Amount: "100000", Date: "2026-08-01", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: usdAcc.Account.ID(), Amount: "500", Date: "2026-08-01", Description: "Deposit",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	// No INR/USD rate seeded at all.

	result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{
		ActorID: testActorID, AsOf: "2026-08-20", TargetCurrency: "USD", Policy: app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("AccountBalances: %v, want success with the shortfall reported instead", err)
	}

	// The USD account (identity, no rate needed) still converts...
	usdBalance := accountBalanceFor(t, result, usdAcc.Account.ID())
	if usdBalance.Converted == nil {
		t.Errorf("USD account Converted = nil, want an identity conversion")
	}

	// ...but the INR account, with no rate available, must not be silently
	// dropped from Balances or converted under some other policy: it's
	// still present (with its own-currency Balance intact) and reported in
	// Unconverted, not converted.
	inrBalance := accountBalanceFor(t, result, inrAcc.Account.ID())
	if inrBalance.Converted != nil {
		t.Errorf("INR account Converted = %+v, want nil (no rate available)", inrBalance.Converted)
	}
	if inrBalance.Balance.AmountMinor() != 10000000 {
		t.Errorf("INR account Balance = %d, want 10000000 (unchanged, still reported)", inrBalance.Balance.AmountMinor())
	}
	if len(result.Unconverted) != 1 {
		t.Fatalf("len(Unconverted) = %d, want 1", len(result.Unconverted))
	}
	if result.Unconverted[0].Account.ID() != inrAcc.Account.ID() {
		t.Errorf("Unconverted[0].Account = %s, want %s", result.Unconverted[0].Account.ID(), inrAcc.Account.ID())
	}
	if result.Unconverted[0].Reason == "" {
		t.Errorf("Unconverted[0].Reason is empty, want a human-readable explanation")
	}
}

func TestAccountBalances_InvalidPolicyFailsTheWholeQuery(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	mustAccountFixture(t, svc, "HDFC", "bank", "INR")

	_, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{
		ActorID: testActorID, AsOf: "2026-08-20", TargetCurrency: "USD", Policy: "not_a_real_policy",
	})
	wantErrCode(t, err, errs.InvalidInput)
}
