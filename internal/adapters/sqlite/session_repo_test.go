package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func mustSession(id, userID, tokenHash string, createdAt, lastUsedAt, expiresAt time.Time) ports.Session {
	return ports.Session{
		ID:         id,
		UserID:     userID,
		TokenHash:  tokenHash,
		CreatedAt:  createdAt,
		LastUsedAt: lastUsedAt,
		ExpiresAt:  expiresAt,
	}
}

func TestSessionRepository_CreateAndGetByTokenHash(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	created := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	expires := created.Add(24 * time.Hour)
	session := mustSession("sess-1", ports.SeededUserID, "hash-1", created, created, expires)
	if err := repo.Create(ctx, ports.SeededUserID, session); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByTokenHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("GetByTokenHash: %v", err)
	}
	if got.ID != "sess-1" || got.UserID != ports.SeededUserID || got.TokenHash != "hash-1" {
		t.Errorf("GetByTokenHash = %+v", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, created)
	}
	if !got.LastUsedAt.Equal(created) {
		t.Errorf("LastUsedAt = %v, want %v", got.LastUsedAt, created)
	}
	if !got.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, expires)
	}
}

func TestSessionRepository_Create_RejectsMismatchedActor(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	session := mustSession("sess-1", ports.SeededUserID, "hash-1", now, now, now.Add(time.Hour))

	err := repo.Create(ctx, otherUserID, session)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Errorf("Create with mismatched actor: err = %v, want errs.NotAllowed", err)
	}
}

func TestSessionRepository_Create_DuplicateTokenHashConflicts(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	a := mustSession("sess-a", ports.SeededUserID, "dup-hash", now, now, now.Add(time.Hour))
	b := mustSession("sess-b", ports.SeededUserID, "dup-hash", now, now, now.Add(time.Hour))

	if err := repo.Create(ctx, ports.SeededUserID, a); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	err := repo.Create(ctx, ports.SeededUserID, b)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.Conflict {
		t.Errorf("Create duplicate token hash: err = %v, want errs.Conflict", err)
	}
}

func TestSessionRepository_GetByTokenHash_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewSessionRepository(db)

	_, err := repo.GetByTokenHash(context.Background(), "does-not-exist")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("GetByTokenHash(missing): err = %v, want errs.NotFound", err)
	}
}

// TestSessionRepository_Touch_SlidesExpiry covers the sliding-expiry
// bookkeeping ADR-0006 calls for: Touch overwrites LastUsedAt and
// ExpiresAt with whatever the application layer's clock decided.
func TestSessionRepository_Touch_SlidesExpiry(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	created := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	session := mustSession("sess-1", ports.SeededUserID, "hash-1", created, created, created.Add(time.Hour))
	if err := repo.Create(ctx, ports.SeededUserID, session); err != nil {
		t.Fatalf("Create: %v", err)
	}

	renewed := created.Add(30 * time.Minute)
	newExpiry := renewed.Add(time.Hour)
	if err := repo.Touch(ctx, ports.SeededUserID, "sess-1", renewed, newExpiry); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	got, err := repo.GetByTokenHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("GetByTokenHash: %v", err)
	}
	if !got.LastUsedAt.Equal(renewed) {
		t.Errorf("LastUsedAt after Touch = %v, want %v", got.LastUsedAt, renewed)
	}
	if !got.ExpiresAt.Equal(newExpiry) {
		t.Errorf("ExpiresAt after Touch = %v, want %v", got.ExpiresAt, newExpiry)
	}
}

func TestSessionRepository_Touch_NotFoundForOtherActor(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	session := mustSession("sess-1", ports.SeededUserID, "hash-1", now, now, now.Add(time.Hour))
	if err := repo.Create(ctx, ports.SeededUserID, session); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := repo.Touch(ctx, otherUserID, "sess-1", now, now.Add(2*time.Hour))
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("Touch(other actor's session): err = %v, want errs.NotFound", err)
	}
}

// TestSessionRepository_Delete_RemovesRow covers ADR-0006's "revocation is
// a DELETE" for sessions: unlike an API token's soft Revoke, a deleted
// session is gone, not merely flagged.
func TestSessionRepository_Delete_RemovesRow(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	session := mustSession("sess-1", ports.SeededUserID, "hash-1", now, now, now.Add(time.Hour))
	if err := repo.Create(ctx, ports.SeededUserID, session); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, ports.SeededUserID, "sess-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := repo.GetByTokenHash(ctx, "hash-1")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("GetByTokenHash after Delete: err = %v, want errs.NotFound", err)
	}
}

func TestSessionRepository_Delete_NotFoundForOtherActor(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	session := mustSession("sess-1", ports.SeededUserID, "hash-1", now, now, now.Add(time.Hour))
	if err := repo.Create(ctx, ports.SeededUserID, session); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := repo.Delete(ctx, otherUserID, "sess-1")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("Delete(other actor's session): err = %v, want errs.NotFound", err)
	}
}
