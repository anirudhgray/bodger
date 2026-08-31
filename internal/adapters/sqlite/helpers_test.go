package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/platform/clock"
)

// testClockInstant is the fixed instant every test's frozen clock starts
// at, chosen with no significance beyond being unambiguous.
var testClockInstant = time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)

// newTestDB opens a real temp-file SQLite database (ADR-0007: never
// in-memory — WAL and locking behave differently enough that an in-memory
// test would prove the wrong thing), migrates it up, and registers cleanup.
// Every repository test in this package uses this, so each gets its own
// fresh, fully migrated file.
func newTestDB(t *testing.T) (*DB, *clock.Frozen) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "bodger.db")
	clk := clock.NewFrozen(testClockInstant)

	db, err := Open(clk, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	if err := db.MigrateUp(context.Background()); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	return db, clk
}

// seedOtherUser inserts a second user row directly, for cross-user
// isolation tests that need a second real user_id to satisfy the
// accounts/categories/transactions foreign key to users(id) — distinct
// from ports.SeededUserID, which the first migration already inserts.
func seedOtherUser(t *testing.T, db *DB, userID string) {
	t.Helper()
	_, err := db.write.ExecContext(context.Background(),
		`INSERT INTO users (id, created_at) VALUES (?, ?)`, userID, formatTime(testClockInstant))
	if err != nil {
		t.Fatalf("seedOtherUser(%q): %v", userID, err)
	}
}
