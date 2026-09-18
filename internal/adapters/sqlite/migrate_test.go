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
// Update this alongside adding a new migration.
const latestMigrationVersion = 16

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

// TestMigrate00009_ArchivedAtRoundTrip exercises the 00009 migration's
// create-copy-drop-rename dance in both directions: an archived and an
// active row of each table, taken down to the pre-00009 (archived bool)
// shape and back up to the archived_at shape, must preserve the
// archived/active *state* — the down migration collapses archived_at to a
// bool, so the up migration re-expanding it can't recover the original
// instant, only that the row was (or wasn't) archived.
func TestMigrate00009_ArchivedAtRoundTrip(t *testing.T) {
	db, clk := newTestDB(t) // newTestDB already migrates all the way up
	ctx := context.Background()

	now := formatTime(clk.Now())

	insertAccount := func(id string, archivedAt any) {
		t.Helper()
		_, err := db.write.ExecContext(ctx, `
			INSERT INTO accounts (id, user_id, name, kind, currency, institution, opening_balance_minor, opening_balance_date, archived_at, sort_order, created_at, updated_at)
			VALUES (?, ?, ?, 'bank', 'USD', ?, 0, NULL, ?, 7, ?, ?)
		`, id, ports.SeededUserID, id+"-name", "Test Bank", archivedAt, now, now)
		if err != nil {
			t.Fatalf("insert account %q: %v", id, err)
		}
	}
	insertCategory := func(id string, archivedAt any) {
		t.Helper()
		_, err := db.write.ExecContext(ctx, `
			INSERT INTO categories (id, user_id, parent_id, name, kind, archived_at, sort_order, created_at, updated_at)
			VALUES (?, ?, NULL, ?, 'expense', ?, 3, ?, ?)
		`, id, ports.SeededUserID, id+"-name", archivedAt, now, now)
		if err != nil {
			t.Fatalf("insert category %q: %v", id, err)
		}
	}

	insertAccount("acc-archived", now)
	insertAccount("acc-active", nil)
	insertCategory("cat-archived", now)
	insertCategory("cat-active", nil)

	if err := db.migrateDownTo(ctx, 8); err != nil {
		t.Fatalf("migrateDownTo(8): %v", err)
	}

	// The pre-00009 schema has no institution/sort_order columns at all —
	// confirming that, not just that `archived` holds the right value,
	// is what proves the table was actually rebuilt rather than just
	// having a column renamed.
	for _, table := range []string{"accounts", "categories"} {
		var count int
		err := db.write.QueryRowContext(ctx, fmt.Sprintf(
			`SELECT count(*) FROM pragma_table_info('%s') WHERE name IN ('institution', 'sort_order', 'archived_at')`, table,
		)).Scan(&count)
		if err != nil {
			t.Fatalf("inspect %s columns: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s still has a 00009 column after migrating down to 8", table)
		}
	}

	checkArchivedBool := func(table, id string, want int) {
		t.Helper()
		var got int
		err := db.write.QueryRowContext(ctx, fmt.Sprintf(`SELECT archived FROM %s WHERE id = ?`, table), id).Scan(&got)
		if err != nil {
			t.Fatalf("query %s.archived for %q: %v", table, id, err)
		}
		if got != want {
			t.Errorf("%s %q archived = %d, want %d", table, id, got, want)
		}
	}
	checkArchivedBool("accounts", "acc-archived", 1)
	checkArchivedBool("accounts", "acc-active", 0)
	checkArchivedBool("categories", "cat-archived", 1)
	checkArchivedBool("categories", "cat-active", 0)

	if err := db.migrateUpTo(ctx, 9); err != nil {
		t.Fatalf("migrateUpTo(9): %v", err)
	}

	checkArchivedAt := func(table, id string, wantArchived bool) {
		t.Helper()
		var archivedAt sql.NullString
		err := db.write.QueryRowContext(ctx, fmt.Sprintf(`SELECT archived_at FROM %s WHERE id = ?`, table), id).Scan(&archivedAt)
		if err != nil {
			t.Fatalf("query %s.archived_at for %q: %v", table, id, err)
		}
		if archivedAt.Valid != wantArchived {
			t.Errorf("%s %q archived_at valid = %v, want %v", table, id, archivedAt.Valid, wantArchived)
		}
	}
	checkArchivedAt("accounts", "acc-archived", true)
	checkArchivedAt("accounts", "acc-active", false)
	checkArchivedAt("categories", "cat-archived", true)
	checkArchivedAt("categories", "cat-active", false)

	// institution and sort_order round-trip through the down migration by
	// definition (the pre-00009 schema can't hold them at all), so the
	// re-upped row necessarily reverts to NULL/0 rather than the values
	// it was inserted with — documented, not a bug, since 00009's Down is
	// a genuine schema rollback, not a lossless snapshot.
	var institution sql.NullString
	if err := db.write.QueryRowContext(ctx, `SELECT institution FROM accounts WHERE id = ?`, "acc-archived").Scan(&institution); err != nil {
		t.Fatalf("query accounts.institution: %v", err)
	}
	if institution.Valid {
		t.Errorf("accounts.institution = %q after up-down-up, want NULL (not preserved across the Down rollback)", institution.String)
	}
}

// TestMigrate00016_DropsBothRecurringTables exercises the 00016
// migration's Down in isolation, on top of the generic up-down-up cycle
// above: scheduled_occurrences must be dropped before recurring_rules (it
// references it), so a Down listing them the other way round would fail
// here rather than only on a real rollback.
func TestMigrate00016_DropsBothRecurringTables(t *testing.T) {
	db, _ := newTestDB(t) // newTestDB already migrates all the way up
	ctx := context.Background()

	countTables := func() int {
		t.Helper()
		var n int
		err := db.write.QueryRowContext(ctx, `
			SELECT count(*) FROM sqlite_master
			WHERE type = 'table' AND name IN ('recurring_rules', 'scheduled_occurrences')
		`).Scan(&n)
		if err != nil {
			t.Fatalf("count recurring tables: %v", err)
		}
		return n
	}

	if got := countTables(); got != 2 {
		t.Fatalf("after migrating up, %d of the two recurring tables exist, want 2", got)
	}

	if err := db.migrateDownTo(ctx, 15); err != nil {
		t.Fatalf("migrateDownTo(15): %v", err)
	}
	if got := countTables(); got != 0 {
		t.Errorf("%d recurring tables survived migrating down to 15, want 0", got)
	}

	if err := db.migrateUpTo(ctx, 16); err != nil {
		t.Fatalf("migrateUpTo(16): %v", err)
	}
	if got := countTables(); got != 2 {
		t.Errorf("after re-migrating up, %d of the two recurring tables exist, want 2", got)
	}
}

// TestMigrate00016_ScheduleShapeCheck proves the schema's own frequency
// CHECK is live, independently of recurring.Schedule's constructors: a row
// whose positional columns don't match its declared frequency can't be
// written even by SQL that bypasses the domain layer entirely (ADR-0014's
// "unwritable as well as unrepresentable").
func TestMigrate00016_ScheduleShapeCheck(t *testing.T) {
	db, clk := newTestDB(t)
	ctx := context.Background()
	now := formatTime(clk.Now())
	seedAccountAndCategory(t, db, ports.SeededUserID, "acc-1", "cat-1")

	insert := func(id, frequency string, weekday, dayOfMonth, monthOfYear any) error {
		_, err := db.write.ExecContext(ctx, `
			INSERT INTO recurring_rules (id, user_id, account_id, category_id, amount_minor, description,
			                             frequency, interval_count, weekday, day_of_month, month_of_year,
			                             starts_on, ends_on, archived_at, created_at, updated_at)
			VALUES (?, ?, 'acc-1', 'cat-1', 50000, 'Rent', ?, 1, ?, ?, ?, '2026-01-01', NULL, NULL, ?, ?)
		`, id, ports.SeededUserID, frequency, weekday, dayOfMonth, monthOfYear, now, now)
		return err
	}

	tests := []struct {
		name        string
		frequency   string
		weekday     any
		dayOfMonth  any
		monthOfYear any
		wantErr     bool
	}{
		{name: "weekly with a weekday", frequency: "weekly", weekday: 1},
		{name: "monthly with a day", frequency: "monthly", dayOfMonth: 31},
		{name: "yearly with a month and a day", frequency: "yearly", dayOfMonth: 29, monthOfYear: 2},
		{name: "weekly without a weekday", frequency: "weekly", wantErr: true},
		{name: "weekly carrying a day of month", frequency: "weekly", weekday: 1, dayOfMonth: 15, wantErr: true},
		{name: "monthly carrying a weekday", frequency: "monthly", weekday: 1, dayOfMonth: 15, wantErr: true},
		{name: "yearly without a month", frequency: "yearly", dayOfMonth: 29, wantErr: true},
		{name: "daily is not a supported frequency", frequency: "daily", dayOfMonth: 1, wantErr: true},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := insert(fmt.Sprintf("rule-%d", i), tt.frequency, tt.weekday, tt.dayOfMonth, tt.monthOfYear)
			if tt.wantErr && err == nil {
				t.Error("insert succeeded, want a CHECK constraint violation")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("insert failed: %v", err)
			}
		})
	}
}

// TestMigrate00016_OccurrenceStatusAgreesWithTransaction is the schema
// half of data-model.md §11's invariant: a materialised occurrence must
// carry its transaction and a pending or skipped one must not, enforced
// even against a direct write that never touched the domain layer.
func TestMigrate00016_OccurrenceStatusAgreesWithTransaction(t *testing.T) {
	db, clk := newTestDB(t)
	ctx := context.Background()
	now := formatTime(clk.Now())
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")
	txnID := seedTransactionForOccurrence(t, db, ports.SeededUserID, "r1")

	insert := func(id, status string, transactionID any, occurrenceDate string) error {
		_, err := db.write.ExecContext(ctx, `
			INSERT INTO scheduled_occurrences (id, rule_id, occurrence_date, status, transaction_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, id, ruleID, occurrenceDate, status, transactionID, now, now)
		return err
	}

	tests := []struct {
		name          string
		status        string
		transactionID any
		wantErr       bool
	}{
		{name: "pending without a transaction", status: "pending"},
		{name: "skipped without a transaction", status: "skipped"},
		{name: "materialised with a transaction", status: "materialised", transactionID: txnID},
		{name: "pending with a transaction", status: "pending", transactionID: txnID, wantErr: true},
		{name: "skipped with a transaction", status: "skipped", transactionID: txnID, wantErr: true},
		{name: "materialised without a transaction", status: "materialised", wantErr: true},
		{name: "an unknown status", status: "bogus", wantErr: true},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A distinct date per case, so UNIQUE (rule_id,
			// occurrence_date) never masks the CHECK under test.
			err := insert(fmt.Sprintf("occ-%d", i), tt.status, tt.transactionID, fmt.Sprintf("2026-01-%02d", i+1))
			if tt.wantErr && err == nil {
				t.Error("insert succeeded, want a CHECK constraint violation")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("insert failed: %v", err)
			}
		})
	}
}
