package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestGetTransaction(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	groceries := mustCategoryFixture(t, svc, "Groceries", "expense")

	created, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "800",
		CategoryRef: groceries.Category.ID(), Date: "2026-08-14", Description: "Groceries",
		Tags: []string{"food"},
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	got, err := svc.GetTransaction(ctx, app.GetTransactionQuery{ActorID: testActorID, TransactionRef: created.Transaction.ID()})
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if got.Transaction.ID() != created.Transaction.ID() {
		t.Errorf("GetTransaction ID = %q, want %q", got.Transaction.ID(), created.Transaction.ID())
	}
	if len(got.Tags) != 1 || got.Tags[0].String() != "food" {
		t.Errorf("GetTransaction Tags = %+v, want [food]", got.Tags)
	}
}

func TestGetTransaction_UnknownRef(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.GetTransaction(ctx, app.GetTransactionQuery{ActorID: testActorID, TransactionRef: "nonexistent"})
	wantErrCode(t, err, errs.NotFound)
}

func TestGetTransaction_RequiresActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.GetTransaction(ctx, app.GetTransactionQuery{TransactionRef: "whatever"})
	wantErrCode(t, err, errs.InvalidInput)
}
