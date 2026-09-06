package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain/fx"
)

func mustRate(t *testing.T, base, quote, value string) fx.Rate {
	t.Helper()
	dec, err := decimal.NewFromString(value)
	if err != nil {
		t.Fatalf("decimal.NewFromString(%q): %v", value, err)
	}
	rate, err := fx.NewRate(base, quote, dec)
	if err != nil {
		t.Fatalf("fx.NewRate: %v", err)
	}
	return rate
}

func TestFxRateRepository_Store_UpsertsOnSameKey(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewFxRateRepository(db)
	ctx := context.Background()

	date := mustDate(t, 2026, time.August, 14)
	rate1 := mustRate(t, "INR", "USD", "0.0115")
	if err := repo.Store(ctx, rate1, date, "frankfurter"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	rate2 := mustRate(t, "INR", "USD", "0.0116")
	if err := repo.Store(ctx, rate2, date, "frankfurter"); err != nil {
		t.Fatalf("Store (again): %v", err)
	}

	var count int
	var storedRate string
	row := db.read.QueryRowContext(ctx, `
		SELECT COUNT(*), MAX(rate) FROM fx_rates
		WHERE base = ? AND quote = ? AND rate_date = ? AND source = ?
	`, "INR", "USD", "2026-08-14", "frankfurter")
	if err := row.Scan(&count, &storedRate); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1 (a second Store on the same key should upsert, not insert a second row)", count)
	}
	if storedRate != "0.011600000000" {
		t.Errorf("stored rate = %q, want %q (second Store should have replaced the first)", storedRate, "0.011600000000")
	}
}

func TestFxRateRepository_Store_DistinctSourcesCoexist(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewFxRateRepository(db)
	ctx := context.Background()

	date := mustDate(t, 2026, time.August, 14)
	rateA := mustRate(t, "INR", "USD", "0.0115")
	rateB := mustRate(t, "INR", "USD", "0.0120")
	if err := repo.Store(ctx, rateA, date, "frankfurter"); err != nil {
		t.Fatalf("Store (frankfurter): %v", err)
	}
	if err := repo.Store(ctx, rateB, date, "manual"); err != nil {
		t.Fatalf("Store (manual): %v", err)
	}

	var count int
	row := db.read.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM fx_rates WHERE base = ? AND quote = ? AND rate_date = ?
	`, "INR", "USD", "2026-08-14")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2 (source is part of the primary key, so two sources for the same day are two rows)", count)
	}
}

func TestFxRateRepository_Store_RejectsEmptySource(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewFxRateRepository(db)
	ctx := context.Background()

	date := mustDate(t, 2026, time.August, 14)
	rate := mustRate(t, "INR", "USD", "0.0115")
	if err := repo.Store(ctx, rate, date, ""); err == nil {
		t.Fatalf("Store with empty source: want error, got nil")
	}
}
