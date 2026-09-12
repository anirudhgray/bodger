package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// stageSuspectedDuplicateBatch stages a single row that lands as
// ImportRecordStatusPending with a tier-2 DuplicateMatch — the same setup
// TestStageImport_TierTwoSuspectedDuplicateFlaggedNotExcluded
// (import_stage_test.go) proves, reused here as the starting point every
// ResolveImportRecord test needs.
func stageSuspectedDuplicateBatch(t *testing.T, svc *app.Service) (app.StageImportResult, string) {
	t.Helper()
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	date, err := domain.NewDate(2026, time.August, 1)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	existingTxnID := seedCommittedTransaction(t, svc, acc.Account.ID(), -450, "USD", "STARBUCKS COFFEE 4521", date, "")

	data := "Date,Description,Amount\n2026-08-03,Starbucks Coffee,-4.50\n"
	result, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "statement.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("len(Records) = %d, want 1", len(result.Records))
	}
	return result, existingTxnID
}

func TestResolveImportRecord_ConfirmedExcludesRecord(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged, _ := stageSuspectedDuplicateBatch(t, svc)
	rec := staged.Records[0]

	result, err := svc.ResolveImportRecord(ctx, app.ResolveImportRecordCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.DuplicateResolutionConfirmed),
	})
	if err != nil {
		t.Fatalf("ResolveImportRecord: %v", err)
	}
	if result.Record.Status() != importing.ImportRecordStatusExcluded {
		t.Errorf("Status() = %q, want %q", result.Record.Status(), importing.ImportRecordStatusExcluded)
	}
	dm, ok := result.Record.DuplicateMatch()
	if !ok || dm.Resolution() != importing.DuplicateResolutionConfirmed {
		t.Errorf("DuplicateMatch() = (%+v, %v), want resolution %q", dm, ok, importing.DuplicateResolutionConfirmed)
	}

	// Persisted, not just returned.
	stored, err := svc.ImportRecords.Get(ctx, testActorID, rec.ID())
	if err != nil {
		t.Fatalf("ImportRecords.Get: %v", err)
	}
	if stored.Status() != importing.ImportRecordStatusExcluded {
		t.Errorf("stored Status() = %q, want %q", stored.Status(), importing.ImportRecordStatusExcluded)
	}
}

func TestResolveImportRecord_DismissedClearsRecordForCommit(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged, _ := stageSuspectedDuplicateBatch(t, svc)
	rec := staged.Records[0]

	result, err := svc.ResolveImportRecord(ctx, app.ResolveImportRecordCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.DuplicateResolutionDismissed),
	})
	if err != nil {
		t.Fatalf("ResolveImportRecord: %v", err)
	}
	if result.Record.Status() != importing.ImportRecordStatusReady {
		t.Errorf("Status() = %q, want %q", result.Record.Status(), importing.ImportRecordStatusReady)
	}

	// A dismissed record is now free to commit alongside the rest of its
	// batch — the whole point of resolving it.
	commitResult, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("CommitImportBatch after dismissal: %v", err)
	}
	if len(commitResult.Transactions) != 1 {
		t.Fatalf("len(Transactions) = %d, want 1", len(commitResult.Transactions))
	}
}

func TestResolveImportRecord_InvalidResolutionRejected(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged, _ := stageSuspectedDuplicateBatch(t, svc)
	rec := staged.Records[0]

	_, err := svc.ResolveImportRecord(ctx, app.ResolveImportRecordCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: "maybe",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestResolveImportRecord_NoDuplicateMatchRefused(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged := stageBasicBatch(t, svc) // clean rows, no DuplicateMatch on either

	_, err := svc.ResolveImportRecord(ctx, app.ResolveImportRecordCommand{
		ActorID: testActorID, ImportRecordRef: staged.Records[0].ID(), Resolution: string(importing.DuplicateResolutionConfirmed),
	})
	wantErrCode(t, err, errs.PreconditionFailed)
}

func TestResolveImportRecord_AlreadyResolvedRefused(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged, _ := stageSuspectedDuplicateBatch(t, svc)
	rec := staged.Records[0]

	if _, err := svc.ResolveImportRecord(ctx, app.ResolveImportRecordCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.DuplicateResolutionDismissed),
	}); err != nil {
		t.Fatalf("first ResolveImportRecord: %v", err)
	}

	_, err := svc.ResolveImportRecord(ctx, app.ResolveImportRecordCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.DuplicateResolutionConfirmed),
	})
	wantErrCode(t, err, errs.PreconditionFailed)
}

func TestResolveImportRecord_ExactDuplicateRefused(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	date, err := domain.NewDate(2026, time.August, 1)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	seedCommittedTransaction(t, svc, acc.Account.ID(), -450, "USD", "Coffee Shop", date, "ext-1")

	data := "Date,Description,Amount,Ref\n2026-08-01,Coffee Shop,-4.50,ext-1\n"
	mapping := basicCSVMapping()
	mapping.ExternalIDColumn = "Ref"
	result, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "statement.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: mapping,
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	rec := result.Records[0]
	if rec.Status() != importing.ImportRecordStatusExcluded {
		t.Fatalf("test setup: Status() = %q, want %q", rec.Status(), importing.ImportRecordStatusExcluded)
	}

	_, err = svc.ResolveImportRecord(ctx, app.ResolveImportRecordCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.DuplicateResolutionDismissed),
	})
	wantErrCode(t, err, errs.PreconditionFailed)
}

func TestListImportRecords_ReturnsBatchRows(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged := stageBasicBatch(t, svc)

	result, err := svc.ListImportRecords(ctx, app.ListImportRecordsQuery{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("ListImportRecords: %v", err)
	}
	if len(result.Records) != len(staged.Records) {
		t.Errorf("len(Records) = %d, want %d", len(result.Records), len(staged.Records))
	}
}

func TestListImportRecords_UnknownBatchIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.ListImportRecords(context.Background(), app.ListImportRecordsQuery{ActorID: testActorID, ImportBatchRef: "does-not-exist"})
	wantErrCode(t, err, errs.NotFound)
}

func TestListImportBatches_ReturnsEveryBatch(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	first := stageBasicBatch(t, svc)

	result, err := svc.ListImportBatches(ctx, app.ListImportBatchesQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListImportBatches: %v", err)
	}
	found := false
	for _, b := range result.Batches {
		if b.ID() == first.Batch.ID() {
			found = true
		}
	}
	if !found {
		t.Errorf("ListImportBatches() didn't include %q", first.Batch.ID())
	}
}

func TestGetImportBatch_ReturnsStatus(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged := stageBasicBatch(t, svc)

	result, err := svc.GetImportBatch(ctx, app.GetImportBatchQuery{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("GetImportBatch: %v", err)
	}
	if result.Batch.Status() != importing.ImportBatchStatusStaged {
		t.Errorf("Status() = %q, want %q", result.Batch.Status(), importing.ImportBatchStatusStaged)
	}
}

func TestGetImportBatch_UnknownIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.GetImportBatch(context.Background(), app.GetImportBatchQuery{ActorID: testActorID, ImportBatchRef: "does-not-exist"})
	wantErrCode(t, err, errs.NotFound)
}
