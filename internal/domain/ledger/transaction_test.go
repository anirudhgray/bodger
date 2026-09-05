package ledger_test

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
)

func mustPosting(t *testing.T, id, accountID string, amountMinor int64, currency string, categoryID *string) ledger.Posting {
	t.Helper()
	p, err := ledger.NewPosting(id, accountID, mustMoney(t, amountMinor, currency), categoryID, 0)
	if err != nil {
		t.Fatalf("NewPosting() = %v, want success", err)
	}
	return p
}

func TestNewOutflow(t *testing.T) {
	t.Parallel()

	bookedDate := mustDate(t, 2026, 8, 14)

	t.Run("a simple expense", func(t *testing.T) {
		t.Parallel()
		groceries := "cat-groceries"
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-hdfc", -80000, "INR", &groceries),
		}
		tx, err := ledger.NewOutflow("tx-1", "user-1", bookedDate, "More & More", postings)
		if err != nil {
			t.Fatalf("NewOutflow() = %v, want success", err)
		}
		if tx.Kind() != ledger.TransactionKindOutflow {
			t.Errorf("Kind() = %s, want outflow", tx.Kind())
		}
		if tx.ID() != "tx-1" || tx.UserID() != "user-1" {
			t.Errorf("ID()/UserID() = %q/%q, want tx-1/user-1", tx.ID(), tx.UserID())
		}
		total, err := tx.Total()
		if err != nil {
			t.Fatalf("Total() = %v, want success", err)
		}
		if !total.Equal(mustMoney(t, -80000, "INR")) {
			t.Errorf("Total() = %s, want -800.00 INR", total)
		}
	})

	t.Run("a split sums to the entered amount with no stored total", func(t *testing.T) {
		t.Parallel()
		groceries, household, personal := "cat-groceries", "cat-household", "cat-personal"
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-hdfc", -350000, "INR", &groceries),
			mustPosting(t, "post-2", "acc-hdfc", -100000, "INR", &household),
			mustPosting(t, "post-3", "acc-hdfc", -50000, "INR", &personal),
		}
		tx, err := ledger.NewOutflow("tx-1", "user-1", bookedDate, "More & More", postings)
		if err != nil {
			t.Fatalf("NewOutflow() = %v, want success", err)
		}
		total, err := tx.Total()
		if err != nil {
			t.Fatalf("Total() = %v, want success", err)
		}
		if !total.Equal(mustMoney(t, -500000, "INR")) {
			t.Errorf("Total() = %s, want -5000.00 INR (the sum of the split's own postings)", total)
		}
		if got := len(tx.Postings()); got != 3 {
			t.Errorf("len(Postings()) = %d, want 3", got)
		}
	})

	t.Run("rejects a positive posting", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-hdfc", 80000, "INR", nil),
		}
		_, err := ledger.NewOutflow("tx-1", "user-1", bookedDate, "Oops", postings)
		if !errors.Is(err, ledger.ErrOutflowPostingNotNegative) {
			t.Fatalf("NewOutflow(positive posting) error = %v, want ErrOutflowPostingNotNegative", err)
		}
	})

	t.Run("rejects a zero posting", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-hdfc", 0, "INR", nil),
		}
		_, err := ledger.NewOutflow("tx-1", "user-1", bookedDate, "Oops", postings)
		if !errors.Is(err, ledger.ErrOutflowPostingNotNegative) {
			t.Fatalf("NewOutflow(zero posting) error = %v, want ErrOutflowPostingNotNegative", err)
		}
	})

	t.Run("rejects postings on more than one account", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-hdfc", -50000, "INR", nil),
			mustPosting(t, "post-2", "acc-icici", -50000, "INR", nil),
		}
		_, err := ledger.NewOutflow("tx-1", "user-1", bookedDate, "Oops", postings)
		if !errors.Is(err, ledger.ErrPostingsMultipleAccounts) {
			t.Fatalf("NewOutflow(multi-account) error = %v, want ErrPostingsMultipleAccounts", err)
		}
	})

	t.Run("rejects an empty description", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{mustPosting(t, "post-1", "acc-hdfc", -100, "USD", nil)}
		_, err := ledger.NewOutflow("tx-1", "user-1", bookedDate, "", postings)
		if !errors.Is(err, ledger.ErrTransactionEmptyDescription) {
			t.Fatalf("NewOutflow(empty description) error = %v, want ErrTransactionEmptyDescription", err)
		}
	})

	t.Run("rejects an empty id", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{mustPosting(t, "post-1", "acc-hdfc", -100, "USD", nil)}
		_, err := ledger.NewOutflow("", "user-1", bookedDate, "Coffee", postings)
		if !errors.Is(err, ledger.ErrTransactionEmptyID) {
			t.Fatalf("NewOutflow(empty id) error = %v, want ErrTransactionEmptyID", err)
		}
	})

	t.Run("rejects an empty user id", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{mustPosting(t, "post-1", "acc-hdfc", -100, "USD", nil)}
		_, err := ledger.NewOutflow("tx-1", "", bookedDate, "Coffee", postings)
		if !errors.Is(err, ledger.ErrTransactionEmptyUserID) {
			t.Fatalf("NewOutflow(empty user id) error = %v, want ErrTransactionEmptyUserID", err)
		}
	})

	t.Run("rejects no postings", func(t *testing.T) {
		t.Parallel()
		_, err := ledger.NewOutflow("tx-1", "user-1", bookedDate, "Coffee", nil)
		if !errors.Is(err, ledger.ErrTransactionNoPostings) {
			t.Fatalf("NewOutflow(no postings) error = %v, want ErrTransactionNoPostings", err)
		}
	})
}

func TestNewInflow(t *testing.T) {
	t.Parallel()

	bookedDate := mustDate(t, 2026, 8, 1)

	t.Run("a simple salary credit", func(t *testing.T) {
		t.Parallel()
		salary := "cat-salary"
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-hdfc", 15000000, "INR", &salary),
		}
		tx, err := ledger.NewInflow("tx-1", "user-1", bookedDate, "Acme Corp salary", postings)
		if err != nil {
			t.Fatalf("NewInflow() = %v, want success", err)
		}
		if tx.Kind() != ledger.TransactionKindInflow {
			t.Errorf("Kind() = %s, want inflow", tx.Kind())
		}
	})

	t.Run("rejects a negative posting", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{mustPosting(t, "post-1", "acc-hdfc", -100, "USD", nil)}
		_, err := ledger.NewInflow("tx-1", "user-1", bookedDate, "Oops", postings)
		if !errors.Is(err, ledger.ErrInflowPostingNotPositive) {
			t.Fatalf("NewInflow(negative posting) error = %v, want ErrInflowPostingNotPositive", err)
		}
	})

	t.Run("rejects a zero posting", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{mustPosting(t, "post-1", "acc-hdfc", 0, "USD", nil)}
		_, err := ledger.NewInflow("tx-1", "user-1", bookedDate, "Oops", postings)
		if !errors.Is(err, ledger.ErrInflowPostingNotPositive) {
			t.Fatalf("NewInflow(zero posting) error = %v, want ErrInflowPostingNotPositive", err)
		}
	})

	t.Run("rejects postings on more than one account", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-hdfc", 5000, "USD", nil),
			mustPosting(t, "post-2", "acc-icici", 5000, "USD", nil),
		}
		_, err := ledger.NewInflow("tx-1", "user-1", bookedDate, "Oops", postings)
		if !errors.Is(err, ledger.ErrPostingsMultipleAccounts) {
			t.Fatalf("NewInflow(multi-account) error = %v, want ErrPostingsMultipleAccounts", err)
		}
	})
}

func TestNewTransfer(t *testing.T) {
	t.Parallel()

	bookedDate := mustDate(t, 2026, 8, 5)

	t.Run("a same-currency transfer balances to zero", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-savings", -2000000, "INR", nil),
			mustPosting(t, "post-2", "acc-checking", 2000000, "INR", nil),
		}
		tx, rate, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "Move to spending account", postings)
		if err != nil {
			t.Fatalf("NewTransfer() = %v, want success", err)
		}
		if tx.Kind() != ledger.TransactionKindTransfer {
			t.Errorf("Kind() = %s, want transfer", tx.Kind())
		}
		for _, p := range tx.Postings() {
			if _, ok := p.CategoryID(); ok {
				t.Errorf("posting %s has a category, want none on a transfer", p.ID())
			}
		}
		if !rate.IsIdentity() {
			t.Errorf("rate = %s %s/%s, want the trivial 1:1 identity rate", rate, rate.Quote(), rate.Base())
		}
		if rate.Base() != "INR" || rate.Quote() != "INR" {
			t.Errorf("rate base/quote = %s/%s, want INR/INR", rate.Base(), rate.Quote())
		}
	})

	t.Run("order of legs doesn't matter", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-checking", 2000000, "INR", nil),
			mustPosting(t, "post-2", "acc-savings", -2000000, "INR", nil),
		}
		if _, _, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "Move money", postings); err != nil {
			t.Fatalf("NewTransfer() = %v, want success", err)
		}
	})

	t.Run("rejects anything other than two postings", func(t *testing.T) {
		t.Parallel()
		one := []ledger.Posting{mustPosting(t, "post-1", "acc-savings", -100, "USD", nil)}
		if _, _, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "Oops", one); !errors.Is(err, ledger.ErrTransferPostingCount) {
			t.Fatalf("NewTransfer(1 posting) error = %v, want ErrTransferPostingCount", err)
		}

		three := []ledger.Posting{
			mustPosting(t, "post-1", "acc-a", -100, "USD", nil),
			mustPosting(t, "post-2", "acc-b", 50, "USD", nil),
			mustPosting(t, "post-3", "acc-c", 50, "USD", nil),
		}
		if _, _, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "Oops", three); !errors.Is(err, ledger.ErrTransferPostingCount) {
			t.Fatalf("NewTransfer(3 postings) error = %v, want ErrTransferPostingCount", err)
		}
	})

	t.Run("rejects both postings on the same account", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-savings", -100, "USD", nil),
			mustPosting(t, "post-2", "acc-savings", 100, "USD", nil),
		}
		_, _, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "Oops", postings)
		if !errors.Is(err, ledger.ErrTransferSameAccount) {
			t.Fatalf("NewTransfer(same account) error = %v, want ErrTransferSameAccount", err)
		}
	})

	t.Run("rejects a category on either leg", func(t *testing.T) {
		t.Parallel()
		cat := "cat-whatever"

		fromHasCategory := []ledger.Posting{
			mustPosting(t, "post-1", "acc-savings", -100, "USD", &cat),
			mustPosting(t, "post-2", "acc-checking", 100, "USD", nil),
		}
		if _, _, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "Oops", fromHasCategory); !errors.Is(err, ledger.ErrTransferPostingHasCategory) {
			t.Fatalf("NewTransfer(from has category) error = %v, want ErrTransferPostingHasCategory", err)
		}

		toHasCategory := []ledger.Posting{
			mustPosting(t, "post-1", "acc-savings", -100, "USD", nil),
			mustPosting(t, "post-2", "acc-checking", 100, "USD", &cat),
		}
		if _, _, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "Oops", toHasCategory); !errors.Is(err, ledger.ErrTransferPostingHasCategory) {
			t.Fatalf("NewTransfer(to has category) error = %v, want ErrTransferPostingHasCategory", err)
		}
	})

	t.Run("rejects postings with the same sign", func(t *testing.T) {
		t.Parallel()
		bothNegative := []ledger.Posting{
			mustPosting(t, "post-1", "acc-savings", -100, "USD", nil),
			mustPosting(t, "post-2", "acc-checking", -100, "USD", nil),
		}
		if _, _, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "Oops", bothNegative); !errors.Is(err, ledger.ErrTransferPostingsMustOppose) {
			t.Fatalf("NewTransfer(both negative) error = %v, want ErrTransferPostingsMustOppose", err)
		}

		bothPositive := []ledger.Posting{
			mustPosting(t, "post-1", "acc-savings", 100, "USD", nil),
			mustPosting(t, "post-2", "acc-checking", 100, "USD", nil),
		}
		if _, _, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "Oops", bothPositive); !errors.Is(err, ledger.ErrTransferPostingsMustOppose) {
			t.Fatalf("NewTransfer(both positive) error = %v, want ErrTransferPostingsMustOppose", err)
		}
	})

	t.Run("rejects a zero-amount leg", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-savings", 0, "USD", nil),
			mustPosting(t, "post-2", "acc-checking", 0, "USD", nil),
		}
		_, _, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "Oops", postings)
		if !errors.Is(err, ledger.ErrTransferPostingsMustOppose) {
			t.Fatalf("NewTransfer(zero amounts) error = %v, want ErrTransferPostingsMustOppose", err)
		}
	})

	t.Run("rejects a same-currency transfer that doesn't balance", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-savings", -100000, "USD", nil),
			mustPosting(t, "post-2", "acc-checking", 90000, "USD", nil),
		}
		_, _, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "Oops", postings)
		if !errors.Is(err, ledger.ErrTransferNotBalanced) {
			t.Fatalf("NewTransfer(unbalanced) error = %v, want ErrTransferNotBalanced", err)
		}
	})

	t.Run("accepts a cross-currency transfer and derives the implied rate", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-hdfc-inr", -2000000, "INR", nil),
			mustPosting(t, "post-2", "acc-chase-usd", 23000, "USD", nil),
		}
		tx, rate, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "FX transfer", postings)
		if err != nil {
			t.Fatalf("NewTransfer(cross-currency) = %v, want success", err)
		}
		if tx.Kind() != ledger.TransactionKindTransfer {
			t.Errorf("Kind() = %s, want transfer", tx.Kind())
		}
		// ₹20,000 out, $230 in: 230/20000 = 0.0115 USD per INR.
		if rate.Base() != "INR" || rate.Quote() != "USD" {
			t.Fatalf("rate base/quote = %s/%s, want INR/USD", rate.Base(), rate.Quote())
		}
		want := decimal.RequireFromString("0.0115")
		if !rate.Value().Equal(want) {
			t.Errorf("rate value = %s, want %s", rate.Value(), want)
		}
	})

	t.Run("order of legs doesn't matter for a cross-currency transfer either", func(t *testing.T) {
		t.Parallel()
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-chase-usd", 23000, "USD", nil),
			mustPosting(t, "post-2", "acc-hdfc-inr", -2000000, "INR", nil),
		}
		_, rate, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "FX transfer", postings)
		if err != nil {
			t.Fatalf("NewTransfer(cross-currency, reordered legs) = %v, want success", err)
		}
		want := decimal.RequireFromString("0.0115")
		if !rate.Value().Equal(want) || rate.Base() != "INR" || rate.Quote() != "USD" {
			t.Errorf("rate = %s %s/%s, want 0.0115 INR/USD regardless of posting order", rate, rate.Base(), rate.Quote())
		}
	})

	t.Run("a cross-currency transfer's legs need not sum to zero", func(t *testing.T) {
		t.Parallel()
		// These two amounts would fail ErrTransferNotBalanced if summed
		// as same-currency minor units — cross-currency deliberately
		// suspends that rule (data-model.md §3/§5).
		postings := []ledger.Posting{
			mustPosting(t, "post-1", "acc-hdfc-inr", -2000000, "INR", nil),
			mustPosting(t, "post-2", "acc-chase-usd", 1, "USD", nil),
		}
		if _, _, err := ledger.NewTransfer("tx-1", "user-1", bookedDate, "FX transfer", postings); err != nil {
			t.Fatalf("NewTransfer(cross-currency, lopsided legs) = %v, want success (zero-sum rule suspended)", err)
		}
	})
}

func TestCreditCardPurchaseAndPaymentDoNotDoubleCount(t *testing.T) {
	t.Parallel()

	dining := "cat-dining"

	// The purchase: an expense, on the card account, on the date of the
	// purchase.
	purchase, err := ledger.NewOutflow(
		"tx-purchase", "user-1", mustDate(t, 2026, 8, 10), "Restaurant",
		[]ledger.Posting{mustPosting(t, "post-1", "acc-card", -500000, "INR", &dining)},
	)
	if err != nil {
		t.Fatalf("NewOutflow(purchase) = %v, want success", err)
	}
	if purchase.Kind() != ledger.TransactionKindOutflow {
		t.Fatalf("purchase.Kind() = %s, want outflow", purchase.Kind())
	}
	if _, ok := purchase.Postings()[0].CategoryID(); !ok {
		t.Fatalf("purchase posting has no category, want one — it's the expense")
	}

	// Paying the bill: a transfer, not an expense.
	payment, _, err := ledger.NewTransfer(
		"tx-payment", "user-1", mustDate(t, 2026, 9, 1), "Card bill payment",
		[]ledger.Posting{
			mustPosting(t, "post-2", "acc-bank", -500000, "INR", nil),
			mustPosting(t, "post-3", "acc-card", 500000, "INR", nil),
		},
	)
	if err != nil {
		t.Fatalf("NewTransfer(payment) = %v, want success", err)
	}
	if payment.Kind() != ledger.TransactionKindTransfer {
		t.Fatalf("payment.Kind() = %s, want transfer", payment.Kind())
	}
	for _, p := range payment.Postings() {
		if _, ok := p.CategoryID(); ok {
			t.Fatalf("payment posting %s has a category, want none — a transfer never appears in expense totals", p.ID())
		}
	}

	// A category/expense report only ever looks at outflow and inflow
	// postings; kind==transfer is filtered out, which is exactly why the
	// card payment can never be double-counted as a second expense.
	var expenseTotal int64
	for _, tx := range []ledger.Transaction{purchase, payment} {
		if tx.Kind() == ledger.TransactionKindTransfer {
			continue
		}
		total, err := tx.Total()
		if err != nil {
			t.Fatalf("Total() = %v, want success", err)
		}
		expenseTotal += total.AmountMinor()
	}
	if want := int64(-500000); expenseTotal != want {
		t.Errorf("expenseTotal = %d, want %d (the purchase only, once)", expenseTotal, want)
	}

	// And the card account's balance nets back to zero: -5000 from the
	// purchase, +5000 from paying it off.
	card, err := ledger.NewAccount("acc-card", "user-1", "Card", ledger.AccountKindCreditCard, mustMoney(t, 0, "INR"), nil, nil, 0, nil)
	if err != nil {
		t.Fatalf("NewAccount() = %v, want success", err)
	}
	balance, err := ledger.Balance(card, mustDate(t, 2026, 9, 30), []ledger.Transaction{purchase, payment})
	if err != nil {
		t.Fatalf("Balance() = %v, want success", err)
	}
	if !balance.IsZero() {
		t.Errorf("card Balance() = %s, want 0.00 INR", balance)
	}
}

func TestRefundNetsCategoryToZeroWithoutMutatingOriginal(t *testing.T) {
	t.Parallel()

	clothing := "cat-clothing"

	original, err := ledger.NewOutflow(
		"tx-original", "user-1", mustDate(t, 2026, 8, 1), "Blue Tokai Shirt",
		[]ledger.Posting{mustPosting(t, "post-1", "acc-hdfc", -200000, "INR", &clothing)},
	)
	if err != nil {
		t.Fatalf("NewOutflow(original) = %v, want success", err)
	}

	// Snapshot everything about the original before the refund exists, so
	// we can prove it's untouched afterwards.
	originalDescriptionBefore := original.Description()
	originalTotalBefore, err := original.Total()
	if err != nil {
		t.Fatalf("Total() = %v, want success", err)
	}

	refund, err := ledger.NewInflow(
		"tx-refund", "user-1", mustDate(t, 2026, 8, 15), "Shirt refund",
		[]ledger.Posting{mustPosting(t, "post-2", "acc-hdfc", 200000, "INR", &clothing)},
		ledger.WithRelatedTransaction(original.ID()),
	)
	if err != nil {
		t.Fatalf("NewInflow(refund) = %v, want success", err)
	}

	relatedID, ok := refund.RelatedTransactionID()
	if !ok || relatedID != original.ID() {
		t.Fatalf("refund.RelatedTransactionID() = (%s, %v), want (%s, true)", relatedID, ok, original.ID())
	}

	// The category nets to zero.
	originalAmount := original.Postings()[0].Amount()
	refundAmount := refund.Postings()[0].Amount()
	net, err := originalAmount.Add(refundAmount)
	if err != nil {
		t.Fatalf("Add() = %v, want success", err)
	}
	if !net.IsZero() {
		t.Errorf("net category amount = %s, want 0.00 INR", net)
	}

	// The original is untouched: same description, same total, same
	// postings. Nothing about constructing the refund could have mutated
	// it — original is a value, and no method on Transaction mutates its
	// receiver — but assert the observable facts explicitly anyway.
	if original.Description() != originalDescriptionBefore {
		t.Errorf("original.Description() changed to %q, want %q", original.Description(), originalDescriptionBefore)
	}
	originalTotalAfter, err := original.Total()
	if err != nil {
		t.Fatalf("Total() = %v, want success", err)
	}
	if !originalTotalAfter.Equal(originalTotalBefore) {
		t.Errorf("original.Total() changed to %s, want %s", originalTotalAfter, originalTotalBefore)
	}
	if original.IsDeleted() {
		t.Errorf("original.IsDeleted() = true, want false")
	}
}

func TestTransactionDelete(t *testing.T) {
	t.Parallel()

	postings := []ledger.Posting{mustPosting(t, "post-1", "acc-hdfc", -100, "USD", nil)}
	tx, err := ledger.NewOutflow("tx-1", "user-1", mustDate(t, 2026, 8, 1), "Coffee", postings)
	if err != nil {
		t.Fatalf("NewOutflow() = %v, want success", err)
	}
	if tx.IsDeleted() {
		t.Fatalf("IsDeleted() = true before Delete(), want false")
	}
	if _, ok := tx.DeletedAt(); ok {
		t.Fatalf("DeletedAt() ok = true before Delete(), want false")
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	deleted := tx.Delete(at)

	if tx.IsDeleted() {
		t.Errorf("original.IsDeleted() = true after Delete() was called on a copy, want false")
	}
	if !deleted.IsDeleted() {
		t.Errorf("deleted.IsDeleted() = false, want true")
	}
	got, ok := deleted.DeletedAt()
	if !ok || !got.Equal(at) {
		t.Errorf("deleted.DeletedAt() = (%s, %v), want (%s, true)", got, ok, at)
	}
}

func TestTransactionOptions(t *testing.T) {
	t.Parallel()

	postings := []ledger.Posting{mustPosting(t, "post-1", "acc-hdfc", -100, "USD", nil)}

	t.Run("WithNotes", func(t *testing.T) {
		t.Parallel()
		tx, err := ledger.NewOutflow("tx-1", "user-1", mustDate(t, 2026, 8, 1), "Coffee", postings, ledger.WithNotes("with a friend"))
		if err != nil {
			t.Fatalf("NewOutflow() = %v, want success", err)
		}
		if tx.Notes() != "with a friend" {
			t.Errorf("Notes() = %q, want %q", tx.Notes(), "with a friend")
		}
	})

	t.Run("WithPostedDate", func(t *testing.T) {
		t.Parallel()
		posted := mustDate(t, 2026, 8, 3)
		tx, err := ledger.NewOutflow("tx-1", "user-1", mustDate(t, 2026, 8, 1), "Coffee", postings, ledger.WithPostedDate(posted))
		if err != nil {
			t.Fatalf("NewOutflow() = %v, want success", err)
		}
		got, ok := tx.PostedDate()
		if !ok || !got.Equal(posted) {
			t.Errorf("PostedDate() = (%s, %v), want (%s, true)", got, ok, posted)
		}
	})

	t.Run("WithImportProvenance", func(t *testing.T) {
		t.Parallel()
		tx, err := ledger.NewOutflow("tx-1", "user-1", mustDate(t, 2026, 8, 1), "Coffee", postings, ledger.WithImportProvenance("batch-1", "ext-1"))
		if err != nil {
			t.Fatalf("NewOutflow() = %v, want success", err)
		}
		if got, ok := tx.ImportRecordID(); !ok || got != "batch-1" {
			t.Errorf("ImportRecordID() = (%s, %v), want (batch-1, true)", got, ok)
		}
		if got, ok := tx.ExternalID(); !ok || got != "ext-1" {
			t.Errorf("ExternalID() = (%s, %v), want (ext-1, true)", got, ok)
		}
	})

	t.Run("no options leaves optionals unset", func(t *testing.T) {
		t.Parallel()
		tx, err := ledger.NewOutflow("tx-1", "user-1", mustDate(t, 2026, 8, 1), "Coffee", postings)
		if err != nil {
			t.Fatalf("NewOutflow() = %v, want success", err)
		}
		if _, ok := tx.PostedDate(); ok {
			t.Errorf("PostedDate() ok = true, want false")
		}
		if _, ok := tx.ImportRecordID(); ok {
			t.Errorf("ImportRecordID() ok = true, want false")
		}
		if _, ok := tx.ExternalID(); ok {
			t.Errorf("ExternalID() ok = true, want false")
		}
		if _, ok := tx.RelatedTransactionID(); ok {
			t.Errorf("RelatedTransactionID() ok = true, want false")
		}
	})
}

func TestTransactionPostingsReturnsACopy(t *testing.T) {
	t.Parallel()

	postings := []ledger.Posting{mustPosting(t, "post-1", "acc-hdfc", -100, "USD", nil)}
	tx, err := ledger.NewOutflow("tx-1", "user-1", mustDate(t, 2026, 8, 1), "Coffee", postings)
	if err != nil {
		t.Fatalf("NewOutflow() = %v, want success", err)
	}

	got := tx.Postings()
	got[0] = mustPosting(t, "post-tampered", "acc-other", 999, "USD", nil)

	if tx.Postings()[0].ID() != "post-1" {
		t.Errorf("mutating the returned slice affected the transaction's own postings")
	}
}
