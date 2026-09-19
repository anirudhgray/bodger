package sqlite

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// RecurringMaterializationRepository implements
// ports.RecurringMaterializationRepository over a *DB. It shares this
// package's existing write helpers (insertTransactionRow, insertPostings,
// updateScheduledOccurrenceRow) with TransactionRepository and
// ScheduledOccurrenceRepository — the only thing this type adds is
// bundling them into the single database transaction issue #279 requires
// for a materialisation.
type RecurringMaterializationRepository struct {
	db *DB
}

// NewRecurringMaterializationRepository constructs a
// RecurringMaterializationRepository backed by db.
func NewRecurringMaterializationRepository(db *DB) *RecurringMaterializationRepository {
	return &RecurringMaterializationRepository{db: db}
}

var _ ports.RecurringMaterializationRepository = (*RecurringMaterializationRepository)(nil)

// Materialize implements ports.RecurringMaterializationRepository.
func (r *RecurringMaterializationRepository) Materialize(ctx context.Context, actorID string, occurrence recurring.ScheduledOccurrence, txn ledger.Transaction) error {
	if err := requireActor(actorID, txn.UserID()); err != nil {
		return err
	}

	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	if err := requireRuleOwnedByActor(ctx, tx, actorID, occurrence.RuleID()); err != nil {
		return err
	}

	now := formatTime(r.db.clock.Now())

	// The transaction row (and its postings) is inserted before the
	// scheduled_occurrences row is updated to point at it:
	// scheduled_occurrences.transaction_id is a foreign key to
	// transactions(id), and this database runs with foreign_keys=1, so the
	// referenced row must already exist in this same transaction — the
	// same ordering ImportCommitRepository.Commit follows for
	// import_record.transaction_id.
	if err := insertTransactionRow(ctx, tx, actorID, txn, now); err != nil {
		return err
	}
	if err := insertPostings(ctx, tx, txn.ID(), txn.Postings()); err != nil {
		return err
	}
	if err := updateScheduledOccurrenceRow(ctx, tx, actorID, occurrence, now); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}
