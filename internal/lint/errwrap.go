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

// wrapMethod is the method name that makes a fmt.Errorf or errors.New call
// legitimate: *errs.Error.Wrap(cause error) takes the internal cause that
// is logged in full and never serialised (ADR-0011). Anything constructed
// inside a Wrap(...) call is a cause by definition; anything constructed
// outside one and returned is an error with no code, no user-safe message,
// and no surface mapping.
//
// The match is on the method name alone, not on the receiver's type. That
// is deliberate: the receiver of a Wrap call is normally a chain
// (errs.New(...).Explain(...)) whose type only a full type-checking pass
// could resolve, and this check is intentionally a parse, not a
// go/analysis pass (issue #12). The cost is that a Wrap method on some
// other type would also silence the check — a trade worth making until it
// actually happens.
const wrapMethod = "Wrap"

// BareError is one flagged call site: a fmt.Errorf or errors.New whose
// result is not being handed to Wrap, and so escapes the package
// unclassified.
type BareError struct {
	// File is the path as passed to the parser, so it reads back as a
	// location relative to whatever root was scanned.
	File string
	Line int
	// Call is the source spelling of the offending call's function, e.g.
	// "fmt.Errorf".
	Call string
}

func (b BareError) String() string {
	return fmt.Sprintf("%s:%d: %s", b.File, b.Line, b.Call)
}

// FindBareErrors parses every non-test Go file in the tree rooted at root
// and returns each fmt.Errorf or errors.New call that is not nested inside
// a Wrap(...) call.
//
// The check is scoped by what the caller points it at, and nothing else:
// internal/domain returns plain sentinel-wrapping errors on purpose, and an
// adapter's errors are meant to become a Wrap cause, so only internal/app
// is ever passed here (see errwrap_test.go).
//
// Known limitation: a dot-import of "fmt" or "errors" would make the
// offending calls unqualified and invisible to this check. Nothing in the
// repository dot-imports anything, goimports is not going to write one, and
// closing the hole properly needs the type information this check
// deliberately does without.
func FindBareErrors(root string) ([]BareError, error) {
	var found []BareError

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// testdata is the conventional home for deliberately broken
			// or illustrative source, which must not fail a real build.
			if d.Name() == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return fmt.Errorf("lint: parse %s: %w", path, err)
		}
		found = append(found, bareErrors(fset, file)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// bareErrors walks one parsed file, carrying a "we are inside a Wrap call's
// arguments" flag down the tree. Everything nested inside those arguments
// is a cause — including a fmt.Errorf that itself wraps a driver error with
// %w — so the whole subtree is exempt, not just the argument expression
// itself.
func bareErrors(fset *token.FileSet, file *ast.File) []BareError {
	pkgs := errorPackageNames(file)
	if len(pkgs) == 0 {
		return nil
	}

	var found []BareError

	var visit func(n ast.Node, inWrap bool)
	visit = func(n ast.Node, inWrap bool) {
		ast.Inspect(n, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isWrapCall(call) {
				// Only the arguments are inside Wrap's parentheses. The
				// receiver chain it hangs off — errs.New(...).Explain(...)
				// — is not, so it keeps the flag it arrived with.
				visit(call.Fun, inWrap)
				for _, arg := range call.Args {
					visit(arg, true)
				}
				return false
			}
			if !inWrap {
				if name, ok := errorConstructor(call, pkgs); ok {
					pos := fset.Position(call.Pos())
					found = append(found, BareError{File: pos.Filename, Line: pos.Line, Call: name})
				}
			}
			return true
		})
	}
	visit(file, false)

	return found
}

// errorPackageNames maps the local name of each "fmt" and "errors" import
// to its path, so an aliased import is still recognised and a same-named
// import of something else is not.
func errorPackageNames(file *ast.File) map[string]string {
	names := make(map[string]string, 2)
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if path != "fmt" && path != "errors" {
			continue
		}
		local := path
		if imp.Name != nil {
			// A blank or dot import gives no qualifier to match on.
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				continue
			}
			local = imp.Name.Name
		}
		names[local] = path
	}
	return names
}

func isWrapCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == wrapMethod
}

// errorConstructor reports whether call is fmt.Errorf or errors.New, and
// returns its source spelling for the failure message.
func errorConstructor(call *ast.CallExpr, pkgs map[string]string) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	switch pkgs[ident.Name] {
	case "fmt":
		if sel.Sel.Name != "Errorf" {
			return "", false
		}
	case "errors":
		if sel.Sel.Name != "New" {
			return "", false
		}
	default:
		return "", false
	}
	return ident.Name + "." + sel.Sel.Name, true
}
