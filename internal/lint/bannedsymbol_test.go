package lint

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBannedSymbols is the check itself: no file outside
// internal/platform/clock calls time.Now(), and no file outside
// internal/platform/config calls os.Getenv — ADR-0005's "time is
// injected, never read directly, and configuration is layered, never
// grabbed straight from the environment" as something the build fails on
// rather than a comment reviewers might miss.
func TestBannedSymbols(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot(): %v", err)
	}
	found, err := FindBannedSymbols(root)
	if err != nil {
		t.Fatalf("FindBannedSymbols(%q): %v", root, err)
	}
	for _, b := range found {
		t.Error(b.String())
	}
}

// TestBannedSymbols_DetectsViolations proves the checker actually catches
// a stray time.Now() in a handler and a stray os.Getenv in an adapter —
// the automated form of two of issue #9's four "done when" demonstration
// requirements (the other two are the import-graph violations covered by
// TestImportGraph_DetectsViolations).
func TestBannedSymbols_DetectsViolations(t *testing.T) {
	root := t.TempDir()

	write := func(relPath, content string) {
		full := filepath.Join(root, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", full, err)
		}
	}

	// A handler reading the wall clock directly instead of taking a
	// Clock — the "time.Now() in a handler" violation issue #9 names.
	write("internal/surface/http/handler.go", `package http

import "time"

func now() time.Time { return time.Now() }
`)

	// An adapter reading the environment directly instead of going
	// through layered config — the "os.Getenv in an adapter" violation.
	write("internal/adapters/sqlite/db.go", `package sqlite

import "os"

func dbPath() string { return os.Getenv("BODGER_DB_PATH") }
`)

	// Conforming files in each symbol's actual home, which must NOT be
	// flagged.
	write("internal/platform/clock/clock.go", `package clock

import "time"

func Now() time.Time { return time.Now() }
`)
	write("internal/platform/config/config.go", `package config

import "os"

func dbPath() string { return os.Getenv("BODGER_DB_PATH") }
`)

	found, err := FindBannedSymbols(root)
	if err != nil {
		t.Fatalf("FindBannedSymbols(%q): %v", root, err)
	}

	want := map[string]bool{
		"internal/surface/http/handler.go": false,
		"internal/adapters/sqlite/db.go":   false,
	}
	for _, b := range found {
		if _, ok := want[b.File]; ok {
			want[b.File] = true
		} else {
			t.Errorf("unexpected violation reported: %s", b)
		}
	}
	for file, seen := range want {
		if !seen {
			t.Errorf("expected a violation in %s, found none", file)
		}
	}
}
