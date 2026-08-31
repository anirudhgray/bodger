package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
)

// AccountRepository persists Accounts. Every method takes actorID and
// filters on it (ADR-0006): an account belonging to a different user is
// invisible to Get and List, and Create and Update reject an account whose
// own UserID doesn't match actorID.
//
// Defined here, in the layer that consumes it (ADR-0007's "the repository
// boundary is what keeps this reversible") — internal/adapters/sqlite
// implements it, and the application layer (issue #5) depends only on this
// interface, never on the adapter.
type AccountRepository interface {
	// Create persists a new account. It returns a *errs.Error with code
	// NotAllowed if account.UserID() != actorID, and Conflict if
	// actorID already has an account with this name.
	Create(ctx context.Context, actorID string, account ledger.Account) error

	// Get returns the account identified by id, owned by actorID. It
	// returns a *errs.Error with code NotFound if no such account exists
	// for that actor — including when the account exists but belongs to
	// a different user.
	Get(ctx context.Context, actorID, id string) (ledger.Account, error)

	// List returns every account actorID owns, in name order. It never
	// returns another user's accounts.
	List(ctx context.Context, actorID string) ([]ledger.Account, error)

	// Update replaces the stored state of the account identified by
	// account.ID(), owned by actorID. It returns a *errs.Error with code
	// NotFound if no such account exists for that actor, and NotAllowed
	// if account.UserID() != actorID.
	Update(ctx context.Context, actorID string, account ledger.Account) error
}
