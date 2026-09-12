package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/app/importparse"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// stageBasicBatch stages a clean two-row CSV (one outflow, one inflow,
// neither a duplicate of anything) against a fresh account, and returns
// the resulting batch and records — every commit/rollback test that
// doesn't care about duplicate handling starts from this.
func stageBasicBatch(t *testing.T, svc *app.Service) app.StageImportResult {
	t.Helper()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	data := "Date,Description,Amount\n" +
		"2026-08-01,Coffee Shop,-4.50\n" +
		"2026-08-02,Paycheck,1500.00\n"
	result, err := svc.StageImport(context.Background(), app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "statement.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	return result
}

func TestCommitImportBatch_HappyPath(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged := stageBasicBatch(t, svc)

	result, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("CommitImportBatch: %v", err)
	}
	if result.Batch.Status() != importing.ImportBatchStatusCommitted {
		t.Errorf("Batch.Status() = %q, want %q", result.Batch.Status(), importing.ImportBatchStatusCommitted)
	}
	if len(result.Transactions) != 2 {
		t.Fatalf("len(Transactions) = %d, want 2", len(result.Transactions))
	}

	// Persisted, not just returned: the batch and every record must
	// actually be updated through the repositories.
	storedBatch, err := svc.ImportBatches.Get(ctx, testActorID, staged.Batch.ID())
	if err != nil {
		t.Fatalf("ImportBatches.Get: %v", err)
	}
	if storedBatch.Status() != importing.ImportBatchStatusCommitted {
		t.Errorf("stored batch Status() = %q, want %q", storedBatch.Status(), importing.ImportBatchStatusCommitted)
	}

	storedRecords, err := svc.ImportRecords.ListByImportBatch(ctx, testActorID, staged.Batch.ID())
	if err != nil {
		t.Fatalf("ImportRecords.ListByImportBatch: %v", err)
	}
	if len(storedRecords) != 2 {
		t.Fatalf("stored records = %d, want 2", len(storedRecords))
	}
	for _, rec := range storedRecords {
		if rec.Status() != importing.ImportRecordStatusCommitted {
			t.Errorf("record %q Status() = %q, want %q", rec.ID(), rec.Status(), importing.ImportRecordStatusCommitted)
		}
		txnID, ok := rec.TransactionID()
		if !ok {
			t.Errorf("record %q TransactionID() ok = false, want true", rec.ID())
			continue
		}
		txn, _, err := svc.Transactions.Get(ctx, testActorID, txnID)
		if err != nil {
			t.Errorf("Transactions.Get(%q): %v", txnID, err)
			continue
		}
		gotRecordID, ok := txn.ImportRecordID()
		if !ok || gotRecordID != rec.ID() {
			t.Errorf("txn %q ImportRecordID() = (%q, %v), want (%q, true)", txnID, gotRecordID, ok, rec.ID())
		}
	}
}

// TestCommitImportBatch_ExcludedRecordsProduceNoTransaction commits a
// batch where one row is a tier-1 exact duplicate (auto-excluded per
// ADR-0008) alongside one genuine row: the excluded record must not gain
// a transaction, and the batch must still commit successfully — an
// excluded record isn't an unresolved review item.
func TestCommitImportBatch_ExcludedRecordsProduceNoTransaction(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	date, err := domain.NewDate(2026, time.August, 1)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	seedCommittedTransaction(t, svc, acc.Account.ID(), -450, "USD", "Coffee Shop", date, "bank-ext-1")

	data := "Date,Description,Amount,Ref\n" +
		"2026-08-01,Coffee Shop,-4.50,bank-ext-1\n" +
		"2026-08-02,Groceries,-20.00,bank-ext-2\n"
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "statement.csv", SourceFormat: "csv",
		FileContent: []byte(data),
		ColumnMapping: importparse.ColumnMapping{
			DateColumn: "Date", DescriptionColumn: "Description", AmountColumn: "Amount", ExternalIDColumn: "Ref",
		},
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if len(staged.Records) != 2 {
		t.Fatalf("len(Records) = %d, want 2", len(staged.Records))
	}
	if staged.Records[0].Status() != importing.ImportRecordStatusExcluded {
		t.Fatalf("test setup: Records[0].Status() = %q, want %q", staged.Records[0].Status(), importing.ImportRecordStatusExcluded)
	}
	if staged.Records[1].Status() != importing.ImportRecordStatusReady {
		t.Fatalf("test setup: Records[1].Status() = %q, want %q", staged.Records[1].Status(), importing.ImportRecordStatusReady)
	}

	result, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("CommitImportBatch: %v", err)
	}
	if len(result.Transactions) != 1 {
		t.Fatalf("len(Transactions) = %d, want 1 (only the non-excluded row produces one)", len(result.Transactions))
	}

	records, err := svc.ImportRecords.ListByImportBatch(ctx, testActorID, staged.Batch.ID())
	if err != nil {
		t.Fatalf("ImportRecords.ListByImportBatch: %v", err)
	}
	for _, rec := range records {
		if rec.SortOrder() == 0 {
			if rec.Status() != importing.ImportRecordStatusExcluded {
				t.Errorf("excluded record Status() after commit = %q, want %q (unchanged)", rec.Status(), importing.ImportRecordStatusExcluded)
			}
			if _, ok := rec.TransactionID(); ok {
				t.Error("excluded record gained a TransactionID after commit, want none")
			}
		} else {
			if rec.Status() != importing.ImportRecordStatusCommitted {
				t.Errorf("non-excluded record Status() after commit = %q, want %q", rec.Status(), importing.ImportRecordStatusCommitted)
			}
		}
	}
}

func TestCommitImportBatch_RefusesUnresolvedSuspectedDuplicate(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	date, err := domain.NewDate(2026, time.August, 1)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	// No external ID, so tier-1 doesn't apply; date/amount/description are
	// close enough to trip tier-2, which leaves the record pending.
	seedCommittedTransaction(t, svc, acc.Account.ID(), -450, "USD", "STARBUCKS COFFEE 4521", date, "")

	data := "Date,Description,Amount\n2026-08-03,Starbucks Coffee,-4.50\n"
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "statement.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if staged.Records[0].Status() != importing.ImportRecordStatusPending {
		t.Fatalf("test setup: record status = %q, want %q", staged.Records[0].Status(), importing.ImportRecordStatusPending)
	}

	_, err = svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	wantErrCode(t, err, errs.PreconditionFailed)

	// Refused before any state changed.
	storedBatch, getErr := svc.ImportBatches.Get(ctx, testActorID, staged.Batch.ID())
	if getErr != nil {
		t.Fatalf("ImportBatches.Get: %v", getErr)
	}
	if storedBatch.Status() != importing.ImportBatchStatusStaged {
		t.Errorf("batch Status() after a refused commit = %q, want %q (unchanged)", storedBatch.Status(), importing.ImportBatchStatusStaged)
	}
}

func TestCommitImportBatch_RefusesUnknownBatch(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.CommitImportBatch(context.Background(), app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: "no-such-batch"})
	wantErrCode(t, err, errs.NotFound)
}

func TestCommitImportBatch_RefusesMissingActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.CommitImportBatch(context.Background(), app.CommitImportBatchCommand{ImportBatchRef: "batch-1"})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCommitImportBatch_RefusesMissingBatchRef(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.CommitImportBatch(context.Background(), app.CommitImportBatchCommand{ActorID: testActorID})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCommitImportBatch_RefusesDoubleCommit(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged := stageBasicBatch(t, svc)

	if _, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()}); err != nil {
		t.Fatalf("CommitImportBatch (first): %v", err)
	}
	_, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	wantErrCode(t, err, errs.PreconditionFailed)
}

func TestRollbackImportBatch_HappyPath(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged := stageBasicBatch(t, svc)

	committed, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("CommitImportBatch: %v", err)
	}

	rolledBack, err := svc.RollbackImportBatch(ctx, app.RollbackImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("RollbackImportBatch: %v", err)
	}
	if rolledBack.Batch.Status() != importing.ImportBatchStatusRolledBack {
		t.Errorf("Batch.Status() = %q, want %q", rolledBack.Batch.Status(), importing.ImportBatchStatusRolledBack)
	}
	if len(rolledBack.TransactionIDs) != len(committed.Transactions) {
		t.Fatalf("len(TransactionIDs) = %d, want %d", len(rolledBack.TransactionIDs), len(committed.Transactions))
	}

	// Every transaction the batch created is now soft-deleted (gone from
	// Get, same as any other soft-deleted transaction).
	for _, id := range rolledBack.TransactionIDs {
		if _, _, err := svc.Transactions.Get(ctx, testActorID, id); err == nil {
			t.Errorf("Transactions.Get(%q) after rollback: want an error (soft-deleted), got nil", id)
		}
	}

	// The records themselves keep their committed status and transaction
	// ID — rollback undoes the ledger effect, not the historical fact of
	// what this batch produced.
	records, err := svc.ImportRecords.ListByImportBatch(ctx, testActorID, staged.Batch.ID())
	if err != nil {
		t.Fatalf("ImportRecords.ListByImportBatch: %v", err)
	}
	for _, rec := range records {
		if rec.Status() != importing.ImportRecordStatusCommitted {
			t.Errorf("record %q Status() after rollback = %q, want %q (unchanged)", rec.ID(), rec.Status(), importing.ImportRecordStatusCommitted)
		}
		if _, ok := rec.TransactionID(); !ok {
			t.Errorf("record %q TransactionID() ok = false after rollback, want true (unchanged)", rec.ID())
		}
	}

	// Provenance in the batch -> transactions direction now reports no
	// live transactions.
	txnResult, err := svc.ImportBatchTransactions(ctx, app.ImportBatchTransactionsQuery{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("ImportBatchTransactions: %v", err)
	}
	if len(txnResult.Transactions) != 0 {
		t.Errorf("ImportBatchTransactions after rollback returned %d transactions, want 0", len(txnResult.Transactions))
	}
}

func TestRollbackImportBatch_RefusesNonCommittedBatch(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged := stageBasicBatch(t, svc)

	_, err := svc.RollbackImportBatch(ctx, app.RollbackImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	wantErrCode(t, err, errs.PreconditionFailed)
}

func TestRollbackImportBatch_RefusesRepeatedRollback(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged := stageBasicBatch(t, svc)

	if _, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()}); err != nil {
		t.Fatalf("CommitImportBatch: %v", err)
	}
	if _, err := svc.RollbackImportBatch(ctx, app.RollbackImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()}); err != nil {
		t.Fatalf("RollbackImportBatch (first): %v", err)
	}
	_, err := svc.RollbackImportBatch(ctx, app.RollbackImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	wantErrCode(t, err, errs.PreconditionFailed)
}

func TestCommitImportBatch_RefusesRecommitAfterRollback(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged := stageBasicBatch(t, svc)

	if _, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()}); err != nil {
		t.Fatalf("CommitImportBatch: %v", err)
	}
	if _, err := svc.RollbackImportBatch(ctx, app.RollbackImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()}); err != nil {
		t.Fatalf("RollbackImportBatch: %v", err)
	}
	_, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	wantErrCode(t, err, errs.PreconditionFailed)
}

func TestImportBatchTransactions_HappyPath(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged := stageBasicBatch(t, svc)

	committed, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("CommitImportBatch: %v", err)
	}

	result, err := svc.ImportBatchTransactions(ctx, app.ImportBatchTransactionsQuery{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("ImportBatchTransactions: %v", err)
	}
	if len(result.Transactions) != len(committed.Transactions) {
		t.Fatalf("len(Transactions) = %d, want %d", len(result.Transactions), len(committed.Transactions))
	}
	wantIDs := map[string]bool{}
	for _, txn := range committed.Transactions {
		wantIDs[txn.ID()] = true
	}
	for _, txn := range result.Transactions {
		if !wantIDs[txn.ID()] {
			t.Errorf("ImportBatchTransactions returned unexpected transaction %q", txn.ID())
		}
	}
}

func TestImportBatchTransactions_RefusesUnknownBatch(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.ImportBatchTransactions(context.Background(), app.ImportBatchTransactionsQuery{ActorID: testActorID, ImportBatchRef: "no-such-batch"})
	wantErrCode(t, err, errs.NotFound)
}

func TestTransactionImportRecord_HappyPath(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged := stageBasicBatch(t, svc)

	committed, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("CommitImportBatch: %v", err)
	}

	txn := committed.Transactions[0]
	wantRecordID, ok := txn.ImportRecordID()
	if !ok {
		t.Fatalf("test setup: committed transaction has no ImportRecordID")
	}

	result, err := svc.TransactionImportRecord(ctx, app.TransactionImportRecordQuery{ActorID: testActorID, TransactionRef: txn.ID()})
	if err != nil {
		t.Fatalf("TransactionImportRecord: %v", err)
	}
	if result.Record.ID() != wantRecordID {
		t.Errorf("Record.ID() = %q, want %q", result.Record.ID(), wantRecordID)
	}
	if result.Record.ImportBatchID() != staged.Batch.ID() {
		t.Errorf("Record.ImportBatchID() = %q, want %q", result.Record.ImportBatchID(), staged.Batch.ID())
	}
}

// TestTransactionImportRecord_NotFoundForNonImportedTransaction confirms
// the query correctly reports NotFound for a perfectly ordinary
// hand-entered transaction, which has no import provenance at all.
func TestTransactionImportRecord_NotFoundForNonImportedTransaction(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	txn, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "50", Description: "Coffee", Date: "2026-08-01",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	_, err = svc.TransactionImportRecord(ctx, app.TransactionImportRecordQuery{ActorID: testActorID, TransactionRef: txn.Transaction.ID()})
	wantErrCode(t, err, errs.NotFound)
}

func TestTransactionImportRecord_RefusesUnknownTransaction(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.TransactionImportRecord(context.Background(), app.TransactionImportRecordQuery{ActorID: testActorID, TransactionRef: "no-such-transaction"})
	wantErrCode(t, err, errs.NotFound)
}
