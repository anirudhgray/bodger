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

// stageOccurrenceMatchBatch stages a single row that lands as
// ImportRecordStatusPending with an OccurrenceMatch against occ — the
// starting point every ResolveImportRecordOccurrenceMatch test needs.
func stageOccurrenceMatchBatch(t *testing.T, svc *app.Service) (app.StageImportResult, string) {
	t.Helper()
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	rent := mustCreateCategory(t, svc, "Rent")

	_, occ := mustCreateRuleWithOccurrence(t, svc, acc.Account.ID(), rent, "1200.00", "Rent", "2026-08-01")

	data := "Date,Description,Amount\n2026-08-03,Rent Payment,-1200.00\n"
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
	if result.Records[0].Status() != importing.ImportRecordStatusPending {
		t.Fatalf("test setup: Status() = %q, want %q", result.Records[0].Status(), importing.ImportRecordStatusPending)
	}
	return result, occ.ID()
}

// stageBothPendingReasonsBatch stages a single row whose amount, date, and
// description simultaneously trip findSuspectedDuplicate (against
// existingTxnID, an already-committed transaction) and findOccurrenceMatch
// (against occID, a pending occurrence) — issue #309's coexistence case:
// a record pending for two independent reasons at once.
func stageBothPendingReasonsBatch(t *testing.T, svc *app.Service) (rec importing.ImportRecord, batchID, occID string) {
	t.Helper()
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	rent := mustCreateCategory(t, svc, "Rent")

	date, err := domain.NewDate(2026, time.August, 1)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	seedCommittedTransaction(t, svc, acc.Account.ID(), -120000, "USD", "Rent Payment", date, "")

	_, occ := mustCreateRuleWithOccurrence(t, svc, acc.Account.ID(), rent, "1200.00", "Rent", "2026-08-01")

	// "Rent" shares a token with both "Rent Payment" (the committed
	// transaction's own description) and "Rent" (the rule's own
	// description), so both findSuspectedDuplicate and findOccurrenceMatch
	// fire for this single row.
	data := "Date,Description,Amount\n2026-08-03,Rent,-1200.00\n"
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
	got := result.Records[0]
	if got.Status() != importing.ImportRecordStatusPending {
		t.Fatalf("test setup: Status() = %q, want %q", got.Status(), importing.ImportRecordStatusPending)
	}
	if _, ok := got.DuplicateMatch(); !ok {
		t.Fatal("test setup: DuplicateMatch() ok = false, want true")
	}
	if _, ok := got.OccurrenceMatch(); !ok {
		t.Fatal("test setup: OccurrenceMatch() ok = false, want true")
	}
	return got, result.Batch.ID(), occ.ID()
}

func TestResolveImportRecordOccurrenceMatch_MaterializedExcludesRecordAndMaterialisesOccurrence(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged, occID := stageOccurrenceMatchBatch(t, svc)
	rec := staged.Records[0]

	result, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.OccurrenceMatchResolutionMaterialized),
	})
	if err != nil {
		t.Fatalf("ResolveImportRecordOccurrenceMatch: %v", err)
	}
	if result.Record.Status() != importing.ImportRecordStatusExcluded {
		t.Errorf("Status() = %q, want %q", result.Record.Status(), importing.ImportRecordStatusExcluded)
	}
	om, ok := result.Record.OccurrenceMatch()
	if !ok || om.Resolution() != importing.OccurrenceMatchResolutionMaterialized {
		t.Errorf("OccurrenceMatch() = (%+v, %v), want resolution %q", om, ok, importing.OccurrenceMatchResolutionMaterialized)
	}
	if result.Occurrence == nil {
		t.Fatal("Occurrence = nil, want the materialised occurrence")
	}
	if result.Occurrence.ID() != occID {
		t.Errorf("Occurrence.ID() = %q, want %q", result.Occurrence.ID(), occID)
	}
	if result.Transaction == nil {
		t.Fatal("Transaction = nil, want the transaction MaterialiseOccurrence produced")
	}

	// Persisted, not just returned.
	stored, err := svc.ImportRecords.Get(ctx, testActorID, rec.ID())
	if err != nil {
		t.Fatalf("ImportRecords.Get: %v", err)
	}
	if stored.Status() != importing.ImportRecordStatusExcluded {
		t.Errorf("stored Status() = %q, want %q", stored.Status(), importing.ImportRecordStatusExcluded)
	}

	occ, err := svc.ScheduledOccurrences.Get(ctx, testActorID, occID)
	if err != nil {
		t.Fatalf("ScheduledOccurrences.Get: %v", err)
	}
	if occ.Status() != "materialised" {
		t.Errorf("occurrence Status() = %q, want materialised", occ.Status())
	}
}

func TestResolveImportRecordOccurrenceMatch_DismissedClearsRecordAndLeavesOccurrencePending(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged, occID := stageOccurrenceMatchBatch(t, svc)
	rec := staged.Records[0]

	result, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.OccurrenceMatchResolutionDismissed),
	})
	if err != nil {
		t.Fatalf("ResolveImportRecordOccurrenceMatch: %v", err)
	}
	if result.Record.Status() != importing.ImportRecordStatusReady {
		t.Errorf("Status() = %q, want %q", result.Record.Status(), importing.ImportRecordStatusReady)
	}
	if result.Occurrence != nil {
		t.Errorf("Occurrence = %+v, want nil for a dismiss resolution", result.Occurrence)
	}
	if result.Transaction != nil {
		t.Errorf("Transaction = %+v, want nil for a dismiss resolution", result.Transaction)
	}

	// The occurrence itself is left completely untouched — still pending.
	occ, err := svc.ScheduledOccurrences.Get(ctx, testActorID, occID)
	if err != nil {
		t.Fatalf("ScheduledOccurrences.Get: %v", err)
	}
	if occ.Status() != "pending" {
		t.Errorf("occurrence Status() = %q, want pending (dismiss must leave it untouched)", occ.Status())
	}

	// A dismissed record is now free to commit.
	commitResult, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("CommitImportBatch after dismissal: %v", err)
	}
	if len(commitResult.Transactions) != 1 {
		t.Fatalf("len(Transactions) = %d, want 1", len(commitResult.Transactions))
	}
}

func TestResolveImportRecordOccurrenceMatch_InvalidResolutionRejected(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged, _ := stageOccurrenceMatchBatch(t, svc)

	_, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: testActorID, ImportRecordRef: staged.Records[0].ID(), Resolution: "maybe",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestResolveImportRecordOccurrenceMatch_NoOccurrenceMatchRefused(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged := stageBasicBatch(t, svc) // clean rows, no OccurrenceMatch on either

	_, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: testActorID, ImportRecordRef: staged.Records[0].ID(), Resolution: string(importing.OccurrenceMatchResolutionDismissed),
	})
	wantErrCode(t, err, errs.PreconditionFailed)
}

func TestResolveImportRecordOccurrenceMatch_AlreadyResolvedRefused(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	staged, _ := stageOccurrenceMatchBatch(t, svc)
	rec := staged.Records[0]

	if _, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.OccurrenceMatchResolutionDismissed),
	}); err != nil {
		t.Fatalf("first ResolveImportRecordOccurrenceMatch: %v", err)
	}

	_, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.OccurrenceMatchResolutionMaterialized),
	})
	wantErrCode(t, err, errs.PreconditionFailed)
}

// TestImportRecord_PendingForTwoReasons_ResolvingOneLeavesTheOtherPending
// is issue #309's explicitly-required coexistence proof: a record pending
// for both an unresolved DuplicateMatch and an unresolved OccurrenceMatch
// at once must stay ImportRecordStatusPending after only one of the two is
// resolved, and only transition once the second is resolved too —
// resolving one must never silently resolve or bypass the other.
func TestImportRecord_PendingForTwoReasons_ResolvingOneLeavesTheOtherPending(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	rec, _, _ := stageBothPendingReasonsBatch(t, svc)

	// Resolve the occurrence match (dismiss) first.
	afterOccurrence, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.OccurrenceMatchResolutionDismissed),
	})
	if err != nil {
		t.Fatalf("ResolveImportRecordOccurrenceMatch: %v", err)
	}
	if afterOccurrence.Record.Status() != importing.ImportRecordStatusPending {
		t.Fatalf("Status() after resolving only the occurrence match = %q, want %q (the duplicate match is still unresolved)",
			afterOccurrence.Record.Status(), importing.ImportRecordStatusPending)
	}
	om, ok := afterOccurrence.Record.OccurrenceMatch()
	if !ok || om.Resolution() != importing.OccurrenceMatchResolutionDismissed {
		t.Errorf("OccurrenceMatch() = (%+v, %v), want resolution %q already recorded", om, ok, importing.OccurrenceMatchResolutionDismissed)
	}

	// Now resolve the duplicate match too (dismiss it as well) — only now
	// should the record leave pending.
	afterDuplicate, err := svc.ResolveImportRecord(ctx, app.ResolveImportRecordCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.DuplicateResolutionDismissed),
	})
	if err != nil {
		t.Fatalf("ResolveImportRecord: %v", err)
	}
	if afterDuplicate.Record.Status() != importing.ImportRecordStatusReady {
		t.Errorf("Status() after resolving both reasons = %q, want %q", afterDuplicate.Record.Status(), importing.ImportRecordStatusReady)
	}
}

// TestImportRecord_PendingForTwoReasons_MaterializeExcludesEvenIfDuplicateDismissed
// proves SettleStatus's exclusion-wins-over-ready tiebreak: if the
// occurrence match resolves to materialize (excluding the record, since
// the occurrence's own new transaction now covers this money) while the
// coexisting DuplicateMatch later resolves to dismissed ("not a
// duplicate" — proceed to commit), the record must still end up Excluded,
// not Ready — committing it too would double-count the now-materialised
// occurrence's money regardless of what the DuplicateMatch decided.
func TestImportRecord_PendingForTwoReasons_MaterializeExcludesEvenIfDuplicateDismissed(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	rec, _, occID := stageBothPendingReasonsBatch(t, svc)

	afterOccurrence, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.OccurrenceMatchResolutionMaterialized),
	})
	if err != nil {
		t.Fatalf("ResolveImportRecordOccurrenceMatch: %v", err)
	}
	if afterOccurrence.Record.Status() != importing.ImportRecordStatusPending {
		t.Fatalf("Status() after resolving only the occurrence match = %q, want %q (the duplicate match is still unresolved)",
			afterOccurrence.Record.Status(), importing.ImportRecordStatusPending)
	}
	// The occurrence itself is materialised immediately regardless of the
	// record's own status still being pending — the side effect doesn't
	// wait on the coexisting DuplicateMatch.
	occ, err := svc.ScheduledOccurrences.Get(ctx, testActorID, occID)
	if err != nil {
		t.Fatalf("ScheduledOccurrences.Get: %v", err)
	}
	if occ.Status() != "materialised" {
		t.Errorf("occurrence Status() = %q, want materialised", occ.Status())
	}

	afterDuplicate, err := svc.ResolveImportRecord(ctx, app.ResolveImportRecordCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.DuplicateResolutionDismissed),
	})
	if err != nil {
		t.Fatalf("ResolveImportRecord: %v", err)
	}
	if afterDuplicate.Record.Status() != importing.ImportRecordStatusExcluded {
		t.Errorf("Status() = %q, want %q — a materialized occurrence match must win over a dismissed duplicate match",
			afterDuplicate.Record.Status(), importing.ImportRecordStatusExcluded)
	}
}

// stageDescriptionMismatchOccurrenceBatch stages a single row whose
// amount, date, and currency match a pending occurrence exactly, but
// whose description does not pass descriptionsSimilar's 0.5 Jaccard
// threshold — issue #307's real-world gap, found in manual testing:
// findOccurrenceMatch's own staging-time gate requires description
// similarity on top of the amount/date/currency match, so this row
// clears review as ImportRecordStatusReady with no OccurrenceMatch
// attached at all, even though SuggestForImportBatch's own looser
// candidate narrowing (no description filter, per ADR-0015 "the model
// answers the semantics") would offer the same occurrence as an AI
// suggestion.
func stageDescriptionMismatchOccurrenceBatch(t *testing.T, svc *app.Service) (rec importing.ImportRecord, batchID, occID string) {
	t.Helper()
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	rent := mustCreateCategory(t, svc, "Rent")

	_, occ := mustCreateRuleWithOccurrence(t, svc, acc.Account.ID(), rent, "1800.00", "Monthly Rent", "2026-08-01")

	// "RENT PMT ONLINE" vs. the rule's own "Monthly Rent": one shared
	// token ("rent") out of four distinct tokens total (Jaccard 0.25) —
	// below descriptionSimilarityThreshold (0.5), unlike
	// stageOccurrenceMatchBatch's "Rent Payment" against "Rent".
	data := "Date,Description,Amount\n2026-08-01,RENT PMT ONLINE,-1800.00\n"
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
	got := result.Records[0]
	if got.Status() != importing.ImportRecordStatusReady {
		t.Fatalf("test setup: Status() = %q, want %q (this fixture's whole point is that findOccurrenceMatch misses it)",
			got.Status(), importing.ImportRecordStatusReady)
	}
	if _, ok := got.OccurrenceMatch(); ok {
		t.Fatal("test setup: OccurrenceMatch() ok = true, want false")
	}
	return got, result.Batch.ID(), occ.ID()
}

// TestResolveImportRecordOccurrenceMatch_AIOnlyMatch_MaterializedExcludesReadyRecord
// is issue #307's own regression test for a real double-count bug found
// in manual testing against a live typesafe.ai key: typesafe.ai correctly
// suggested an occurrence match findOccurrenceMatch's own staging-time
// gate had missed (a real bank statement description rarely shares
// enough tokens with a user's own rule description), but accepting it
// through the review screen's "Accept match" button did nothing — before
// this fix, ResolveImportRecordOccurrenceMatch refused (PreconditionFailed)
// any record with no pre-existing OccurrenceMatch at all, and the row
// went on to commit as an ordinary transaction while the occurrence it
// duplicated stayed pending, double-counting the same money.
func TestResolveImportRecordOccurrenceMatch_AIOnlyMatch_MaterializedExcludesReadyRecord(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	rec, batchID, occID := stageDescriptionMismatchOccurrenceBatch(t, svc)

	result, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.OccurrenceMatchResolutionMaterialized),
		OccurrenceID: occID,
	})
	if err != nil {
		t.Fatalf("ResolveImportRecordOccurrenceMatch: %v", err)
	}
	if result.Record.Status() != importing.ImportRecordStatusExcluded {
		t.Errorf("Status() = %q, want %q (a Ready record accepting an AI-only occurrence match must still exclude, or its money double-counts)",
			result.Record.Status(), importing.ImportRecordStatusExcluded)
	}
	om, ok := result.Record.OccurrenceMatch()
	if !ok || om.Resolution() != importing.OccurrenceMatchResolutionMaterialized || om.OccurrenceID() != occID {
		t.Errorf("OccurrenceMatch() = (%+v, %v), want resolution %q against %q", om, ok, importing.OccurrenceMatchResolutionMaterialized, occID)
	}
	if result.Occurrence == nil || result.Occurrence.ID() != occID {
		t.Fatalf("Occurrence = %+v, want the materialised occurrence %q", result.Occurrence, occID)
	}
	if result.Transaction == nil {
		t.Fatal("Transaction = nil, want the transaction MaterialiseOccurrence produced")
	}

	// Persisted, not just returned.
	stored, err := svc.ImportRecords.Get(ctx, testActorID, rec.ID())
	if err != nil {
		t.Fatalf("ImportRecords.Get: %v", err)
	}
	if stored.Status() != importing.ImportRecordStatusExcluded {
		t.Errorf("stored Status() = %q, want %q", stored.Status(), importing.ImportRecordStatusExcluded)
	}

	// The whole point: committing the batch afterward must produce
	// nothing for this row — the occurrence's own transaction already
	// covers the money.
	commitResult, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: batchID})
	if err != nil {
		t.Fatalf("CommitImportBatch: %v", err)
	}
	if len(commitResult.Transactions) != 0 {
		t.Errorf("len(Transactions) = %d, want 0 (the excluded row must produce nothing on commit)", len(commitResult.Transactions))
	}
}

// TestResolveImportRecordOccurrenceMatch_AIOnlyMatch_RequiresOccurrenceID
// pins ResolveImportRecordOccurrenceMatch's existing refusal for a record
// with no OccurrenceMatch and no OccurrenceID given —
// TestResolveImportRecordOccurrenceMatch_NoOccurrenceMatchRefused already
// covers this for a dismissed resolution; this covers materialized, on a
// Ready (not Pending) record, to prove the new Ready/Pending precondition
// branch doesn't accidentally loosen it.
func TestResolveImportRecordOccurrenceMatch_AIOnlyMatch_RequiresOccurrenceID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	rec, _, _ := stageDescriptionMismatchOccurrenceBatch(t, svc)

	_, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.OccurrenceMatchResolutionMaterialized),
	})
	wantErrCode(t, err, errs.PreconditionFailed)
}

// TestResolveImportRecordOccurrenceMatch_AIOnlyMatch_IneligibleOccurrenceRejected
// proves the server-side re-validation: an OccurrenceID that doesn't
// actually name a date/amount/currency-eligible pending occurrence for
// this record is refused, even though it's a real, existing occurrence on
// the same account — a client (or a stale/tampered AI suggestion) must
// never be able to link arbitrary money to an arbitrary occurrence.
func TestResolveImportRecordOccurrenceMatch_AIOnlyMatch_IneligibleOccurrenceRejected(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	rec, _, _ := stageDescriptionMismatchOccurrenceBatch(t, svc)
	accountID, ok := rec.ResolvedAccountID()
	if !ok {
		t.Fatal("test setup: rec has no resolved account")
	}

	// A second rule/occurrence on the same account, but a different
	// amount — never eligible against rec's own -1800.00, regardless of
	// description.
	other := mustCreateCategory(t, svc, "Utilities")
	_, unrelatedOcc := mustCreateRuleWithOccurrence(t, svc, accountID, other, "75.00", "Electric", "2026-08-01")

	_, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.OccurrenceMatchResolutionMaterialized),
		OccurrenceID: unrelatedOcc.ID(),
	})
	wantErrCode(t, err, errs.PreconditionFailed)
}

// TestResolveImportRecordOccurrenceMatch_AIOnlyMatch_AlreadyCommittedRejected
// proves the terminal-status guard: once a Ready record has already
// committed (exactly what happens today if nobody notices an AI
// suggestion before clicking "Commit import"), accepting the same
// suggestion afterward must refuse rather than materialise a second,
// now-genuinely-double-counted transaction.
func TestResolveImportRecordOccurrenceMatch_AIOnlyMatch_AlreadyCommittedRejected(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	rec, batchID, occID := stageDescriptionMismatchOccurrenceBatch(t, svc)

	if _, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: batchID}); err != nil {
		t.Fatalf("CommitImportBatch: %v", err)
	}

	_, err := svc.ResolveImportRecordOccurrenceMatch(ctx, app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: testActorID, ImportRecordRef: rec.ID(), Resolution: string(importing.OccurrenceMatchResolutionMaterialized),
		OccurrenceID: occID,
	})
	wantErrCode(t, err, errs.PreconditionFailed)
}
