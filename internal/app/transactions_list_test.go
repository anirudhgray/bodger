package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
)

func TestListTransactions_DateRangeFilter(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	dates := []string{"2026-07-31", "2026-08-01", "2026-08-15", "2026-08-31", "2026-09-01"}
	for i, d := range dates {
		if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
			ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "10", Date: d, Description: descFor(i),
		}); err != nil {
			t.Fatalf("RecordOutflow(%s): %v", d, err)
		}
	}

	result, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{
		ActorID: testActorID, DateFrom: "2026-08-01", DateTo: "2026-08-31",
	})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(result.Transactions) != 3 {
		t.Fatalf("len(Transactions) = %d, want 3 (the three transactions inside August 2026)", len(result.Transactions))
	}
	for _, txn := range result.Transactions {
		d := txn.BookedDate().String()
		if d < "2026-08-01" || d > "2026-08-31" {
			t.Errorf("BookedDate = %s, out of range [2026-08-01, 2026-08-31]", d)
		}
	}
}

func descFor(i int) string {
	return []string{"before", "first", "middle", "last", "after"}[i]
}

// TestListTransactions_AugustIdenticalUnderUTCAndKolkata is issue #6's
// "done when": "'August 2026' returns identical ListTransactions results
// under TZ=UTC and TZ=Asia/Kolkata." DateFrom/DateTo resolve through
// normalize.DateOf in the actor's configured timezone (a Service field,
// not the host process's TZ), so this asserts the invariant without
// needing to fork the test process under two different TZ env vars.
func TestListTransactions_AugustIdenticalUnderUTCAndKolkata(t *testing.T) {
	frozen := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)

	run := func(tz string) []string {
		svc := newTestService(t, frozen, tz)
		ctx := context.Background()
		acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")
		for i, d := range []string{"2026-07-31", "2026-08-01", "2026-08-14", "2026-08-31", "2026-09-01"} {
			if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
				ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "10", Date: d, Description: descFor(i),
			}); err != nil {
				t.Fatalf("RecordOutflow(%s) under %s: %v", d, tz, err)
			}
		}
		result, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{ActorID: testActorID, DateFrom: "2026-08-01", DateTo: "2026-08-31"})
		if err != nil {
			t.Fatalf("ListTransactions under %s: %v", tz, err)
		}
		var descriptions []string
		for _, txn := range result.Transactions {
			descriptions = append(descriptions, txn.Description())
		}
		return descriptions
	}

	utc := run("UTC")
	kolkata := run("Asia/Kolkata")
	if len(utc) != len(kolkata) {
		t.Fatalf("UTC returned %d transactions, Asia/Kolkata returned %d, want equal", len(utc), len(kolkata))
	}
	for i := range utc {
		if utc[i] != kolkata[i] {
			t.Errorf("result[%d]: UTC=%q, Asia/Kolkata=%q, want equal", i, utc[i], kolkata[i])
		}
	}
}

func TestListTransactions_AccountFilter(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	a := mustAccountFixture(t, svc, "A", "bank", "USD")
	b := mustAccountFixture(t, svc, "B", "bank", "USD")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{ActorID: testActorID, AccountRef: a.Account.ID(), Amount: "10", Description: "on A"}); err != nil {
		t.Fatalf("RecordOutflow(A): %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{ActorID: testActorID, AccountRef: b.Account.ID(), Amount: "10", Description: "on B"}); err != nil {
		t.Fatalf("RecordOutflow(B): %v", err)
	}

	result, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{ActorID: testActorID, AccountRef: a.Account.ID()})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(result.Transactions) != 1 || result.Transactions[0].Description() != "on A" {
		t.Errorf("Transactions = %+v, want only the transaction on account A", result.Transactions)
	}
}

func TestListTransactions_CategorySubtreeIncludedByDefault(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	food := mustCategoryFixture(t, svc, "Food", "expense")
	groceries, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Groceries", Kind: "expense", ParentRef: food.Category.ID()})
	if err != nil {
		t.Fatalf("CreateCategory(Groceries): %v", err)
	}
	other := mustCategoryFixture(t, svc, "Transport", "expense")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "10", CategoryRef: groceries.Category.ID(), Description: "groceries run",
	}); err != nil {
		t.Fatalf("RecordOutflow(groceries): %v", err)
	}
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "10", CategoryRef: other.Category.ID(), Description: "bus fare",
	}); err != nil {
		t.Fatalf("RecordOutflow(transport): %v", err)
	}

	// Filtering by the PARENT ("Food") must include the child's
	// ("Groceries") transaction - ADR-0009's subtree-by-default.
	result, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{ActorID: testActorID, CategoryRef: food.Category.ID()})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(result.Transactions) != 1 || result.Transactions[0].Description() != "groceries run" {
		t.Errorf("Transactions = %+v, want only the groceries-run transaction (subtree of Food)", result.Transactions)
	}
}

func TestListTransactions_KindFilter(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	a := mustAccountFixture(t, svc, "A", "bank", "USD")
	b := mustAccountFixture(t, svc, "B", "bank", "USD")

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{ActorID: testActorID, AccountRef: a.Account.ID(), Amount: "10", Description: "spend"}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{ActorID: testActorID, AccountRef: a.Account.ID(), Amount: "10", Description: "earn"}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	if _, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{ActorID: testActorID, FromAccountRef: a.Account.ID(), ToAccountRef: b.Account.ID(), Amount: "10", Description: "move"}); err != nil {
		t.Fatalf("RecordTransfer: %v", err)
	}

	result, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{ActorID: testActorID, Kind: "transfer"})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(result.Transactions) != 1 || result.Transactions[0].Kind() != "transfer" {
		t.Errorf("Transactions = %+v, want only the transfer", result.Transactions)
	}
}

func TestListTransactions_DeterministicSortWithTiebreaks(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	// All five booked on the same date, created in this order - the sort
	// must be (booked_date DESC, created_at DESC, id DESC), so with a tied
	// booked_date, later-created wins, in strictly reverse creation order.
	var ids []string
	for i := range 5 {
		result, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
			ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "10", Date: "2026-08-14", Description: descForN(i),
		})
		if err != nil {
			t.Fatalf("RecordOutflow #%d: %v", i, err)
		}
		ids = append(ids, result.Transaction.ID())
	}

	// Run the same query twice: the result must be identical both times.
	first, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListTransactions (first): %v", err)
	}
	second, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListTransactions (second): %v", err)
	}
	if len(first.Transactions) != 5 || len(second.Transactions) != 5 {
		t.Fatalf("got %d and %d transactions, want 5 and 5", len(first.Transactions), len(second.Transactions))
	}
	for i := range first.Transactions {
		if first.Transactions[i].ID() != second.Transactions[i].ID() {
			t.Fatalf("result[%d] differs between runs: %q vs %q - sort must be deterministic", i, first.Transactions[i].ID(), second.Transactions[i].ID())
		}
	}

	// Reverse creation order.
	for i, txn := range first.Transactions {
		want := ids[len(ids)-1-i]
		if txn.ID() != want {
			t.Errorf("result[%d].ID() = %q, want %q (reverse creation order under a tied booked_date)", i, txn.ID(), want)
		}
	}
}

func descForN(i int) string {
	return []string{"one", "two", "three", "four", "five"}[i]
}

func TestListTransactions_Pagination(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	var ids []string
	for i := range 5 {
		result, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
			ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "10", Date: "2026-08-14", Description: descForN(i),
		})
		if err != nil {
			t.Fatalf("RecordOutflow #%d: %v", i, err)
		}
		ids = append(ids, result.Transaction.ID())
	}
	// Newest-created-first order (matches the tiebreak test above).
	wantOrder := []string{ids[4], ids[3], ids[2], ids[1], ids[0]}

	page1, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{ActorID: testActorID, Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListTransactions (page1): %v", err)
	}
	page2, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{ActorID: testActorID, Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListTransactions (page2): %v", err)
	}
	page3, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{ActorID: testActorID, Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("ListTransactions (page3): %v", err)
	}

	if len(page1.Transactions) != 2 || len(page2.Transactions) != 2 || len(page3.Transactions) != 1 {
		t.Fatalf("page sizes = %d, %d, %d, want 2, 2, 1", len(page1.Transactions), len(page2.Transactions), len(page3.Transactions))
	}

	var got []string
	for _, p := range []app.ListTransactionsResult{page1, page2, page3} {
		for _, txn := range p.Transactions {
			got = append(got, txn.ID())
		}
	}
	for i := range wantOrder {
		if got[i] != wantOrder[i] {
			t.Errorf("paged result[%d] = %q, want %q", i, got[i], wantOrder[i])
		}
	}
}

func TestListTransactions_DefaultLimitAppliesWhenUnset(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	for i := range 60 {
		if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
			ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "1", Date: "2026-08-14", Description: "tx",
		}); err != nil {
			t.Fatalf("RecordOutflow #%d: %v", i, err)
		}
	}

	result, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(result.Transactions) != 50 {
		t.Errorf("len(Transactions) = %d, want 50 (the default page size)", len(result.Transactions))
	}
}

func TestListTransactions_ExcludesSoftDeleted(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	created, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "10", Description: "gone soon",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if _, err := svc.DeleteTransaction(ctx, app.DeleteTransactionCommand{ActorID: testActorID, TransactionRef: created.Transaction.ID()}); err != nil {
		t.Fatalf("DeleteTransaction: %v", err)
	}

	result, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	for _, txn := range result.Transactions {
		if txn.ID() == created.Transaction.ID() {
			t.Error("soft-deleted transaction still appears in ListTransactions")
		}
	}
}
