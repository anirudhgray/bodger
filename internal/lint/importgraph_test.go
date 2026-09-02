package lint

import (
	"os"
	"path/filepath"
	"testing"
)

// TestImportGraph is the check itself: no file in internal/domain,
// internal/app, internal/adapters/*, or internal/surface/* may import a
// project-local package its layer doesn't permit (docs/architecture.md
// §2). A failure here means "just parse it in the handler" (or construct
// a Money outside the application layer, or reach into an adapter from a
// surface) has become possible again, which is exactly what ADR-0005
// says must not compile.
func TestImportGraph(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot(): %v", err)
	}
	found, err := FindImportViolations(root)
	if err != nil {
		t.Fatalf("FindImportViolations(%q): %v", root, err)
	}
	for _, v := range found {
		t.Error(v.String())
	}
}

func TestMatchImportRule(t *testing.T) {
	tests := []struct {
		dir     string
		wantDir string
		wantOK  bool
	}{
		{dir: "internal/domain", wantDir: "internal/domain", wantOK: true},
		{dir: "internal/domain/ledger", wantDir: "internal/domain", wantOK: true},
		{dir: "internal/app", wantDir: "internal/app", wantOK: true},
		{dir: "internal/app/normalize", wantDir: "internal/app", wantOK: true},
		{dir: "internal/adapters/sqlite", wantDir: "internal/adapters", wantOK: true},
		{dir: "internal/surface/cli", wantDir: "internal/surface", wantOK: true},
		{dir: "internal/surface/conformance", wantDir: "internal/surface", wantOK: true},
		// A directory that merely shares a prefix with a ruled one, but
		// not at a path segment boundary, must not match it.
		{dir: "internal/appendix", wantOK: false},
		{dir: "internal/ports", wantOK: false},
		{dir: "internal/platform/clock", wantOK: false},
		{dir: "cmd/bodger", wantOK: false},
	}
	for _, tt := range tests {
		rule, ok := matchImportRule(tt.dir)
		if ok != tt.wantOK {
			t.Errorf("matchImportRule(%q) ok = %v, want %v", tt.dir, ok, tt.wantOK)
			continue
		}
		if ok && rule.dir != tt.wantDir {
			t.Errorf("matchImportRule(%q) matched %q, want %q", tt.dir, rule.dir, tt.wantDir)
		}
	}
}

func TestImportAllowed(t *testing.T) {
	tests := []struct {
		name    string
		ownDir  string
		allowed []string
		path    string
		want    bool
	}{
		{name: "own package", ownDir: "internal/app", allowed: nil, path: "internal/app", want: true},
		{name: "own subtree", ownDir: "internal/app", allowed: nil, path: "internal/app/normalize", want: true},
		{name: "explicitly allowed", ownDir: "internal/app", allowed: []string{"internal/domain"}, path: "internal/domain/ledger", want: true},
		{name: "not allowed", ownDir: "internal/surface", allowed: []string{"internal/app", "internal/platform"}, path: "internal/domain/ledger", want: false},
		{name: "not allowed adapter from surface", ownDir: "internal/surface", allowed: []string{"internal/app", "internal/platform"}, path: "internal/adapters/sqlite", want: false},
		{name: "sibling package is not a prefix match", ownDir: "internal/app", allowed: nil, path: "internal/appendix", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := importAllowed(tt.ownDir, tt.allowed, tt.path); got != tt.want {
				t.Errorf("importAllowed(%q, %v, %q) = %v, want %v", tt.ownDir, tt.allowed, tt.path, got, tt.want)
			}
		})
	}
}

// TestImportGraph_DetectsViolations builds a throwaway directory tree
// outside the real repository and proves FindImportViolations actually
// fails the build shape it claims to: a domain package reaching outside
// its own subtree, and a surface package importing domain directly. This
// is the automated form of the issue #9 "done when" requirement to
// demonstrate each violation is caught — kept as a permanent test rather
// than a one-off manual check, so a future refactor of this checker is
// re-proven on every run instead of trusted from a comment.
func TestImportGraph_DetectsViolations(t *testing.T) {
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

	// A domain file reaching for the application layer — the modelled
	// violation of "internal/domain imports nothing project-local
	// outside its own subtree".
	write("internal/domain/reachout.go", `package domain

import "github.com/anirudhgray/bodger/internal/app"

var _ = app.Service{}
`)

	// A surface file importing domain directly — the modelled violation
	// issue #9 calls out as "the point": a handler that could otherwise
	// just parse a Money itself.
	write("internal/surface/http/handler.go", `package http

import "github.com/anirudhgray/bodger/internal/domain/money"

var _ = money.Money{}
`)

	// A surface file importing an adapter directly — the second half of
	// the surface rule.
	write("internal/surface/cli/cmd.go", `package cli

import "github.com/anirudhgray/bodger/internal/adapters/sqlite"

var _ = sqlite.DB{}
`)

	// A conforming file in each of those same layers, which must NOT be
	// flagged — proves the check doesn't just fail everything under a
	// ruled directory.
	write("internal/domain/money.go", `package domain

type Money struct{}
`)
	write("internal/app/service.go", `package app

import (
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/platform/clock"
)

var _ = domain.Money{}
var _ = clock.Clock(nil)
`)
	write("internal/surface/http/ok.go", `package http

import "github.com/anirudhgray/bodger/internal/app"

var _ = app.Service{}
`)

	found, err := FindImportViolations(root)
	if err != nil {
		t.Fatalf("FindImportViolations(%q): %v", root, err)
	}

	want := map[string]bool{
		"internal/domain/reachout.go":      false,
		"internal/surface/http/handler.go": false,
		"internal/surface/cli/cmd.go":      false,
	}
	for _, v := range found {
		if _, ok := want[v.File]; ok {
			want[v.File] = true
		} else {
			t.Errorf("unexpected violation reported: %s", v)
		}
	}
	for file, seen := range want {
		if !seen {
			t.Errorf("expected a violation in %s, found none", file)
		}
	}
}
