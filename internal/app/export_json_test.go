package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
)

func TestExportJSON_RequiresActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.ExportJSON(context.Background(), app.ExportJSONQuery{})
	if err == nil {
		t.Fatal("want an error for a missing actor ID")
	}
}

func TestExportJSON_EnvelopeShape(t *testing.T) {
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

	out, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output isn't valid JSON: %v\n%s", err, out)
	}

	if doc["format"] != "bodger.export/v1" {
		t.Errorf(`doc["format"] = %v, want "bodger.export/v1"`, doc["format"])
	}
	for _, key := range []string{"accounts", "categories", "transactions"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("doc missing key %q", key)
		}
	}
	// ADR-0008: budgets/budget lines (no domain type yet -- M7) and FX
	// rates (no port method to enumerate every stored row) are both
	// genuinely absent from this version of the payload -- see
	// ExportSnapshot's doc comment. Asserting their absence here pins that
	// decision so a future accidental `"budgets": null` doesn't slip in
	// unnoticed.
	for _, key := range []string{"budgets", "budget_lines", "fx_rates"} {
		if _, ok := doc[key]; ok {
			t.Errorf("doc unexpectedly has key %q", key)
		}
	}

	accounts, ok := doc["accounts"].([]any)
	if !ok || len(accounts) != 1 {
		t.Fatalf("accounts = %v, want a one-element array", doc["accounts"])
	}
	accountObj := accounts[0].(map[string]any)
	if accountObj["id"] != acc.Account.ID() {
		t.Errorf("account id = %v, want %q", accountObj["id"], acc.Account.ID())
	}
	if accountObj["name"] != "HDFC Savings" {
		t.Errorf("account name = %v, want HDFC Savings", accountObj["name"])
	}
	openingBalance, ok := accountObj["opening_balance"].(map[string]any)
	if !ok {
		t.Fatalf("opening_balance = %v, want an object (money.Money's own wire shape)", accountObj["opening_balance"])
	}
	if openingBalance["currency"] != "INR" {
		t.Errorf("opening_balance.currency = %v, want INR", openingBalance["currency"])
	}

	transactions, ok := doc["transactions"].([]any)
	if !ok || len(transactions) != 1 {
		t.Fatalf("transactions = %v, want a one-element array", doc["transactions"])
	}
	txnObj := transactions[0].(map[string]any)
	if txnObj["booked_date"] != "2026-08-14" {
		t.Errorf("booked_date = %v, want 2026-08-14", txnObj["booked_date"])
	}
	if txnObj["description"] != "Big Bazaar" {
		t.Errorf("description = %v, want Big Bazaar", txnObj["description"])
	}
	tags, ok := txnObj["tags"].([]any)
	if !ok || len(tags) != 2 {
		t.Fatalf("tags = %v, want a two-element array", txnObj["tags"])
	}
	if tags[0] != "food" || tags[1] != "monthly" {
		t.Errorf("tags = %v, want [food monthly] (alphabetical)", tags)
	}
	postings, ok := txnObj["postings"].([]any)
	if !ok || len(postings) != 1 {
		t.Fatalf("postings = %v, want a one-element array", txnObj["postings"])
	}
	postingObj := postings[0].(map[string]any)
	if postingObj["account_id"] != acc.Account.ID() {
		t.Errorf("posting account_id = %v, want %q", postingObj["account_id"], acc.Account.ID())
	}
	if postingObj["category_id"] != cat.Category.ID() {
		t.Errorf("posting category_id = %v, want %q", postingObj["category_id"], cat.Category.ID())
	}
}

// TestExportJSON_NoTimestampFields guards ADR-0008's "no incidental
// timestamps... of the export itself in the payload": the canonical JSON
// document does not carry any generated-at/exported-at style field,
// because such a field would make two exports of the identical underlying
// data at two different moments produce two different documents.
func TestExportJSON_NoTimestampFields(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")

	out, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output isn't valid JSON: %v", err)
	}
	for _, forbidden := range []string{"generated_at", "exported_at", "created_at", "timestamp"} {
		if _, ok := doc[forbidden]; ok {
			t.Errorf("doc has a top-level %q field -- export must carry no timestamp of the export itself", forbidden)
		}
	}
}

// TestExportJSON_DeterministicAcrossRepeatedCalls is this issue's central
// requirement: "same input -> byte-identical output across repeated
// runs". It builds a reasonably rich fixture (several accounts,
// categories, a split transaction, a transfer, tags) and asserts that
// calling ExportJSON many times over the unchanged underlying data
// produces byte-for-byte identical output every time -- not just
// equivalent JSON, the literal same bytes.
func TestExportJSON_DeterministicAcrossRepeatedCalls(t *testing.T) {
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

	first, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportJSON (first): %v", err)
	}
	if len(first) == 0 {
		t.Fatal("ExportJSON returned no bytes")
	}

	for i := 0; i < 20; i++ {
		again, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: testActorID})
		if err != nil {
			t.Fatalf("ExportJSON (repeat %d): %v", i, err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("repeat %d: output differs from first call\nfirst:\n%s\nagain:\n%s", i, first, again)
		}
	}
}

// TestExportJSON_SplitPostingsOrderedBySortOrder verifies the canonical
// JSON writer lists a split transaction's postings in sort_order order
// even when they were constructed in the opposite order -- the observable
// effect of the export package's internal sortedPostings helper, checked
// here through ExportJSON's actual output rather than by reaching into an
// unexported function.
func TestExportJSON_SplitPostingsOrderedBySortOrder(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	acc := mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")
	groceriesID, householdID := mustSplitOutflowFixture(t, svc, acc)

	out, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output isn't valid JSON: %v", err)
	}
	txns := doc["transactions"].([]any)
	if len(txns) != 1 {
		t.Fatalf("len(transactions) = %d, want 1", len(txns))
	}
	postings := txns[0].(map[string]any)["postings"].([]any)
	if len(postings) != 2 {
		t.Fatalf("len(postings) = %d, want 2", len(postings))
	}
	if got := postings[0].(map[string]any)["category_id"]; got != groceriesID {
		t.Errorf("postings[0].category_id = %v, want groceries %q (sort_order 0)", got, groceriesID)
	}
	if got := postings[1].(map[string]any)["category_id"]; got != householdID {
		t.Errorf("postings[1].category_id = %v, want household %q (sort_order 1)", got, householdID)
	}
}

func TestExportJSON_EscapesNothingUnexpectedInDescriptions(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	acc := mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: acc.Account.ID(), Amount: "500",
		Date: "2026-08-14", Description: `AT&T, "Home Internet"`,
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	out, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output isn't valid JSON: %v", err)
	}
	txns := doc["transactions"].([]any)
	got := txns[0].(map[string]any)["description"]
	want := `AT&T, "Home Internet"`
	if got != want {
		t.Errorf("description round-tripped to %q, want %q", got, want)
	}
	if !strings.Contains(string(out), "\n") {
		t.Error("expected indented, multi-line JSON output")
	}
}
