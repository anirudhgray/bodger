package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func mustImportBatch(t *testing.T, id, userID, targetAccountID string) importing.ImportBatch {
	t.Helper()
	b, err := importing.NewImportBatch(id, userID, "csv", id+".csv", "sha256:"+id, targetAccountID)
	if err != nil {
		t.Fatalf("importing.NewImportBatch: %v", err)
	}
	return b
}

// seedImportBatch creates an account (satisfying import_batch's
// target_account_id foreign key) and an import batch against it, and
// returns the batch as stored.
func seedImportBatch(t *testing.T, db *DB, userID, batchID, accountID string) importing.ImportBatch {
	t.Helper()
	ctx := context.Background()
	accRepo := NewAccountRepository(db)
	acc := mustAccount(t, accountID, userID, accountID+"-name")
	if err := accRepo.Create(ctx, userID, acc); err != nil {
		t.Fatalf("seed account %q: %v", accountID, err)
	}

	batch := mustImportBatch(t, batchID, userID, accountID)
	repo := NewImportBatchRepository(db)
	if err := repo.Create(ctx, userID, batch); err != nil {
		t.Fatalf("seed import batch %q: %v", batchID, err)
	}
	return batch
}

func TestImportBatchRepository_CreateGet(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")

	repo := NewImportBatchRepository(db)
	got, err := repo.Get(ctx, ports.SeededUserID, "batch-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != batch.ID() || got.UserID() != batch.UserID() || got.SourceFormat() != batch.SourceFormat() ||
		got.Filename() != batch.Filename() || got.FileHash() != batch.FileHash() ||
		got.TargetAccountID() != batch.TargetAccountID() || got.Status() != batch.Status() {
		t.Errorf("Get() = %+v, want a round trip of %+v", got, batch)
	}
	if got.Status() != importing.ImportBatchStatusStaged {
		t.Errorf("Status() = %q, want %q for a freshly created batch", got.Status(), importing.ImportBatchStatusStaged)
	}
}

func TestImportBatchRepository_Create_RejectsMismatchedActor(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	accRepo := NewAccountRepository(db)
	if err := accRepo.Create(ctx, ports.SeededUserID, mustAccount(t, "acc-1", ports.SeededUserID, "acc-1-name")); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	repo := NewImportBatchRepository(db)
	batch := mustImportBatch(t, "batch-1", ports.SeededUserID, "acc-1")
	err := repo.Create(ctx, otherUserID, batch)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("Create(mismatched actor) error = %v, want *errs.Error with code NotAllowed", err)
	}
}

func TestImportBatchRepository_Get_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewImportBatchRepository(db)
	_, err := repo.Get(context.Background(), ports.SeededUserID, "no-such-batch")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Get(unknown) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestImportBatchRepository_Get_CrossUserIsolation(t *testing.T) {
	db, _ := newTestDB(t)
	seedOtherUser(t, db, otherUserID)
	seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")

	repo := NewImportBatchRepository(db)
	_, err := repo.Get(context.Background(), otherUserID, "batch-1")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Get(other user's batch) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestImportBatchRepository_List(t *testing.T) {
	db, _ := newTestDB(t)
	seedOtherUser(t, db, otherUserID)
	ctx := context.Background()

	seedImportBatch(t, db, ports.SeededUserID, "batch-a", "acc-a")
	seedImportBatch(t, db, ports.SeededUserID, "batch-b", "acc-b")
	seedImportBatch(t, db, otherUserID, "batch-other", "acc-other")

	repo := NewImportBatchRepository(db)
	got, err := repo.List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d batches, want 2 (never another user's)", len(got))
	}
	// Every insert lands under the same frozen-clock instant, so the tie
	// is broken by id DESC — the most recently *created* (highest id in
	// insertion order here) batch comes first.
	if got[0].ID() != "batch-b" || got[1].ID() != "batch-a" {
		t.Errorf("List order = [%s, %s], want [batch-b, batch-a]", got[0].ID(), got[1].ID())
	}
}

func TestImportBatchRepository_Update(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")

	reviewed, err := batch.MarkReviewed()
	if err != nil {
		t.Fatalf("MarkReviewed: %v", err)
	}

	repo := NewImportBatchRepository(db)
	if err := repo.Update(ctx, ports.SeededUserID, reviewed); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "batch-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status() != importing.ImportBatchStatusReviewed {
		t.Errorf("Status() after Update = %q, want %q", got.Status(), importing.ImportBatchStatusReviewed)
	}
}

func TestImportBatchRepository_Update_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	accRepo := NewAccountRepository(db)
	if err := accRepo.Create(ctx, ports.SeededUserID, mustAccount(t, "acc-1", ports.SeededUserID, "acc-1-name")); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	repo := NewImportBatchRepository(db)
	batch := mustImportBatch(t, "no-such-batch", ports.SeededUserID, "acc-1")
	err := repo.Update(ctx, ports.SeededUserID, batch)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Update(unknown) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestImportBatchRepository_Update_RejectsMismatchedActor(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	batch := seedImportBatch(t, db, ports.SeededUserID, "batch-1", "acc-1")

	repo := NewImportBatchRepository(db)
	err := repo.Update(ctx, otherUserID, batch)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("Update(mismatched actor) error = %v, want *errs.Error with code NotAllowed", err)
	}
}
