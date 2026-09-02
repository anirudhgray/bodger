// Package version holds build metadata for the bodger binary.
//
// The three variables below are the seam .goreleaser.yaml injects via
// -ldflags -X at release-build time. A plain `go build` or `go run` never
// sets them, so they keep these defaults — that's expected, not an error.
package version

import "fmt"

var (
	// Version is the release tag (e.g. "v1.0.0"), set by GoReleaser from
	// the pushed git tag. "dev" for a non-release build.
	Version = "dev"
	// Commit is the short git commit SHA the binary was built from.
	Commit = "none"
	// Date is the build timestamp (RFC3339), set by GoReleaser.
	Date = "unknown"
)

// String renders the build metadata for display, e.g. in `bodger --version`.
func String() string {
	if Version == "dev" {
		return Version
	}
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
