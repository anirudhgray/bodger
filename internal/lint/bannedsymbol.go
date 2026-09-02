package lint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// BannedSymbol is one call to a symbol that only its one designated home
// package may use — a wall clock or an environment variable read that
// bypassed the injected clock or layered config (ADR-0005: "time is
// injected, never read directly").
type BannedSymbol struct {
	File   string
	Line   int
	Call   string
	Reason string
}

func (b BannedSymbol) String() string {
	return fmt.Sprintf("%s:%d: %s — %s", b.File, b.Line, b.Call, b.Reason)
}

// bannedSymbolRule names one package-qualified call and the single
// directory (relative to the repository root) allowed to make it.
var bannedSymbolRules = []struct {
	pkg, fn string
	onlyIn  string
	reason  string
}{
	{
		pkg: "time", fn: "Now",
		onlyIn: "internal/platform/clock",
		reason: "time.Now() may only be called from internal/platform/clock — every other caller must take a Clock and ask it, so a frozen clock in a test controls every consumer",
	},
	{
		pkg: "os", fn: "Getenv",
		onlyIn: "internal/platform/config",
		reason: "os.Getenv may only be called from internal/platform/config — every other caller must go through the layered config it produces",
	},
}

// FindBannedSymbols walks every non-test Go file under root (the
// repository root) and reports each call to a symbol in
// bannedSymbolRules made outside that symbol's one designated package.
//
// Scoped to non-test files: internal/platform/clock's own tests
// legitimately compare against a real time.Now() reading in places, and
// nothing about what a test does to prove a package's behaviour is a
// build-time contract on production callers. testdata and nocompile
// directories are skipped for the same reason FindImportViolations skips
// them.
func FindBannedSymbols(root string) ([]BannedSymbol, error) {
	internalDir := filepath.Join(root, "internal")
	var found []BannedSymbol

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

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return fmt.Errorf("lint: parse %s: %w", path, err)
		}

		found = append(found, bannedSymbolsInFile(fset, file, rel, relDir)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// bannedSymbolsInFile checks one already-parsed file, given its
// repo-relative path and directory, against every rule whose designated
// home directory the file is not inside.
func bannedSymbolsInFile(fset *token.FileSet, file *ast.File, relFile, relDir string) []BannedSymbol {
	pkgNames := importedPackageNames(file)

	var found []BannedSymbol
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		importPath, ok := pkgNames[ident.Name]
		if !ok {
			return true
		}

		for _, rule := range bannedSymbolRules {
			if importPath != rule.pkg || sel.Sel.Name != rule.fn {
				continue
			}
			if relDir == rule.onlyIn || strings.HasPrefix(relDir, rule.onlyIn+"/") {
				continue
			}
			pos := fset.Position(call.Pos())
			found = append(found, BannedSymbol{
				File:   relFile,
				Line:   pos.Line,
				Call:   ident.Name + "." + sel.Sel.Name + "()",
				Reason: rule.reason,
			})
		}
		return true
	})
	return found
}

// importedPackageNames maps the local name of each of this file's
// imports to its import path, so an aliased import is still recognised.
func importedPackageNames(file *ast.File) map[string]string {
	names := make(map[string]string, len(file.Imports))
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		local := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			local = path[idx+1:]
		}
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				continue
			}
			local = imp.Name.Name
		}
		names[local] = path
	}
	return names
}
