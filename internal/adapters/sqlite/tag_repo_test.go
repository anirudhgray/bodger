package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/ports"
)

func TestTagRepository_List(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	txnRepo := NewTransactionRepository(db)
	tagRepo := NewTagRepository(db)
	ctx := context.Background()

	catID := "cat-1"
	p := mustPosting(t, "post-1", "acc-1", -1000, "INR", &catID)
	txn, err := ledger.NewOutflow("txn-1", ports.SeededUserID, mustDate(t, 2026, time.August, 14), "Trip expense", []ledger.Posting{p})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}
	tags := []ledger.Tag{mustTag(t, "#Japan-Trip-2026"), mustTag(t, "reimbursable")}
	if err := txnRepo.Create(ctx, ports.SeededUserID, txn, tags); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := tagRepo.List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List = %+v, want 2 tags", got)
	}
	values := map[string]bool{}
	for _, tag := range got {
		values[tag.String()] = true
	}
	if !values["japan-trip-2026"] || !values["reimbursable"] {
		t.Errorf("List values = %v, want japan-trip-2026 and reimbursable", values)
	}
}

func TestTagRepository_List_FiltersByActor(t *testing.T) {
	db, _ := newTestDB(t)
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")
	seedOtherUser(t, db, otherUserID)
	seedAccountAndCategory(t, db, otherUserID, "acc-other", "cat-other")

	txnRepo := NewTransactionRepository(db)
	tagRepo := NewTagRepository(db)
	ctx := context.Background()

	catID := "cat-1"
	p := mustPosting(t, "post-1", "acc-1", -1000, "INR", &catID)
	txn, err := ledger.NewOutflow("txn-1", ports.SeededUserID, mustDate(t, 2026, time.August, 14), "Mine", []ledger.Posting{p})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}
	if err := txnRepo.Create(ctx, ports.SeededUserID, txn, []ledger.Tag{mustTag(t, "mine")}); err != nil {
		t.Fatalf("Create mine: %v", err)
	}

	otherCatID := "cat-other"
	pOther := mustPosting(t, "post-other", "acc-other", -1000, "INR", &otherCatID)
	txnOther, err := ledger.NewOutflow("txn-other", otherUserID, mustDate(t, 2026, time.August, 14), "Theirs", []ledger.Posting{pOther})
	if err != nil {
		t.Fatalf("NewOutflow: %v", err)
	}
	if err := txnRepo.Create(ctx, otherUserID, txnOther, []ledger.Tag{mustTag(t, "theirs")}); err != nil {
		t.Fatalf("Create theirs: %v", err)
	}

	got, err := tagRepo.List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].String() != "mine" {
		t.Errorf("List(seeded user) = %+v, want only [mine]", got)
	}
}
