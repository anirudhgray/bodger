package app

import (
	"context"
	"errors"
	"strings"

	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// CommitImportBatchCommand identifies the staged import batch to commit.
type CommitImportBatchCommand struct {
	ActorID        string
	ImportBatchRef string
}

// CommitImportBatchResult is the batch CommitImportBatch moved to
// ImportBatchStatusCommitted, and every real transaction it created — one
// per newly-committed ImportRecord (excluded records produce nothing).
type CommitImportBatchResult struct {
	Batch        importing.ImportBatch
	Transactions []ledger.Transaction
}

// CommitImportBatch implements issue #211's commit use case: ADR-0008's
// "the only pipeline stage that touches the ledger." It writes every
// resolved (ImportRecordStatusReady) record's transaction, in one database
// transaction — all or nothing — via ImportCommits.Commit, and never calls
// it at all if any record is still ImportRecordStatusPending: ADR-0008's
// "the system never silently merges a tier-2 match" means an unresolved
// suspected duplicate (or, for a record whose transfer candidate was also
// flagged as a suspected duplicate, an unresolved transfer proposal) has
// to stay pending until reviewed, and pending is exactly the status this
// refuses to commit past. A transfer candidate that never became a
// suspected duplicate is advisory only (ADR-0008: "proposed, never applied
// automatically") and never reaches pending by itself, so it never blocks
// a commit either — matching the domain package's own WithTransferCandidate
// doc comment.
//
// A batch that's still ImportBatchStatusStaged is moved through
// ImportBatchStatusReviewed on its way to ImportBatchStatusCommitted in
// the same call: nothing else in this codebase calls MarkReviewed
// separately, and the precondition check just above is exactly what
// "reviewed" means in practice — every record has cleared or bypassed
// duplicate/transfer review. A batch already ImportBatchStatusReviewed
// (a future review use case's doing) commits directly. Any other status
// (already committed, or rolled back) is refused.
func (s *Service) CommitImportBatch(ctx context.Context, cmd CommitImportBatchCommand) (CommitImportBatchResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return CommitImportBatchResult{}, err
	}
	batchID := strings.TrimSpace(cmd.ImportBatchRef)
	if batchID == "" {
		return CommitImportBatchResult{}, errs.New(errs.InvalidInput).Explain("An import batch ID is required.").Field("import_batch_ref")
	}

	batch, err := s.ImportBatches.Get(ctx, cmd.ActorID, batchID)
	if err != nil {
		return CommitImportBatchResult{}, err
	}

	if batch.Status() != importing.ImportBatchStatusStaged && batch.Status() != importing.ImportBatchStatusReviewed {
		return CommitImportBatchResult{}, errs.New(errs.PreconditionFailed).
			Explain("Import batch %q is %s and can't be committed.", batch.ID(), batch.Status()).
			Field("import_batch_ref")
	}

	records, err := s.ImportRecords.ListByImportBatch(ctx, cmd.ActorID, batch.ID())
	if err != nil {
		return CommitImportBatchResult{}, err
	}
	for _, rec := range records {
		if rec.Status() == importing.ImportRecordStatusPending {
			return CommitImportBatchResult{}, errs.New(errs.PreconditionFailed).
				Explain("Import batch %q has unresolved duplicate or transfer review items; resolve them before committing.", batch.ID()).
				Field("import_batch_ref")
		}
	}

	reviewedBatch := batch
	if batch.Status() == importing.ImportBatchStatusStaged {
		reviewedBatch, err = batch.MarkReviewed()
		if err != nil {
			return CommitImportBatchResult{}, errs.New(errs.Internal).Wrap(err)
		}
	}
	committedBatch, err := reviewedBatch.MarkCommitted()
	if err != nil {
		return CommitImportBatchResult{}, errs.New(errs.Internal).Wrap(err)
	}

	var committedRecords []importing.ImportRecord
	var txns []ledger.Transaction
	for _, rec := range records {
		if rec.Status() != importing.ImportRecordStatusReady {
			continue
		}
		txn, err := s.buildImportCommitTransaction(cmd.ActorID, rec)
		if err != nil {
			return CommitImportBatchResult{}, err
		}
		committedRec, err := rec.MarkCommitted(txn.ID())
		if err != nil {
			return CommitImportBatchResult{}, errs.New(errs.Internal).Wrap(err)
		}
		committedRecords = append(committedRecords, committedRec)
		txns = append(txns, txn)
	}

	if err := s.ImportCommits.Commit(ctx, cmd.ActorID, committedBatch, committedRecords, txns); err != nil {
		return CommitImportBatchResult{}, err
	}

	return CommitImportBatchResult{Batch: committedBatch, Transactions: txns}, nil
}

// buildImportCommitTransaction turns one ready-to-commit ImportRecord into
// the real ledger.Transaction it produces: an outflow for a negative
// amount, an inflow for a positive one — imports never produce a transfer,
// since nothing in the staged pipeline pairs two records into one
// transaction (a transfer candidate is advisory only, per
// importing.WithTransferCandidate's doc comment). The transaction carries
// ImportRecordID and ExternalID (ledger.WithImportProvenance) for ADR-0008's
// two-directional provenance, and PostedDate when the record has one.
//
// By the time this runs, rec's amount, description, and booked date were
// already normalised and validated by the parser that staged it (#210) —
// so a ledger constructor failing here means the staged pipeline's own
// invariant broke, not that anything a user is doing right now is invalid.
// That's why failures here are Internal, matching wrapTransferError's
// identical reasoning for the equivalent "can't actually happen" branches
// in RecordTransfer.
func (s *Service) buildImportCommitTransaction(actorID string, rec importing.ImportRecord) (ledger.Transaction, error) {
	accountID, ok := rec.ResolvedAccountID()
	if !ok {
		return ledger.Transaction{}, errs.New(errs.Internal).
			Explain("Import record %q has no resolved account and can't be committed.", rec.ID())
	}

	var categoryIDPtr *string
	if categoryID, ok := rec.ResolvedCategoryID(); ok {
		c := categoryID
		categoryIDPtr = &c
	}

	posting, err := ledger.NewPosting(s.IDs.NewID(), accountID, rec.Amount(), categoryIDPtr, 0)
	if err != nil {
		return ledger.Transaction{}, errs.New(errs.Internal).Explain("Couldn't build a posting for import record %q.", rec.ID()).Wrap(err)
	}

	var opts []ledger.TransactionOption
	if postedDate, ok := rec.PostedDate(); ok {
		opts = append(opts, ledger.WithPostedDate(postedDate))
	}
	externalID, _ := rec.ExternalID()
	opts = append(opts, ledger.WithImportProvenance(rec.ID(), externalID))

	txnID := s.IDs.NewID()
	postings := []ledger.Posting{posting}
	if rec.Amount().IsNegative() {
		txn, err := ledger.NewOutflow(txnID, actorID, rec.BookedDate(), rec.Description(), postings, opts...)
		if err != nil {
			return ledger.Transaction{}, errs.New(errs.Internal).Explain("Couldn't commit import record %q.", rec.ID()).Wrap(err)
		}
		return txn, nil
	}
	txn, err := ledger.NewInflow(txnID, actorID, rec.BookedDate(), rec.Description(), postings, opts...)
	if err != nil {
		return ledger.Transaction{}, errs.New(errs.Internal).Explain("Couldn't commit import record %q.", rec.ID()).Wrap(err)
	}
	return txn, nil
}

// RollbackImportBatchCommand identifies the committed import batch to roll
// back.
type RollbackImportBatchCommand struct {
	ActorID        string
	ImportBatchRef string
}

// RollbackImportBatchResult is the batch RollbackImportBatch moved to
// ImportBatchStatusRolledBack, and the IDs of the transactions it
// soft-deleted.
type RollbackImportBatchResult struct {
	Batch          importing.ImportBatch
	TransactionIDs []string
}

// RollbackImportBatch implements issue #211's rollback use case: ADR-0008's
// "rolling back a batch soft-deletes exactly the transactions it created,
// and a batch remains rollback-able after the fact." Only a currently
// ImportBatchStatusCommitted batch can be rolled back — attempting it
// twice, or on a batch that was never committed, fails on
// ImportBatch.MarkRolledBack's own transition check before this ever
// reaches ImportCommits.Rollback, so a repeated call is a clean
// PreconditionFailed rather than a partial no-op.
//
// The records that produced these transactions keep their
// ImportRecordStatusCommitted status and TransactionID after rollback —
// ADR-0002/ADR-0008 both treat rollback as undoing the ledger effect, not
// the historical fact that this batch produced these records, which is
// what keeps TransactionImportRecord answering correctly for a
// soft-deleted transaction's originating record even after its batch has
// been rolled back.
func (s *Service) RollbackImportBatch(ctx context.Context, cmd RollbackImportBatchCommand) (RollbackImportBatchResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return RollbackImportBatchResult{}, err
	}
	batchID := strings.TrimSpace(cmd.ImportBatchRef)
	if batchID == "" {
		return RollbackImportBatchResult{}, errs.New(errs.InvalidInput).Explain("An import batch ID is required.").Field("import_batch_ref")
	}

	batch, err := s.ImportBatches.Get(ctx, cmd.ActorID, batchID)
	if err != nil {
		return RollbackImportBatchResult{}, err
	}

	rolledBack, err := batch.MarkRolledBack()
	if err != nil {
		return RollbackImportBatchResult{}, errs.New(errs.PreconditionFailed).
			Explain("Import batch %q is %s and can't be rolled back.", batch.ID(), batch.Status()).
			Field("import_batch_ref")
	}

	records, err := s.ImportRecords.ListByImportBatch(ctx, cmd.ActorID, batch.ID())
	if err != nil {
		return RollbackImportBatchResult{}, err
	}
	var txnIDs []string
	for _, rec := range records {
		if id, ok := rec.TransactionID(); ok {
			txnIDs = append(txnIDs, id)
		}
	}

	if err := s.ImportCommits.Rollback(ctx, cmd.ActorID, rolledBack, txnIDs, s.Clock.Now()); err != nil {
		return RollbackImportBatchResult{}, err
	}

	return RollbackImportBatchResult{Batch: rolledBack, TransactionIDs: txnIDs}, nil
}

// ImportBatchTransactionsQuery identifies the import batch whose
// transactions to list — ADR-0008's provenance query in the
// batch-to-transactions direction.
type ImportBatchTransactionsQuery struct {
	ActorID        string
	ImportBatchRef string
}

// ImportBatchTransactionsResult is every currently live (non-soft-deleted)
// transaction the queried batch created.
type ImportBatchTransactionsResult struct {
	Transactions []ledger.Transaction
}

// ImportBatchTransactions answers "which transactions did this import
// produce" by walking the batch's own records and fetching the
// transaction each committed one names — the direction ADR-0008 calls
// "batch -> its transactions." A record whose transaction was later
// rolled back is silently skipped (Transactions.Get excludes a
// soft-deleted transaction, the same as everywhere else in this package):
// this answers "what does this batch currently affect," not "what did it
// ever affect" — ListImportRecords (a separate, out-of-scope concern) is
// where the full historical record, rolled back or not, is visible via
// each record's own TransactionID.
func (s *Service) ImportBatchTransactions(ctx context.Context, q ImportBatchTransactionsQuery) (ImportBatchTransactionsResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ImportBatchTransactionsResult{}, err
	}
	batchID := strings.TrimSpace(q.ImportBatchRef)
	if batchID == "" {
		return ImportBatchTransactionsResult{}, errs.New(errs.InvalidInput).Explain("An import batch ID is required.").Field("import_batch_ref")
	}

	// Confirms the batch exists (and belongs to this actor) before
	// walking its records, so an unknown batch ID reports NotFound rather
	// than a silently empty transaction list.
	if _, err := s.ImportBatches.Get(ctx, q.ActorID, batchID); err != nil {
		return ImportBatchTransactionsResult{}, err
	}

	records, err := s.ImportRecords.ListByImportBatch(ctx, q.ActorID, batchID)
	if err != nil {
		return ImportBatchTransactionsResult{}, err
	}

	var txns []ledger.Transaction
	for _, rec := range records {
		transactionID, ok := rec.TransactionID()
		if !ok {
			continue
		}
		txn, _, err := s.Transactions.Get(ctx, q.ActorID, transactionID)
		if err != nil {
			if isNotFound(err) {
				// Rolled back: the transaction this record produced was
				// soft-deleted, so it's excluded from "currently live."
				continue
			}
			return ImportBatchTransactionsResult{}, err
		}
		txns = append(txns, txn)
	}
	return ImportBatchTransactionsResult{Transactions: txns}, nil
}

// TransactionImportRecordQuery identifies the transaction whose
// originating import record to look up — ADR-0008's provenance query in
// the transaction-to-record direction.
type TransactionImportRecordQuery struct {
	ActorID        string
	TransactionRef string
}

// TransactionImportRecordResult is the ImportRecord that produced the
// queried transaction.
type TransactionImportRecordResult struct {
	Record importing.ImportRecord
}

// TransactionImportRecord answers "which import produced this
// transaction" — the direction ADR-0008 calls "transaction -> its
// originating record." It returns a *errs.Error with code NotFound both
// when the transaction itself doesn't exist for this actor, and when it
// exists but wasn't produced by an import (no ImportRecordID) — either
// way, there is no import record to return.
func (s *Service) TransactionImportRecord(ctx context.Context, q TransactionImportRecordQuery) (TransactionImportRecordResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return TransactionImportRecordResult{}, err
	}
	transactionID := strings.TrimSpace(q.TransactionRef)
	if transactionID == "" {
		return TransactionImportRecordResult{}, errs.New(errs.InvalidInput).Explain("A transaction ID is required.").Field("transaction_ref")
	}

	txn, _, err := s.Transactions.Get(ctx, q.ActorID, transactionID)
	if err != nil {
		return TransactionImportRecordResult{}, err
	}

	importRecordID, ok := txn.ImportRecordID()
	if !ok {
		return TransactionImportRecordResult{}, errs.New(errs.NotFound).
			Explain("Transaction %q wasn't produced by an import.", transactionID).
			Field("transaction_ref")
	}

	record, err := s.ImportRecords.Get(ctx, q.ActorID, importRecordID)
	if err != nil {
		return TransactionImportRecordResult{}, err
	}
	return TransactionImportRecordResult{Record: record}, nil
}

// isNotFound reports whether err is an *errs.Error coded NotFound —
// ImportBatchTransactions' way of distinguishing "this record's
// transaction was rolled back" (expected, skip it) from any other failure
// reading it (propagate).
func isNotFound(err error) bool {
	var e *errs.Error
	return errors.As(err, &e) && e.Code == errs.NotFound
}
