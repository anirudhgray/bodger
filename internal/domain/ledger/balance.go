package ledger

import (
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

// Balance computes account's balance as of asOf: its opening balance plus
// every posting on it from a non-deleted transaction booked on or before
// asOf — data-model.md §4 and §14, exactly:
//
//	balance(account, as_of) = opening_balance
//	                        + Σ posting.amount_minor
//	                          where posting.account_id = account
//	                            and transaction.booked_date <= as_of
//	                            and transaction.deleted_at is null
//
// Nothing about a balance is ever stored; this is the only place it's
// computed, deliberately pure so a caller can run it over a repository
// query's results without this package ever touching a database.
func Balance(account Account, asOf domain.Date, transactions []Transaction) (money.Money, error) {
	total := account.OpeningBalance()
	for _, tx := range transactions {
		if tx.IsDeleted() {
			continue
		}
		if tx.BookedDate().After(asOf) {
			continue
		}
		for _, p := range tx.Postings() {
			if p.AccountID() != account.ID() {
				continue
			}
			var err error
			total, err = total.Add(p.Amount())
			if err != nil {
				return money.Money{}, err
			}
		}
	}
	return total, nil
}
