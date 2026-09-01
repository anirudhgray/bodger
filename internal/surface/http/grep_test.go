package http_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoWallClock pins one of issue #8's "done when" items directly: a
// recursive grep for the wall-clock call this package must never make,
// under internal/surface/http/, must return nothing.
// docs/architecture.md §2 says a surface may never ask what time it is —
// only internal/app (via the injected clock) resolves "now" — and this
// package has no general import-graph/banned-symbol check yet (that's
// issue #9's job, see internal/app/service_test.go's
// TestService_UsesInjectedClockNotWallClock doc comment for the same
// deferral). Until that lands, this package enforces its own half of the
// rule so the check is mechanical rather than a promise in a doc comment.
//
// wallClockCall is built by concatenation rather than written as one
// literal, deliberately: a literal occurrence of it in this very file
// (in this comment, in the error message, anywhere) would make this test
// fail against itself the same way a real `grep -rn` over this directory
// would.
func TestNoWallClock(t *testing.T) {
	wallClockCall := "time." + "Now()"

	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", path, err)
		}
		if strings.Contains(string(data), wallClockCall) {
			t.Errorf("%s reads the wall clock directly — surfaces never decide what \"now\" is; "+
				"internal/app/normalize.DateOf resolves it through the injected clock instead", path)
		}
	}
}
