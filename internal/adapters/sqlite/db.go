// Package sqlite is bodger's persistence adapter (ADR-0007): a
// modernc.org/sqlite (pure-Go, no cgo) implementation of the repository
// interfaces internal/ports declares. The application layer never sees a
// *sql.DB, never sees SQL, and never sees a driver-specific type — it
// depends only on internal/ports.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver

	"github.com/anirudhgray/bodger/internal/platform/clock"
)

// defaultMaxReadConns is the read pool's connection limit. SQLite's WAL
// mode allows any number of concurrent readers; this is a modest cap on
// bodger's own resource use, not a limit SQLite imposes.
const defaultMaxReadConns = 8

// DB is bodger's SQLite connection: separate write and read pools over one
// database file, both opened with the pragmas ADR-0007 specifies.
//
// Writes use a single connection (SetMaxOpenConns(1)) because SQLite
// permits one writer; reads use a separate pooled connection. This removes
// the SQLITE_BUSY class of bug rather than retrying around it.
type DB struct {
	path  string
	write *sql.DB
	read  *sql.DB
	clock clock.Clock
}

// dsn builds a modernc.org/sqlite connection string for path with the
// pragmas ADR-0007 requires: WAL journalling, a five-second busy timeout,
// foreign keys on, and synchronous=NORMAL (safe under WAL, and cheaper than
// FULL at this scale). These are per-connection settings in SQLite, so
// every connection either pool opens gets them applied identically via the
// DSN — there is no separate "run this PRAGMA on every new connection"
// hook to keep in sync with this string.
func dsn(path string) string {
	v := url.Values{}
	v.Set("_journal_mode", "WAL")
	v.Set("_busy_timeout", "5000")
	v.Set("_foreign_keys", "1")
	v.Set("_synchronous", "NORMAL")
	return path + "?" + v.Encode()
}

// Open opens path (created if it doesn't exist) with two connection pools:
// a single-connection writer and a pooled reader, both configured with
// ADR-0007's pragmas. clk stamps the created_at/updated_at audit columns
// this package's repositories write — it is never consulted for anything
// domain-visible (booked_date, "now" for business logic), which stays the
// application layer's job (ADR-0005).
//
// Open does not run migrations; call (*DB).MigrateUp for that once the
// pools are open.
func Open(clk clock.Clock, path string) (*DB, error) {
	if clk == nil {
		return nil, fmt.Errorf("sqlite: clock must not be nil")
	}
	if path == "" {
		return nil, fmt.Errorf("sqlite: path must not be empty")
	}

	write, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("sqlite: open write pool: %w", err)
	}
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)

	read, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		_ = write.Close()
		return nil, fmt.Errorf("sqlite: open read pool: %w", err)
	}
	read.SetMaxOpenConns(defaultMaxReadConns)
	read.SetMaxIdleConns(defaultMaxReadConns)

	if err := write.Ping(); err != nil {
		_ = write.Close()
		_ = read.Close()
		return nil, fmt.Errorf("sqlite: ping write pool: %w", err)
	}
	if err := read.Ping(); err != nil {
		_ = write.Close()
		_ = read.Close()
		return nil, fmt.Errorf("sqlite: ping read pool: %w", err)
	}

	return &DB{path: path, write: write, read: read, clock: clk}, nil
}

// Close closes both connection pools.
func (db *DB) Close() error {
	writeErr := db.write.Close()
	readErr := db.read.Close()
	if writeErr != nil {
		return writeErr
	}
	return readErr
}

// PingContext checks that both pools can reach the database.
func (db *DB) PingContext(ctx context.Context) error {
	if err := db.write.PingContext(ctx); err != nil {
		return err
	}
	return db.read.PingContext(ctx)
}
