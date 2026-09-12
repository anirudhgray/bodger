package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/importing"
)

// ImportBatchRepository persists ImportBatches. Every method takes
// actorID and filters on it (ADR-0006): a batch belonging to a different
// user is invisible to Get and List, and Create and Update reject a batch
// whose own UserID doesn't match actorID.
//
// Defined here, in the layer that consumes it (ADR-0007's "the repository
// boundary is what keeps this reversible") — internal/adapters/sqlite
// implements it, and the application layer depends only on this
// interface, never on the adapter.
type ImportBatchRepository interface {
	// Create persists a new import batch. It returns a *errs.Error with
	// code NotAllowed if batch.UserID() != actorID.
	Create(ctx context.Context, actorID string, batch importing.ImportBatch) error

	// Get returns the import batch identified by id, owned by actorID. It
	// returns a *errs.Error with code NotFound if no such batch exists for
	// that actor — including when the batch exists but belongs to a
	// different user.
	Get(ctx context.Context, actorID, id string) (importing.ImportBatch, error)

	// List returns every import batch actorID owns, most recently created
	// first.
	List(ctx context.Context, actorID string) ([]importing.ImportBatch, error)

	// Update replaces the stored state of the import batch identified by
	// batch.ID(), owned by actorID — status transitions (MarkReviewed,
	// MarkCommitted, MarkRolledBack) are persisted this way, the same as
	// every other field. It returns a *errs.Error with code NotFound if no
	// such batch exists for that actor, and NotAllowed if
	// batch.UserID() != actorID.
	Update(ctx context.Context, actorID string, batch importing.ImportBatch) error
}
