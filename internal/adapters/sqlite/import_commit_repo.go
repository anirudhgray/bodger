package sqlite

import (
	"context"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// ImportCommitRepository implements ports.ImportCommitRepository over a
// *DB. It shares this package's existing write helpers
// (insertTransactionRow, insertPostings) with TransactionRepository, and
// its own update helpers with ImportBatchRepository/ImportRecordRepository
// — the only thing this type adds is bundling all of them into the single
// database transaction ADR-0008 requires for a commit or a rollback.
type ImportCommitRepository struct {
	db *DB
}

// NewImportCommitRepository constructs an ImportCommitRepository backed by
// db.
func NewImportCommitRepository(db *DB) *ImportCommitRepository {
	return &ImportCommitRepository{db: db}
}

var _ ports.ImportCommitRepository = (*ImportCommitRepository)(nil)

// Commit implements ports.ImportCommitRepository.
func (r *ImportCommitRepository) Commit(ctx context.Context, actorID string, batch importing.ImportBatch, records []importing.ImportRecord, txns []ledger.Transaction) error {
	if err := requireActor(actorID, batch.UserID()); err != nil {
		return err
	}
	for _, rec := range records {
		if err := requireActor(actorID, rec.UserID()); err != nil {
			return err
		}
	}
	for _, txn := range txns {
		if err := requireActor(actorID, txn.UserID()); err != nil {
			return err
		}
	}

	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	now := formatTime(r.db.clock.Now())

	// Every transaction row (and its postings) is inserted before any
	// import_record row is updated to point at one: import_record.
	// transaction_id is a foreign key to transactions(id), and this
	// database runs with foreign_keys=1, so the referenced row must
	// already exist in this same transaction.
	for _, txn := range txns {
		if err := insertTransactionRow(ctx, tx, actorID, txn, now); err != nil {
			return err
		}
		if err := insertPostings(ctx, tx, txn.ID(), txn.Postings()); err != nil {
			return err
		}
	}

	for _, rec := range records {
		if err := markImportRecordCommitted(ctx, tx, actorID, rec, now); err != nil {
			return err
		}
	}

	if err := markImportBatchStatus(ctx, tx, actorID, batch, now); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}

// Rollback implements ports.ImportCommitRepository.
func (r *ImportCommitRepository) Rollback(ctx context.Context, actorID string, batch importing.ImportBatch, transactionIDs []string, at time.Time) error {
	if err := requireActor(actorID, batch.UserID()); err != nil {
		return err
	}

	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	now := formatTime(r.db.clock.Now())

	if len(transactionIDs) > 0 {
		if err := softDeleteBatchTransactions(ctx, tx, actorID, batch.ID(), transactionIDs, formatTime(at), now); err != nil {
			return err
		}
	}

	if err := markImportBatchStatus(ctx, tx, actorID, batch, now); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}

// markImportRecordCommitted writes only the two columns a commit actually
// changes on an import_record row — status and transaction_id — rather
// than ImportRecordRepository.Update's full column set, since every other
// field was already written when the record was staged (#210) and isn't
// touched by committing it.
func markImportRecordCommitted(ctx context.Context, tx execer, actorID string, record importing.ImportRecord, now string) *errs.Error {
	transactionID, _ := record.TransactionID()
	result, err := tx.ExecContext(ctx, `
		UPDATE import_record
		SET status = ?, transaction_id = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`, string(record.Status()), transactionID, now, record.ID(), actorID)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't mark import record %q committed.", record.ID())
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No import record with ID %q.", record.ID()).Field("id")
	}
	return nil
}

// markImportBatchStatus writes only the status column a commit or
// rollback changes on an import_batch row — the same "only what actually
// changed" reasoning as markImportRecordCommitted.
func markImportBatchStatus(ctx context.Context, tx execer, actorID string, batch importing.ImportBatch, now string) *errs.Error {
	result, err := tx.ExecContext(ctx, `
		UPDATE import_batch
		SET status = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`, string(batch.Status()), now, batch.ID(), actorID)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't update import batch %q.", batch.ID())
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No import batch with ID %q.", batch.ID()).Field("id")
	}
	return nil
}

// softDeleteBatchTransactions soft-deletes every non-deleted transaction
// in transactionIDs that is actually linked, via its import_record_id, to
// one of importBatchID's own records — the belt-and-braces half of
// "rollback soft-deletes exactly the transactions a batch created"
// (ADR-0008): even if a caller's transactionIDs slice were ever wrong,
// this WHERE clause keeps the blast radius to this batch's own rows. The
// WHERE deleted_at IS NULL guard also makes a second Rollback call over
// the same IDs a no-op rather than an error, which is what "a batch
// remains rollback-able after the fact" needs to actually mean in
// practice, though app.RollbackImportBatch's own state-machine check
// (ImportBatch.MarkRolledBack) already refuses a second rollback before
// this is ever reached.
func softDeleteBatchTransactions(ctx context.Context, tx execer, actorID, importBatchID string, transactionIDs []string, deletedAt, now string) *errs.Error {
	placeholders := make([]byte, 0, len(transactionIDs)*2)
	args := make([]any, 0, len(transactionIDs)+4)
	args = append(args, deletedAt, now, actorID)
	for i, id := range transactionIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}
	args = append(args, importBatchID, actorID)

	query := `
		UPDATE transactions
		SET deleted_at = ?, updated_at = ?
		WHERE user_id = ? AND deleted_at IS NULL AND id IN (` + string(placeholders) + `)
		  AND import_record_id IN (SELECT id FROM import_record WHERE import_batch_id = ? AND user_id = ?)
	`
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return wrapWriteError(err).Explain("Couldn't roll back the transactions import batch %q created.", importBatchID)
	}
	return nil
}
