// Command genopenapi regenerates internal/surface/http/openapi.json from
// internal/surface/http's routeTable and DTO structs (issue #36). It's
// wired to internal/surface/http's //go:generate directive
// (openapi_gen.go) and run via `go generate ./...` — see
// docs/contributing.md. It never starts a server or opens a database:
// the whole document is built by reflecting over Go types and walking
// the route registry, entirely in memory.
package main

import (
	"fmt"
	"os"

	httpsurface "github.com/anirudhgray/bodger/internal/surface/http"
)

func main() {
	if err := httpsurface.WriteOpenAPIDocument(); err != nil {
		fmt.Fprintln(os.Stderr, "genopenapi:", err)
		os.Exit(1)
	}
}
