package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestRecordTransfer(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	savings := mustAccountFixture(t, svc, "Savings", "bank", "INR")
	checking := mustAccountFixture(t, svc, "Checking", "bank", "INR")

	result, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID:        testActorID,
		FromAccountRef: savings.Account.ID(),
		ToAccountRef:   checking.Account.ID(),
		Amount:         "20000",
		Date:           "2026-08-05",
		Description:    "Move to spending account",
	})
	if err != nil {
		t.Fatalf("RecordTransfer: %v", err)
	}
	if result.Transaction.Kind() != "transfer" {
		t.Errorf("Kind = %s, want transfer", result.Transaction.Kind())
	}
	postings := result.Transaction.Postings()
	if len(postings) != 2 {
		t.Fatalf("len(Postings) = %d, want 2", len(postings))
	}

	var sawOut, sawIn bool
	for _, p := range postings {
		amt := p.Amount().AmountMinor()
		switch p.AccountID() {
		case savings.Account.ID():
			sawOut = true
			if amt != -2000000 {
				t.Errorf("savings posting amount = %d, want -2000000", amt)
			}
		case checking.Account.ID():
			sawIn = true
			if amt != 2000000 {
				t.Errorf("checking posting amount = %d, want 2000000", amt)
			}
		default:
			t.Errorf("unexpected posting account %q", p.AccountID())
		}
		if _, ok := p.CategoryID(); ok {
			t.Error("a transfer posting must not carry a category")
		}
	}
	if !sawOut || !sawIn {
		t.Errorf("sawOut=%v sawIn=%v, want both true", sawOut, sawIn)
	}
}

func TestRecordTransfer_SameAccountRejected(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Savings", "bank", "INR")

	_, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: acc.Account.ID(), ToAccountRef: acc.Account.ID(),
		Amount: "100", Description: "Nonsense",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestRecordTransfer_CrossCurrencyRejected(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	inr := mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")
	usd := mustAccountFixture(t, svc, "Chase USD", "bank", "USD")

	_, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: inr.Account.ID(), ToAccountRef: usd.Account.ID(),
		Amount: "20000", Description: "Cross-currency",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestRecordTransfer_ZeroAmountRejected(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	a := mustAccountFixture(t, svc, "A", "bank", "USD")
	b := mustAccountFixture(t, svc, "B", "bank", "USD")

	_, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: a.Account.ID(), ToAccountRef: b.Account.ID(),
		Amount: "0", Description: "Nothing",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestRecordTransfer_NegativeAmountTreatedAsMagnitude(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	a := mustAccountFixture(t, svc, "A", "bank", "USD")
	b := mustAccountFixture(t, svc, "B", "bank", "USD")

	result, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: a.Account.ID(), ToAccountRef: b.Account.ID(),
		Amount: "-50", Description: "Move",
	})
	if err != nil {
		t.Fatalf("RecordTransfer: %v", err)
	}
	for _, p := range result.Transaction.Postings() {
		if p.AccountID() == a.Account.ID() && p.Amount().AmountMinor() != -5000 {
			t.Errorf("from-account amount = %d, want -5000", p.Amount().AmountMinor())
		}
		if p.AccountID() == b.Account.ID() && p.Amount().AmountMinor() != 5000 {
			t.Errorf("to-account amount = %d, want 5000", p.Amount().AmountMinor())
		}
	}
}
