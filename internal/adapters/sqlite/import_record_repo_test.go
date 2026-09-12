package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func mustImportRecord(t *testing.T, id, userID, importBatchID string, sortOrder int, opts ...importing.ImportRecordOption) importing.ImportRecord {
	t.Helper()
	r, err := importing.NewImportRecord(
		id, userID, importBatchID, `{"raw":"row"}`,
		mustDate(t, 2026, time.August, 14), id+"-description", mustMoney(t, -1234, "USD"), sortOrder, opts...,
	)
	if err != nil {
		t.Fatalf("importing.NewImportRecord: %v", err)
	}
	return r
}

// seedTransaction creates a minimal real transaction (satisfying
// import_record's transaction_id/duplicate_matched_transaction_id foreign
// keys), on an account and category that must already exist.
func seedTransaction(t *testing.T, db *DB, userID, transactionID, accountID, categoryID string) {
	t.Helper()
	catID := categoryID
	p := mustPosting(t, transactionID+"-posting", accountID, -500, "USD", &catID)
	txn, err := ledger.NewOutflow(transactionID, userID, mustDate(t, 2026, time.August, 10), "Seed transaction", []ledger.Posting{p})
	if err != nil {
		t.Fatalf("ledger.NewOutflow: %v", err)
	}
	if err := NewTransactionRepository(db).Create(context.Background(), userID, txn, nil); err != nil {
		t.Fatalf("seed transaction %q: %v", transactionID, err)
	}
}

func TestImportRecordRepository_CreateBatchGet_RoundTripsEveryField(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-mapped", "cat-mapped")
	seedTransaction(t, db, ports.SeededUserID, "txn-matched", "acc-mapped", "cat-mapped")

	postedDate := mustDate(t, 2026, time.August, 16)
	match, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-matched")
	if err != nil {
		t.Fatalf("NewDuplicateMatch: %v", err)
	}
	match, err = match.Resolve(importing.DuplicateResolutionDismissed)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	record := mustImportRecord(t, "record-1", ports.SeededUserID, batch.ID(), 2,
		importing.WithPostedDate(postedDate),
		importing.WithExternalID("ext-1"),
		importing.WithResolvedAccount("acc-mapped"),
		importing.WithResolvedCategory("cat-mapped"),
		importing.WithDuplicateMatch(match),
	)

	repo := NewImportRecordRepository(db)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []importing.ImportRecord{record}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "record-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != record.ID() || got.UserID() != record.UserID() || got.ImportBatchID() != record.ImportBatchID() {
		t.Fatalf("Get() identity = %+v, want a round trip of %+v", got, record)
	}
	if got.RawPayload() != record.RawPayload() {
		t.Errorf("RawPayload() = %q, want %q", got.RawPayload(), record.RawPayload())
	}
	if !got.BookedDate().Equal(record.BookedDate()) {
		t.Errorf("BookedDate() = %s, want %s", got.BookedDate(), record.BookedDate())
	}
	if got.Description() != record.Description() {
		t.Errorf("Description() = %q, want %q", got.Description(), record.Description())
	}
	if !got.Amount().Equal(record.Amount()) {
		t.Errorf("Amount() = %s, want %s", got.Amount(), record.Amount())
	}
	if got.SortOrder() != 2 {
		t.Errorf("SortOrder() = %d, want 2", got.SortOrder())
	}
	if d, ok := got.PostedDate(); !ok || !d.Equal(postedDate) {
		t.Errorf("PostedDate() = (%s, %v), want (%s, true)", d, ok, postedDate)
	}
	if v, ok := got.ExternalID(); !ok || v != "ext-1" {
		t.Errorf("ExternalID() = (%q, %v), want (%q, true)", v, ok, "ext-1")
	}
	if v, ok := got.ResolvedAccountID(); !ok || v != "acc-mapped" {
		t.Errorf("ResolvedAccountID() = (%q, %v), want (%q, true)", v, ok, "acc-mapped")
	}
	if v, ok := got.ResolvedCategoryID(); !ok || v != "cat-mapped" {
		t.Errorf("ResolvedCategoryID() = (%q, %v), want (%q, true)", v, ok, "cat-mapped")
	}
	dm, ok := got.DuplicateMatch()
	if !ok {
		t.Fatal("DuplicateMatch() ok = false, want true")
	}
	if dm.Tier() != importing.DuplicateMatchTierSuspected {
		t.Errorf("DuplicateMatch().Tier() = %q, want %q", dm.Tier(), importing.DuplicateMatchTierSuspected)
	}
	if dm.MatchedTransactionID() != "txn-matched" {
		t.Errorf("DuplicateMatch().MatchedTransactionID() = %q, want %q", dm.MatchedTransactionID(), "txn-matched")
	}
	if dm.Resolution() != importing.DuplicateResolutionDismissed {
		t.Errorf("DuplicateMatch().Resolution() = %q, want %q", dm.Resolution(), importing.DuplicateResolutionDismissed)
	}
	if got.Status() != importing.ImportRecordStatusPending {
		t.Errorf("Status() = %q, want %q", got.Status(), importing.ImportRecordStatusPending)
	}
}

// TestImportRecordRepository_TransferCandidateRoundTrips exercises the
// advisory transfer_candidate_record_id column (issue #210): it must
// survive a write/read round trip independently of any DuplicateMatch, and
// is nullable — records rarely have one.
func TestImportRecordRepository_TransferCandidateRoundTrips(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")
	repo := NewImportRecordRepository(db)

	// The referenced record must exist first: transfer_candidate_record_id
	// is a foreign key, and this database runs with foreign_keys=1.
	other := mustImportRecord(t, "record-other", ports.SeededUserID, batch.ID(), 0)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []importing.ImportRecord{other}); err != nil {
		t.Fatalf("CreateBatch(record-other): %v", err)
	}

	record := mustImportRecord(t, "record-1", ports.SeededUserID, batch.ID(), 1,
		importing.WithTransferCandidate("record-other"),
	)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []importing.ImportRecord{record}); err != nil {
		t.Fatalf("CreateBatch(record-1): %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "record-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v, ok := got.TransferCandidateRecordID(); !ok || v != "record-other" {
		t.Errorf("TransferCandidateRecordID() = (%q, %v), want (%q, true)", v, ok, "record-other")
	}

	gotOther, err := repo.Get(ctx, ports.SeededUserID, "record-other")
	if err != nil {
		t.Fatalf("Get(record-other): %v", err)
	}
	if _, ok := gotOther.TransferCandidateRecordID(); ok {
		t.Error("TransferCandidateRecordID() ok = true for record-other, want false (never set)")
	}
}

func TestImportRecordRepository_CreateBatch_RejectsMismatchedActor(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")

	record := mustImportRecord(t, "record-1", ports.SeededUserID, batch.ID(), 0)
	repo := NewImportRecordRepository(db)
	err := repo.CreateBatch(ctx, otherUserID, []importing.ImportRecord{record})
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("CreateBatch(mismatched actor) error = %v, want *errs.Error with code NotAllowed", err)
	}
}

func TestImportRecordRepository_CreateBatch_Empty(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewImportRecordRepository(db)
	if err := repo.CreateBatch(context.Background(), ports.SeededUserID, nil); err != nil {
		t.Fatalf("CreateBatch(nil) = %v, want success (a no-op)", err)
	}
}

// TestImportRecordRepository_CreateBatch_AllOrNothing confirms CreateBatch
// writes every row in one transaction: if any record in the slice fails to
// insert, none of the preceding records in the same call are left behind.
func TestImportRecordRepository_CreateBatch_AllOrNothing(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")

	good := mustImportRecord(t, "record-good", ports.SeededUserID, batch.ID(), 0)
	// A duplicate ID with the "good" record's own is a primary-key
	// violation on the second insert; if CreateBatch commits row by row
	// instead of atomically, "record-good" would survive this failure.
	duplicateID := mustImportRecord(t, "record-good", ports.SeededUserID, batch.ID(), 1)

	repo := NewImportRecordRepository(db)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []importing.ImportRecord{good, duplicateID}); err == nil {
		t.Fatal("CreateBatch(...) with a duplicate id: want error, got nil")
	}

	_, err := repo.Get(ctx, ports.SeededUserID, "record-good")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Get(record-good) after a failed CreateBatch error = %v, want *errs.Error with code NotFound (nothing from the failed batch should persist)", err)
	}
}

func TestImportRecordRepository_Get_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewImportRecordRepository(db)
	_, err := repo.Get(context.Background(), ports.SeededUserID, "no-such-record")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Get(unknown) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestImportRecordRepository_Get_CrossUserIsolation(t *testing.T) {
	db, _ := newTestDB(t)
	seedOtherUser(t, db, otherUserID)
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")
	record := mustImportRecord(t, "record-1", ports.SeededUserID, batch.ID(), 0)
	if err := NewImportRecordRepository(db).CreateBatch(context.Background(), ports.SeededUserID, []importing.ImportRecord{record}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	repo := NewImportRecordRepository(db)
	_, err := repo.Get(context.Background(), otherUserID, "record-1")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Get(other user's record) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestImportRecordRepository_ListByImportBatch(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	batchA := seedImportBatch(t, db, ports.SeededUserID, "batch-a", "acc-a")
	batchB := seedImportBatch(t, db, ports.SeededUserID, "batch-b", "acc-b")

	repo := NewImportRecordRepository(db)
	// Inserted out of sort_order to prove ListByImportBatch orders by
	// sort_order, not insertion order.
	recA2 := mustImportRecord(t, "record-a-2", ports.SeededUserID, batchA.ID(), 2)
	recA1 := mustImportRecord(t, "record-a-1", ports.SeededUserID, batchA.ID(), 1)
	recB1 := mustImportRecord(t, "record-b-1", ports.SeededUserID, batchB.ID(), 0)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []importing.ImportRecord{recA2, recA1, recB1}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	got, err := repo.ListByImportBatch(ctx, ports.SeededUserID, batchA.ID())
	if err != nil {
		t.Fatalf("ListByImportBatch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListByImportBatch(batch-a) returned %d records, want 2", len(got))
	}
	if got[0].ID() != "record-a-1" || got[1].ID() != "record-a-2" {
		t.Errorf("ListByImportBatch order = [%s, %s], want [record-a-1, record-a-2] (sort_order ascending)", got[0].ID(), got[1].ID())
	}
}

func TestImportRecordRepository_Update(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")
	record := mustImportRecord(t, "record-1", ports.SeededUserID, batch.ID(), 0)
	repo := NewImportRecordRepository(db)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []importing.ImportRecord{record}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	ready, err := record.MarkReady()
	if err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	if err := repo.Update(ctx, ports.SeededUserID, ready); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "record-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status() != importing.ImportRecordStatusReady {
		t.Errorf("Status() after Update = %q, want %q", got.Status(), importing.ImportRecordStatusReady)
	}

	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-committed", "cat-committed")
	seedTransaction(t, db, ports.SeededUserID, "txn-committed", "acc-committed", "cat-committed")
	committed, err := got.MarkCommitted("txn-committed")
	if err != nil {
		t.Fatalf("MarkCommitted: %v", err)
	}
	if err := repo.Update(ctx, ports.SeededUserID, committed); err != nil {
		t.Fatalf("Update (committed): %v", err)
	}

	gotCommitted, err := repo.Get(ctx, ports.SeededUserID, "record-1")
	if err != nil {
		t.Fatalf("Get (committed): %v", err)
	}
	if gotCommitted.Status() != importing.ImportRecordStatusCommitted {
		t.Errorf("Status() = %q, want %q", gotCommitted.Status(), importing.ImportRecordStatusCommitted)
	}
	if v, ok := gotCommitted.TransactionID(); !ok || v != "txn-committed" {
		t.Errorf("TransactionID() = (%q, %v), want (%q, true)", v, ok, "txn-committed")
	}
}

func TestImportRecordRepository_Update_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")

	repo := NewImportRecordRepository(db)
	record := mustImportRecord(t, "no-such-record", ports.SeededUserID, batch.ID(), 0)
	err := repo.Update(ctx, ports.SeededUserID, record)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Update(unknown) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestImportRecordRepository_Update_RejectsMismatchedActor(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")
	record := mustImportRecord(t, "record-1", ports.SeededUserID, batch.ID(), 0)
	if err := NewImportRecordRepository(db).CreateBatch(ctx, ports.SeededUserID, []importing.ImportRecord{record}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	repo := NewImportRecordRepository(db)
	err := repo.Update(ctx, otherUserID, record)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("Update(mismatched actor) error = %v, want *errs.Error with code NotAllowed", err)
	}
}
