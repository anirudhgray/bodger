// Command bodger is the single bodger binary: serve, mcp, and every CLI
// subcommand (see "Where code goes" in the contributing guide).
//
// This file is a bootstrap placeholder, not the real entry point. The CLI
// and REST API surfaces have not landed yet; it exists only so
// `go build ./cmd/bodger` — and therefore `make build`, which CI runs —
// has something to compile now that internal/platform packages exist (the
// Makefile only builds Go targets once at least one .go file is present in
// the repository). Replace this file's body once real commands exist;
// nothing here is meant to survive that.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "bodger: not yet implemented. This binary has no commands yet.")
	os.Exit(1)
}
