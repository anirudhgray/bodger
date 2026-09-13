// Command erdgen builds a scratch SQLite database with every migration
// applied, for tbls to introspect when (re)generating docs/schema's ER
// diagram — see docs/contributing.md's "Regenerating the schema ERD".
//
// It exists so `make erd`/`make check-erd` can produce a real, fully
// migrated database without running the bodger server: it reuses
// internal/adapters/sqlite's own embedded goose migrations rather than
// re-implementing schema setup. It is dev tooling invoked by the
// Makefile, not part of the bodger CLI surface — it does not register
// with internal/surface/cli.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/anirudhgray/bodger/internal/adapters/sqlite"
	"github.com/anirudhgray/bodger/internal/platform/clock"
)

func main() {
	path := flag.String("db", "", "path to write the scratch, fully-migrated database to (required)")
	flag.Parse()

	if *path == "" {
		fmt.Fprintln(os.Stderr, "erdgen: -db is required")
		os.Exit(1)
	}
	if err := run(*path); err != nil {
		fmt.Fprintf(os.Stderr, "erdgen: %v\n", err)
		os.Exit(1)
	}
}

func run(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing existing scratch database: %w", err)
	}

	db, err := sqlite.Open(clock.New(), path)
	if err != nil {
		return fmt.Errorf("opening scratch database: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.MigrateUp(context.Background()); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}
