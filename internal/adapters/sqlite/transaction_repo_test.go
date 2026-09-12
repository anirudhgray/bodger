package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func mustDate(t *testing.T, year int, month time.Month, day int) domain.Date {
	t.Helper()
	d, err := domain.NewDate(year, month, day)
	if err != nil {
		t.Fatalf("domain.NewDate: %v", err)
	}
	return d
}

func mustPosting(t *testing.T, id, accountID string, minor int64, currency string, categoryID *string) ledger.Posting {
	t.Helper()
	p, err := ledger.NewPosting(id, accountID, mustMoney(t, minor, currency), categoryID, 0)
	if err != nil {
		t.Fatalf("ledger.NewPosting: %v", err)
	}
	return p
}

func mustTag(t *testing.T, raw string) ledger.Tag {
	t.Helper()
	tag, err := ledger.NewTag(raw)
	if err != nil {
		t.Fatalf("ledger.NewTag: %v", err)
	}
	return tag
}

// seedAccountAndCategory creates a bank account and an expense category for
// userID, satisfying the foreign keys postings need.
func seedAccountAndCategory(t *testing.T, db *DB, userID, accountID, categoryID string) {
	t.Helper()
	ctx := context.Background()
	accRepo := NewAccountRepository(db)
	acc := mustAccount(t, accountID, userID, accountID+"-name")
	if err := accRepo.Create(ctx, userID, acc); err != nil {
		t.Fatalf("seed account %q: %v", accountID, err)
	}
	catRepo := NewCategoryRepository(db)
	cat := mustCategory(t, categoryID, userID, nil, categoryID+"-name", ledger.CategoryKindExpense)
	if err := catRepo.Create(ctx, userID, cat); err != nil {
		t.Fatalf("seed category %q: %v", categoryID, err)
	}
}

func TestTransactionRepository_CreateGet(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	repo := NewTransactionRepository(db)
	ctx := context.Background()

	catID := "cat-1"
	p := mustPosting(t, "post-1", "acc-1", -80000, "INR", &catID)
	txn, err := ledger.NewOutflow("txn-1", ports.SeededUserID, mustDate(t, 2026, time.August, 14), "Groceries", []ledger.Posting{p})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}
	tags := []ledger.Tag{mustTag(t, "reimbursable")}

	if err := repo.Create(ctx, ports.SeededUserID, txn, tags); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, gotTags, err := repo.Get(ctx, ports.SeededUserID, "txn-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Description() != "Groceries" || got.Kind() != ledger.TransactionKindOutflow {
		t.Errorf("Get = %+v", got)
	}
	if len(got.Postings()) != 1 || got.Postings()[0].Amount().AmountMinor() != -80000 {
		t.Errorf("Get postings = %+v", got.Postings())
	}
	if len(gotTags) != 1 || gotTags[0].String() != "reimbursable" {
		t.Errorf("Get tags = %+v, want [reimbursable]", gotTags)
	}
}

func TestTransactionRepository_Create_Transfer(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-a", "cat-1")
	// Second account for the transfer's other leg.
	accRepo := NewAccountRepository(db)
	accB := mustAccount(t, "acc-b", ports.SeededUserID, "acc-b-name")
	if err := accRepo.Create(context.Background(), ports.SeededUserID, accB); err != nil {
		t.Fatalf("seed acc-b: %v", err)
	}
	repo := NewTransactionRepository(db)
	ctx := context.Background()

	out := mustPosting(t, "post-out", "acc-a", -200000, "INR", nil)
	in := mustPosting(t, "post-in", "acc-b", 200000, "INR", nil)
	txn, _, err := ledger.NewTransfer("txn-transfer", ports.SeededUserID, mustDate(t, 2026, time.August, 5), "Move money", []ledger.Posting{out, in})
	if err != nil {
		t.Fatalf("NewTransfer: %v", err)
	}

	if err := repo.Create(ctx, ports.SeededUserID, txn, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, _, err := repo.Get(ctx, ports.SeededUserID, "txn-transfer")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind() != ledger.TransactionKindTransfer || len(got.Postings()) != 2 {
		t.Errorf("Get transfer = %+v", got)
	}
	if _, _, ok := got.FxRate(); ok {
		t.Errorf("FxRate() ok = true, want false for a same-currency transfer")
	}
}

// TestTransactionRepository_Create_TransferPersistsFxRate exercises issue
// #133's write-then-read round trip: a cross-currency transfer's implied
// rate, attached via ledger.Transaction.WithFxRate (the way
// internal/app.RecordTransfer attaches it), must survive Create followed
// by Get through the transactions.fx_rate_used/fx_rate_source columns.
func TestTransactionRepository_Create_TransferPersistsFxRate(t *testing.T) {
	db, _ := newTestDB(t)
	accRepo := NewAccountRepository(db)
	ctx := context.Background()
	inrAcc := mustAccountWithCurrency(t, "acc-inr", ports.SeededUserID, "HDFC Savings", "INR")
	if err := accRepo.Create(ctx, ports.SeededUserID, inrAcc); err != nil {
		t.Fatalf("seed acc-inr: %v", err)
	}
	usdAcc := mustAccountWithCurrency(t, "acc-usd", ports.SeededUserID, "Chase USD", "USD")
	if err := accRepo.Create(ctx, ports.SeededUserID, usdAcc); err != nil {
		t.Fatalf("seed acc-usd: %v", err)
	}

	repo := NewTransactionRepository(db)

	out := mustPosting(t, "post-out", "acc-inr", -2000000, "INR", nil)
	in := mustPosting(t, "post-in", "acc-usd", 23000, "USD", nil)
	txn, rate, err := ledger.NewTransfer("txn-fx-transfer", ports.SeededUserID, mustDate(t, 2026, time.August, 5), "FX transfer", []ledger.Posting{out, in})
	if err != nil {
		t.Fatalf("NewTransfer: %v", err)
	}
	txn = txn.WithFxRate(rate, ledger.FxRateSourceImplied)

	if err := repo.Create(ctx, ports.SeededUserID, txn, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, _, err := repo.Get(ctx, ports.SeededUserID, "txn-fx-transfer")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gotRate, gotSource, ok := got.FxRate()
	if !ok {
		t.Fatalf("FxRate() ok = false, want true")
	}
	if gotSource != ledger.FxRateSourceImplied {
		t.Errorf("FxRate() source = %q, want %q", gotSource, ledger.FxRateSourceImplied)
	}
	if gotRate.Base() != "INR" || gotRate.Quote() != "USD" {
		t.Errorf("FxRate() base/quote = %s/%s, want INR/USD", gotRate.Base(), gotRate.Quote())
	}
	want := decimal.RequireFromString("0.0115")
	if !gotRate.Value().Equal(want) {
		t.Errorf("FxRate() value = %s, want %s", gotRate.Value(), want)
	}

	// Guard against a false-positive: fx.DeriveImpliedRate is deterministic
	// given the two postings, so this also verifies the round trip is
	// actually reading the stored columns rather than something that would
	// coincidentally match a recomputation for any implied rate.
	var rawUsed, rawSource string
	row := db.read.QueryRowContext(ctx, `SELECT fx_rate_used, fx_rate_source FROM transactions WHERE id = ?`, "txn-fx-transfer")
	if err := row.Scan(&rawUsed, &rawSource); err != nil {
		t.Fatalf("scan raw columns: %v", err)
	}
	if rawSource != ledger.FxRateSourceImplied {
		t.Errorf("raw fx_rate_source = %q, want %q", rawSource, ledger.FxRateSourceImplied)
	}
	rawValue, err := decimal.NewFromString(rawUsed)
	if err != nil {
		t.Fatalf("parse raw fx_rate_used %q: %v", rawUsed, err)
	}
	if !rawValue.Equal(want) {
		t.Errorf("raw fx_rate_used = %s, want %s", rawValue, want)
	}
}

func TestTransactionRepository_List_FiltersDeletedAndActor(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	seedOtherUser(t, db, otherUserID)
	seedAccountAndCategory(t, db, otherUserID, "acc-other", "cat-other")

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	catID := "cat-1"
	p1 := mustPosting(t, "post-1", "acc-1", -1000, "INR", &catID)
	txn1, err := ledger.NewOutflow("txn-1", ports.SeededUserID, mustDate(t, 2026, time.August, 1), "One", []ledger.Posting{p1})
	if err != nil {
		t.Fatalf("NewOutflow txn1: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txn1, nil); err != nil {
		t.Fatalf("Create txn1: %v", err)
	}

	p2 := mustPosting(t, "post-2", "acc-1", -2000, "INR", &catID)
	txn2, err := ledger.NewOutflow("txn-2", ports.SeededUserID, mustDate(t, 2026, time.August, 2), "Two (to be deleted)", []ledger.Posting{p2})
	if err != nil {
		t.Fatalf("NewOutflow txn2: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txn2, nil); err != nil {
		t.Fatalf("Create txn2: %v", err)
	}

	// Soft-delete txn2 via Update, per ports.TransactionRepository's
	// documented contract (Delete is Update with DeletedAt set).
	deleted := txn2.Delete(testClockInstant)
	if err := repo.Update(ctx, ports.SeededUserID, deleted, nil); err != nil {
		t.Fatalf("Update (soft delete) txn2: %v", err)
	}

	otherCatID := "cat-other"
	pOther := mustPosting(t, "post-other", "acc-other", -3000, "INR", &otherCatID)
	txnOther, err := ledger.NewOutflow("txn-other", otherUserID, mustDate(t, 2026, time.August, 3), "Other user's", []ledger.Posting{pOther})
	if err != nil {
		t.Fatalf("NewOutflow txnOther: %v", err)
	}
	if err := repo.Create(ctx, otherUserID, txnOther, nil); err != nil {
		t.Fatalf("Create txnOther: %v", err)
	}

	list, err := repo.List(ctx, ports.SeededUserID, ports.TransactionFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID() != "txn-1" {
		t.Errorf("List(seeded user) = %+v, want only txn-1 (txn-2 deleted, txn-other belongs to a different user)", list)
	}

	// Get must also refuse a soft-deleted transaction.
	if _, _, err := repo.Get(ctx, ports.SeededUserID, "txn-2"); err == nil {
		t.Error("Get(soft-deleted txn-2): want NotFound, got nil")
	} else {
		var e *errs.Error
		if !errors.As(err, &e) || e.Code != errs.NotFound {
			t.Errorf("Get(soft-deleted txn-2): err = %v, want errs.NotFound", err)
		}
	}
}

func TestTransactionRepository_List_FilterByAccountAndAsOf(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	accRepo := NewAccountRepository(db)
	acc2 := mustAccount(t, "acc-2", ports.SeededUserID, "acc-2-name")
	if err := accRepo.Create(context.Background(), ports.SeededUserID, acc2); err != nil {
		t.Fatalf("seed acc-2: %v", err)
	}

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	catID := "cat-1"

	early := mustPosting(t, "post-early", "acc-1", -1000, "INR", &catID)
	txnEarly, err := ledger.NewOutflow("txn-early", ports.SeededUserID, mustDate(t, 2026, time.August, 1), "Early on acc-1", []ledger.Posting{early})
	if err != nil {
		t.Fatalf("NewOutflow txnEarly: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnEarly, nil); err != nil {
		t.Fatalf("Create txnEarly: %v", err)
	}

	late := mustPosting(t, "post-late", "acc-1", -1000, "INR", &catID)
	txnLate, err := ledger.NewOutflow("txn-late", ports.SeededUserID, mustDate(t, 2026, time.August, 20), "Late on acc-1", []ledger.Posting{late})
	if err != nil {
		t.Fatalf("NewOutflow txnLate: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnLate, nil); err != nil {
		t.Fatalf("Create txnLate: %v", err)
	}

	onOtherAccount := mustPosting(t, "post-acc2", "acc-2", -1000, "INR", nil)
	txnOtherAccount, err := ledger.NewOutflow("txn-acc2", ports.SeededUserID, mustDate(t, 2026, time.August, 10), "On acc-2", []ledger.Posting{onOtherAccount})
	if err != nil {
		t.Fatalf("NewOutflow txnOtherAccount: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnOtherAccount, nil); err != nil {
		t.Fatalf("Create txnOtherAccount: %v", err)
	}

	asOf := mustDate(t, 2026, time.August, 15)
	list, err := repo.List(ctx, ports.SeededUserID, ports.TransactionFilter{AccountID: "acc-1", ToDate: &asOf})
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if len(list) != 1 || list[0].ID() != "txn-early" {
		t.Errorf("List(acc-1, asOf=2026-08-15) = %+v, want only txn-early", list)
	}
}

func TestTransactionRepository_List_FilterByCurrency(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	repo := NewTransactionRepository(db)
	ctx := context.Background()

	inr := mustPosting(t, "post-inr", "acc-1", -1000, "INR", nil)
	txnINR, err := ledger.NewOutflow("txn-inr", ports.SeededUserID, mustDate(t, 2026, time.August, 1), "INR spend", []ledger.Posting{inr})
	if err != nil {
		t.Fatalf("NewOutflow txnINR: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnINR, nil); err != nil {
		t.Fatalf("Create txnINR: %v", err)
	}

	usd := mustPosting(t, "post-usd", "acc-1", -1000, "USD", nil)
	txnUSD, err := ledger.NewOutflow("txn-usd", ports.SeededUserID, mustDate(t, 2026, time.August, 2), "USD spend", []ledger.Posting{usd})
	if err != nil {
		t.Fatalf("NewOutflow txnUSD: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnUSD, nil); err != nil {
		t.Fatalf("Create txnUSD: %v", err)
	}

	list, err := repo.List(ctx, ports.SeededUserID, ports.TransactionFilter{Currencies: []string{"USD"}})
	if err != nil {
		t.Fatalf("List filtered by currency: %v", err)
	}
	if len(list) != 1 || list[0].ID() != "txn-usd" {
		t.Errorf("List(Currencies=[USD]) = %+v, want only txn-usd", list)
	}
}

func TestTransactionRepository_List_FilterByAmountRange(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	repo := NewTransactionRepository(db)
	ctx := context.Background()

	// 500.00 INR
	small := mustPosting(t, "post-small", "acc-1", -50000, "INR", nil)
	txnSmall, err := ledger.NewOutflow("txn-small", ports.SeededUserID, mustDate(t, 2026, time.August, 1), "Small", []ledger.Posting{small})
	if err != nil {
		t.Fatalf("NewOutflow txnSmall: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnSmall, nil); err != nil {
		t.Fatalf("Create txnSmall: %v", err)
	}

	// 5,000.00 INR
	large := mustPosting(t, "post-large", "acc-1", -500000, "INR", nil)
	txnLarge, err := ledger.NewOutflow("txn-large", ports.SeededUserID, mustDate(t, 2026, time.August, 2), "Large", []ledger.Posting{large})
	if err != nil {
		t.Fatalf("NewOutflow txnLarge: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnLarge, nil); err != nil {
		t.Fatalf("Create txnLarge: %v", err)
	}

	list, err := repo.List(ctx, ports.SeededUserID, ports.TransactionFilter{AmountMin: "1000"})
	if err != nil {
		t.Fatalf("List filtered by amount range: %v", err)
	}
	if len(list) != 1 || list[0].ID() != "txn-large" {
		t.Errorf("List(AmountMin=1000) = %+v, want only txn-large", list)
	}

	list, err = repo.List(ctx, ports.SeededUserID, ports.TransactionFilter{AmountMax: "1000"})
	if err != nil {
		t.Fatalf("List filtered by amount max: %v", err)
	}
	if len(list) != 1 || list[0].ID() != "txn-small" {
		t.Errorf("List(AmountMax=1000) = %+v, want only txn-small", list)
	}
}

func TestTransactionRepository_List_FilterByDescription(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	repo := NewTransactionRepository(db)
	ctx := context.Background()

	groceries := mustPosting(t, "post-groceries", "acc-1", -1000, "INR", nil)
	txnGroceries, err := ledger.NewOutflow("txn-groceries", ports.SeededUserID, mustDate(t, 2026, time.August, 1), "Whole Foods Groceries", []ledger.Posting{groceries})
	if err != nil {
		t.Fatalf("NewOutflow txnGroceries: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnGroceries, nil); err != nil {
		t.Fatalf("Create txnGroceries: %v", err)
	}

	rent := mustPosting(t, "post-rent", "acc-1", -1000, "INR", nil)
	txnRent, err := ledger.NewOutflow("txn-rent", ports.SeededUserID, mustDate(t, 2026, time.August, 2), "Monthly Rent", []ledger.Posting{rent})
	if err != nil {
		t.Fatalf("NewOutflow txnRent: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnRent, nil); err != nil {
		t.Fatalf("Create txnRent: %v", err)
	}

	list, err := repo.List(ctx, ports.SeededUserID, ports.TransactionFilter{Description: "groceries"})
	if err != nil {
		t.Fatalf("List filtered by description: %v", err)
	}
	if len(list) != 1 || list[0].ID() != "txn-groceries" {
		t.Errorf("List(Description=groceries) = %+v, want only txn-groceries", list)
	}
}

// TestTransactionRepository_List_FilterByExternalID exercises issue #210's
// tier-1 exact-duplicate lookup filter: ADR-0008's "(account_id,
// external_id) is unique" is checked by combining this filter with
// AccountID, so this test also confirms a same-external-id transaction on
// a different account doesn't match.
func TestTransactionRepository_List_FilterByExternalID(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-2", "cat-2")
	repo := NewTransactionRepository(db)
	ctx := context.Background()

	matched := mustPosting(t, "post-matched", "acc-1", -1000, "INR", nil)
	txnMatched, err := ledger.NewOutflow("txn-matched", ports.SeededUserID, mustDate(t, 2026, time.August, 1), "Coffee", []ledger.Posting{matched},
		ledger.WithImportProvenance("", "bank-ext-1"))
	if err != nil {
		t.Fatalf("NewOutflow txnMatched: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnMatched, nil); err != nil {
		t.Fatalf("Create txnMatched: %v", err)
	}

	// Same external ID, different account: must not match when AccountID
	// scopes the lookup to acc-1.
	otherAccount := mustPosting(t, "post-other-account", "acc-2", -1000, "INR", nil)
	txnOtherAccount, err := ledger.NewOutflow("txn-other-account", ports.SeededUserID, mustDate(t, 2026, time.August, 1), "Coffee", []ledger.Posting{otherAccount},
		ledger.WithImportProvenance("", "bank-ext-1"))
	if err != nil {
		t.Fatalf("NewOutflow txnOtherAccount: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnOtherAccount, nil); err != nil {
		t.Fatalf("Create txnOtherAccount: %v", err)
	}

	unrelated := mustPosting(t, "post-unrelated", "acc-1", -1000, "INR", nil)
	txnUnrelated, err := ledger.NewOutflow("txn-unrelated", ports.SeededUserID, mustDate(t, 2026, time.August, 2), "Rent", []ledger.Posting{unrelated})
	if err != nil {
		t.Fatalf("NewOutflow txnUnrelated: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnUnrelated, nil); err != nil {
		t.Fatalf("Create txnUnrelated: %v", err)
	}

	list, err := repo.List(ctx, ports.SeededUserID, ports.TransactionFilter{AccountID: "acc-1", ExternalID: "bank-ext-1"})
	if err != nil {
		t.Fatalf("List filtered by external id: %v", err)
	}
	if len(list) != 1 || list[0].ID() != "txn-matched" {
		t.Errorf("List(AccountID=acc-1, ExternalID=bank-ext-1) = %+v, want only txn-matched", list)
	}
}

func TestTransactionRepository_List_FilterByTags(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	repo := NewTransactionRepository(db)
	ctx := context.Background()

	vacation := mustTag(t, "vacation")
	food := mustTag(t, "food")

	both := mustPosting(t, "post-both", "acc-1", -1000, "INR", nil)
	txnBoth, err := ledger.NewOutflow("txn-both", ports.SeededUserID, mustDate(t, 2026, time.August, 1), "Both tags", []ledger.Posting{both})
	if err != nil {
		t.Fatalf("NewOutflow txnBoth: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnBoth, []ledger.Tag{vacation, food}); err != nil {
		t.Fatalf("Create txnBoth: %v", err)
	}

	vacationOnly := mustPosting(t, "post-vacation", "acc-1", -1000, "INR", nil)
	txnVacation, err := ledger.NewOutflow("txn-vacation", ports.SeededUserID, mustDate(t, 2026, time.August, 2), "Vacation only", []ledger.Posting{vacationOnly})
	if err != nil {
		t.Fatalf("NewOutflow txnVacation: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnVacation, []ledger.Tag{vacation}); err != nil {
		t.Fatalf("Create txnVacation: %v", err)
	}

	untagged := mustPosting(t, "post-untagged", "acc-1", -1000, "INR", nil)
	txnUntagged, err := ledger.NewOutflow("txn-untagged", ports.SeededUserID, mustDate(t, 2026, time.August, 3), "Untagged", []ledger.Posting{untagged})
	if err != nil {
		t.Fatalf("NewOutflow txnUntagged: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txnUntagged, nil); err != nil {
		t.Fatalf("Create txnUntagged: %v", err)
	}

	any, err := repo.List(ctx, ports.SeededUserID, ports.TransactionFilter{Tags: []string{"vacation", "food"}, TagMode: "any"})
	if err != nil {
		t.Fatalf("List TagMode=any: %v", err)
	}
	if len(any) != 2 {
		t.Errorf("List(Tags=[vacation,food], TagMode=any) = %d results, want 2", len(any))
	}

	all, err := repo.List(ctx, ports.SeededUserID, ports.TransactionFilter{Tags: []string{"vacation", "food"}, TagMode: "all"})
	if err != nil {
		t.Fatalf("List TagMode=all: %v", err)
	}
	if len(all) != 1 || all[0].ID() != "txn-both" {
		t.Errorf("List(Tags=[vacation,food], TagMode=all) = %+v, want only txn-both", all)
	}
}

func TestTransactionRepository_Update_WritesRevision(t *testing.T) {
	db, clk := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	repo := NewTransactionRepository(db)
	ctx := context.Background()

	catID := "cat-1"
	p := mustPosting(t, "post-1", "acc-1", -80000, "INR", &catID)
	txn, err := ledger.NewOutflow("txn-1", ports.SeededUserID, mustDate(t, 2026, time.August, 14), "Groceries", []ledger.Posting{p})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, txn, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	clk.Advance(time.Hour)
	editedPosting := mustPosting(t, "post-1", "acc-1", -90000, "INR", &catID)
	edited, err := ledger.NewOutflow("txn-1", ports.SeededUserID, mustDate(t, 2026, time.August, 14), "Groceries (corrected)", []ledger.Posting{editedPosting})
	if err != nil {
		t.Fatalf("NewOutflow edited: %v", err)
	}
	if err := repo.Update(ctx, ports.SeededUserID, edited, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _, err := repo.Get(ctx, ports.SeededUserID, "txn-1")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Description() != "Groceries (corrected)" || got.Postings()[0].Amount().AmountMinor() != -90000 {
		t.Errorf("Get after update = %+v", got)
	}

	var revisionCount int
	var previousState string
	err = db.write.QueryRowContext(ctx, `
		SELECT count(*), previous_state FROM transaction_revisions WHERE transaction_id = ?
	`, "txn-1").Scan(&revisionCount, &previousState)
	if err != nil {
		t.Fatalf("query transaction_revisions: %v", err)
	}
	if revisionCount != 1 {
		t.Errorf("revisionCount = %d, want 1", revisionCount)
	}
	if previousState == "" {
		t.Error("previous_state is empty, want the pre-edit snapshot")
	}
}

func TestTransactionRepository_Update_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	repo := NewTransactionRepository(db)

	p := mustPosting(t, "post-1", "acc-1", -1000, "INR", nil)
	txn, err := ledger.NewOutflow("does-not-exist", ports.SeededUserID, mustDate(t, 2026, time.August, 14), "Ghost", []ledger.Posting{p})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}

	err = repo.Update(context.Background(), ports.SeededUserID, txn, nil)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("Update(missing): err = %v, want errs.NotFound", err)
	}
}

func TestTransactionRepository_Create_RejectsActorMismatch(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	repo := NewTransactionRepository(db)

	p := mustPosting(t, "post-1", "acc-1", -1000, "INR", nil)
	txn, err := ledger.NewOutflow("txn-1", ports.SeededUserID, mustDate(t, 2026, time.August, 14), "Groceries", []ledger.Posting{p})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}

	err = repo.Create(context.Background(), otherUserID, txn, nil)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Errorf("Create with mismatched actor: err = %v, want errs.NotAllowed", err)
	}
}
