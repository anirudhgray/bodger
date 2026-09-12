package app_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestExportSnapshot_RequiresActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.ExportSnapshot(context.Background(), app.ExportSnapshotQuery{})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestExportSnapshot_IncludesAccountsCategoriesAndTransactionsWithTags(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	acc := mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")
	cat := mustCategoryFixture(t, svc, "Groceries", "expense")

	txnResult, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "500", CategoryRef: cat.Category.ID(),
		Date: "2026-08-14", Description: "Big Bazaar", Tags: []string{"food", "monthly"},
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	snapshot, err := svc.ExportSnapshot(ctx, app.ExportSnapshotQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportSnapshot: %v", err)
	}

	if len(snapshot.Accounts) != 1 || snapshot.Accounts[0].ID() != acc.Account.ID() {
		t.Fatalf("Accounts = %+v, want exactly the one fixture account", snapshot.Accounts)
	}
	if len(snapshot.Categories) != 1 || snapshot.Categories[0].ID() != cat.Category.ID() {
		t.Fatalf("Categories = %+v, want exactly the one fixture category", snapshot.Categories)
	}
	if len(snapshot.Transactions) != 1 {
		t.Fatalf("len(Transactions) = %d, want 1", len(snapshot.Transactions))
	}

	et := snapshot.Transactions[0]
	if et.Transaction.ID() != txnResult.Transaction.ID() {
		t.Errorf("Transaction ID = %q, want %q", et.Transaction.ID(), txnResult.Transaction.ID())
	}
	// ports.TransactionRepository.List doesn't load tags -- ExportSnapshot
	// is expected to have fetched them itself (via Get), which is exactly
	// what this asserts.
	if len(et.Tags) != 2 {
		t.Fatalf("len(Tags) = %d, want 2 (List alone wouldn't have populated these)", len(et.Tags))
	}
}

func TestExportSnapshot_ScopedToActor(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	// Give the account/category repositories a second actor's data too --
	// ExportSnapshot must never leak it into testActorID's snapshot
	// (ADR-0006).
	other, err := svc.CreateAccount(ctx, app.CreateAccountCommand{ActorID: "actor-2", Name: "Other's Account", Kind: "bank", Currency: "USD"})
	if err != nil {
		t.Fatalf("CreateAccount(actor-2): %v", err)
	}

	mustAccountFixture(t, svc, "My Account", "bank", "INR")

	snapshot, err := svc.ExportSnapshot(ctx, app.ExportSnapshotQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportSnapshot: %v", err)
	}
	for _, a := range snapshot.Accounts {
		if a.ID() == other.Account.ID() {
			t.Fatalf("ExportSnapshot(testActorID) leaked actor-2's account %q", a.ID())
		}
	}
}

func TestExportSnapshot_OrderIsDeterministicAcrossRepeatedCalls(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	// Several accounts/categories/transactions -- memAccounts/memCategories/
	// memTransactions store rows in Go maps, whose range order is
	// randomised on every single iteration, so calling List (and therefore
	// ExportSnapshot) several times over the exact same underlying data is
	// already a real test of "does this depend on repository return
	// order" without needing to construct that scenario by hand.
	names := []string{"Savings", "Checking", "Wallet", "Credit Card"}
	var accounts []app.AccountResult
	for _, n := range names {
		accounts = append(accounts, mustAccountFixture(t, svc, n, "bank", "INR"))
	}
	cat := mustCategoryFixture(t, svc, "Groceries", "expense")
	for i, n := range names {
		if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
			ActorID: testActorID, AccountRef: accounts[i].Account.ID(), Amount: "100", CategoryRef: cat.Category.ID(),
			Date: "2026-08-14", Description: n,
		}); err != nil {
			t.Fatalf("RecordOutflow(%s): %v", n, err)
		}
	}

	first, err := svc.ExportSnapshot(ctx, app.ExportSnapshotQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportSnapshot (first): %v", err)
	}
	for i := 0; i < 10; i++ {
		again, err := svc.ExportSnapshot(ctx, app.ExportSnapshotQuery{ActorID: testActorID})
		if err != nil {
			t.Fatalf("ExportSnapshot (repeat %d): %v", i, err)
		}
		if !sameAccountOrder(first.Accounts, again.Accounts) {
			t.Fatalf("repeat %d: Accounts order changed:\nfirst=%v\nagain=%v", i, ids(first.Accounts), ids(again.Accounts))
		}
		if !sameCategoryOrder(first.Categories, again.Categories) {
			t.Fatalf("repeat %d: Categories order changed", i)
		}
		if !sameTxnOrder(first.Transactions, again.Transactions) {
			t.Fatalf("repeat %d: Transactions order changed", i)
		}
	}

	// The order actually produced is ascending by ID -- asserting this
	// (rather than just "stable") is what proves ExportSnapshot imposes
	// its own order instead of merely happening to be lucky with the
	// backing map's iteration on this run.
	if !sort.SliceIsSorted(first.Accounts, func(i, j int) bool { return first.Accounts[i].ID() < first.Accounts[j].ID() }) {
		t.Error("Accounts are not sorted by ID ascending")
	}
	if !sort.SliceIsSorted(first.Categories, func(i, j int) bool { return first.Categories[i].ID() < first.Categories[j].ID() }) {
		t.Error("Categories are not sorted by ID ascending")
	}
	if !sort.SliceIsSorted(first.Transactions, func(i, j int) bool {
		return first.Transactions[i].Transaction.ID() < first.Transactions[j].Transaction.ID()
	}) {
		t.Error("Transactions are not sorted by ID ascending")
	}
}

func ids(accounts []ledger.Account) []string {
	out := make([]string, len(accounts))
	for i, a := range accounts {
		out[i] = a.ID()
	}
	return out
}

func sameAccountOrder(a, b []ledger.Account) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID() != b[i].ID() {
			return false
		}
	}
	return true
}

func sameCategoryOrder(a, b []ledger.Category) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID() != b[i].ID() {
			return false
		}
	}
	return true
}

func sameTxnOrder(a, b []app.ExportedTransaction) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Transaction.ID() != b[i].Transaction.ID() {
			return false
		}
	}
	return true
}

// mustSplitOutflowFixture builds a split outflow directly through the
// domain layer (RecordOutflow only supports a single posting -- splits
// aren't a command in this package yet): two postings on acc, deliberately
// constructed with sort_order 1 before sort_order 0, so a caller that
// merely preserved construction order (rather than sorting by SortOrder,
// as sortedPostings does) would see them out of order. Returns the two
// category IDs in sort_order order (groceries=0, household=1) for the
// caller to assert against.
func mustSplitOutflowFixture(t *testing.T, svc *app.Service, acc app.AccountResult) (groceriesID, householdID string) {
	t.Helper()
	ctx := context.Background()

	groceries := mustCategoryFixture(t, svc, "Groceries", "expense")
	household := mustCategoryFixture(t, svc, "Household", "expense")
	groceriesID = groceries.Category.ID()
	householdID = household.Category.ID()

	amt1, err := money.NewMoney(-30000, "INR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	amt2, err := money.NewMoney(-15000, "INR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	p1, err := ledger.NewPosting(svc.IDs.NewID(), acc.Account.ID(), amt2, &householdID, 1)
	if err != nil {
		t.Fatalf("NewPosting: %v", err)
	}
	p0, err := ledger.NewPosting(svc.IDs.NewID(), acc.Account.ID(), amt1, &groceriesID, 0)
	if err != nil {
		t.Fatalf("NewPosting: %v", err)
	}

	bookedDate, err := domain.NewDate(2026, time.August, 14)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	txn, err := ledger.NewOutflow(svc.IDs.NewID(), testActorID, bookedDate, "Big Bazaar split", []ledger.Posting{p1, p0})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}
	if err := svc.Transactions.Create(ctx, testActorID, txn, nil); err != nil {
		t.Fatalf("Transactions.Create: %v", err)
	}
	return groceriesID, householdID
}
