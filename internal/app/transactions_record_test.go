package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func mustAccountFixture(t *testing.T, svc *app.Service, name, kind, currency string) app.AccountResult {
	t.Helper()
	result, err := svc.CreateAccount(context.Background(), app.CreateAccountCommand{
		ActorID: testActorID, Name: name, Kind: kind, Currency: currency,
	})
	if err != nil {
		t.Fatalf("CreateAccount(%q): %v", name, err)
	}
	return result
}

func mustCategoryFixture(t *testing.T, svc *app.Service, name, kind string) app.CategoryResult {
	t.Helper()
	result, err := svc.CreateCategory(context.Background(), app.CreateCategoryCommand{
		ActorID: testActorID, Name: name, Kind: kind,
	})
	if err != nil {
		t.Fatalf("CreateCategory(%q): %v", name, err)
	}
	return result
}

func TestRecordOutflow(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	acc := mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")
	cat := mustCategoryFixture(t, svc, "Groceries", "expense")

	result, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID:     testActorID,
		AccountRef:  acc.Account.ID(),
		Amount:      "800",
		CategoryRef: cat.Category.ID(),
		Date:        "2026-08-14",
		Description: "More & More",
		Tags:        []string{"#groceries", "Weekly"},
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if result.Transaction.Kind() != "outflow" {
		t.Errorf("Kind = %s, want outflow", result.Transaction.Kind())
	}
	postings := result.Transaction.Postings()
	if len(postings) != 1 {
		t.Fatalf("len(Postings) = %d, want 1", len(postings))
	}
	if postings[0].Amount().AmountMinor() != -80000 {
		t.Errorf("Amount = %d, want -80000", postings[0].Amount().AmountMinor())
	}
	if len(result.Tags) != 2 {
		t.Errorf("len(Tags) = %d, want 2", len(result.Tags))
	}
}

func TestRecordOutflow_NegativeInputStillNegative(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	result, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "-50", Description: "Coffee",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if result.Transaction.Postings()[0].Amount().AmountMinor() != -5000 {
		t.Errorf("Amount = %d, want -5000", result.Transaction.Postings()[0].Amount().AmountMinor())
	}
}

func TestRecordOutflow_EmptyDateResolvesToToday(t *testing.T) {
	frozen := time.Date(2026, time.July, 31, 18, 45, 0, 0, time.UTC) // 2026-08-01 in Asia/Kolkata
	svc := newTestService(t, frozen, "Asia/Kolkata")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "INR")

	result, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Description: "Tea",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if got := result.Transaction.BookedDate().String(); got != "2026-08-01" {
		t.Errorf("BookedDate = %s, want 2026-08-01 (the hostile-instant zone check)", got)
	}
}

func TestRecordOutflow_ZeroAmountRejected(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	_, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "0", Description: "Nothing",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestRecordOutflow_EmptyDescriptionRejected(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	_, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "10", Description: "   ",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestRecordOutflow_UnknownAccountRef(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.RecordOutflow(context.Background(), app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: "does-not-exist", Amount: "10", Description: "Ghost",
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestRecordOutflow_CurrencyPrecedenceFallsBackToAccount(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")

	result, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Description: "Chai",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if result.Transaction.Postings()[0].Currency() != "INR" {
		t.Errorf("Currency = %s, want INR (from the account, since Currency was empty)", result.Transaction.Postings()[0].Currency())
	}
}

// TestRecordOutflow_CurrencyPrecedence_AccountStillWinsOverReportingCurrency
// pins down ADR-0004's ladder order at this call site now that the "user"
// rung is wired in (issue #132): an account always has its own currency,
// so the account rung wins over the actor's reporting currency exactly as
// it won before this issue existed - the "no behaviour change for a fresh
// install" guarantee, extended to actors who *have* set a reporting
// currency too.
func TestRecordOutflow_CurrencyPrecedence_AccountStillWinsOverReportingCurrency(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	if err := svc.SetReportingCurrency(ctx, testActorID, "GBP"); err != nil {
		t.Fatalf("SetReportingCurrency: %v", err)
	}

	result, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "100", Description: "Chai",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if result.Transaction.Postings()[0].Currency() != "INR" {
		t.Errorf("Currency = %s, want INR (the account still wins over the actor's reporting currency)", result.Transaction.Postings()[0].Currency())
	}
}

func TestRecordInflow(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")
	cat := mustCategoryFixture(t, svc, "Salary", "income")

	result, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID:     testActorID,
		AccountRef:  acc.Account.ID(),
		Amount:      "150000",
		CategoryRef: cat.Category.ID(),
		Date:        "2026-08-01",
		Description: "Acme Corp salary",
	})
	if err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	if result.Transaction.Postings()[0].Amount().AmountMinor() != 15000000 {
		t.Errorf("Amount = %d, want 15000000", result.Transaction.Postings()[0].Amount().AmountMinor())
	}
}

func TestRecordInflow_NegativeInputBecomesPositive(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Cash", "cash", "USD")

	result, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "-20", Description: "Refund",
	})
	if err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}
	if result.Transaction.Postings()[0].Amount().AmountMinor() != 2000 {
		t.Errorf("Amount = %d, want 2000", result.Transaction.Postings()[0].Amount().AmountMinor())
	}
}

// TestRecordInflow_CanUseAnExpenseCategory proves refunds work: an inflow
// posting is not constrained to income-kind categories, because a refund's
// posting carries the *same* category as the original outflow it refunds
// (data-model.md §7) — an expense category, on an inflow transaction.
func TestRecordInflow_CanUseAnExpenseCategory(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")
	clothing := mustCategoryFixture(t, svc, "Clothing", "expense")

	result, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "2000",
		CategoryRef: clothing.Category.ID(), Description: "Shirt refund",
	})
	if err != nil {
		t.Fatalf("RecordInflow with an expense category: %v", err)
	}
	categoryID, ok := result.Transaction.Postings()[0].CategoryID()
	if !ok || categoryID != clothing.Category.ID() {
		t.Errorf("CategoryID = %q, %v, want %q, true", categoryID, ok, clothing.Category.ID())
	}
}

func TestRecordOutflow_CrossActorAccountRefIsInvisible(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC", "bank", "INR")

	_, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: "someone-else", AccountRef: acc.Account.ID(), Amount: "10", Description: "Not mine",
	})
	wantErrCode(t, err, errs.NotFound)
}
