package app_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/app/importparse"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/ports"
)

// suggestionRowByRecordID indexes a fake provider call's rows by
// RecordID — SuggestForImportBatch's own contract is that a caller must
// never assume positional alignment, so every test below looks a row up
// this way rather than by index.
func suggestionRowByRecordID(rows []ports.SuggestionRow) map[string]ports.SuggestionRow {
	out := make(map[string]ports.SuggestionRow, len(rows))
	for _, r := range rows {
		out[r.RecordID] = r
	}
	return out
}

func suggestionResultByRecordID(suggestions []app.ImportRowSuggestion) map[string]app.ImportRowSuggestion {
	out := make(map[string]app.ImportRowSuggestion, len(suggestions))
	for _, s := range suggestions {
		out[s.RecordID] = s
	}
	return out
}

func categoryOptionIDs(opts []ports.CategoryOption) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.ID
	}
	return out
}

// TestSuggestForImportBatch_Unconfigured proves ADR-0015's "when there is
// no key, say so": a Service with no svc.Suggestions wired (newTestService
// leaves it at its zero value, exactly as an instance with no
// BODGER_TYPESAFE_API_KEY does) returns Configured: false, no error, and
// never touches a provider — fake is deliberately never assigned to
// svc.Suggestions, standing in for "what would have been called had a key
// been configured," so its empty call log proves the provider path was
// never reached.
func TestSuggestForImportBatch_Unconfigured(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	mustCategoryFixture(t, svc, "Groceries", "expense")

	data := "Date,Description,Amount\n2026-08-01,Whole Foods,-50.00\n"
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}

	fake := newFakeSuggestionProvider() // never wired to svc.Suggestions

	result, err := svc.SuggestForImportBatch(ctx, app.SuggestForImportBatchQuery{
		ActorID: testActorID, ImportBatchRef: staged.Batch.ID(),
	})
	if err != nil {
		t.Fatalf("SuggestForImportBatch: %v", err)
	}
	if result.Configured {
		t.Error("Configured = true, want false with no key configured")
	}
	if len(result.Suggestions) != 0 || result.RowsSuggested != 0 || result.RowsFailed != 0 || len(result.TooManyCategoryOptions) != 0 {
		t.Errorf("unconfigured result carries data: %+v", result)
	}
	if len(fake.calls) != 0 {
		t.Errorf("fake recorded %d calls, want 0 — the provider must never be called when unconfigured", len(fake.calls))
	}
}

// TestSuggestForImportBatch_CategoryOptionsMatchAmountSign proves
// ADR-0015's category narrowing: an outflow (negative amount) row is
// offered only expense categories, an inflow (positive amount) row only
// income — decided in code from the row's own amount sign, never a mixed
// set left for the model to sort out.
func TestSuggestForImportBatch_CategoryOptionsMatchAmountSign(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	groceries := mustCategoryFixture(t, svc, "Groceries", "expense")
	salary := mustCategoryFixture(t, svc, "Salary", "income")

	data := "Date,Description,Amount\n2026-08-01,Whole Foods,-50.00\n2026-08-02,Paycheck,2000.00\n"
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}

	fake := newFakeSuggestionProvider()
	svc.Suggestions = fake

	if _, err := svc.SuggestForImportBatch(ctx, app.SuggestForImportBatchQuery{
		ActorID: testActorID, ImportBatchRef: staged.Batch.ID(),
	}); err != nil {
		t.Fatalf("SuggestForImportBatch: %v", err)
	}

	if len(fake.calls) != 1 {
		t.Fatalf("fake recorded %d calls, want 1", len(fake.calls))
	}
	byRecordID := suggestionRowByRecordID(fake.calls[0])

	outflow := byRecordID[staged.Records[0].ID()]
	if got := categoryOptionIDs(outflow.Categories); len(got) != 1 || got[0] != groceries.Category.ID() {
		t.Errorf("outflow row Categories = %v, want only [%s]", got, groceries.Category.ID())
	}

	inflow := byRecordID[staged.Records[1].ID()]
	if got := categoryOptionIDs(inflow.Categories); len(got) != 1 || got[0] != salary.Category.ID() {
		t.Errorf("inflow row Categories = %v, want only [%s]", got, salary.Category.ID())
	}
}

// TestSuggestForImportBatch_OccurrenceCandidateNarrowing proves
// ADR-0015's occurrence-candidate narrowing: only a same-currency,
// same-amount, in-window pending occurrence survives as a candidate.
// duplicateDateWindowDays is 3 (import_duplicate.go), so a row booked
// exactly 3 days from the occurrence is still in-window and one booked 8
// days away is not; a currency or amount mismatch excludes a candidate
// regardless of how close the date is.
func TestSuggestForImportBatch_OccurrenceCandidateNarrowing(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	rent := mustCategoryFixture(t, svc, "Rent", "expense")
	_, occ := mustCreateRuleWithOccurrence(t, svc, acc.Account.ID(), rent.Category.ID(), "1200.00", "Rent", "2026-08-01")

	data := strings.Join([]string{
		"Date,Description,Amount,Currency",
		"2026-08-04,Rent Payment,-1200.00,USD", // 3 days away: in window, same amount+currency
		"2026-08-09,Rent Payment,-1200.00,USD", // 8 days away: outside the window
		"2026-08-02,Rent Payment,-1200.00,EUR", // in window, right amount, wrong currency
		"2026-08-02,Rent Payment,-1199.00,USD", // in window, right currency, wrong amount
		"",
	}, "\n")
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(data),
		ColumnMapping: importparse.ColumnMapping{
			DateColumn: "Date", DescriptionColumn: "Description", AmountColumn: "Amount", CurrencyColumn: "Currency",
		},
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if len(staged.Records) != 4 {
		t.Fatalf("staged %d records, want 4", len(staged.Records))
	}

	fake := newFakeSuggestionProvider()
	svc.Suggestions = fake

	if _, err := svc.SuggestForImportBatch(ctx, app.SuggestForImportBatchQuery{
		ActorID: testActorID, ImportBatchRef: staged.Batch.ID(),
	}); err != nil {
		t.Fatalf("SuggestForImportBatch: %v", err)
	}

	if len(fake.calls) != 1 {
		t.Fatalf("fake recorded %d calls, want 1", len(fake.calls))
	}
	byRecordID := suggestionRowByRecordID(fake.calls[0])

	inWindow := byRecordID[staged.Records[0].ID()]
	if len(inWindow.OccurrenceCandidates) != 1 || inWindow.OccurrenceCandidates[0].OccurrenceID != occ.ID() {
		t.Errorf("in-window row OccurrenceCandidates = %+v, want [%s]", inWindow.OccurrenceCandidates, occ.ID())
	}

	labels := []string{"8-days-away", "wrong-currency", "wrong-amount"}
	for i, label := range labels {
		row := byRecordID[staged.Records[i+1].ID()]
		if len(row.OccurrenceCandidates) != 0 {
			t.Errorf("%s row OccurrenceCandidates = %+v, want none (empty — the occurrence question isn't asked)", label, row.OccurrenceCandidates)
		}
	}
}

// TestSuggestForImportBatch_ConfidenceThreshold proves ADR-0015's 0.5
// display gate: below it a suggestion is discarded, at or above it the
// suggestion is returned — and, regardless of how high the confidence is,
// nothing this method returns is ever written to the ImportRecord itself.
func TestSuggestForImportBatch_ConfidenceThreshold(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	groceries := mustCategoryFixture(t, svc, "Groceries", "expense")

	data := "Date,Description,Amount\n2026-08-01,Store A,-10.00\n2026-08-02,Store B,-20.00\n2026-08-03,Store C,-30.00\n"
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}

	fake := newFakeSuggestionProvider()
	fake.setAnswer(ports.RowSuggestion{RecordID: staged.Records[0].ID(), CategoryID: groceries.Category.ID(), CategoryConfidence: 0.49})
	fake.setAnswer(ports.RowSuggestion{RecordID: staged.Records[1].ID(), CategoryID: groceries.Category.ID(), CategoryConfidence: 0.51})
	fake.setAnswer(ports.RowSuggestion{RecordID: staged.Records[2].ID(), CategoryID: groceries.Category.ID(), CategoryConfidence: 0.95})
	svc.Suggestions = fake

	result, err := svc.SuggestForImportBatch(ctx, app.SuggestForImportBatchQuery{
		ActorID: testActorID, ImportBatchRef: staged.Batch.ID(),
	})
	if err != nil {
		t.Fatalf("SuggestForImportBatch: %v", err)
	}

	byRecordID := suggestionResultByRecordID(result.Suggestions)
	if _, ok := byRecordID[staged.Records[0].ID()]; ok {
		t.Error("a 0.49-confidence suggestion was returned, want discarded")
	}
	s51, ok := byRecordID[staged.Records[1].ID()]
	if !ok || s51.Category == nil || s51.Category.Confidence != 0.51 {
		t.Errorf("0.51-confidence suggestion = %+v, ok=%v, want Category.Confidence=0.51", s51, ok)
	}
	s95, ok := byRecordID[staged.Records[2].ID()]
	if !ok || s95.Category == nil || s95.Category.Confidence != 0.95 {
		t.Errorf("0.95-confidence suggestion = %+v, ok=%v, want Category.Confidence=0.95", s95, ok)
	}
	if len(result.Suggestions) != 2 {
		t.Errorf("len(Suggestions) = %d, want 2", len(result.Suggestions))
	}
	// Ranked by confidence: the 0.95 suggestion outranks the 0.51 one.
	if result.Suggestions[0].RecordID != staged.Records[2].ID() {
		t.Errorf("Suggestions[0].RecordID = %q, want the 0.95-confidence row first", result.Suggestions[0].RecordID)
	}

	// "still not applied to anything" — no confidence ever writes.
	unchanged, err := svc.ImportRecords.Get(ctx, testActorID, staged.Records[2].ID())
	if err != nil {
		t.Fatalf("ImportRecords.Get: %v", err)
	}
	if _, ok := unchanged.ResolvedCategoryID(); ok {
		t.Error("ResolvedCategoryID() ok = true after a 0.95-confidence suggestion — a suggestion must never be applied")
	}
}

// TestSuggestForImportBatch_TooManyCategoryOptions proves ADR-0015's
// option-ceiling refusal: an actor with more than 254 categories of one
// kind gets no category suggestion for an affected row, distinctly
// reported via TooManyCategoryOptions rather than silently offering a
// truncated (and therefore possibly-wrong) option set. A row that also has
// an eligible occurrence candidate is still sent — just with no category
// question — proving this is a per-question refusal, not a per-row one.
func TestSuggestForImportBatch_TooManyCategoryOptions(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")

	var categories []app.CategoryResult
	for i := 0; i < 255; i++ {
		categories = append(categories, mustCategoryFixture(t, svc, fmt.Sprintf("Expense %03d", i), "expense"))
	}
	_, occ := mustCreateRuleWithOccurrence(t, svc, acc.Account.ID(), categories[0].Category.ID(), "75.00", "Utility", "2026-08-01")

	data := "Date,Description,Amount\n" +
		"2026-08-01,Store A,-10.00\n" + // no occurrence candidate: nothing to ask at all
		"2026-08-02,Utility Bill,-75.00\n" // matches the pending occurrence: still sent, for the occurrence question only
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}

	fake := newFakeSuggestionProvider()
	svc.Suggestions = fake

	result, err := svc.SuggestForImportBatch(ctx, app.SuggestForImportBatchQuery{
		ActorID: testActorID, ImportBatchRef: staged.Batch.ID(),
	})
	if err != nil {
		t.Fatalf("SuggestForImportBatch: %v", err)
	}

	wantTooMany := map[string]bool{staged.Records[0].ID(): true, staged.Records[1].ID(): true}
	if len(result.TooManyCategoryOptions) != 2 {
		t.Fatalf("TooManyCategoryOptions = %v, want both records", result.TooManyCategoryOptions)
	}
	for _, id := range result.TooManyCategoryOptions {
		if !wantTooMany[id] {
			t.Errorf("unexpected RecordID %q in TooManyCategoryOptions", id)
		}
	}

	// Only the second row had anything else worth asking about.
	if len(fake.calls) != 1 || len(fake.calls[0]) != 1 {
		t.Fatalf("fake received %v, want exactly one row across one call", fake.calls)
	}
	sent := fake.calls[0][0]
	if sent.RecordID != staged.Records[1].ID() {
		t.Errorf("the row sent had RecordID %q, want %q", sent.RecordID, staged.Records[1].ID())
	}
	if len(sent.Categories) != 0 {
		t.Errorf("sent.Categories = %v, want empty — no truncated option set", sent.Categories)
	}
	if len(sent.OccurrenceCandidates) != 1 || sent.OccurrenceCandidates[0].OccurrenceID != occ.ID() {
		t.Errorf("sent.OccurrenceCandidates = %+v, want [%s]", sent.OccurrenceCandidates, occ.ID())
	}
}

// TestSuggestForImportBatch_CapsAt200Rows proves ADR-0015's spend bound:
// 201 eligible rows staged in one batch produce exactly 200 suggestion
// requests, never 201.
func TestSuggestForImportBatch_CapsAt200Rows(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	mustCategoryFixture(t, svc, "Groceries", "expense")

	var b strings.Builder
	b.WriteString("Date,Description,Amount\n")
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 201; i++ {
		d := base.AddDate(0, 0, i)
		fmt.Fprintf(&b, "%s,Row %d,-%d.00\n", d.Format("2006-01-02"), i, i+1)
	}
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "big.csv", SourceFormat: "csv",
		FileContent: []byte(b.String()), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if len(staged.Records) != 201 {
		t.Fatalf("staged %d records, want 201", len(staged.Records))
	}

	fake := newFakeSuggestionProvider()
	svc.Suggestions = fake

	result, err := svc.SuggestForImportBatch(ctx, app.SuggestForImportBatchQuery{
		ActorID: testActorID, ImportBatchRef: staged.Batch.ID(),
	})
	if err != nil {
		t.Fatalf("SuggestForImportBatch: %v", err)
	}
	if result.RowsSuggested != 200 {
		t.Errorf("RowsSuggested = %d, want 200", result.RowsSuggested)
	}
	if len(fake.calls) != 1 || len(fake.calls[0]) != 200 {
		got := 0
		if len(fake.calls) > 0 {
			got = len(fake.calls[0])
		}
		t.Errorf("fake received %d rows across %d call(s), want 200 rows in 1 call", got, len(fake.calls))
	}
}

// TestSuggestForImportBatch_SkipsExcludedAndAlreadyCategorised proves
// ADR-0015's row-selection rules: a tier-1 exact duplicate (auto-excluded
// at staging) and a row whose category was already resolved
// deterministically (buildImportRecord's own CategoryHint match) are both
// left out of the suggestion request entirely.
func TestSuggestForImportBatch_SkipsExcludedAndAlreadyCategorised(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	mustCategoryFixture(t, svc, "Groceries", "expense")

	date, err := domain.NewDate(2026, time.August, 1)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	seedCommittedTransaction(t, svc, acc.Account.ID(), -450, "USD", "Coffee Shop", date, "bank-ext-1")

	data := "Date,Description,Amount,Ref,Category\n" +
		"2026-08-01,Coffee Shop,-4.50,bank-ext-1,\n" + // tier-1 exact duplicate -> excluded
		"2026-08-02,Whole Foods,-50.00,,Groceries\n" + // deterministically categorised
		"2026-08-03,Gas Station,-30.00,,\n" // eligible
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(data),
		ColumnMapping: importparse.ColumnMapping{
			DateColumn: "Date", DescriptionColumn: "Description", AmountColumn: "Amount",
			ExternalIDColumn: "Ref", CategoryColumn: "Category",
		},
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if len(staged.Records) != 3 {
		t.Fatalf("staged %d records, want 3", len(staged.Records))
	}
	if staged.Records[0].Status() != importing.ImportRecordStatusExcluded {
		t.Fatalf("test setup: record 0 Status() = %q, want excluded", staged.Records[0].Status())
	}
	if _, ok := staged.Records[1].ResolvedCategoryID(); !ok {
		t.Fatalf("test setup: record 1 has no ResolvedCategoryID")
	}

	fake := newFakeSuggestionProvider()
	svc.Suggestions = fake

	result, err := svc.SuggestForImportBatch(ctx, app.SuggestForImportBatchQuery{
		ActorID: testActorID, ImportBatchRef: staged.Batch.ID(),
	})
	if err != nil {
		t.Fatalf("SuggestForImportBatch: %v", err)
	}
	if result.RowsSuggested != 1 {
		t.Errorf("RowsSuggested = %d, want 1", result.RowsSuggested)
	}
	if len(fake.calls) != 1 || len(fake.calls[0]) != 1 || fake.calls[0][0].RecordID != staged.Records[2].ID() {
		t.Errorf("fake received %+v, want exactly [%s]", fake.calls, staged.Records[2].ID())
	}
}

// TestSuggestForImportBatch_ProviderFailureLeavesEveryRowReviewable proves
// ADR-0015's "a provider failure must never make the review unusable":
// every staged row is still listable through ListImportRecords, and
// SuggestForImportBatch itself returns no error, only a failure count.
func TestSuggestForImportBatch_ProviderFailureLeavesEveryRowReviewable(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	mustCategoryFixture(t, svc, "Groceries", "expense")

	data := "Date,Description,Amount\n2026-08-01,Store A,-10.00\n2026-08-02,Store B,-20.00\n"
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}

	fake := newFakeSuggestionProvider()
	fake.setFails(staged.Records[0].ID())
	fake.setFails(staged.Records[1].ID())
	svc.Suggestions = fake

	result, err := svc.SuggestForImportBatch(ctx, app.SuggestForImportBatchQuery{
		ActorID: testActorID, ImportBatchRef: staged.Batch.ID(),
	})
	if err != nil {
		t.Fatalf("SuggestForImportBatch returned an error on provider failure: %v", err)
	}
	if !result.Configured {
		t.Error("Configured = false, want true — the instance has a key, the provider just failed")
	}
	if len(result.Suggestions) != 0 {
		t.Errorf("Suggestions = %+v, want none", result.Suggestions)
	}
	if result.RowsFailed != 2 {
		t.Errorf("RowsFailed = %d, want 2", result.RowsFailed)
	}

	listed, err := svc.ListImportRecords(ctx, app.ListImportRecordsQuery{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()})
	if err != nil {
		t.Fatalf("ListImportRecords: %v", err)
	}
	if len(listed.Records) != 2 {
		t.Errorf("ListImportRecords returned %d records, want 2 — a provider failure must never make the review unusable", len(listed.Records))
	}
}

// TestSuggestForImportBatch_PartialFailure proves ADR-0015's "partial
// success is a success": one row failing never discards another row's
// good answer, and the caller learns how many rows failed.
func TestSuggestForImportBatch_PartialFailure(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "Checking", "bank", "USD")
	groceries := mustCategoryFixture(t, svc, "Groceries", "expense")

	data := "Date,Description,Amount\n2026-08-01,Store A,-10.00\n2026-08-02,Store B,-20.00\n2026-08-03,Store C,-30.00\n"
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Filename: "a.csv", SourceFormat: "csv",
		FileContent: []byte(data), ColumnMapping: basicCSVMapping(),
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}

	fake := newFakeSuggestionProvider()
	fake.setAnswer(ports.RowSuggestion{RecordID: staged.Records[0].ID(), CategoryID: groceries.Category.ID(), CategoryConfidence: 0.8})
	fake.setAnswer(ports.RowSuggestion{RecordID: staged.Records[1].ID(), CategoryID: groceries.Category.ID(), CategoryConfidence: 0.9})
	fake.setFails(staged.Records[2].ID())
	svc.Suggestions = fake

	result, err := svc.SuggestForImportBatch(ctx, app.SuggestForImportBatchQuery{
		ActorID: testActorID, ImportBatchRef: staged.Batch.ID(),
	})
	if err != nil {
		t.Fatalf("SuggestForImportBatch: %v", err)
	}
	if len(result.Suggestions) != 2 {
		t.Errorf("len(Suggestions) = %d, want 2", len(result.Suggestions))
	}
	if result.RowsFailed != 1 {
		t.Errorf("RowsFailed = %d, want 1", result.RowsFailed)
	}
}
