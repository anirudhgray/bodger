package app_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
)

func TestExportCSV_RequiresActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.ExportCSV(context.Background(), app.ExportCSVQuery{})
	if err == nil {
		t.Fatal("want an error for a missing actor ID")
	}
}

func mustParseCSV(t *testing.T, out []byte) [][]string {
	t.Helper()
	rows, err := csv.NewReader(bytes.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("output isn't valid CSV: %v\n%s", err, out)
	}
	return rows
}

func TestExportCSV_HeaderAndOneRowPerPosting(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	acc := mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")
	cat := mustCategoryFixture(t, svc, "Groceries", "expense")
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "500", CategoryRef: cat.Category.ID(),
		Date: "2026-08-14", Description: "Big Bazaar", Notes: "weekly shop", Tags: []string{"food", "monthly"},
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	out, err := svc.ExportCSV(ctx, app.ExportCSVQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	rows := mustParseCSV(t, out)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (header + 1 posting)", len(rows))
	}

	header := rows[0]
	wantHeader := []string{
		"transaction_id", "booked_date", "posted_date", "kind", "description", "notes", "tags",
		"external_id", "related_transaction_id", "posting_id", "account_id", "category_id", "amount", "currency",
	}
	if len(header) != len(wantHeader) {
		t.Fatalf("header = %v, want %v", header, wantHeader)
	}
	for i, col := range wantHeader {
		if header[i] != col {
			t.Errorf("header[%d] = %q, want %q", i, header[i], col)
		}
	}

	col := func(name string) string {
		for i, h := range header {
			if h == name {
				return rows[1][i]
			}
		}
		t.Fatalf("no column %q", name)
		return ""
	}
	if col("booked_date") != "2026-08-14" {
		t.Errorf("booked_date = %q, want 2026-08-14", col("booked_date"))
	}
	if col("kind") != "outflow" {
		t.Errorf("kind = %q, want outflow", col("kind"))
	}
	if col("description") != "Big Bazaar" {
		t.Errorf("description = %q, want Big Bazaar", col("description"))
	}
	if col("notes") != "weekly shop" {
		t.Errorf("notes = %q, want %q", col("notes"), "weekly shop")
	}
	if col("tags") != "food|monthly" {
		t.Errorf("tags = %q, want food|monthly", col("tags"))
	}
	if col("account_id") != acc.Account.ID() {
		t.Errorf("account_id = %q, want %q", col("account_id"), acc.Account.ID())
	}
	if col("category_id") != cat.Category.ID() {
		t.Errorf("category_id = %q, want %q", col("category_id"), cat.Category.ID())
	}
	if col("amount") != "-500.00" {
		t.Errorf("amount = %q, want -500.00", col("amount"))
	}
	if col("currency") != "INR" {
		t.Errorf("currency = %q, want INR", col("currency"))
	}
}

// TestExportCSV_SplitTransactionProducesOneRowPerPosting is ADR-0008's
// "one row per posting, transaction fields denormalised across it": a
// two-posting split outflow becomes two CSV rows sharing every
// transaction-level field (transaction_id, booked_date, description, ...)
// and differing only in their own posting_id/account_id/category_id/
// amount.
func TestExportCSV_SplitTransactionProducesOneRowPerPosting(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	acc := mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")
	groceriesID, householdID := mustSplitOutflowFixture(t, svc, acc)

	out, err := svc.ExportCSV(ctx, app.ExportCSVQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	rows := mustParseCSV(t, out)
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3 (header + 2 postings)", len(rows))
	}

	header := rows[0]
	idx := func(name string) int {
		for i, h := range header {
			if h == name {
				return i
			}
		}
		t.Fatalf("no column %q", name)
		return -1
	}
	txnIDCol, categoryCol, amountCol := idx("transaction_id"), idx("category_id"), idx("amount")
	descCol := idx("description")

	if rows[1][txnIDCol] != rows[2][txnIDCol] {
		t.Errorf("both posting rows should share transaction_id: %q vs %q", rows[1][txnIDCol], rows[2][txnIDCol])
	}
	if rows[1][descCol] != rows[2][descCol] {
		t.Errorf("both posting rows should share description: %q vs %q", rows[1][descCol], rows[2][descCol])
	}
	if rows[1][categoryCol] != groceriesID {
		t.Errorf("row 1 category_id = %q, want groceries %q (sort_order 0)", rows[1][categoryCol], groceriesID)
	}
	if rows[2][categoryCol] != householdID {
		t.Errorf("row 2 category_id = %q, want household %q (sort_order 1)", rows[2][categoryCol], householdID)
	}
	if rows[1][amountCol] != "-300.00" {
		t.Errorf("row 1 amount = %q, want -300.00", rows[1][amountCol])
	}
	if rows[2][amountCol] != "-150.00" {
		t.Errorf("row 2 amount = %q, want -150.00", rows[2][amountCol])
	}
}

func TestExportCSV_QuotesDescriptionsWithCommasAndQuotes(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")
	// A raw newline can't reach here: normalize.Text (ADR-0005) collapses
	// internal whitespace, including newlines, to a single space before a
	// description is ever stored -- so a comma and an embedded quote are
	// this test's actual RFC 4180 stress case for a value export can
	// really encounter.
	tricky := `Comma, "quote", and more`
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "500",
		Date: "2026-08-14", Description: tricky,
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	out, err := svc.ExportCSV(ctx, app.ExportCSVQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	rows := mustParseCSV(t, out)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	descCol := 0
	for i, h := range rows[0] {
		if h == "description" {
			descCol = i
		}
	}
	if rows[1][descCol] != tricky {
		t.Errorf("description round-tripped to %q, want %q", rows[1][descCol], tricky)
	}
	if !strings.Contains(string(out), `"Comma, ""quote""`) {
		t.Errorf("expected RFC 4180 double-quote escaping in raw output, got:\n%s", out)
	}
}

// TestExportCSV_DeterministicAcrossRepeatedCalls mirrors
// TestExportJSON_DeterministicAcrossRepeatedCalls for the CSV writer: same
// underlying data must produce byte-identical CSV output on every call.
func TestExportCSV_DeterministicAcrossRepeatedCalls(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	savings := mustAccountFixture(t, svc, "Savings", "bank", "INR")
	checking := mustAccountFixture(t, svc, "Checking", "bank", "INR")
	mustSplitOutflowFixture(t, svc, savings)
	if _, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: savings.Account.ID(), ToAccountRef: checking.Account.ID(),
		Amount: "20000", Date: "2026-08-14", Description: "Move to checking",
	}); err != nil {
		t.Fatalf("RecordTransfer: %v", err)
	}
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: checking.Account.ID(), Amount: "1000",
		Date: "2026-08-14", Description: "Refund", Tags: []string{"refund"},
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}

	first, err := svc.ExportCSV(ctx, app.ExportCSVQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportCSV (first): %v", err)
	}
	if len(first) == 0 {
		t.Fatal("ExportCSV returned no bytes")
	}
	for i := 0; i < 20; i++ {
		again, err := svc.ExportCSV(ctx, app.ExportCSVQuery{ActorID: testActorID})
		if err != nil {
			t.Fatalf("ExportCSV (repeat %d): %v", i, err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("repeat %d: output differs from first call\nfirst:\n%s\nagain:\n%s", i, first, again)
		}
	}
}
