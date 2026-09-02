package lint

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// modulePrefix is this repository's module path, exactly as declared in
// go.mod, with a trailing slash so it can be stripped from an import path
// to leave the internal/... path the rules below are expressed against.
const modulePrefix = "github.com/anirudhgray/bodger/"

// ImportViolation is one project-local import that its file's layer is not
// permitted to reach, per docs/architecture.md §2's layer table.
type ImportViolation struct {
	File    string
	Package string
	Import  string
	Reason  string
}

func (v ImportViolation) String() string {
	return fmt.Sprintf("%s: imports %q — %s", v.File, v.Import, v.Reason)
}

// importRule pairs a directory (relative to the repository root,
// forward-slash separated, no trailing slash) with the internal/...
// prefixes a file under it may import, in addition to its own subtree —
// every layer may always import deeper into itself (internal/app
// importing internal/app/normalize, for instance), so that is never
// listed explicitly below.
//
// This is docs/architecture.md §2 and issue #9's table, with one addition
// the table's simplified form omits: internal/surface may also import
// internal/ports, because both surfaces already do, for
// ports.SeededUserID — the single M1 user's fixed ID (ADR-0006), used
// directly since there is no authentication yet. ports carries no
// domain-construction capability (no exported constructor, nothing but
// interfaces and this one constant), so this doesn't reopen "just parse
// it in the handler" — it would if a surface could reach a domain or
// money constructor, and this rule still stops that. internal/platform is
// not a row in that table either, so this check has no opinion on what it
// imports.
var importRules = []struct {
	dir     string
	allowed []string
	reason  string
}{
	{
		dir:     "internal/domain",
		allowed: nil,
		reason:  "internal/domain may import nothing project-local outside its own subtree — it is the pure financial model",
	},
	{
		dir:     "internal/app",
		allowed: []string{"internal/domain", "internal/ports", "internal/platform"},
		reason:  "internal/app may only import domain, ports, and platform",
	},
	{
		dir:     "internal/adapters",
		allowed: []string{"internal/ports", "internal/domain", "internal/platform"},
		reason:  "internal/adapters may only import ports, domain, and platform",
	},
	{
		dir:     "internal/surface",
		allowed: []string{"internal/app", "internal/platform", "internal/ports"},
		reason:  "internal/surface may only import app, platform, and ports — never domain directly, and never an adapter",
	},
}

// FindImportViolations walks every non-test Go file under root (the
// repository root) and reports each project-local import that its file's
// layer rule (importRules) does not permit.
//
// Scoped to non-test files for the same reason FindBareErrors is: a test
// file legitimately wires a real adapter and clock to drive a surface
// end to end (internal/surface/http/http_test.go, for instance,
// constructs a SQLite-backed Service), which the production code in the
// same package must never do itself. testdata and nocompile directories
// are skipped outright — nocompile holds files excluded from the real
// build by a //go:build ignore tag (docs/contributing.md's "proving a
// validating constructor" pattern) and would otherwise be misread as
// production code by a parser that doesn't evaluate build constraints.
func FindImportViolations(root string) ([]ImportViolation, error) {
	internalDir := filepath.Join(root, "internal")
	var found []ImportViolation

	err := filepath.WalkDir(internalDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "nocompile" {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relDir := filepath.ToSlash(filepath.Dir(rel))

		rule, ok := matchImportRule(relDir)
		if !ok {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("lint: parse %s: %w", path, err)
		}

		for _, imp := range file.Imports {
			importPath, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			if !strings.HasPrefix(importPath, modulePrefix) {
				continue // not project-local
			}
			localPath := strings.TrimPrefix(importPath, modulePrefix)
			if importAllowed(rule.dir, rule.allowed, localPath) {
				continue
			}
			found = append(found, ImportViolation{
				File:    rel,
				Package: relDir,
				Import:  importPath,
				Reason:  rule.reason,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// matchImportRule finds the rule whose directory is a prefix of dir (a
// path segment boundary, not merely a string prefix, so "internal/appx"
// never matches the "internal/app" rule).
func matchImportRule(dir string) (rule struct {
	dir     string
	allowed []string
	reason  string
}, ok bool) {
	for _, r := range importRules {
		if dir == r.dir || strings.HasPrefix(dir, r.dir+"/") {
			return r, true
		}
	}
	return rule, false
}

// importAllowed reports whether localPath (an internal/... import path
// with the module prefix already stripped) is one a file in ownDir may
// use: anything within ownDir's own subtree, or anything under one of the
// explicitly allowed prefixes.
func importAllowed(ownDir string, allowed []string, localPath string) bool {
	if localPath == ownDir || strings.HasPrefix(localPath, ownDir+"/") {
		return true
	}
	for _, a := range allowed {
		if localPath == a || strings.HasPrefix(localPath, a+"/") {
			return true
		}
	}
	return false
}
