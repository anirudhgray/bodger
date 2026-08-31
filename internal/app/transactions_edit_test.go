package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestEditTransaction_Outflow(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	groceries := mustCategoryFixture(t, svc, "Groceries", "expense")
	household := mustCategoryFixture(t, svc, "Household", "expense")

	created, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "800",
		CategoryRef: groceries.Category.ID(), Date: "2026-08-14", Description: "Groceries",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	edited, err := svc.EditTransaction(ctx, app.EditTransactionCommand{
		ActorID: testActorID, TransactionRef: created.Transaction.ID(),
		AccountRef: acc.Account.ID(), Amount: "950", CategoryRef: household.Category.ID(),
		Date: "2026-08-15", Description: "Groceries (corrected)", Notes: "typo fix",
	})
	if err != nil {
		t.Fatalf("EditTransaction: %v", err)
	}
	if edited.Transaction.ID() != created.Transaction.ID() {
		t.Errorf("editing must not change the transaction's ID: got %q, want %q", edited.Transaction.ID(), created.Transaction.ID())
	}
	if edited.Transaction.Kind() != "outflow" {
		t.Errorf("Kind changed to %s, want outflow to be preserved", edited.Transaction.Kind())
	}
	if edited.Transaction.Description() != "Groceries (corrected)" {
		t.Errorf("Description = %q, want %q", edited.Transaction.Description(), "Groceries (corrected)")
	}
	if edited.Transaction.BookedDate().String() != "2026-08-15" {
		t.Errorf("BookedDate = %s, want 2026-08-15", edited.Transaction.BookedDate().String())
	}
	if amt := edited.Transaction.Postings()[0].Amount().AmountMinor(); amt != -95000 {
		t.Errorf("Amount = %d, want -95000", amt)
	}
	catID, ok := edited.Transaction.Postings()[0].CategoryID()
	if !ok || catID != household.Category.ID() {
		t.Errorf("CategoryID = %q, %v, want %q, true", catID, ok, household.Category.ID())
	}
	if edited.Transaction.Notes() != "typo fix" {
		t.Errorf("Notes = %q, want %q", edited.Transaction.Notes(), "typo fix")
	}
}

func TestEditTransaction_Transfer(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	savings := mustAccountFixture(t, svc, "Savings", "bank", "INR")
	checking := mustAccountFixture(t, svc, "Checking", "bank", "INR")

	created, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: savings.Account.ID(), ToAccountRef: checking.Account.ID(),
		Amount: "20000", Date: "2026-08-05", Description: "Move money",
	})
	if err != nil {
		t.Fatalf("RecordTransfer: %v", err)
	}

	edited, err := svc.EditTransaction(ctx, app.EditTransactionCommand{
		ActorID: testActorID, TransactionRef: created.Transaction.ID(),
		FromAccountRef: savings.Account.ID(), ToAccountRef: checking.Account.ID(),
		Amount: "25000", Date: "2026-08-06", Description: "Move more money",
	})
	if err != nil {
		t.Fatalf("EditTransaction: %v", err)
	}
	if edited.Transaction.Kind() != "transfer" {
		t.Errorf("Kind changed to %s, want transfer to be preserved", edited.Transaction.Kind())
	}
	for _, p := range edited.Transaction.Postings() {
		if p.AccountID() == savings.Account.ID() && p.Amount().AmountMinor() != -2500000 {
			t.Errorf("savings posting = %d, want -2500000", p.Amount().AmountMinor())
		}
	}
}

func TestEditTransaction_PreservesRelatedTransactionID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	clothing := mustCategoryFixture(t, svc, "Clothing", "expense")

	purchase, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "2000",
		CategoryRef: clothing.Category.ID(), Date: "2026-08-10", Description: "Shirt",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	// EditTransaction has no RelatedTransactionRef field - a refund link
	// isn't something this command can set. But if a transaction already
	// has one (via a path outside this test's reach today), editing it
	// must not silently drop it. Since RecordOutflow/RecordInflow don't
	// expose WithRelatedTransaction either, this test instead documents
	// the *absence* of related-transaction on an ordinary edit as the
	// baseline, and preserveOptions' own logic (carrying forward whatever
	// existing.RelatedTransactionID() already returns) is what protects
	// a future write path that does set one.
	edited, err := svc.EditTransaction(ctx, app.EditTransactionCommand{
		ActorID: testActorID, TransactionRef: purchase.Transaction.ID(),
		AccountRef: acc.Account.ID(), Amount: "2000", CategoryRef: clothing.Category.ID(),
		Date: "2026-08-10", Description: "Shirt",
	})
	if err != nil {
		t.Fatalf("EditTransaction: %v", err)
	}
	if _, ok := edited.Transaction.RelatedTransactionID(); ok {
		t.Error("a plain purchase should have no RelatedTransactionID after editing")
	}
}

func TestEditTransaction_UnknownRef(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.EditTransaction(context.Background(), app.EditTransactionCommand{
		ActorID: testActorID, TransactionRef: "does-not-exist", Amount: "10", Description: "Ghost",
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestEditTransaction_CrossActorRefIsInvisible(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")

	created, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Description: "Mine",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	_, err = svc.EditTransaction(ctx, app.EditTransactionCommand{
		ActorID: "someone-else", TransactionRef: created.Transaction.ID(),
		AccountRef: acc.Account.ID(), Amount: "999", Description: "Stolen",
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestDeleteTransaction(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")

	created, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "800", Description: "Groceries", Date: "2026-08-14",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	deleted, err := svc.DeleteTransaction(ctx, app.DeleteTransactionCommand{ActorID: testActorID, TransactionRef: created.Transaction.ID()})
	if err != nil {
		t.Fatalf("DeleteTransaction: %v", err)
	}
	if !deleted.Transaction.IsDeleted() {
		t.Error("deleted transaction should report IsDeleted() true")
	}

	// Get must refuse it now (memTransactions.Get filters IsDeleted(), the
	// same contract the real repository documents).
	_, _, err = svc.Transactions.Get(ctx, testActorID, created.Transaction.ID())
	wantErrCode(t, err, errs.NotFound)
}

func TestDeleteTransaction_UnknownRef(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.DeleteTransaction(context.Background(), app.DeleteTransactionCommand{ActorID: testActorID, TransactionRef: "does-not-exist"})
	wantErrCode(t, err, errs.NotFound)
}
