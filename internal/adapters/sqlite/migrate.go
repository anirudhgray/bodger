package sqlite

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationsSub is migrationsFS rooted at "migrations", so goose sees
// migration files directly rather than nested under a "migrations/"
// prefix.
func migrationsSub() fs.FS {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		// Unreachable: "migrations" is a literal directory this package
		// embeds above; fs.Sub only fails on a malformed path.
		panic(fmt.Sprintf("sqlite: migrations embed is broken: %v", err))
	}
	return sub
}

// MigrateUp applies every pending migration, backing up the database file
// first if any are pending (ADR-0007: "if migrations are pending, the
// server copies the database to bodger.db.pre-<version>.bak before
// applying"). It runs on the write pool, since migrations are DDL and
// DML — writes.
func (db *DB) MigrateUp(ctx context.Context) error {
	// Deliberately not provider.Close()'d: goose's Provider.Close closes
	// the *sql.DB it was given, and db.write is our long-lived write
	// pool — closing it here would take down every later call on this
	// *DB, not just this provider.
	provider, err := goose.NewProvider(goose.DialectSQLite3, db.write, migrationsSub())
	if err != nil {
		return fmt.Errorf("sqlite: create migration provider: %w", err)
	}

	pending, err := provider.HasPending(ctx)
	if err != nil {
		return fmt.Errorf("sqlite: check pending migrations: %w", err)
	}
	if pending {
		if err := db.backupBeforeMigrate(ctx, provider); err != nil {
			return err
		}
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("sqlite: migrate up: %w", err)
	}
	return nil
}

// migrateUpTo applies migrations up to and including version, with no
// backup step. It exists so tests can put a database partway through the
// migration sequence — to exercise MigrateUp's pending-migration backup
// path, in particular — without production code ever calling it.
func (db *DB) migrateUpTo(ctx context.Context, version int64) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, db.write, migrationsSub())
	if err != nil {
		return fmt.Errorf("sqlite: create migration provider: %w", err)
	}

	if _, err := provider.UpTo(ctx, version); err != nil {
		return fmt.Errorf("sqlite: migrate up to %d: %w", version, err)
	}
	return nil
}

// migrateDownTo rolls back migrations to and including everything after
// version, landing the database at exactly version. Like migrateUpTo, it
// exists so tests can exercise a specific migration's Down step (and the
// schema/data it leaves behind) in isolation — production code never
// calls this.
func (db *DB) migrateDownTo(ctx context.Context, version int64) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, db.write, migrationsSub())
	if err != nil {
		return fmt.Errorf("sqlite: create migration provider: %w", err)
	}

	if _, err := provider.DownTo(ctx, version); err != nil {
		return fmt.Errorf("sqlite: migrate down to %d: %w", version, err)
	}
	return nil
}

// MigrateDownToZero rolls back every applied migration, in reverse order.
// It exists for the up→down→up cycle CI and tests run on every migration
// (ADR-0007) — production code never calls this.
func (db *DB) MigrateDownToZero(ctx context.Context) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, db.write, migrationsSub())
	if err != nil {
		return fmt.Errorf("sqlite: create migration provider: %w", err)
	}

	if _, err := provider.DownTo(ctx, 0); err != nil {
		return fmt.Errorf("sqlite: migrate down: %w", err)
	}
	return nil
}

// backupBeforeMigrate copies the database to
// "<dir>/bodger.db.pre-<targetVersion>.bak" alongside the live file, using
// SQLite's own VACUUM INTO rather than a raw file copy: under WAL, recent
// writes can still be sitting in the -wal file, and a plain `cp` of the
// main file alone would silently miss them. VACUUM INTO produces a single,
// consistent, checkpointed snapshot in one atomic statement — cheap at
// personal-ledger scale, and the whole point of taking a backup no one
// verifies until the day they need it.
func (db *DB) backupBeforeMigrate(ctx context.Context, provider *goose.Provider) error {
	current, target, err := provider.GetVersions(ctx)
	if err != nil {
		return fmt.Errorf("sqlite: determine migration versions: %w", err)
	}

	// current == 0 means nothing has ever been migrated on this database
	// — a brand-new install, or one that's just been rolled all the way
	// back to zero (as the up→down→up cycle tests do). Either way there
	// is no prior versioned state to protect, so there's nothing to back
	// up, and skipping avoids re-using the same "pre-<target>.bak" name
	// VACUUM INTO refuses to overwrite.
	if current == 0 {
		return nil
	}

	dir := filepath.Dir(db.path)
	backupPath := filepath.Join(dir, fmt.Sprintf("bodger.db.pre-%d.bak", target))

	if _, err := db.write.ExecContext(ctx, "VACUUM INTO ?", backupPath); err != nil {
		return fmt.Errorf("sqlite: back up database before migrating: %w", err)
	}
	return nil
}
