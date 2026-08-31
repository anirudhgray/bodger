package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/ports"
)

// latestMigrationVersion is the highest numeric prefix under migrations/.
// Update this alongside adding a ninth migration.
const latestMigrationVersion = 8

func TestMigrateUp_SeedsUser(t *testing.T) {
	db, _ := newTestDB(t)

	var id string
	err := db.write.QueryRowContext(context.Background(), `SELECT id FROM users`).Scan(&id)
	if err != nil {
		t.Fatalf("query seeded user: %v", err)
	}
	if id != ports.SeededUserID {
		t.Errorf("seeded user id = %q, want %q", id, ports.SeededUserID)
	}
}

func TestMigrateUp_SeededUserIDStableAcrossRuns(t *testing.T) {
	// Two independent fresh databases must seed the exact same ID — it's
	// a fixed constant, not generated per install.
	db1, _ := newTestDB(t)
	db2, _ := newTestDB(t)

	var id1, id2 string
	if err := db1.write.QueryRowContext(context.Background(), `SELECT id FROM users`).Scan(&id1); err != nil {
		t.Fatalf("query db1 seeded user: %v", err)
	}
	if err := db2.write.QueryRowContext(context.Background(), `SELECT id FROM users`).Scan(&id2); err != nil {
		t.Fatalf("query db2 seeded user: %v", err)
	}
	if id1 != id2 {
		t.Errorf("seeded user id not stable across runs: %q vs %q", id1, id2)
	}
}

func TestOpen_Pragmas(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	for _, pool := range []struct {
		name string
		db   *sql.DB
	}{
		{"write", db.write},
		{"read", db.read},
	} {
		var journalMode string
		if err := pool.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
			t.Fatalf("%s: journal_mode: %v", pool.name, err)
		}
		if journalMode != "wal" {
			t.Errorf("%s: journal_mode = %q, want %q", pool.name, journalMode, "wal")
		}

		var foreignKeys int
		if err := pool.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("%s: foreign_keys: %v", pool.name, err)
		}
		if foreignKeys != 1 {
			t.Errorf("%s: foreign_keys = %d, want 1", pool.name, foreignKeys)
		}

		var synchronous int
		if err := pool.db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
			t.Fatalf("%s: synchronous: %v", pool.name, err)
		}
		if synchronous != 1 { // NORMAL == 1
			t.Errorf("%s: synchronous = %d, want 1 (NORMAL)", pool.name, synchronous)
		}

		var busyTimeout int
		if err := pool.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatalf("%s: busy_timeout: %v", pool.name, err)
		}
		if busyTimeout != 5000 {
			t.Errorf("%s: busy_timeout = %d, want 5000", pool.name, busyTimeout)
		}
	}
}

func TestOpen_WritePoolIsSingleConnection(t *testing.T) {
	db, _ := newTestDB(t)
	if max := db.write.Stats().MaxOpenConnections; max != 1 {
		t.Errorf("write pool MaxOpenConnections = %d, want 1", max)
	}
}

// TestMigrate_UpDownUpCycle is the CI-equivalent check issue #3 requires:
// every migration's down must actually work, verified by running the full
// sequence up, all the way back down, and up again on the same fresh
// database.
func TestMigrate_UpDownUpCycle(t *testing.T) {
	db, _ := newTestDB(t) // already migrated up once by the helper
	ctx := context.Background()

	if err := db.MigrateDownToZero(ctx); err != nil {
		t.Fatalf("MigrateDownToZero: %v", err)
	}

	// Every table should be gone.
	var tableCount int
	err := db.write.QueryRowContext(ctx, `
		SELECT count(*) FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name != 'goose_db_version'
	`).Scan(&tableCount)
	if err != nil {
		t.Fatalf("count tables after down: %v", err)
	}
	if tableCount != 0 {
		t.Errorf("tables remain after migrating down to zero: %d", tableCount)
	}

	if err := db.MigrateUp(ctx); err != nil {
		t.Fatalf("second MigrateUp: %v", err)
	}

	var id string
	if err := db.write.QueryRowContext(ctx, `SELECT id FROM users`).Scan(&id); err != nil {
		t.Fatalf("query seeded user after re-migrating up: %v", err)
	}
	if id != ports.SeededUserID {
		t.Errorf("seeded user id after up-down-up = %q, want %q", id, ports.SeededUserID)
	}
}

// TestMigrateUp_BacksUpBeforePendingMigrations exercises ADR-0007's
// pre-migration backup: when a database is partway through the migration
// sequence, MigrateUp must snapshot it to bodger.db.pre-<target>.bak before
// applying the rest.
func TestMigrateUp_BacksUpBeforePendingMigrations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bodger.db")
	clk := clock.NewFrozen(testClockInstant)

	db, err := Open(clk, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	// Land partway through the sequence, at the users table only.
	if err := db.migrateUpTo(ctx, 1); err != nil {
		t.Fatalf("migrateUpTo(1): %v", err)
	}

	if err := db.MigrateUp(ctx); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}

	backupPath := filepath.Join(dir, fmt.Sprintf("bodger.db.pre-%d.bak", latestMigrationVersion))
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("expected backup file %s: %v", backupPath, err)
	}
	if info.Size() == 0 {
		t.Errorf("backup file %s is empty", backupPath)
	}

	// The backup must reflect the pre-migration state (only the users
	// table), not the fully migrated one.
	backupDB, err := sql.Open("sqlite", backupPath)
	if err != nil {
		t.Fatalf("open backup file: %v", err)
	}
	defer backupDB.Close()

	var accountsTableCount int
	err = backupDB.QueryRowContext(ctx, `
		SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'accounts'
	`).Scan(&accountsTableCount)
	if err != nil {
		t.Fatalf("query backup schema: %v", err)
	}
	if accountsTableCount != 0 {
		t.Errorf("backup file already has the accounts table — it should reflect the pre-migration state")
	}
}

// TestCurrencies_MatchDomainSeed guards the 00002 migration's hardcoded
// currency seed against drifting from internal/domain/money's seed list —
// there is no single source both Go and SQL can read from directly.
func TestCurrencies_MatchDomainSeed(t *testing.T) {
	db, _ := newTestDB(t)

	rows, err := db.read.QueryContext(context.Background(), `SELECT code, minor_unit_exponent FROM currencies ORDER BY code`)
	if err != nil {
		t.Fatalf("query currencies: %v", err)
	}
	defer rows.Close()

	got := map[string]int{}
	for rows.Next() {
		var code string
		var exp int
		if err := rows.Scan(&code, &exp); err != nil {
			t.Fatalf("scan currency: %v", err)
		}
		got[code] = exp
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate currencies: %v", err)
	}

	want := map[string]int{}
	for _, c := range money.Currencies() {
		want[c.Code] = c.MinorUnitExponent
	}

	if len(got) != len(want) {
		t.Fatalf("currencies table has %d rows, domain seed has %d", len(got), len(want))
	}
	for code, exp := range want {
		gotExp, ok := got[code]
		if !ok {
			t.Errorf("currencies table is missing %q", code)
			continue
		}
		if gotExp != exp {
			t.Errorf("currency %q minor_unit_exponent = %d, want %d", code, gotExp, exp)
		}
	}
}
