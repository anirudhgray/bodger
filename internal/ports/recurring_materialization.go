package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
)

// RecurringMaterializationRepository performs the one write issue #279
// requires to be atomic: turning a pending ScheduledOccurrence into a real
// Transaction. Every method takes actorID and filters/validates on it
// (ADR-0006).
//
// This is a separate interface from ScheduledOccurrenceRepository and
// TransactionRepository (rather than an addition to one of them) for the
// same reason ImportCommitRepository is separate from
// ImportBatchRepository/ImportRecordRepository/TransactionRepository:
// Materialize is the one operation that must write to both the
// transactions/postings tables and the scheduled_occurrences table inside
// a single database transaction — the occurrence's own state must never
// disagree with whether its transaction actually exists (data-model.md
// §11's "occurrence becomes actual" boundary, ADR-0014).
type RecurringMaterializationRepository interface {
	// Materialize inserts txn (and its postings) and updates the
	// scheduled_occurrence row identified by occurrence.ID() to
	// occurrence's own state — both inside one database transaction: if
	// either write fails, neither is persisted.
	//
	// occurrence must already be transitioned to
	// recurring.OccurrenceStatusMaterialised (via
	// recurring.ScheduledOccurrence.MarkMaterialised(txn.ID())) before this
	// is called — building that transition is the application layer's job
	// (see app.MaterialiseOccurrence), not this method's; this method does
	// no domain logic, only persistence.
	//
	// Returns a *errs.Error with code NotAllowed if txn.UserID() != actorID
	// or the occurrence's owning rule doesn't belong to actorID, and
	// NotFound if the occurrence doesn't exist for that actor.
	Materialize(ctx context.Context, actorID string, occurrence recurring.ScheduledOccurrence, txn ledger.Transaction) error
}
