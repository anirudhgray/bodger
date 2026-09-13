package app_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

// TestRestoreSnapshot_RoundTripEquivalentAfterReExport is this issue's
// round-trip test, run against the real SQLite adapter: export a
// reasonably rich fixture (several accounts of different kinds/currencies,
// a category tree, a split outflow, a same-currency transfer, a
// cross-currency transfer, an outflow/inflow refund pair, and tags),
// restore that export, export again, and confirm the two describe the same
// domain state. This is a mini version of #215's eventual full
// export -> import -> export byte-identical CI test, scoped to just this
// use case (ADR-0008's round-trip guarantee): because RestoreSnapshot
// regenerates every ID (issue #226's own requirement), "the same" is
// checked structurally here -- matching entities up by name/date+
// description and comparing everything else, including that every
// reference remaps consistently -- rather than by a literal byte
// comparison, which would trivially fail on IDs alone.
func TestRestoreSnapshot_RoundTripEquivalentAfterReExport(t *testing.T) {
	svc, _, _ := newSQLiteTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	actorID := sqliteActorID

	savings := mustAccountFixtureAs(t, svc, actorID, "Savings", "bank", "INR")
	checking := mustAccountFixtureAs(t, svc, actorID, "Checking", "bank", "INR")
	wallet := mustAccountFixtureAs(t, svc, actorID, "USD Wallet", "wallet", "USD")

	groceries := mustCategoryFixtureAs(t, svc, actorID, "Groceries", "expense")
	household := mustCategoryFixtureAs(t, svc, actorID, "Household", "expense")
	groceriesID, householdID := groceries.Category.ID(), household.Category.ID()

	// A split outflow, built directly through the domain layer since
	// RecordOutflow only supports a single posting.
	amt1, err := money.NewMoney(-30000, "INR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	amt2, err := money.NewMoney(-15000, "INR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	p0, err := ledger.NewPosting(svc.IDs.NewID(), savings.Account.ID(), amt1, &groceriesID, 0)
	if err != nil {
		t.Fatalf("NewPosting: %v", err)
	}
	p1, err := ledger.NewPosting(svc.IDs.NewID(), savings.Account.ID(), amt2, &householdID, 1)
	if err != nil {
		t.Fatalf("NewPosting: %v", err)
	}
	bookedDate, err := domain.NewDate(2026, time.August, 14)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	splitTxn, err := ledger.NewOutflow(svc.IDs.NewID(), actorID, bookedDate, "Big Bazaar split", []ledger.Posting{p0, p1})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}
	weeklyTag, err := ledger.NewTag("weekly")
	if err != nil {
		t.Fatalf("NewTag: %v", err)
	}
	if err := svc.Transactions.Create(ctx, actorID, splitTxn, []ledger.Tag{weeklyTag}); err != nil {
		t.Fatalf("Transactions.Create (split): %v", err)
	}

	if _, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: actorID, FromAccountRef: savings.Account.ID(), ToAccountRef: checking.Account.ID(),
		Amount: "20000", Date: "2026-08-14", Description: "Move to checking",
	}); err != nil {
		t.Fatalf("RecordTransfer (same currency): %v", err)
	}
	if _, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: actorID, FromAccountRef: checking.Account.ID(), ToAccountRef: wallet.Account.ID(),
		Amount: "5000", Date: "2026-08-14", Description: "To USD wallet",
	}); err != nil {
		t.Fatalf("RecordTransfer (cross-currency): %v", err)
	}

	if _, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: actorID, AccountRef: checking.Account.ID(), Amount: "1000", CategoryRef: groceriesID,
		Date: "2026-08-14", Description: "Bad purchase",
	}); err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}
	if _, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: actorID, AccountRef: checking.Account.ID(), Amount: "1000", CategoryRef: groceriesID,
		Date: "2026-08-15", Description: "Refund", Tags: []string{"refund"},
	}); err != nil {
		t.Fatalf("RecordInflow: %v", err)
	}

	beforeSnapshot, err := svc.ExportSnapshot(ctx, app.ExportSnapshotQuery{ActorID: actorID})
	if err != nil {
		t.Fatalf("ExportSnapshot (before): %v", err)
	}
	doc, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: actorID})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	if _, err := svc.RestoreSnapshot(ctx, app.RestoreSnapshotQuery{ActorID: actorID, Document: doc}); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}

	afterSnapshot, err := svc.ExportSnapshot(ctx, app.ExportSnapshotQuery{ActorID: actorID})
	if err != nil {
		t.Fatalf("ExportSnapshot (after): %v", err)
	}

	assertSnapshotsEquivalentModuloIDs(t, beforeSnapshot, afterSnapshot)
}

// assertSnapshotsEquivalentModuloIDs compares before (pre-restore) and
// after (post-restore) domain snapshots for equivalence under ADR-0008's
// round-trip rule: every account/category/transaction/posting's own facts
// must match exactly, and every ID reference between them must remap
// consistently, but the IDs themselves are free to differ. Entities are
// matched up by name (accounts, categories -- both unique within their
// scope, data-model.md §4/§6) or by (booked date, description) for
// transactions (unique across this test's own fixture).
func assertSnapshotsEquivalentModuloIDs(t *testing.T, before, after app.ExportSnapshot) {
	t.Helper()

	if len(before.Accounts) != len(after.Accounts) {
		t.Fatalf("account count = %d, want %d", len(after.Accounts), len(before.Accounts))
	}
	afterAccountsByName := make(map[string]ledger.Account, len(after.Accounts))
	for _, a := range after.Accounts {
		afterAccountsByName[a.Name()] = a
	}
	accountIDMap := make(map[string]string, len(before.Accounts))
	for _, b := range before.Accounts {
		a, ok := afterAccountsByName[b.Name()]
		if !ok {
			t.Fatalf("account %q missing after restore", b.Name())
			continue
		}
		accountIDMap[b.ID()] = a.ID()
		if a.Kind() != b.Kind() || a.Currency() != b.Currency() ||
			a.OpeningBalance().AmountMinor() != b.OpeningBalance().AmountMinor() || a.SortOrder() != b.SortOrder() {
			t.Errorf("account %q differs after restore: before kind=%s currency=%s opening=%s, after kind=%s currency=%s opening=%s",
				b.Name(), b.Kind(), b.Currency(), b.OpeningBalance(), a.Kind(), a.Currency(), a.OpeningBalance())
		}
	}

	if len(before.Categories) != len(after.Categories) {
		t.Fatalf("category count = %d, want %d", len(after.Categories), len(before.Categories))
	}
	afterCategoriesByName := make(map[string]ledger.Category, len(after.Categories))
	for _, c := range after.Categories {
		afterCategoriesByName[c.Name()] = c
	}
	categoryIDMap := make(map[string]string, len(before.Categories))
	for _, b := range before.Categories {
		a, ok := afterCategoriesByName[b.Name()]
		if !ok {
			t.Fatalf("category %q missing after restore", b.Name())
			continue
		}
		categoryIDMap[b.ID()] = a.ID()
		if a.Kind() != b.Kind() {
			t.Errorf("category %q kind differs: before=%s after=%s", b.Name(), b.Kind(), a.Kind())
		}
	}
	for _, b := range before.Categories {
		bParent, bHasParent := b.ParentID()
		a := afterCategoriesByName[b.Name()]
		aParent, aHasParent := a.ParentID()
		if bHasParent != aHasParent {
			t.Errorf("category %q parent presence differs after restore", b.Name())
			continue
		}
		if !bHasParent {
			continue
		}
		if wantParentID := categoryIDMap[bParent]; aParent != wantParentID {
			t.Errorf("category %q parent = %q, want remapped %q", b.Name(), aParent, wantParentID)
		}
	}

	if len(before.Transactions) != len(after.Transactions) {
		t.Fatalf("transaction count = %d, want %d", len(after.Transactions), len(before.Transactions))
	}
	type txnKey struct{ date, description string }
	afterByKey := make(map[txnKey]app.ExportedTransaction, len(after.Transactions))
	for _, et := range after.Transactions {
		afterByKey[txnKey{et.Transaction.BookedDate().String(), et.Transaction.Description()}] = et
	}
	for _, bet := range before.Transactions {
		key := txnKey{bet.Transaction.BookedDate().String(), bet.Transaction.Description()}
		aet, ok := afterByKey[key]
		if !ok {
			t.Fatalf("transaction %q on %s missing after restore", key.description, key.date)
			continue
		}
		if aet.Transaction.Kind() != bet.Transaction.Kind() {
			t.Errorf("transaction %q kind differs: before=%s after=%s", key.description, bet.Transaction.Kind(), aet.Transaction.Kind())
		}
		if aet.Transaction.Notes() != bet.Transaction.Notes() {
			t.Errorf("transaction %q notes differ: before=%q after=%q", key.description, bet.Transaction.Notes(), aet.Transaction.Notes())
		}
		if got, want := tagStrings(aet.Tags), tagStrings(bet.Tags); !equalStrings(got, want) {
			t.Errorf("transaction %q tags = %v, want %v", key.description, got, want)
		}

		bPostings, aPostings := sortedBySortOrder(bet.Transaction.Postings()), sortedBySortOrder(aet.Transaction.Postings())
		if len(bPostings) != len(aPostings) {
			t.Fatalf("transaction %q posting count = %d, want %d", key.description, len(aPostings), len(bPostings))
		}
		for i := range bPostings {
			bp, ap := bPostings[i], aPostings[i]
			if ap.Amount().AmountMinor() != bp.Amount().AmountMinor() || ap.Currency() != bp.Currency() {
				t.Errorf("transaction %q posting %d amount differs: before=%s after=%s", key.description, i, bp.Amount(), ap.Amount())
			}
			if wantAccountID := accountIDMap[bp.AccountID()]; ap.AccountID() != wantAccountID {
				t.Errorf("transaction %q posting %d account = %q, want remapped %q", key.description, i, ap.AccountID(), wantAccountID)
			}
			bCat, bHasCat := bp.CategoryID()
			aCat, aHasCat := ap.CategoryID()
			if bHasCat != aHasCat {
				t.Errorf("transaction %q posting %d category presence differs after restore", key.description, i)
				continue
			}
			if bHasCat {
				if wantCategoryID := categoryIDMap[bCat]; aCat != wantCategoryID {
					t.Errorf("transaction %q posting %d category = %q, want remapped %q", key.description, i, aCat, wantCategoryID)
				}
			}
		}
	}
}

// TestRestoreSnapshot_ChildCategoryBeforeParentInDocument is issue #237's
// regression test: SnapshotRepository.Replace inserts categories one row
// at a time and categories.parent_id is a foreign key to categories.id,
// so a document whose categories array happens to list a child before its
// own parent used to fail restore with a FOREIGN KEY constraint error.
// ExportSnapshot's export-by-ID sort (ADR-0008) makes that ordering close
// to a coin flip per parent/child pair in production, since category IDs
// are random UUIDs -- this test doesn't rely on that coin flip landing a
// particular way; it forces the child ahead of its parent in the document
// directly, via mutateDocument, so the regression is deterministic
// regardless of which way the real UUIDs happen to sort.
func TestRestoreSnapshot_ChildCategoryBeforeParentInDocument(t *testing.T) {
	svc, _, _ := newSQLiteTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	actorID := sqliteActorID

	investments := mustCategoryFixtureAs(t, svc, actorID, "Investments", "expense")
	interest, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{
		ActorID: actorID, Name: "Interest", Kind: "expense", ParentRef: investments.Category.ID(),
	})
	if err != nil {
		t.Fatalf("CreateCategory(Interest): %v", err)
	}

	doc, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: actorID})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	doc = mutateDocument(t, doc, func(m map[string]any) {
		cats, _ := m["categories"].([]any)
		if len(cats) != 2 {
			t.Fatalf("test fixture: want exactly 2 categories, got %d", len(cats))
		}
		firstIsChild := func() bool {
			entry, ok := cats[0].(map[string]any)
			return ok && entry["id"] == interest.Category.ID()
		}
		if !firstIsChild() {
			cats[0], cats[1] = cats[1], cats[0]
		}
		m["categories"] = cats
	})

	if _, err := svc.RestoreSnapshot(ctx, app.RestoreSnapshotQuery{ActorID: actorID, Document: doc}); err != nil {
		t.Fatalf("RestoreSnapshot with child category listed before its parent: %v", err)
	}

	categories, err := svc.Categories.List(ctx, actorID)
	if err != nil {
		t.Fatalf("Categories.List: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("len(categories) after restore = %d, want 2", len(categories))
	}
	byName := make(map[string]ledger.Category, len(categories))
	for _, c := range categories {
		byName[c.Name()] = c
	}
	restoredParent, ok := byName["Investments"]
	if !ok {
		t.Fatalf("restored categories = %+v, want an Investments entry", categories)
	}
	restoredChild, ok := byName["Interest"]
	if !ok {
		t.Fatalf("restored categories = %+v, want an Interest entry", categories)
	}
	parentID, hasParent := restoredChild.ParentID()
	if !hasParent || parentID != restoredParent.ID() {
		t.Errorf("restored Interest parent = (%q, %v), want Investments's restored ID %q", parentID, hasParent, restoredParent.ID())
	}
}

func tagStrings(tags []ledger.Tag) []string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = t.String()
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortedBySortOrder(postings []ledger.Posting) []ledger.Posting {
	out := make([]ledger.Posting, len(postings))
	copy(out, postings)
	sort.SliceStable(out, func(i, j int) bool { return out[i].SortOrder() < out[j].SortOrder() })
	return out
}
