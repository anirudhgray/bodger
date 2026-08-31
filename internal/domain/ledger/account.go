// Package ledger holds the pure financial model built on top of
// internal/domain and internal/domain/money: accounts, categories, tags,
// transactions, and postings, plus the invariants and balance computation
// data-model.md §4-§7 and §14 describe. No I/O, no clock, no database —
// see ADR-0005 for why: anything that needs a repository lookup or "now"
// belongs to internal/app, never here.
//
// Every type here has unexported fields and a public validating
// constructor, the same guarantee ADR-0005 calls out for Money, Date, and
// Tag: an invalid Account, Category, Tag, Posting, or Transaction cannot
// exist anywhere in the codebase.
package ledger

import (
	"fmt"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

// AccountKind discriminates the behaviour data-model.md §4 hangs off an
// account: its balance's normal sign, and whether it counts toward net
// worth as an asset or a liability. It is deliberately not a display
// grouping — that's tags and filters (ADR-0003).
type AccountKind string

const (
	AccountKindBank       AccountKind = "bank"
	AccountKindCash       AccountKind = "cash"
	AccountKindCreditCard AccountKind = "credit_card"
	AccountKindWallet     AccountKind = "wallet"
	AccountKindInvestment AccountKind = "investment"
	AccountKindLoan       AccountKind = "loan"
	AccountKindOther      AccountKind = "other"
)

var validAccountKinds = map[AccountKind]bool{
	AccountKindBank:       true,
	AccountKindCash:       true,
	AccountKindCreditCard: true,
	AccountKindWallet:     true,
	AccountKindInvestment: true,
	AccountKindLoan:       true,
	AccountKindOther:      true,
}

// IsLiability reports whether k represents a liability (credit_card, loan)
// rather than an asset — data-model.md §4: kind "decides whether the
// account counts toward net worth as an asset or a liability."
func (k AccountKind) IsLiability() bool {
	return k == AccountKindCreditCard || k == AccountKindLoan
}

// Account is a place the user's money actually sits or is owed — a bank
// account, a wallet, a credit card. Its fields are unexported: the only
// way to produce one is NewAccount.
type Account struct {
	id                 string
	userID             string
	name               string
	kind               AccountKind
	openingBalance     money.Money
	openingBalanceDate *domain.Date
	archived           bool
}

// NewAccount constructs an Account. openingBalanceDate may be nil: an
// account with no declared opening-balance date simply starts its history
// at its first transaction.
//
// The account's currency is openingBalance.Currency() — there is no
// separate currency field to keep in sync with it. money.Money already
// bundles amount and currency, and money.NewMoney has already validated
// the currency by the time it reaches here; a second field could only ever
// disagree with the first, never add information.
func NewAccount(id, userID, name string, kind AccountKind, openingBalance money.Money, openingBalanceDate *domain.Date, archived bool) (Account, error) {
	if id == "" {
		return Account{}, ErrAccountEmptyID
	}
	if userID == "" {
		return Account{}, ErrAccountEmptyUserID
	}
	if name == "" {
		return Account{}, ErrAccountEmptyName
	}
	if !validAccountKinds[kind] {
		return Account{}, fmt.Errorf("%w: %q", ErrAccountInvalidKind, kind)
	}

	var obDate *domain.Date
	if openingBalanceDate != nil {
		d := *openingBalanceDate
		obDate = &d
	}

	return Account{
		id:                 id,
		userID:             userID,
		name:               name,
		kind:               kind,
		openingBalance:     openingBalance,
		openingBalanceDate: obDate,
		archived:           archived,
	}, nil
}

// ID returns the account's identifier.
func (a Account) ID() string { return a.id }

// UserID returns the ID of the user who owns the account.
func (a Account) UserID() string { return a.userID }

// Name returns the account's user-facing name.
func (a Account) Name() string { return a.name }

// Kind returns the account's kind.
func (a Account) Kind() AccountKind { return a.kind }

// Currency is the account's default currency — always
// OpeningBalance().Currency().
func (a Account) Currency() string { return a.openingBalance.Currency() }

// OpeningBalance returns the account's declared starting balance.
func (a Account) OpeningBalance() money.Money { return a.openingBalance }

// OpeningBalanceDate returns the date the opening balance is true as of,
// and false if none was declared.
func (a Account) OpeningBalanceDate() (domain.Date, bool) {
	if a.openingBalanceDate == nil {
		return domain.Date{}, false
	}
	return *a.openingBalanceDate, true
}

// Archived reports whether the account is hidden from pickers. An
// archived account's history remains present — see data-model.md §4.
func (a Account) Archived() bool { return a.archived }
