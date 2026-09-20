package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain/importing"
)

// TestStageImport_PendingOccurrenceDetectedAsCandidateMatch is issue #301's
// regression test: before this fix, findSuspectedDuplicate only ever
// searched s.Transactions (already-committed money), so a pending
// recurring.ScheduledOccurrence — generated ahead of time by
// GenerateOccurrences but never yet paid — was invisible to duplicate
// detection. Importing a bank row for that same rent payment committed as a
// brand-new transaction with nothing flagging it, leaving the occurrence
// sitting there unmaterialised: the same money once in the ledger and once
// in the forecast.
//
// This proves the fix: staging a CSV row whose amount (through the rule's
// own account/category, never through the occurrence itself — ADR-0014),
// booked_date, and description line up with a pending occurrence now
// records that occurrence's ID on the resulting ImportRecord — and (issue
// #309, the follow-up that gives detection an observable effect on
// commit) holds the record pending until the match is resolved via
// ResolveImportRecordOccurrenceMatch.
func TestStageImport_PendingOccurrenceDetectedAsCandidateMatch(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	rent := mustCreateCategory(t, svc, "Rent")

	_, occ := mustCreateRuleWithOccurrence(t, svc, acc.Account.ID(), rent, "1200.00", "Rent", "2026-08-01")
	if occ.Status() != "pending" {
		t.Fatalf("test setup: occurrence status = %q, want pending", occ.Status())
	}

	// Booked a couple of days after the occurrence's projected date (within
	// the +/-3 day window) and a plausible rewording of the rule's own
	// description, not identical — the same "heuristic, not literal
	// equality" shape tier-2's own suspected-duplicate test uses.
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

	r := result.Records[0]
	got, ok := r.MatchedOccurrenceID()
	if !ok {
		t.Fatal("MatchedOccurrenceID() ok = false, want true — the pending occurrence should have been detected")
	}
	if got != occ.ID() {
		t.Errorf("MatchedOccurrenceID() = %q, want %q", got, occ.ID())
	}

	// Unlike a transfer candidate, an occurrence match does name a genuine
	// double-counting risk (the same money, once as a pending occurrence,
	// once as this freshly-imported row) — issue #309 gates commit on it
	// the same way a suspected duplicate is, leaving the record pending
	// until ResolveImportRecordOccurrenceMatch clears it.
	if r.Status() != importing.ImportRecordStatusPending {
		t.Errorf("Status() = %q, want %q — an unresolved occurrence match must block commit", r.Status(), importing.ImportRecordStatusPending)
	}
	if _, ok := r.DuplicateMatch(); ok {
		t.Error("DuplicateMatch() ok = true, want false — no committed transaction exists for this row")
	}
}

// TestStageImport_OccurrenceMatchIgnoresOtherAccounts proves occurrence
// matching is scoped per-account, the same as tier-2's own duplicate
// heuristic: a pending occurrence belonging to a rule on a different
// account must never be reported as a candidate for a row landing on this
// account, even if its amount, date, and description would otherwise line
// up.
func TestStageImport_OccurrenceMatchIgnoresOtherAccounts(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	savings := mustAccountFixture(t, svc, "Savings", "bank", "USD")
	checking := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	rent := mustCreateCategory(t, svc, "Rent")

	// The rule (and its generated occurrence) belongs to savings.
	mustCreateRuleWithOccurrence(t, svc, savings.Account.ID(), rent, "1200.00", "Rent", "2026-08-01")

	// The CSV row targets a different account (checking).
	data := "Date,Description,Amount\n2026-08-01,Rent,-1200.00\n"
	result, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: checking.Account.ID(), Filename: "statement.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if _, ok := result.Records[0].MatchedOccurrenceID(); ok {
		t.Error("MatchedOccurrenceID() ok = true for a different account's occurrence, want false")
	}
}
