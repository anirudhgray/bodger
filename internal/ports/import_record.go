package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/importing"
)

// ImportRecordRepository persists ImportRecords. Every method takes
// actorID and filters on it (ADR-0006), the same as ImportBatchRepository.
//
// Defined here, in the layer that consumes it (ADR-0007's "the repository
// boundary is what keeps this reversible") — internal/adapters/sqlite
// implements it, and the application layer depends only on this
// interface, never on the adapter.
type ImportRecordRepository interface {
	// CreateBatch persists every record in records in one database
	// transaction — a parser (a later, out-of-scope issue) stages an
	// entire source file's rows at once, and an import can run to
	// thousands of rows (ADR-0008), so this is the one write path for that
	// rather than N round trips through a single-row Create. It returns a
	// *errs.Error with code NotAllowed if any record's UserID() != actorID,
	// and nothing in records is written if any single row fails.
	CreateBatch(ctx context.Context, actorID string, records []importing.ImportRecord) error

	// Get returns the import record identified by id, owned by actorID. It
	// returns a *errs.Error with code NotFound if no such record exists
	// for that actor — including when the record exists but belongs to a
	// different user.
	Get(ctx context.Context, actorID, id string) (importing.ImportRecord, error)

	// ListByImportBatch returns every record belonging to importBatchID,
	// owned by actorID, in their original source-file order (sort_order).
	ListByImportBatch(ctx context.Context, actorID, importBatchID string) ([]importing.ImportRecord, error)

	// Update replaces the stored state of the import record identified by
	// record.ID(), owned by actorID — mapping (resolved account/category),
	// duplicate-match resolution, and status transitions (MarkReady,
	// MarkExcluded, MarkCommitted) are all persisted this way. It returns
	// a *errs.Error with code NotFound if no such record exists for that
	// actor, and NotAllowed if record.UserID() != actorID.
	Update(ctx context.Context, actorID string, record importing.ImportRecord) error
}
