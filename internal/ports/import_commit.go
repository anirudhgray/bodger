package ports

import (
	"context"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
)

// ImportCommitRepository performs the one write ADR-0008 requires to be
// atomic — committing a batch — and its mirror, rolling one back. Every
// method takes actorID and filters/validates on it (ADR-0006), the same
// as every other import repository.
//
// This is a separate interface from ImportBatchRepository,
// ImportRecordRepository, and TransactionRepository (rather than an
// addition to one of them) because Commit is the one operation that must
// write to all three tables it touches — transactions, postings, and
// import_record, then import_batch — inside a single database
// transaction: all or nothing, never half an import (ADR-0008). Folding
// it into any one of those existing repositories would make that
// repository responsible for tables it otherwise never writes, for the
// sake of one cross-cutting use case.
type ImportCommitRepository interface {
	// Commit writes every transaction in txns (each already carrying
	// ImportRecordID for provenance, per ADR-0008's "every created
	// transaction carries import_record_id"), updates every record in
	// records to its now-committed status and transaction ID, and updates
	// batch's own status — all inside one database transaction: if any
	// single write fails, nothing in this call is persisted.
	//
	// records and txns must correspond one-to-one: records[i] is the
	// ImportRecord that produced txns[i] (already transitioned to
	// ImportRecordStatusCommitted via ImportRecord.MarkCommitted, carrying
	// the matching transaction ID) — building that pairing is the
	// application layer's job (see app.CommitImportBatch), not this
	// method's. records may be shorter than the batch's full record set:
	// only newly-committed records need a write here, since an already-
	// excluded record's row is untouched by a commit.
	//
	// Returns a *errs.Error with code NotAllowed if batch.UserID() !=
	// actorID or any record's or transaction's UserID() != actorID, and
	// NotFound if batch, or any record or transaction referenced, doesn't
	// exist for that actor.
	Commit(ctx context.Context, actorID string, batch importing.ImportBatch, records []importing.ImportRecord, txns []ledger.Transaction) error

	// Rollback soft-deletes every transaction named in transactionIDs
	// (transactions.deleted_at = at, ADR-0002) and updates batch's own
	// status to rolled back — both inside one database transaction. Only
	// transactions actually linked (via their import_record_id) to a
	// record of this batch are ever touched, regardless of what
	// transactionIDs contains, so a caller passing the wrong ID by mistake
	// can't soft-delete a transaction belonging to a different batch.
	//
	// Rollback does not change the status or TransactionID of the
	// records that produced these transactions: ADR-0008 treats rollback
	// as undoing the ledger effect, not the historical fact that this
	// batch produced these records, so a rolled-back batch's records stay
	// ImportRecordStatusCommitted, pointing at now-soft-deleted
	// transactions — provenance queries keep working, and calling
	// Rollback again over already-deleted transactions is simply a no-op
	// (the WHERE deleted_at IS NULL guard on the update matches nothing
	// the second time).
	//
	// Returns a *errs.Error with code NotAllowed if batch.UserID() !=
	// actorID, and NotFound if batch doesn't exist for that actor.
	Rollback(ctx context.Context, actorID string, batch importing.ImportBatch, transactionIDs []string, at time.Time) error
}
