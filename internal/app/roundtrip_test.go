package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/app/importparse"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

// TestRoundTrip_ExportImportExportIsByteIdenticalModuloIDs is issue #215:
// ADR-0008's round-trip guarantee ("export -> import -> export produces a
// byte-identical canonical JSON document, after normalising surrogate IDs
// and audit timestamps") enforced as a CI test, not an aspiration in a
// document.
//
// "Import" here is #226's RestoreSnapshot, not the staged CSV pipeline
// (#210-212): a canonical export is already fully-resolved domain data, so
// round-tripping it means restoring the snapshot wholesale, the same
// reasoning docs/decisions/0008-import-export-architecture.md's "Scope
// addition" note and #215's own dependency on #226 both spell out.
func TestRoundTrip_ExportImportExportIsByteIdenticalModuloIDs(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	buildRoundTripFixture(t, svc)

	before, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportJSON (before): %v", err)
	}

	if _, err := svc.RestoreSnapshot(ctx, app.RestoreSnapshotQuery{ActorID: testActorID, Document: before}); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}

	after, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ExportJSON (after): %v", err)
	}

	assertRoundTripEquivalent(t, before, after)
}

// buildRoundTripFixture populates svc's ledger with docs/architecture.md
// §7's own fixture list: multiple currencies, transfers, splits, refunds,
// credit cards, previously-imported rows, a month boundary, and a leap
// day. It reuses this package's existing fixture builders
// (mustAccountFixture, mustCategoryFixture, mustSplitOutflowFixture,
// stageBasicBatch's sibling pattern for a committed import) rather than
// hand-rolling a parallel set.
//
// Every account/category name and every transaction's (booked date,
// description) pair is deliberately unique across the whole fixture:
// assertRoundTripEquivalent's comparison re-identifies entities by those
// business keys (never by ID, which a restore always regenerates), so an
// ambiguous name or date pair would make the *comparison* unreliable, not
// just whatever it happened to be testing.
func buildRoundTripFixture(t *testing.T, svc *app.Service) {
	t.Helper()
	ctx := context.Background()

	// Multiple currencies, including a credit card (a liability account,
	// data-model.md §4).
	accINR := mustAccountFixture(t, svc, "HDFC Savings", "bank", "INR")
	accUSD := mustAccountFixture(t, svc, "Chase Checking", "bank", "USD")
	accEUR := mustAccountFixture(t, svc, "Euro Wallet", "wallet", "EUR")
	accCC := mustAccountFixture(t, svc, "Visa Rewards", "credit_card", "USD")
	accImport := mustAccountFixture(t, svc, "Import Checking", "bank", "USD")

	// Splits: mustSplitOutflowFixture (export_test.go) creates its own
	// "Groceries"/"Household" categories and a two-posting "Big Bazaar
	// split" outflow on accINR.
	mustSplitOutflowFixture(t, svc, accINR)

	dining := mustCategoryFixture(t, svc, "Dining", "expense")
	travel := mustCategoryFixture(t, svc, "Travel", "expense")
	flights, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{
		ActorID: testActorID, Name: "Flights", Kind: "expense", ParentRef: travel.Category.ID(),
	})
	if err != nil {
		t.Fatalf("CreateCategory(Flights): %v", err)
	}
	mustCategoryFixture(t, svc, "Salary", "income")
	gifts := mustCategoryFixture(t, svc, "Gifts", "income")

	// An ordinary tagged outflow.
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: accINR.Account.ID(), Amount: "850", CategoryRef: dining.Category.ID(),
		Date: "2026-08-05", Description: "Dinner out", Tags: []string{"food", "friends"},
	}); err != nil {
		t.Fatalf("RecordOutflow(Dinner out): %v", err)
	}

	// A refund: an original outflow, then a hand-built inflow pointing
	// back at it via ledger.WithRelatedTransaction. RecordInflow has no
	// RelatedTransactionRef field -- data-model.md §7's refund is "an
	// ordinary inflow" built this way, so mustRefundFixture goes through
	// the domain layer directly, the same pattern mustSplitOutflowFixture
	// already uses for a shape the app-layer commands can't express.
	originalFlight, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: accUSD.Account.ID(), Amount: "420", CategoryRef: flights.Category.ID(),
		Date: "2026-08-10", Description: "Flight ticket",
	})
	if err != nil {
		t.Fatalf("RecordOutflow(Flight ticket): %v", err)
	}
	mustRefundFixture(t, svc, accUSD.Account.ID(), flights.Category.ID(), originalFlight.Transaction.ID(),
		"2026-08-20", "Flight ticket refund", 42000, "USD")

	// Transfers: cross-currency (an independent ToAmount, so the implied
	// FX rate isn't the trivial 1:1 -- issue #159) and same-currency
	// (paying off the credit card).
	if _, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: accUSD.Account.ID(), ToAccountRef: accEUR.Account.ID(),
		Amount: "100", ToAmount: "92", Date: "2026-08-12", Description: "Move to euro wallet",
	}); err != nil {
		t.Fatalf("RecordTransfer(cross-currency): %v", err)
	}
	if _, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: testActorID, FromAccountRef: accUSD.Account.ID(), ToAccountRef: accCC.Account.ID(),
		Amount: "200", Date: "2026-08-15", Description: "Pay credit card",
	}); err != nil {
		t.Fatalf("RecordTransfer(same-currency): %v", err)
	}

	// A credit card charge.
	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: testActorID, AccountRef: accCC.Account.ID(), Amount: "65.30", CategoryRef: dining.Category.ID(),
		Date: "2026-08-18", Description: "Team lunch",
	}); err != nil {
		t.Fatalf("RecordOutflow(Team lunch): %v", err)
	}

	// Previously-imported rows, straddling a month boundary: staged
	// through the real CSV pipeline (#210-212) and committed (#211/#229),
	// not hand-built -- the round trip needs the resulting
	// import_record_id/external_id provenance to actually be present on a
	// real committed transaction.
	mustImportedMonthBoundaryFixture(t, svc, accImport.Account.ID())

	// A leap day.
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: testActorID, AccountRef: accINR.Account.ID(), Amount: "300", CategoryRef: gifts.Category.ID(),
		Date: "2028-02-29", Description: "Leap day gift",
	}); err != nil {
		t.Fatalf("RecordInflow(Leap day gift): %v", err)
	}
}

// mustRefundFixture builds a refund -- an ordinary inflow, in categoryID,
// that points back at relatedTxnID via ledger.WithRelatedTransaction --
// directly through the domain layer and persists it via
// svc.Transactions.Create, since RecordInflow has no command field for
// RelatedTransactionID.
func mustRefundFixture(t *testing.T, svc *app.Service, accountID, categoryID, relatedTxnID, dateStr, description string, amountMinor int64, currency string) {
	t.Helper()

	date, err := parseFixtureDate(dateStr)
	if err != nil {
		t.Fatalf("parseFixtureDate(%q): %v", dateStr, err)
	}

	amt, err := money.NewMoney(amountMinor, currency)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	posting, err := ledger.NewPosting(svc.IDs.NewID(), accountID, amt, &categoryID, 0)
	if err != nil {
		t.Fatalf("NewPosting: %v", err)
	}

	txn, err := ledger.NewInflow(svc.IDs.NewID(), testActorID, date, description, []ledger.Posting{posting}, ledger.WithRelatedTransaction(relatedTxnID))
	if err != nil {
		t.Fatalf("NewInflow: %v", err)
	}
	if err := svc.Transactions.Create(context.Background(), testActorID, txn, nil); err != nil {
		t.Fatalf("Transactions.Create: %v", err)
	}
}

// mustImportedMonthBoundaryFixture stages and commits a two-row CSV import
// on accountID whose rows straddle a calendar month boundary (2026-01-31
// -> 2026-02-01), giving the fixture both "previously-imported rows" and
// "month boundary" coverage from the same real commit -- both rows carry
// an external ID (the "Ref" column), so the resulting transactions carry
// real import_record_id/external_id provenance for the round trip to
// preserve.
func mustImportedMonthBoundaryFixture(t *testing.T, svc *app.Service, accountID string) {
	t.Helper()
	ctx := context.Background()

	data := "Date,Description,Amount,Ref\n" +
		"2026-01-31,Year End Bonus,1000.00,ext-bonus-1\n" +
		"2026-02-01,Rent Payment,-1200.00,ext-rent-1\n"
	staged, err := svc.StageImport(ctx, app.StageImportCommand{
		ActorID: testActorID, AccountRef: accountID, Filename: "statement.csv", SourceFormat: "csv",
		FileContent: []byte(data),
		ColumnMapping: importparse.ColumnMapping{
			DateColumn: "Date", DescriptionColumn: "Description", AmountColumn: "Amount", ExternalIDColumn: "Ref",
		},
	})
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if _, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: testActorID, ImportBatchRef: staged.Batch.ID()}); err != nil {
		t.Fatalf("CommitImportBatch: %v", err)
	}
}

// parseFixtureDate parses a "YYYY-MM-DD" string into a domain.Date -- the
// small helper mustRefundFixture needs to accept a plain date literal
// rather than building one out of time.Parse/domain.NewDate at every call
// site.
func parseFixtureDate(s string) (domain.Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return domain.Date{}, fmt.Errorf("parseFixtureDate(%q): %w", s, err)
	}
	return domain.NewDate(t.Year(), t.Month(), t.Day())
}

// --- ADR-0008 round-trip comparison helper ---
//
// canonicalizeExport normalises one canonical JSON export document (the
// bodger.export/v1 shape internal/app/export_json.go's jsonEnvelope
// produces) so two documents describing the same underlying ledger data
// compare equal regardless of the surrogate IDs a restore regenerates.
//
// ADR-0008 defines the round-trip guarantee as "byte-identical... after
// normalising surrogate IDs (consistently remapped) and audit
// timestamps". The audit-timestamp half of that is a no-op here on
// purpose: ExportJSON's wire shape never carries created_at/updated_at or
// transaction_revision history to begin with (ADR-0008: "no timestamps of
// the export itself in the payload"), so there is nothing to strip.
//
// A restore assigns brand-new random IDs, and ExportSnapshot always
// orders its slices by ID ascending -- so the two documents'
// accounts/categories/transactions arrays aren't just ID-different, they
// can be in a different relative order too. This resolves that in two
// passes:
//
//  1. Reorder every collection by a business key ADR-0008's "must match
//     exactly" column guarantees is stable across a restore -- an
//     account/category's Name, and a transaction's (BookedDate,
//     Description) pair. buildRoundTripFixture keeps every one of these
//     globally unique for exactly this reason.
//  2. Replace every ID-shaped field (an account/category/transaction/
//     posting's own ID, and every reference to one -- ParentID,
//     AccountID, CategoryID, RelatedTransactionID) with a placeholder
//     assigned by first-appearance order within the now-reordered
//     document. Two documents describing the same data, reordered the
//     same way, assign the same placeholders to the same objects
//     regardless of what the underlying IDs actually were.
//
// import_record_id and external_id are deliberately left untouched:
// RestoreSnapshot never regenerates them (buildRestoreSnapshot passes them
// through via ledger.WithImportProvenance unchanged), so they're expected
// to already be byte-identical. If a regression stopped preserving them,
// this comparison should catch it precisely *because* it does not
// normalise them away.
type rtEnvelope struct {
	Format       string       `json:"format"`
	Accounts     []rtAccount  `json:"accounts"`
	Categories   []rtCategory `json:"categories"`
	Transactions []rtTxn      `json:"transactions"`
}

type rtAccount struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Kind               string      `json:"kind"`
	OpeningBalance     money.Money `json:"opening_balance"`
	OpeningBalanceDate *string     `json:"opening_balance_date,omitempty"`
	Institution        *string     `json:"institution,omitempty"`
	SortOrder          int         `json:"sort_order"`
	ArchivedAt         *string     `json:"archived_at,omitempty"`
}

type rtCategory struct {
	ID         string  `json:"id"`
	ParentID   *string `json:"parent_id,omitempty"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	SortOrder  int     `json:"sort_order"`
	ArchivedAt *string `json:"archived_at,omitempty"`
}

type rtPosting struct {
	ID         string      `json:"id"`
	AccountID  string      `json:"account_id"`
	CategoryID *string     `json:"category_id,omitempty"`
	Amount     money.Money `json:"amount"`
	SortOrder  int         `json:"sort_order"`
}

type rtTxn struct {
	ID                   string      `json:"id"`
	Kind                 string      `json:"kind"`
	BookedDate           string      `json:"booked_date"`
	PostedDate           *string     `json:"posted_date,omitempty"`
	Description          string      `json:"description"`
	Notes                string      `json:"notes,omitempty"`
	ImportRecordID       *string     `json:"import_record_id,omitempty"`
	ExternalID           *string     `json:"external_id,omitempty"`
	RelatedTransactionID *string     `json:"related_transaction_id,omitempty"`
	Tags                 []string    `json:"tags,omitempty"`
	Postings             []rtPosting `json:"postings"`
}

// idNormalizer assigns each distinct source ID a stable placeholder, in
// the order it first encounters them -- see canonicalizeExport's doc
// comment for why first-appearance order is enough to line up two
// independently ID-regenerated documents.
type idNormalizer struct {
	placeholders map[string]string
	prefix       string
	next         int
}

func newIDNormalizer(prefix string) *idNormalizer {
	return &idNormalizer{placeholders: make(map[string]string), prefix: prefix}
}

// assign returns id's placeholder, minting a new one the first time id is
// seen.
func (n *idNormalizer) assign(id string) string {
	if p, ok := n.placeholders[id]; ok {
		return p
	}
	n.next++
	p := fmt.Sprintf("%s-%d", n.prefix, n.next)
	n.placeholders[id] = p
	return p
}

// lookup returns id's placeholder, failing the test if id was never
// assign-ed. A reference to an ID this document never declared as one of
// its own accounts/categories/transactions is exactly the class of bug (a
// dangling or unmapped reference left over from a restore) this
// comparison exists to catch -- it must fail loudly, not silently pass
// through the raw, unmapped ID.
func (n *idNormalizer) lookup(t *testing.T, kind, id string) string {
	t.Helper()
	p, ok := n.placeholders[id]
	if !ok {
		t.Fatalf("round-trip comparison: %s %q was never declared as one of this document's own IDs", kind, id)
	}
	return p
}

// canonicalizeExport parses raw and returns it reordered and ID-normalised
// per this file's doc comment above.
func canonicalizeExport(t *testing.T, raw []byte) rtEnvelope {
	t.Helper()
	var env rtEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("canonicalizeExport: %v", err)
	}

	sort.Slice(env.Accounts, func(i, j int) bool { return env.Accounts[i].Name < env.Accounts[j].Name })
	sort.Slice(env.Categories, func(i, j int) bool { return env.Categories[i].Name < env.Categories[j].Name })
	sort.Slice(env.Transactions, func(i, j int) bool {
		if env.Transactions[i].BookedDate != env.Transactions[j].BookedDate {
			return env.Transactions[i].BookedDate < env.Transactions[j].BookedDate
		}
		return env.Transactions[i].Description < env.Transactions[j].Description
	})

	accountIDs := newIDNormalizer("account")
	for i := range env.Accounts {
		env.Accounts[i].ID = accountIDs.assign(env.Accounts[i].ID)
	}

	categoryIDs := newIDNormalizer("category")
	for i := range env.Categories {
		env.Categories[i].ID = categoryIDs.assign(env.Categories[i].ID)
	}
	for i := range env.Categories {
		if env.Categories[i].ParentID != nil {
			p := categoryIDs.lookup(t, "category parent_id", *env.Categories[i].ParentID)
			env.Categories[i].ParentID = &p
		}
	}

	txnIDs := newIDNormalizer("transaction")
	for i := range env.Transactions {
		txnIDs.assign(env.Transactions[i].ID)
	}
	postingIDs := newIDNormalizer("posting")
	for i := range env.Transactions {
		txn := &env.Transactions[i]
		txn.ID = txnIDs.lookup(t, "transaction id", txn.ID)
		if txn.RelatedTransactionID != nil {
			r := txnIDs.lookup(t, "related_transaction_id", *txn.RelatedTransactionID)
			txn.RelatedTransactionID = &r
		}
		for j := range txn.Postings {
			p := &txn.Postings[j]
			p.ID = postingIDs.assign(p.ID)
			p.AccountID = accountIDs.lookup(t, "posting account_id", p.AccountID)
			if p.CategoryID != nil {
				c := categoryIDs.lookup(t, "posting category_id", *p.CategoryID)
				p.CategoryID = &c
			}
		}
	}

	return env
}

// assertRoundTripEquivalent implements ADR-0008's round-trip guarantee: it
// fails t unless before and after -- two canonical JSON export documents --
// describe identical ledger data once surrogate IDs are normalised (see
// canonicalizeExport). Comparison re-marshals both canonicalised documents
// with a fixed struct field order and diffs the resulting bytes, so a
// genuine mismatch prints as a readable JSON diff.
func assertRoundTripEquivalent(t *testing.T, before, after []byte) {
	t.Helper()

	beforeCanon := canonicalizeExport(t, before)
	afterCanon := canonicalizeExport(t, after)

	beforeJSON, err := json.MarshalIndent(beforeCanon, "", "  ")
	if err != nil {
		t.Fatalf("assertRoundTripEquivalent: marshal before: %v", err)
	}
	afterJSON, err := json.MarshalIndent(afterCanon, "", "  ")
	if err != nil {
		t.Fatalf("assertRoundTripEquivalent: marshal after: %v", err)
	}

	if string(beforeJSON) != string(afterJSON) {
		t.Errorf("export -> restore -> export is not byte-identical after normalising surrogate IDs:\n--- before (normalised) ---\n%s\n--- after (normalised) ---\n%s", beforeJSON, afterJSON)
	}
}
