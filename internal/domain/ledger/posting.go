package ledger

import "github.com/anirudhgray/bodger/internal/domain/money"

// Posting is one line of a Transaction: a signed amount hitting one
// account, optionally attributed to one category. Its fields are
// unexported: the only way to produce one is NewPosting.
//
// Posting carries no separate currency field. data-model.md §5 lists
// amount_minor and currency as two SQL columns, but money.Money already
// bundles the two; duplicating the field here would only be a second
// place for it to disagree with the first. Currency() reads
// amount.Currency() directly.
type Posting struct {
	id         string
	accountID  string
	amount     money.Money
	categoryID *string
	sortOrder  int
}

// NewPosting constructs a Posting. categoryID may be nil. Whether it must
// be nil (a transfer posting never carries a category) is validated by
// NewTransfer, not here — a standalone Posting doesn't yet know which
// transaction it will end up on.
func NewPosting(id, accountID string, amount money.Money, categoryID *string, sortOrder int) (Posting, error) {
	if id == "" {
		return Posting{}, ErrPostingEmptyID
	}
	if accountID == "" {
		return Posting{}, ErrPostingEmptyAccountID
	}

	var catID *string
	if categoryID != nil {
		if *categoryID == "" {
			return Posting{}, ErrPostingEmptyCategoryID
		}
		c := *categoryID
		catID = &c
	}

	return Posting{
		id:         id,
		accountID:  accountID,
		amount:     amount,
		categoryID: catID,
		sortOrder:  sortOrder,
	}, nil
}

// ID returns the posting's identifier.
func (p Posting) ID() string { return p.id }

// AccountID returns the ID of the account this posting is against.
func (p Posting) AccountID() string { return p.accountID }

// Amount returns the posting's signed amount.
func (p Posting) Amount() money.Money { return p.amount }

// Currency is the posting's currency — always Amount().Currency().
func (p Posting) Currency() string { return p.amount.Currency() }

// CategoryID returns the posting's category ID, and false if it has none
// (always the case for a transfer posting).
func (p Posting) CategoryID() (string, bool) {
	if p.categoryID == nil {
		return "", false
	}
	return *p.categoryID, true
}

// SortOrder returns the posting's display order within its transaction.
func (p Posting) SortOrder() int { return p.sortOrder }
