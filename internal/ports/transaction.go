package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
)

// TransactionFilter narrows TransactionRepository.List. The zero value
// lists every non-deleted transaction actorID owns.
type TransactionFilter struct {
	// AccountID, when non-empty, restricts to transactions with at least
	// one posting on this account — the query data-model.md §4's balance
	// formula needs.
	AccountID string
	// AsOf, when set, restricts to transactions booked on or before this
	// date.
	AsOf *domain.Date
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
	// filter, ordered by booked date then id. It does not load tags —
	// callers that need them call Get.
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
