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

// categoryTotalFor finds row's total for kind, failing the test outright if
// ByCategory has no row for it -- every test below asserts against a
// specific kind's row, never "the first row", so a reordering of
// BalanceTotals' own sort never breaks these assertions.
func categoryTotalFor(t *testing.T, rows []app.AccountKindTotal, kind string) app.AccountKindTotal {
	t.Helper()
	for _, r := range rows {
		if string(r.Kind) == kind {
			return r
		}
	}
	t.Fatalf("no ByCategory row for kind %q", kind)
	return app.AccountKindTotal{}
}

// currencyTotalFor finds row's total for currency, failing the test
// outright if ByCurrency has no row for it.
func currencyTotalFor(t *testing.T, rows []app.CurrencyTotal, currency string) app.CurrencyTotal {
	t.Helper()
	for _, r := range rows {
		if r.Currency == currency {
			return r
		}
	}
	t.Fatalf("no ByCurrency row for currency %q", currency)
	return app.CurrencyTotal{}
}

func TestBalanceTotals_OverallByCategoryByCurrency(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	bank := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: bank.Account.ID(), Amount: "1000", Date: "2026-08-01", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	cash := mustAccountFixture(t, svc, "Cash", "cash", "INR")
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: cash.Account.ID(), Amount: "50", Date: "2026-08-01", Description: "Pocket money",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	usd := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: usd.Account.ID(), Amount: "500", Date: "2026-08-01", Description: "Deposit",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	seedRate(t, svc, "USD", "INR", "85", "2026-08-20", "ecb")

	result, err := svc.BalanceTotals(ctx, app.BalanceTotalsQuery{
		ActorID: testActorID, AsOf: "2026-08-20", TargetCurrency: "INR", Policy: app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("BalanceTotals: %v", err)
	}
	if len(result.Unconverted) != 0 {
		t.Fatalf("Unconverted = %+v, want empty", result.Unconverted)
	}

	// Overall: 1000.00 INR (bank) + 50.00 INR (cash) + 500 USD * 85 =
	// 1050.00 + 42500.00 = 43550.00 INR -> 4355000 minor units.
	if got, want := result.Overall.AmountMinor(), int64(4355000); got != want {
		t.Errorf("Overall.AmountMinor() = %d, want %d", got, want)
	}
	if got := result.Overall.Currency(); got != "INR" {
		t.Errorf("Overall.Currency() = %q, want INR", got)
	}

	// ByCategory: bank = HDFC (1000.00 INR) + Checking (42500.00 INR
	// converted) = 43500.00 INR; cash = 50.00 INR.
	bankTotal := categoryTotalFor(t, result.ByCategory, "bank")
	if got, want := bankTotal.Total.AmountMinor(), int64(4350000); got != want {
		t.Errorf("bank category total = %d, want %d", got, want)
	}
	cashTotal := categoryTotalFor(t, result.ByCategory, "cash")
	if got, want := cashTotal.Total.AmountMinor(), int64(5000); got != want {
		t.Errorf("cash category total = %d, want %d", got, want)
	}

	// ByCurrency: raw, unconverted -- INR accounts summed in INR (1050.00),
	// USD accounts summed in USD (500.00), no cross-currency arithmetic.
	inrTotal := currencyTotalFor(t, result.ByCurrency, "INR")
	if got, want := inrTotal.Total.AmountMinor(), int64(105000); got != want {
		t.Errorf("INR currency total = %d, want %d (raw, unconverted)", got, want)
	}
	usdTotal := currencyTotalFor(t, result.ByCurrency, "USD")
	if got, want := usdTotal.Total.AmountMinor(), int64(50000); got != want {
		t.Errorf("USD currency total = %d, want %d (raw, unconverted)", got, want)
	}
}

func TestBalanceTotals_UnconvertibleAccountExcludedFromOverallButNotFromByCurrency(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	inrAcc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: inrAcc.Account.ID(), Amount: "1000", Date: "2026-08-01", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	usdAcc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: usdAcc.Account.ID(), Amount: "500", Date: "2026-08-01", Description: "Deposit",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	// No INR/USD rate seeded at all -- the INR account can't be converted.

	result, err := svc.BalanceTotals(ctx, app.BalanceTotalsQuery{
		ActorID: testActorID, AsOf: "2026-08-20", TargetCurrency: "USD", Policy: app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("BalanceTotals: %v, want success with the shortfall reported instead", err)
	}

	if len(result.Unconverted) != 1 {
		t.Fatalf("len(Unconverted) = %d, want 1", len(result.Unconverted))
	}
	if result.Unconverted[0].Account.ID() != inrAcc.Account.ID() {
		t.Errorf("Unconverted[0].Account = %s, want %s", result.Unconverted[0].Account.ID(), inrAcc.Account.ID())
	}

	// Overall must reflect only the USD account (500.00 USD) -- the
	// unconvertible INR account is excluded, not treated as zero silently
	// merged in (it would still coincidentally look like exactly 500.00 if
	// this test didn't check Unconverted above too).
	if got, want := result.Overall.AmountMinor(), int64(50000); got != want {
		t.Errorf("Overall.AmountMinor() = %d, want %d (USD account only)", got, want)
	}

	// ByCategory's one row (both accounts are "bank") must likewise only
	// carry the convertible account's contribution.
	bankTotal := categoryTotalFor(t, result.ByCategory, "bank")
	if got, want := bankTotal.Total.AmountMinor(), int64(50000); got != want {
		t.Errorf("bank category total = %d, want %d (USD account only)", got, want)
	}

	// ByCurrency needs no conversion at all, so the INR account still
	// contributes its own raw balance here despite being unconvertible.
	inrTotal := currencyTotalFor(t, result.ByCurrency, "INR")
	if got, want := inrTotal.Total.AmountMinor(), int64(100000); got != want {
		t.Errorf("INR currency total = %d, want %d (raw, still present)", got, want)
	}
}

func TestBalanceTotals_NoAccountsIsAllZero(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	result, err := svc.BalanceTotals(ctx, app.BalanceTotalsQuery{
		ActorID: testActorID, AsOf: "2026-08-20", TargetCurrency: "USD", Policy: app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("BalanceTotals: %v", err)
	}
	if !result.Overall.IsZero() {
		t.Errorf("Overall = %s, want zero", result.Overall)
	}
	if got := result.Overall.Currency(); got != "USD" {
		t.Errorf("Overall.Currency() = %q, want USD", got)
	}
	if len(result.ByCategory) != 0 {
		t.Errorf("ByCategory = %+v, want empty", result.ByCategory)
	}
	if len(result.ByCurrency) != 0 {
		t.Errorf("ByCurrency = %+v, want empty", result.ByCurrency)
	}
	if len(result.Unconverted) != 0 {
		t.Errorf("Unconverted = %+v, want empty", result.Unconverted)
	}
}

func TestBalanceTotals_BlankCurrencyResolvesThroughTheReportingCurrencyLadder(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	mustAccountFixture(t, svc, "Cash", "cash", "USD")

	// No reporting currency set for testActorID, and no TargetCurrency
	// override -- must fall through to the instance default (USD, per
	// config.Defaults), the same ladder resolveFetchReportingCurrency
	// applies.
	result, err := svc.BalanceTotals(ctx, app.BalanceTotalsQuery{
		ActorID: testActorID, AsOf: "2026-08-20", Policy: app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("BalanceTotals: %v", err)
	}
	if got := result.Options.ReportingCurrency; got != "USD" {
		t.Errorf("Options.ReportingCurrency = %q, want USD (the instance default)", got)
	}

	if err := svc.SetReportingCurrency(ctx, testActorID, "EUR"); err != nil {
		t.Fatalf("SetReportingCurrency: %v", err)
	}
	result, err = svc.BalanceTotals(ctx, app.BalanceTotalsQuery{
		ActorID: testActorID, AsOf: "2026-08-20", Policy: app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("BalanceTotals: %v", err)
	}
	if got := result.Options.ReportingCurrency; got != "EUR" {
		t.Errorf("Options.ReportingCurrency = %q, want EUR (the actor's own preference)", got)
	}
}

func TestBalanceTotals_RequiresActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.BalanceTotals(context.Background(), app.BalanceTotalsQuery{})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestBalanceTotals_InvalidPolicyFailsTheWholeQuery(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	mustAccountFixture(t, svc, "HDFC", "bank", "INR")

	_, err := svc.BalanceTotals(ctx, app.BalanceTotalsQuery{
		ActorID: testActorID, AsOf: "2026-08-20", Policy: "not_a_real_policy",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

// ---- NetWorthOverTime ----

func TestNetWorthOverTime_OnePointPerMonthEndDate(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	acc, err := svc.CreateAccount(ctx, app.CreateAccountCommand{
		ActorID: testActorID, Name: "Checking", Kind: "bank", Currency: "USD",
		OpeningBalance: "1000", OpeningBalanceDate: "2026-01-01",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "500", Date: "2026-07-15", Description: "Bonus",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "200", Date: "2026-08-15", Description: "Rent",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	result, err := svc.NetWorthOverTime(ctx, app.NetWorthOverTimeQuery{
		ActorID:        testActorID,
		Filter:         app.TransactionFilterInput{DateFrom: "2026-07-01", DateTo: "2026-08-31"},
		TargetCurrency: "USD",
		Policy:         app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("NetWorthOverTime: %v", err)
	}
	if len(result.Points) != 2 {
		t.Fatalf("len(Points) = %d, want 2", len(result.Points))
	}
	if got := result.Points[0].Date.String(); got != "2026-07-31" {
		t.Errorf("Points[0].Date = %s, want 2026-07-31", got)
	}
	// 1000.00 opening + 500.00 inflow (posted by end of July) = 1500.00.
	if got := result.Points[0].Amount.AmountMinor(); got != 150000 {
		t.Errorf("Points[0].Amount = %d, want 150000", got)
	}
	if got := result.Points[1].Date.String(); got != "2026-08-31" {
		t.Errorf("Points[1].Date = %s, want 2026-08-31", got)
	}
	// 1500.00 - 200.00 outflow (posted by end of August) = 1300.00.
	if got := result.Points[1].Amount.AmountMinor(); got != 130000 {
		t.Errorf("Points[1].Amount = %d, want 130000", got)
	}
}

func TestNetWorthOverTime_CustomGranularityIsOneBucket(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Date: "2026-07-15", Description: "Deposit",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}

	result, err := svc.NetWorthOverTime(ctx, app.NetWorthOverTimeQuery{
		ActorID:        testActorID,
		Filter:         app.TransactionFilterInput{DateFrom: "2026-07-01", DateTo: "2026-08-31"},
		TargetCurrency: "USD",
		Policy:         app.PolicyCurrent,
		Granularity:    app.GranularityCustom,
	})
	if err != nil {
		t.Fatalf("NetWorthOverTime: %v", err)
	}
	if len(result.Points) != 1 {
		t.Fatalf("len(Points) = %d, want 1 (a single custom bucket)", len(result.Points))
	}
	if got := result.Points[0].Date.String(); got != "2026-08-31" {
		t.Errorf("Points[0].Date = %s, want 2026-08-31 (the range's own end)", got)
	}
	if got := result.Points[0].Amount.AmountMinor(); got != 10000 {
		t.Errorf("Points[0].Amount = %d, want 10000", got)
	}
}

func TestNetWorthOverTime_MissingRateReportedOncePerAccountNotOnce(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	inrAcc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: inrAcc.Account.ID(), Amount: "1000", Date: "2026-07-01", Description: "Salary",
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	// No INR/USD rate seeded — every period's own conversion should fail
	// to cover this account, but it must appear exactly once in
	// Unconverted, not once per period.

	result, err := svc.NetWorthOverTime(ctx, app.NetWorthOverTimeQuery{
		ActorID:        testActorID,
		Filter:         app.TransactionFilterInput{DateFrom: "2026-07-01", DateTo: "2026-08-31"},
		TargetCurrency: "USD",
		Policy:         app.PolicyCurrent,
	})
	if err != nil {
		t.Fatalf("NetWorthOverTime: %v, want success with the shortfall reported instead", err)
	}
	if len(result.Points) != 2 {
		t.Fatalf("len(Points) = %d, want 2", len(result.Points))
	}
	for _, p := range result.Points {
		if got := p.Amount.AmountMinor(); got != 0 {
			t.Errorf("Point %s Amount = %d, want 0 (the only account couldn't convert)", p.Date, got)
		}
	}
	if len(result.Unconverted) != 1 {
		t.Fatalf("len(Unconverted) = %d, want 1 (deduped across both periods), got %+v", len(result.Unconverted), result.Unconverted)
	}
	if result.Unconverted[0].Account.ID() != inrAcc.Account.ID() {
		t.Errorf("Unconverted[0].Account = %s, want %s", result.Unconverted[0].Account.ID(), inrAcc.Account.ID())
	}
}

func TestNetWorthOverTime_RequiresBothDateBounds(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	for name, filter := range map[string]app.TransactionFilterInput{
		"neither bound": {},
		"only DateFrom": {DateFrom: "2026-07-01"},
		"only DateTo":   {DateTo: "2026-08-31"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.NetWorthOverTime(ctx, app.NetWorthOverTimeQuery{
				ActorID: testActorID, Filter: filter, TargetCurrency: "USD", Policy: app.PolicyCurrent,
			})
			wantErrCode(t, err, errs.InvalidInput)
		})
	}
}

func TestNetWorthOverTime_RequiresActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.NetWorthOverTime(context.Background(), app.NetWorthOverTimeQuery{
		Filter: app.TransactionFilterInput{DateFrom: "2026-07-01", DateTo: "2026-08-31"},
		Policy: app.PolicyCurrent,
	})
	wantErrCode(t, err, errs.InvalidInput)
}
