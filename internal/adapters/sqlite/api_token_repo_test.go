package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func mustAPIToken(id, userID, tokenHash, name string, createdAt time.Time) ports.APIToken {
	return ports.APIToken{
		ID:        id,
		UserID:    userID,
		TokenHash: tokenHash,
		Name:      name,
		CreatedAt: createdAt,
	}
}

func TestAPITokenRepository_CreateAndGetByTokenHash(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAPITokenRepository(db)
	ctx := context.Background()

	created := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	token := mustAPIToken("tok-1", ports.SeededUserID, "hash-1", "laptop", created)
	if err := repo.Create(ctx, ports.SeededUserID, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByTokenHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("GetByTokenHash: %v", err)
	}
	if got.ID != "tok-1" || got.UserID != ports.SeededUserID || got.Name != "laptop" {
		t.Errorf("GetByTokenHash = %+v", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, created)
	}
	if got.LastUsedAt != nil {
		t.Errorf("LastUsedAt = %v, want nil (never used)", got.LastUsedAt)
	}
	if got.ExpiresAt != nil {
		t.Errorf("ExpiresAt = %v, want nil (no expiry set)", got.ExpiresAt)
	}
	if got.RevokedAt != nil {
		t.Errorf("RevokedAt = %v, want nil (not revoked)", got.RevokedAt)
	}
}

func TestAPITokenRepository_Create_WithExpiryRoundTrips(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAPITokenRepository(db)
	ctx := context.Background()

	created := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	expires := created.Add(30 * 24 * time.Hour)
	token := mustAPIToken("tok-1", ports.SeededUserID, "hash-1", "backup script", created)
	token.ExpiresAt = &expires
	if err := repo.Create(ctx, ports.SeededUserID, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByTokenHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("GetByTokenHash: %v", err)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, expires)
	}
}

func TestAPITokenRepository_Create_RejectsMismatchedActor(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAPITokenRepository(db)
	ctx := context.Background()

	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	token := mustAPIToken("tok-1", ports.SeededUserID, "hash-1", "laptop", now)

	err := repo.Create(ctx, otherUserID, token)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Errorf("Create with mismatched actor: err = %v, want errs.NotAllowed", err)
	}
}

func TestAPITokenRepository_Create_DuplicateTokenHashConflicts(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAPITokenRepository(db)
	ctx := context.Background()

	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	a := mustAPIToken("tok-a", ports.SeededUserID, "dup-hash", "a", now)
	b := mustAPIToken("tok-b", ports.SeededUserID, "dup-hash", "b", now)

	if err := repo.Create(ctx, ports.SeededUserID, a); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	err := repo.Create(ctx, ports.SeededUserID, b)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.Conflict {
		t.Errorf("Create duplicate token hash: err = %v, want errs.Conflict", err)
	}
}

func TestAPITokenRepository_GetByTokenHash_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAPITokenRepository(db)

	_, err := repo.GetByTokenHash(context.Background(), "does-not-exist")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("GetByTokenHash(missing): err = %v, want errs.NotFound", err)
	}
}

func TestAPITokenRepository_List_OrdersNewestFirstAndFiltersByActor(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAPITokenRepository(db)
	ctx := context.Background()

	seedOtherUser(t, db, otherUserID)

	t0 := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	older := mustAPIToken("tok-older", ports.SeededUserID, "hash-older", "older", t0)
	newer := mustAPIToken("tok-newer", ports.SeededUserID, "hash-newer", "newer", t0.Add(time.Hour))
	theirs := mustAPIToken("tok-theirs", otherUserID, "hash-theirs", "theirs", t0)

	if err := repo.Create(ctx, ports.SeededUserID, older); err != nil {
		t.Fatalf("Create older: %v", err)
	}
	if err := repo.Create(ctx, ports.SeededUserID, newer); err != nil {
		t.Fatalf("Create newer: %v", err)
	}
	if err := repo.Create(ctx, otherUserID, theirs); err != nil {
		t.Fatalf("Create theirs: %v", err)
	}

	list, err := repo.List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 || list[0].ID != "tok-newer" || list[1].ID != "tok-older" {
		t.Errorf("List(seeded user) = %+v, want [tok-newer, tok-older]", list)
	}
}

// TestAPITokenRepository_List_IncludesRevoked covers the "listable" part of
// ADR-0006's credential table: a revoked token stays in List so a token
// management screen can show history, not just what's currently live.
func TestAPITokenRepository_List_IncludesRevoked(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAPITokenRepository(db)
	ctx := context.Background()

	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	token := mustAPIToken("tok-1", ports.SeededUserID, "hash-1", "laptop", now)
	if err := repo.Create(ctx, ports.SeededUserID, token); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Revoke(ctx, ports.SeededUserID, "tok-1", now.Add(time.Hour)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	list, err := repo.List(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].RevokedAt == nil {
		t.Errorf("List after Revoke = %+v, want one revoked token", list)
	}
}

func TestAPITokenRepository_Touch_RecordsLastUsedAt(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAPITokenRepository(db)
	ctx := context.Background()

	created := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	token := mustAPIToken("tok-1", ports.SeededUserID, "hash-1", "laptop", created)
	if err := repo.Create(ctx, ports.SeededUserID, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	used := created.Add(time.Hour)
	if err := repo.Touch(ctx, ports.SeededUserID, "tok-1", used); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	got, err := repo.GetByTokenHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("GetByTokenHash: %v", err)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(used) {
		t.Errorf("LastUsedAt after Touch = %v, want %v", got.LastUsedAt, used)
	}
}

func TestAPITokenRepository_Touch_NotFoundForOtherActor(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAPITokenRepository(db)
	ctx := context.Background()

	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	token := mustAPIToken("tok-1", ports.SeededUserID, "hash-1", "laptop", now)
	if err := repo.Create(ctx, ports.SeededUserID, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := repo.Touch(ctx, otherUserID, "tok-1", now.Add(time.Hour))
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("Touch(other actor's token): err = %v, want errs.NotFound", err)
	}
}

func TestAPITokenRepository_Revoke_NotFoundForOtherActor(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewAPITokenRepository(db)
	ctx := context.Background()

	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	token := mustAPIToken("tok-1", ports.SeededUserID, "hash-1", "laptop", now)
	if err := repo.Create(ctx, ports.SeededUserID, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := repo.Revoke(ctx, otherUserID, "tok-1", now.Add(time.Hour))
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("Revoke(other actor's token): err = %v, want errs.NotFound", err)
	}
}
