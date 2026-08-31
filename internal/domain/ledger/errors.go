package ledger

import "errors"

// Named errors returned by this package. Callers should match on these
// with errors.Is; the wrapped text (added by the constructor that returns
// them) carries the offending value for humans, the sentinel carries the
// identity for code.
var (
	// ErrAccountInvalidKind is returned when an account's kind isn't one
	// of bank, cash, credit_card, wallet, investment, loan, or other.
	ErrAccountInvalidKind = errors.New("ledger: invalid account kind")

	// ErrAccountEmptyID is returned when an account is constructed with
	// an empty ID.
	ErrAccountEmptyID = errors.New("ledger: account id must not be empty")

	// ErrAccountEmptyUserID is returned when an account is constructed
	// with an empty user ID.
	ErrAccountEmptyUserID = errors.New("ledger: account user id must not be empty")

	// ErrAccountEmptyName is returned when an account is constructed with
	// an empty name.
	ErrAccountEmptyName = errors.New("ledger: account name must not be empty")

	// ErrCategoryInvalidKind is returned when a category's kind isn't
	// expense or income.
	ErrCategoryInvalidKind = errors.New("ledger: invalid category kind")

	// ErrCategoryEmptyID is returned when a category is constructed with
	// an empty ID.
	ErrCategoryEmptyID = errors.New("ledger: category id must not be empty")

	// ErrCategoryEmptyUserID is returned when a category is constructed
	// with an empty user ID.
	ErrCategoryEmptyUserID = errors.New("ledger: category user id must not be empty")

	// ErrCategoryEmptyName is returned when a category is constructed
	// with an empty name.
	ErrCategoryEmptyName = errors.New("ledger: category name must not be empty")

	// ErrCategorySelfParent is returned when a category's parent ID is
	// its own ID.
	ErrCategorySelfParent = errors.New("ledger: category cannot be its own parent")

	// ErrInvalidTag is returned by NewTag when the raw input normalises
	// to nothing — e.g. empty, whitespace-only, or made up entirely of
	// characters that aren't letters or digits.
	ErrInvalidTag = errors.New("ledger: invalid tag")

	// ErrPostingEmptyID is returned when a posting is constructed with an
	// empty ID.
	ErrPostingEmptyID = errors.New("ledger: posting id must not be empty")

	// ErrPostingEmptyAccountID is returned when a posting is constructed
	// with an empty account ID.
	ErrPostingEmptyAccountID = errors.New("ledger: posting account id must not be empty")

	// ErrPostingEmptyCategoryID is returned when a posting's category ID
	// pointer is non-nil but points at an empty string.
	ErrPostingEmptyCategoryID = errors.New("ledger: posting category id must not be empty when set")

	// ErrTransactionEmptyID is returned when a transaction is constructed
	// with an empty ID.
	ErrTransactionEmptyID = errors.New("ledger: transaction id must not be empty")

	// ErrTransactionEmptyUserID is returned when a transaction is
	// constructed with an empty user ID.
	ErrTransactionEmptyUserID = errors.New("ledger: transaction user id must not be empty")

	// ErrTransactionEmptyDescription is returned when a transaction is
	// constructed with an empty description.
	ErrTransactionEmptyDescription = errors.New("ledger: transaction description must not be empty")

	// ErrTransactionNoPostings is returned when a transaction is
	// constructed with no postings at all.
	ErrTransactionNoPostings = errors.New("ledger: transaction must have at least one posting")

	// ErrPostingsMultipleAccounts is returned by NewOutflow and NewInflow
	// when their postings don't all share the same account —
	// data-model.md §14: outflow/inflow postings are "all on one
	// account".
	ErrPostingsMultipleAccounts = errors.New("ledger: all postings on an outflow or inflow must be on the same account")

	// ErrOutflowPostingNotNegative is returned by NewOutflow when any
	// posting's amount isn't strictly negative.
	ErrOutflowPostingNotNegative = errors.New("ledger: every outflow posting must be negative")

	// ErrInflowPostingNotPositive is returned by NewInflow when any
	// posting's amount isn't strictly positive.
	ErrInflowPostingNotPositive = errors.New("ledger: every inflow posting must be positive")

	// ErrTransferPostingCount is returned by NewTransfer when it isn't
	// given exactly two postings.
	ErrTransferPostingCount = errors.New("ledger: a transfer must have exactly two postings")

	// ErrTransferSameAccount is returned by NewTransfer when both
	// postings are on the same account.
	ErrTransferSameAccount = errors.New("ledger: a transfer's two postings must be on distinct accounts")

	// ErrTransferPostingHasCategory is returned by NewTransfer when
	// either posting carries a category. Transfers never have one.
	ErrTransferPostingHasCategory = errors.New("ledger: a transfer posting must not carry a category")

	// ErrTransferPostingsMustOppose is returned by NewTransfer when the
	// two postings aren't one negative and one positive.
	ErrTransferPostingsMustOppose = errors.New("ledger: a transfer's two postings must have opposite signs")

	// ErrCrossCurrencyTransferUnsupported is returned by NewTransfer when
	// its two postings are in different currencies. M1 rejects
	// cross-currency transfers outright; the exemption from the zero-sum
	// rule and the implied-rate recording are M3 (ADR-0003 §Cross-currency
	// transfers).
	ErrCrossCurrencyTransferUnsupported = errors.New("ledger: cross-currency transfers are not supported yet")

	// ErrTransferNotBalanced is returned by NewTransfer when a
	// same-currency transfer's two postings don't sum to exactly zero.
	ErrTransferNotBalanced = errors.New("ledger: a same-currency transfer's postings must sum to zero")
)
