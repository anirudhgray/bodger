// Command bodger is the single bodger binary: serve, mcp, and all CLI
// commands (see docs/architecture.md).
//
// This is a placeholder entry point. `make build` needs something under
// cmd/bodger to compile from the moment any Go code lands in the repo, and
// this issue (#1, domain value objects) is that first Go code — the real
// commands land with the CLI (#7) and REST API (#8) surfaces. Until then
// this prints a short notice rather than doing nothing silently.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "bodger: not yet implemented — see https://github.com/anirudhgray/bodger/issues/7 and /issues/8")
	os.Exit(1)
}
