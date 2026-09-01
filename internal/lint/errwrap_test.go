package lint

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// appDir is the one tree this rule applies to. internal/domain returns
// plain errors wrapping its own sentinels by design, and an adapter's
// errors exist precisely to become an *errs.Error's cause — pointing the
// check at either would be wrong, not merely noisy.
const appDir = "../app"

// TestApp_ReturnsRegistryErrors is the check itself: no exported
// application-layer method may return an error that isn't one of the
// registry's (ADR-0011). A fmt.Errorf that escapes internal/app reaches a
// surface with no code, so no HTTP status and no exit code, and quite
// possibly a driver message rendered straight to the user.
//
// The fix for a failure here is never to delete the error — it is to give
// it a code: errs.New(errs.<code>).Explain(...) for something the user did,
// with .Wrap(err) when there is an underlying cause worth logging.
func TestApp_ReturnsRegistryErrors(t *testing.T) {
	found, err := FindBareErrors(appDir)
	if err != nil {
		t.Fatalf("FindBareErrors(%q): %v", appDir, err)
	}
	for _, b := range found {
		t.Errorf("%s: returns a bare error — build it with errs.New(...), or pass it to .Wrap(...) as the cause of one", b)
	}
}

func TestBareErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "bare fmt.Errorf is flagged",
			src: `package p
import "fmt"
func f() error { return fmt.Errorf("app: nope") }`,
			want: []string{"fmt.Errorf"},
		},
		{
			name: "bare errors.New is flagged",
			src: `package p
import "errors"
func f() error { return errors.New("app: nope") }`,
			want: []string{"errors.New"},
		},
		{
			name: "assigned but never wrapped is flagged",
			src: `package p
import "fmt"
func f() error {
	err := fmt.Errorf("app: nope")
	return err
}`,
			want: []string{"fmt.Errorf"},
		},
		{
			name: "aliased import is flagged",
			src: `package p
import xfmt "fmt"
func f() error { return xfmt.Errorf("app: nope") }`,
			want: []string{"xfmt.Errorf"},
		},
		{
			name: "inside Wrap is allowed",
			src: `package p
import "fmt"
func f() error {
	return errs.New(errs.Internal).Wrap(fmt.Errorf("app: %w", errSome))
}`,
			want: nil,
		},
		{
			name: "inside a multi-line Wrap is allowed",
			src: `package p
import "fmt"
func f() error {
	return errs.New(errs.Internal).
		Explain("Something went wrong.").
		Wrap(
			fmt.Errorf("app: %w", errSome),
		)
}`,
			want: nil,
		},
		{
			name: "nested inside a Wrap argument is allowed",
			src: `package p
import (
	"errors"
	"fmt"
)
func f() error {
	return errs.New(errs.Internal).Wrap(fmt.Errorf("app: %w", errors.New("inner")))
}`,
			want: nil,
		},
		{
			name: "Wrap's own receiver chain is still checked",
			src: `package p
import "fmt"
func f() error {
	return errs.New(errs.Internal).Explain(fmt.Errorf("app: nope").Error()).Wrap(cause)
}`,
			want: []string{"fmt.Errorf"},
		},
		{
			name: "a same-named method on another package is not fmt.Errorf",
			src: `package p
import "fmt"
func f() error { return log.Errorf("app: nope") }
var _ = fmt.Sprintf`,
			want: nil,
		},
		{
			name: "unimported qualifier is ignored",
			src: `package p
func f() error { return fmt.Errorf("app: nope") }`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "snippet.go", tt.src, 0)
			if err != nil {
				t.Fatalf("parsing the snippet failed: %v", err)
			}

			found := bareErrors(fset, file)
			if len(found) != len(tt.want) {
				t.Fatalf("bareErrors found %v, want %v", calls(found), tt.want)
			}
			for i, want := range tt.want {
				if found[i].Call != want {
					t.Errorf("finding %d = %q, want %q", i, found[i].Call, want)
				}
			}
		})
	}
}

// TestFindBareErrors_Scope pins the two ways this check is bounded: it
// reads only the tree it is given (which is what leaves internal/domain and
// the adapters alone — their errors are meant to be plain, or to become a
// Wrap cause), and it ignores _test.go files, where a fmt.Errorf standing
// in for a repository failure is exactly the right thing to write.
func TestFindBareErrors_Scope(t *testing.T) {
	root := t.TempDir()
	scanned := filepath.Join(root, "scanned")
	ignored := filepath.Join(root, "ignored")
	for _, dir := range []string{scanned, ignored} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, err)
		}
	}

	src := `package p
import "fmt"
func f() error { return fmt.Errorf("bare") }`

	write := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}
	write(filepath.Join(scanned, "a.go"))
	write(filepath.Join(scanned, "a_test.go"))
	write(filepath.Join(ignored, "b.go"))

	found, err := FindBareErrors(scanned)
	if err != nil {
		t.Fatalf("FindBareErrors: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("FindBareErrors found %v, want exactly the one non-test file under the scanned directory", calls(found))
	}
	if got := filepath.Base(found[0].File); got != "a.go" {
		t.Errorf("flagged %q, want a.go — _test.go files and directories outside the root are out of scope", got)
	}
}

func calls(found []BareError) []string {
	out := make([]string, len(found))
	for i, b := range found {
		out[i] = b.String()
	}
	return out
}
