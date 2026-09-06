package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func TestUserRepository_GetByID_SeededUserHasNoPasswordYet(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	user, err := repo.GetByID(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if user.ID != ports.SeededUserID {
		t.Errorf("ID = %q, want %q", user.ID, ports.SeededUserID)
	}
	if user.PasswordHash != nil {
		t.Errorf("PasswordHash = %q, want nil (no password set yet)", *user.PasswordHash)
	}
	if user.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want the migration's seeded timestamp")
	}
}

func TestUserRepository_GetByID_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewUserRepository(db)

	_, err := repo.GetByID(context.Background(), "does-not-exist")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("GetByID(missing): err = %v, want errs.NotFound", err)
	}
}

func TestUserRepository_SetPasswordHash_RoundTrips(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	if err := repo.SetPasswordHash(ctx, ports.SeededUserID, "encoded-hash"); err != nil {
		t.Fatalf("SetPasswordHash: %v", err)
	}

	user, err := repo.GetByID(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if user.PasswordHash == nil || *user.PasswordHash != "encoded-hash" {
		t.Errorf("PasswordHash = %v, want %q", user.PasswordHash, "encoded-hash")
	}
}

func TestUserRepository_SetPasswordHash_NotFoundForMissingUser(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewUserRepository(db)

	err := repo.SetPasswordHash(context.Background(), "does-not-exist", "encoded-hash")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("SetPasswordHash(missing user): err = %v, want errs.NotFound", err)
	}
}

func TestUserRepository_GetByID_SeededUserHasNoReportingCurrencyYet(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewUserRepository(db)

	user, err := repo.GetByID(context.Background(), ports.SeededUserID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if user.ReportingCurrency != nil {
		t.Errorf("ReportingCurrency = %q, want nil (no reporting currency set yet)", *user.ReportingCurrency)
	}
}

func TestUserRepository_SetReportingCurrency_RoundTrips(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	if err := repo.SetReportingCurrency(ctx, ports.SeededUserID, "EUR"); err != nil {
		t.Fatalf("SetReportingCurrency: %v", err)
	}

	user, err := repo.GetByID(ctx, ports.SeededUserID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if user.ReportingCurrency == nil || *user.ReportingCurrency != "EUR" {
		t.Errorf("ReportingCurrency = %v, want %q", user.ReportingCurrency, "EUR")
	}
}

func TestUserRepository_SetReportingCurrency_NotFoundForMissingUser(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewUserRepository(db)

	err := repo.SetReportingCurrency(context.Background(), "does-not-exist", "EUR")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Errorf("SetReportingCurrency(missing user): err = %v, want errs.NotFound", err)
	}
}
