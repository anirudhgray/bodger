package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
)

// TransactionFilter narrows TransactionRepository.List. The zero value
// lists every non-deleted transaction actorID owns, unpaginated.
//
// This is issue #6's reduced M1 filter — date range, account, category
// (subtree included by default), and kind — not ADR-0009's full
// TransactionFilter (amount range, tags, currencies, description search,
// import batch), which is M4 scope. internal/app is this port's only
// consumer today (ADR-0007: ports are defined by the consumer), and it
// never sets a field this struct doesn't have — extending it further is
// exactly the "add it once, every surface gains it" story ADR-0009
// describes, deferred until M4 actually needs it.
type TransactionFilter struct {
	// AccountID, when non-empty, restricts to transactions with at least
	// one posting on this account — the query data-model.md §4's balance
	// formula needs.
	AccountID string
	// CategoryID, when non-empty, restricts to transactions with at least
	// one posting whose category is this category or a descendant of it
	// in the user's category tree. Subtree inclusion is not optional here
	// (ADR-0009: "CategoryRefs includes the subtree by default... that is
	// what users mean") — there is no separate flag to turn it off.
	CategoryID string
	// Kind, when non-empty, restricts to transactions of this kind
	// (outflow, inflow, or transfer).
	Kind ledger.TransactionKind
	// FromDate, when set, restricts to transactions booked on or after
	// this date (inclusive, ADR-0009).
	FromDate *domain.Date
	// ToDate, when set, restricts to transactions booked on or before
	// this date (inclusive, ADR-0009). This replaces the field this port
	// used to call AsOf — the same "on or before" semantics, renamed
	// because a filter with a lower bound too needs a name that pairs
	// with it.
	ToDate *domain.Date
	// Limit caps the number of rows returned. Limit <= 0 means no limit —
	// this is a low-level port semantic (contrast with
	// app.ListTransactionsQuery, which defaults Limit to a page size
	// before it ever reaches here); AccountBalances, for one, deliberately
	// leaves this zero because a balance computation needs every matching
	// posting, not a page of them.
	Limit int
	// Offset skips this many matching rows, in the sort order below,
	// before the first row returned. Used with Limit for M1's offset
	// pagination (ADR-0009); cursor pagination is M2.
	Offset int
}

// TransactionRepository persists Transactions and their Postings together,
// plus the Tags attached to a Transaction. Every method takes actorID and
// filters on it (ADR-0006), and deleted_at is null filtering happens here,
// never left to a caller (issue #3's "done when" list) — List and Get never
// return a soft-deleted transaction.
type TransactionRepository interface {
	// Create persists txn and its postings, and associates tags with it,
	// all in one database transaction. It returns a *errs.Error with
	// code NotAllowed if txn.UserID() != actorID.
	Create(ctx context.Context, actorID string, txn ledger.Transaction, tags []ledger.Tag) error

	// Get returns the transaction identified by id, owned by actorID,
	// with its tags, and a *errs.Error with code NotFound if no such
	// non-deleted transaction exists for that actor.
	Get(ctx context.Context, actorID, id string) (ledger.Transaction, []ledger.Tag, error)

	// List returns every non-deleted transaction actorID owns matching
	// filter, sorted (booked_date DESC, created_at DESC, id DESC) — fully
	// specified, deliberately, because a non-deterministic tiebreak would
	// make offset pagination (and any test asserting exact order)
	// unreliable (ADR-0009). It does not load tags — callers that need
	// them call Get.
	List(ctx context.Context, actorID string, filter TransactionFilter) ([]ledger.Transaction, error)

	// Update replaces the stored state of the transaction identified by
	// txn.ID(), owned by actorID, including its postings and tags, and
	// records the previous state to the audit trail (data-model.md §7).
	// Soft deletion is an Update call with a txn whose DeletedAt is set
	// (ledger.Transaction.Delete) — there is no separate Delete method.
	// Returns a *errs.Error with code NotFound if no such non-deleted
	// transaction exists for that actor, and NotAllowed if
	// txn.UserID() != actorID.
	Update(ctx context.Context, actorID string, txn ledger.Transaction, tags []ledger.Tag) error
}
