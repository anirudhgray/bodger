package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
)

// CategoryRepository persists Categories. Every method takes actorID and
// filters on it (ADR-0006), the same way AccountRepository does.
type CategoryRepository interface {
	// Create persists a new category. It returns a *errs.Error with code
	// NotAllowed if category.UserID() != actorID, InvalidInput if
	// category's parent doesn't exist or belongs to a different user or
	// setting it would create a cycle, and Conflict if actorID already
	// has a sibling category with this name.
	Create(ctx context.Context, actorID string, category ledger.Category) error

	// Get returns the category identified by id, owned by actorID. It
	// returns a *errs.Error with code NotFound if no such category
	// exists for that actor.
	Get(ctx context.Context, actorID, id string) (ledger.Category, error)

	// List returns every category actorID owns.
	List(ctx context.Context, actorID string) ([]ledger.Category, error)

	// Update replaces the stored state of the category identified by
	// category.ID(), owned by actorID. Same error conditions as Create,
	// plus NotFound if the category doesn't exist for that actor.
	Update(ctx context.Context, actorID string, category ledger.Category) error
}
