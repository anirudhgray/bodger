package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
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

func TestRecordTransfer_CrossCurrencyDerivesAndPersistsImpliedRate(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	inr := mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")
	usd := mustAccountFixture(t, svc, "Chase USD", "bank", "USD")

	result, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: inr.Account.ID(), ToAccountRef: usd.Account.ID(),
		Amount: "20000", Description: "Cross-currency",
	})
	if err != nil {
		t.Fatalf("RecordTransfer: %v", err)
	}
	if result.Transaction.Kind() != ledger.TransactionKindTransfer {
		t.Errorf("Kind = %s, want transfer", result.Transaction.Kind())
	}

	postings := result.Transaction.Postings()
	if len(postings) != 2 {
		t.Fatalf("len(Postings) = %d, want 2", len(postings))
	}
	for _, p := range postings {
		switch p.AccountID() {
		case inr.Account.ID():
			if p.Currency() != "INR" || p.Amount().AmountMinor() != -2000000 {
				t.Errorf("from-account posting = %s %d, want -2000000 INR", p.Currency(), p.Amount().AmountMinor())
			}
		case usd.Account.ID():
			if p.Currency() != "USD" || p.Amount().AmountMinor() != 2000000 {
				t.Errorf("to-account posting = %s %d, want 2000000 USD", p.Currency(), p.Amount().AmountMinor())
			}
		default:
			t.Errorf("unexpected posting account %q", p.AccountID())
		}
	}

	rate, source, ok := result.Transaction.FxRate()
	if !ok {
		t.Fatalf("FxRate() ok = false, want true for a cross-currency transfer")
	}
	if source != ledger.FxRateSourceImplied {
		t.Errorf("FxRate() source = %q, want %q", source, ledger.FxRateSourceImplied)
	}
	if rate.Base() != "INR" || rate.Quote() != "USD" {
		t.Errorf("FxRate() base/quote = %s/%s, want INR/USD", rate.Base(), rate.Quote())
	}
	// ₹20,000 out, $20,000 in (same raw amount reused across the two
	// currencies, per buildTransferPostings' doc comment — there is no
	// separate to-amount field yet): 20000/20000 = 1.
	want := decimal.RequireFromString("1")
	if !rate.Value().Equal(want) {
		t.Errorf("FxRate() value = %s, want %s", rate.Value(), want)
	}
}

func TestRecordTransfer_SameCurrencyHasNoPersistedFxRate(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	savings := mustAccountFixture(t, svc, "Savings", "bank", "INR")
	checking := mustAccountFixture(t, svc, "Checking", "bank", "INR")

	result, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: savings.Account.ID(), ToAccountRef: checking.Account.ID(),
		Amount: "500", Description: "Same-currency",
	})
	if err != nil {
		t.Fatalf("RecordTransfer: %v", err)
	}
	if _, _, ok := result.Transaction.FxRate(); ok {
		t.Errorf("FxRate() ok = true, want false for a same-currency transfer")
	}
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
