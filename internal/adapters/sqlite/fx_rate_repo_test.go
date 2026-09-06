package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
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

func TestFxRateRepository_StoreBatch_InsertsAllRows(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewFxRateRepository(db)
	ctx := context.Background()

	rows := []ports.FxRateRow{
		{Rate: mustRate(t, "INR", "USD", "0.0115"), Date: mustDate(t, 2026, time.August, 12), Source: "frankfurter"},
		{Rate: mustRate(t, "INR", "USD", "0.0116"), Date: mustDate(t, 2026, time.August, 13), Source: "frankfurter"},
		{Rate: mustRate(t, "EUR", "USD", "1.0800"), Date: mustDate(t, 2026, time.August, 13), Source: "frankfurter"},
	}
	if err := repo.StoreBatch(ctx, rows); err != nil {
		t.Fatalf("StoreBatch: %v", err)
	}

	var count int
	row := db.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM fx_rates`)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3 (one row per StoreBatch entry)", count)
	}
}

func TestFxRateRepository_StoreBatch_UpsertsAgainstExistingRow(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewFxRateRepository(db)
	ctx := context.Background()

	date := mustDate(t, 2026, time.August, 14)
	if err := repo.Store(ctx, mustRate(t, "INR", "USD", "0.0115"), date, "frankfurter"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	rows := []ports.FxRateRow{
		{Rate: mustRate(t, "INR", "USD", "0.0199"), Date: date, Source: "frankfurter"},
	}
	if err := repo.StoreBatch(ctx, rows); err != nil {
		t.Fatalf("StoreBatch: %v", err)
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
		t.Fatalf("count = %d, want 1 (StoreBatch should upsert the same as Store)", count)
	}
	if storedRate != "0.019900000000" {
		t.Errorf("stored rate = %q, want %q", storedRate, "0.019900000000")
	}
}

func TestFxRateRepository_StoreBatch_Empty(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewFxRateRepository(db)
	ctx := context.Background()

	if err := repo.StoreBatch(ctx, nil); err != nil {
		t.Fatalf("StoreBatch(nil): %v", err)
	}
}

func TestFxRateRepository_StoreBatch_RejectsInvalidRow(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewFxRateRepository(db)
	ctx := context.Background()

	rows := []ports.FxRateRow{
		{Rate: mustRate(t, "INR", "USD", "0.0115"), Date: mustDate(t, 2026, time.August, 12), Source: "frankfurter"},
		{Rate: mustRate(t, "EUR", "USD", "1.0800"), Date: mustDate(t, 2026, time.August, 13), Source: ""},
	}
	if err := repo.StoreBatch(ctx, rows); err == nil {
		t.Fatalf("StoreBatch with an invalid row: want error, got nil")
	}

	var count int
	row := db.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM fx_rates`)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0 (a rejected batch should write nothing, not a partial set)", count)
	}
}

func TestFxRateRepository_Lookup_ExactMatch(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewFxRateRepository(db)
	ctx := context.Background()

	aug14 := mustDate(t, 2026, time.August, 14)
	if err := repo.Store(ctx, mustRate(t, "INR", "USD", "0.0115"), aug14, "frankfurter"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	sel, err := repo.Lookup(ctx, "INR", "USD", aug14, fx.DefaultStalenessWindowDays)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if sel.Stale {
		t.Errorf("Stale = true, want false for an exact match")
	}
	if !sel.Date.Equal(aug14) {
		t.Errorf("Date = %v, want %v", sel.Date, aug14)
	}
	if sel.Rate.Value().String() != "0.0115" {
		t.Errorf("Rate = %v, want 0.0115", sel.Rate.Value())
	}
}

func TestFxRateRepository_Lookup_NearestEarlierWithinWindow(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewFxRateRepository(db)
	ctx := context.Background()

	aug10 := mustDate(t, 2026, time.August, 10)
	aug14 := mustDate(t, 2026, time.August, 14)
	if err := repo.Store(ctx, mustRate(t, "INR", "USD", "0.0115"), aug10, "frankfurter"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	sel, err := repo.Lookup(ctx, "INR", "USD", aug14, fx.DefaultStalenessWindowDays)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !sel.Stale {
		t.Errorf("Stale = false, want true for a substitute rate")
	}
	if !sel.Date.Equal(aug10) {
		t.Errorf("Date = %v, want %v", sel.Date, aug10)
	}
}

func TestFxRateRepository_Lookup_NoneWithinWindow(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewFxRateRepository(db)
	ctx := context.Background()

	aug1 := mustDate(t, 2026, time.August, 1)
	aug14 := mustDate(t, 2026, time.August, 14)
	if err := repo.Store(ctx, mustRate(t, "INR", "USD", "0.0115"), aug1, "frankfurter"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	_, err := repo.Lookup(ctx, "INR", "USD", aug14, fx.DefaultStalenessWindowDays)
	if err == nil {
		t.Fatalf("Lookup: want an error, got nil")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) || appErr.Code != errs.NotFound {
		t.Errorf("Lookup error = %v, want *errs.Error{Code: NotFound}", err)
	}
}

func TestFxRateRepository_Lookup_RejectsNegativeWindow(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewFxRateRepository(db)
	ctx := context.Background()

	aug14 := mustDate(t, 2026, time.August, 14)
	_, err := repo.Lookup(ctx, "INR", "USD", aug14, -1)
	if err == nil {
		t.Fatalf("Lookup with negative window: want an error, got nil")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) || appErr.Code != errs.InvalidInput {
		t.Errorf("Lookup error = %v, want *errs.Error{Code: InvalidInput}", err)
	}
}
