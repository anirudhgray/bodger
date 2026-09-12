package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/app/importparse"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// basicCSVMapping maps the three required importparse columns onto a
// plain "Date,Description,Amount" header — the shape every StageImport
// test below uses unless it specifically needs an optional column.
func basicCSVMapping() importparse.ColumnMapping {
	return importparse.ColumnMapping{
		DateColumn:        "Date",
		DescriptionColumn: "Description",
		AmountColumn:      "Amount",
	}
}

// seedCommittedTransaction creates a real, already-committed single-
// posting outflow directly through the domain and repository layers
// (bypassing RecordOutflow, which has no import-provenance/external-id
// parameter — only issue #211's Commit produces those) — standing in for
// "a transaction that reached the ledger some other way," which is
// exactly what tier-1/tier-2 duplicate detection compares a freshly
// staged row against.
func seedCommittedTransaction(t *testing.T, svc *app.Service, accountID string, amountMinor int64, currency, description string, date domain.Date, externalID string) string {
	t.Helper()
	amt, err := money.NewMoney(amountMinor, currency)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	posting, err := ledger.NewPosting(svc.IDs.NewID(), accountID, amt, nil, 0)
	if err != nil {
		t.Fatalf("NewPosting: %v", err)
	}
	var opts []ledger.TransactionOption
	if externalID != "" {
		opts = append(opts, ledger.WithImportProvenance("", externalID))
	}
	id := svc.IDs.NewID()
	txn, err := ledger.NewOutflow(id, testActorID, date, description, []ledger.Posting{posting}, opts...)
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}
	if err := svc.Transactions.Create(context.Background(), testActorID, txn, nil); err != nil {
		t.Fatalf("Transactions.Create: %v", err)
	}
	return id
}

func TestStageImport_BasicFile(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	data := "Date,Description,Amount\n" +
		"2026-08-01,Coffee Shop,-4.50\n" +
		"2026-08-02,Paycheck,1500.00\n"

	result, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID:       testActorID,
		AccountRef:    acc.Account.ID(),
		Filename:      "statement.csv",
		SourceFormat:  "csv",
		FileContent:   []byte(data),
		ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}

	if result.Batch.SourceFormat() != "csv" {
		t.Errorf("Batch.SourceFormat() = %q, want csv", result.Batch.SourceFormat())
	}
	if result.Batch.Filename() != "statement.csv" {
		t.Errorf("Batch.Filename() = %q, want statement.csv", result.Batch.Filename())
	}
	if result.Batch.TargetAccountID() != acc.Account.ID() {
		t.Errorf("Batch.TargetAccountID() = %q, want %q", result.Batch.TargetAccountID(), acc.Account.ID())
	}
	if result.Batch.Status() != importing.ImportBatchStatusStaged {
		t.Errorf("Batch.Status() = %q, want %q", result.Batch.Status(), importing.ImportBatchStatusStaged)
	}
	if len(result.Records) != 2 {
		t.Fatalf("len(Records) = %d, want 2", len(result.Records))
	}

	for i, want := range []struct {
		description string
		amountMinor int64
	}{
		{"Coffee Shop", -450},
		{"Paycheck", 150000},
	} {
		r := result.Records[i]
		if r.Description() != want.description {
			t.Errorf("Records[%d].Description() = %q, want %q", i, r.Description(), want.description)
		}
		if r.Amount().AmountMinor() != want.amountMinor {
			t.Errorf("Records[%d].Amount() minor = %d, want %d", i, r.Amount().AmountMinor(), want.amountMinor)
		}
		if r.SortOrder() != i {
			t.Errorf("Records[%d].SortOrder() = %d, want %d", i, r.SortOrder(), i)
		}
		if accID, ok := r.ResolvedAccountID(); !ok || accID != acc.Account.ID() {
			t.Errorf("Records[%d].ResolvedAccountID() = (%q, %v), want (%q, true)", i, accID, ok, acc.Account.ID())
		}
		if r.Status() != importing.ImportRecordStatusReady {
			t.Errorf("Records[%d].Status() = %q, want %q (no duplicate concern)", i, r.Status(), importing.ImportRecordStatusReady)
		}
		if _, ok := r.DuplicateMatch(); ok {
			t.Errorf("Records[%d].DuplicateMatch() ok = true, want false", i)
		}
	}

	// Persistence: both the batch and its records must actually be
	// written through the repositories, not just returned in memory.
	storedBatch, err := svc.ImportBatches.Get(ctx, testActorID, result.Batch.ID())
	if err != nil {
		t.Fatalf("ImportBatches.Get: %v", err)
	}
	if storedBatch.ID() != result.Batch.ID() {
		t.Errorf("stored batch ID = %q, want %q", storedBatch.ID(), result.Batch.ID())
	}
	storedRecords, err := svc.ImportRecords.ListByImportBatch(ctx, testActorID, result.Batch.ID())
	if err != nil {
		t.Fatalf("ImportRecords.ListByImportBatch: %v", err)
	}
	if len(storedRecords) != 2 {
		t.Fatalf("stored records = %d, want 2", len(storedRecords))
	}
}

func TestStageImport_TierOneExactDuplicateAutoExcluded(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	date, err := domain.NewDate(2026, time.August, 1)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	existingTxnID := seedCommittedTransaction(t, svc, acc.Account.ID(), -450, "USD", "Coffee Shop", date, "bank-ext-1")

	data := "Date,Description,Amount,Ref\n2026-08-01,Coffee Shop,-4.50,bank-ext-1\n"
	result, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID:      testActorID,
		AccountRef:   acc.Account.ID(),
		Filename:     "statement.csv",
		SourceFormat: "csv",
		FileContent:  []byte(data),
		ColumnMapping: importparse.ColumnMapping{
			DateColumn: "Date", DescriptionColumn: "Description", AmountColumn: "Amount", ExternalIDColumn: "Ref",
		},
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("len(Records) = %d, want 1", len(result.Records))
	}

	r := result.Records[0]
	if r.Status() != importing.ImportRecordStatusExcluded {
		t.Errorf("Status() = %q, want %q (a tier-1 exact match is skipped automatically)", r.Status(), importing.ImportRecordStatusExcluded)
	}
	dm, ok := r.DuplicateMatch()
	if !ok {
		t.Fatal("DuplicateMatch() ok = false, want true")
	}
	if dm.Tier() != importing.DuplicateMatchTierExact {
		t.Errorf("DuplicateMatch().Tier() = %q, want %q", dm.Tier(), importing.DuplicateMatchTierExact)
	}
	if dm.MatchedTransactionID() != existingTxnID {
		t.Errorf("DuplicateMatch().MatchedTransactionID() = %q, want %q", dm.MatchedTransactionID(), existingTxnID)
	}
}

func TestStageImport_TierTwoSuspectedDuplicateFlaggedNotExcluded(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	date, err := domain.NewDate(2026, time.August, 1)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	// No external ID on the existing transaction, so tier-1 can't apply —
	// this exercises tier-2's own heuristic in isolation. The booked date
	// is two days off (within the +/-3 day window) and the description is
	// a plausible rewording, not identical, to prove the heuristic (not a
	// literal equality check) is what's firing.
	existingTxnID := seedCommittedTransaction(t, svc, acc.Account.ID(), -450, "USD", "STARBUCKS COFFEE 4521", date, "")

	dateTwoDaysLater, err := domain.NewDate(2026, time.August, 3)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
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

	r := result.Records[0]
	if !r.BookedDate().Equal(dateTwoDaysLater) {
		t.Fatalf("test setup: record booked date = %s, want %s", r.BookedDate(), dateTwoDaysLater)
	}
	if r.Status() != importing.ImportRecordStatusPending {
		t.Errorf("Status() = %q, want %q (a tier-2 match is never auto-applied)", r.Status(), importing.ImportRecordStatusPending)
	}
	dm, ok := r.DuplicateMatch()
	if !ok {
		t.Fatal("DuplicateMatch() ok = false, want true")
	}
	if dm.Tier() != importing.DuplicateMatchTierSuspected {
		t.Errorf("DuplicateMatch().Tier() = %q, want %q", dm.Tier(), importing.DuplicateMatchTierSuspected)
	}
	if dm.MatchedTransactionID() != existingTxnID {
		t.Errorf("DuplicateMatch().MatchedTransactionID() = %q, want %q", dm.MatchedTransactionID(), existingTxnID)
	}
	if dm.Resolved() {
		t.Error("DuplicateMatch().Resolved() = true, want false (nothing has reviewed it yet)")
	}
}

// TestStageImport_FalsePositiveTwoIdenticalCoffeesAreNeverSilentlyMerged is
// ADR-0008's own named false-positive case: "the false-positive case is
// two genuine identical coffees on the same day, and silently dropping
// one of those is a wrong balance the user has no way to notice." A
// statement covering that day is imported for the first time and turns
// out to list two coffee purchases, one of which happens to match an
// existing transaction already on the ledger (from a prior manual entry
// or import). Both new rows are genuine, distinct purchases; the
// pipeline must keep both as separate, independently reviewable
// ImportRecords rather than silently excluding either one just because
// each looks like a plausible duplicate of the same existing transaction.
func TestStageImport_FalsePositiveTwoIdenticalCoffeesAreNeverSilentlyMerged(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	date, err := domain.NewDate(2026, time.August, 1)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	existingTxnID := seedCommittedTransaction(t, svc, acc.Account.ID(), -450, "USD", "Coffee Shop", date, "")

	// Two genuinely separate coffee purchases, same account/amount/
	// currency/date/description as each other AND as the one already on
	// the ledger.
	data := "Date,Description,Amount\n" +
		"2026-08-01,Coffee Shop,-4.50\n" +
		"2026-08-01,Coffee Shop,-4.50\n"
	result, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "statement.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}

	// The invariant this test exists for: both coffees must survive as
	// two separate records. A silent merge would show up here as 1, or as
	// one of the two coming back excluded below.
	if len(result.Records) != 2 {
		t.Fatalf("len(Records) = %d, want 2 — neither genuine purchase may be silently dropped", len(result.Records))
	}

	for i, r := range result.Records {
		if r.Status() == importing.ImportRecordStatusExcluded {
			t.Errorf("Records[%d].Status() = %q, want anything but excluded — a tier-2 match must never be auto-applied", i, r.Status())
		}
		if r.Status() != importing.ImportRecordStatusPending {
			t.Errorf("Records[%d].Status() = %q, want %q (awaiting the user's review)", i, r.Status(), importing.ImportRecordStatusPending)
		}
		dm, ok := r.DuplicateMatch()
		if !ok {
			t.Fatalf("Records[%d].DuplicateMatch() ok = false, want true (both rows plausibly match the existing transaction)", i)
		}
		if dm.Tier() != importing.DuplicateMatchTierSuspected {
			t.Errorf("Records[%d].DuplicateMatch().Tier() = %q, want %q", i, dm.Tier(), importing.DuplicateMatchTierSuspected)
		}
		if dm.MatchedTransactionID() != existingTxnID {
			t.Errorf("Records[%d].DuplicateMatch().MatchedTransactionID() = %q, want %q", i, dm.MatchedTransactionID(), existingTxnID)
		}
	}
}

func TestStageImport_TransferCandidateDetectedAcrossBatches(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	checking := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	savings := mustAccountFixture(t, svc, "Savings", "bank", "USD")

	// First leg: money leaving checking.
	outflowData := "Date,Description,Amount\n2026-08-01,Transfer to savings,-500.00\n"
	outResult, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: checking.Account.ID(), Filename: "checking.csv", SourceFormat: "csv",
		FileContent: []byte(outflowData), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport(checking): %v", err)
	}
	if _, ok := outResult.Records[0].TransferCandidateRecordID(); ok {
		t.Error("the first-staged leg found a transfer candidate that didn't exist yet, want false")
	}

	// Second leg: the same movement arriving in savings, a few days later
	// (within the +/-3 day window), staged afterwards.
	inflowData := "Date,Description,Amount\n2026-08-03,Transfer from checking,500.00\n"
	inResult, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: savings.Account.ID(), Filename: "savings.csv", SourceFormat: "csv",
		FileContent: []byte(inflowData), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport(savings): %v", err)
	}

	got, ok := inResult.Records[0].TransferCandidateRecordID()
	if !ok {
		t.Fatal("TransferCandidateRecordID() ok = false, want true")
	}
	if got != outResult.Records[0].ID() {
		t.Errorf("TransferCandidateRecordID() = %q, want %q (the checking outflow)", got, outResult.Records[0].ID())
	}
	// A transfer candidate is advisory only — it must never change the
	// record's own status or exclude it from commit by itself.
	if inResult.Records[0].Status() != importing.ImportRecordStatusReady {
		t.Errorf("Status() = %q, want %q — a transfer candidate is proposed, never applied automatically", inResult.Records[0].Status(), importing.ImportRecordStatusReady)
	}
}

func TestStageImport_TransferCandidateIgnoresSameAccount(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	outflowData := "Date,Description,Amount\n2026-08-01,Coffee Shop,-500.00\n"
	if _, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(outflowData), ColumnMapping: basicCSVMapping(),
	}); err != nil {
		t.Fatalf("StageImport(a): %v", err)
	}

	// A same-account, opposite-signed, matching-amount row in a later
	// batch must not be treated as a cross-account transfer candidate —
	// same target account as the first batch.
	inflowData := "Date,Description,Amount\n2026-08-02,Refund,500.00\n"
	result, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "b.csv", SourceFormat: "csv",
		FileContent: []byte(inflowData), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport(b): %v", err)
	}
	if _, ok := result.Records[0].TransferCandidateRecordID(); ok {
		t.Error("TransferCandidateRecordID() ok = true for a same-account match, want false")
	}
}

func TestStageImport_CategoryHintResolved(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	cat := mustCategoryFixture(t, svc, "Groceries", "expense")

	data := "Date,Description,Amount,Category\n2026-08-01,Whole Foods,-50.00,Groceries\n"
	result, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(data),
		ColumnMapping: importparse.ColumnMapping{
			DateColumn: "Date", DescriptionColumn: "Description", AmountColumn: "Amount", CategoryColumn: "Category",
		},
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	got, ok := result.Records[0].ResolvedCategoryID()
	if !ok || got != cat.Category.ID() {
		t.Errorf("ResolvedCategoryID() = (%q, %v), want (%q, true)", got, ok, cat.Category.ID())
	}
}

func TestStageImport_UnresolvedCategoryHintLeavesFieldUnset(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	data := "Date,Description,Amount,Category\n2026-08-01,Whole Foods,-50.00,Nonexistent Category\n"
	result, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(data),
		ColumnMapping: importparse.ColumnMapping{
			DateColumn: "Date", DescriptionColumn: "Description", AmountColumn: "Amount", CategoryColumn: "Category",
		},
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if _, ok := result.Records[0].ResolvedCategoryID(); ok {
		t.Error("ResolvedCategoryID() ok = true for a category hint with no match, want false")
	}
	if result.Records[0].Status() != importing.ImportRecordStatusReady {
		t.Errorf("Status() = %q, want %q — an unresolved category hint isn't a fatal error", result.Records[0].Status(), importing.ImportRecordStatusReady)
	}
}

func TestStageImport_CurrencyFallsBackToAccountCurrency(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Euro account", "bank", "EUR")

	data := "Date,Description,Amount\n2026-08-01,Coffee,-4.50\n"
	result, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if got := result.Records[0].Amount().Currency(); got != "EUR" {
		t.Errorf("Amount().Currency() = %q, want EUR (the account's own currency)", got)
	}
}

// TestStageImport_EmptyFormatDefaultsToCSV proves the application layer,
// not a surface, is what decides an omitted format means "csv" —
// ADR-0005: "no surface may resolve any default." Issue #212's HTTP/CLI
// surfaces both pass an omitted format through as "" rather than
// hardcoding "csv" themselves.
func TestStageImport_EmptyFormatDefaultsToCSV(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	result, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "",
		FileContent: []byte("Date,Description,Amount\n2026-08-01,Coffee,-4.50\n"), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if result.Batch.SourceFormat() != "csv" {
		t.Errorf("Batch.SourceFormat() = %q, want csv", result.Batch.SourceFormat())
	}
}

func TestStageImport_RejectsUnsupportedFormat(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	_, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.ofx", SourceFormat: "ofx",
		FileContent: []byte("garbage"), ColumnMapping: basicCSVMapping(),
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestStageImport_RejectsUnknownAccountRef(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: "no-such-account", Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte("Date,Description,Amount\n2026-08-01,Coffee,-4.50\n"), ColumnMapping: basicCSVMapping(),
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestStageImport_RejectsMissingActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.StageImport(ctx, app.StageImportCommand{
		AccountRef: "irrelevant", Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte("Date,Description,Amount\n2026-08-01,Coffee,-4.50\n"), ColumnMapping: basicCSVMapping(),
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestStageImport_RejectsMissingFilename(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	_, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "   ", SourceFormat: "csv",
		FileContent: []byte("Date,Description,Amount\n2026-08-01,Coffee,-4.50\n"), ColumnMapping: basicCSVMapping(),
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestStageImport_PropagatesParserErrors(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	_, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(""), ColumnMapping: basicCSVMapping(),
	})
	wantErrCode(t, err, errs.InvalidInput)
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *errs.Error", err)
	}
}
