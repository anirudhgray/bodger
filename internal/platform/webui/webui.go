// Package webui embeds bodger's web UI build output (issue #58) so
// `bodger serve` can serve it without a second process — ADR-0001's
// single-binary deployment shape, and docs/architecture.md §3's "Ships
// in: Static assets embedded in the binary" for the web UI surface.
//
// dist/ holds whatever `make build-web` last produced there:
// web/vite.config.ts points Vite's build.outDir at this package's dist/
// directory, so `cd web && npm run build` writes straight into it. Only
// dist/index.html is committed, as a placeholder — go:embed requires its
// pattern to match at least one file, so a Go-only checkout that never
// ran `make build-web` (`go build`, `go test`, `go vet` on their own)
// still compiles; it serves that placeholder page instead of a real UI
// until someone builds one. A real `npm run build` overwrites it with the
// real index.html; everything else dist/ ever contains — JS and CSS
// bundles, source maps — is untracked (see the repository root
// .gitignore).
//
// This package sits under internal/platform rather than
// internal/surface/http, its only caller, because internal/lint's
// import-graph check (issue #9, docs/architecture.md §6) only lets a
// surface import internal/app, internal/platform, and internal/ports —
// and embedded static assets have no more business logic in them than the
// clock or config do.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distFS embed.FS

// Dist returns the embedded build output rooted at dist/, so a caller
// serving it sees "index.html" and "assets/..." paths rather than
// "dist/index.html".
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Unreachable: "dist" is a literal directory this file's own
		// go:embed directive names, so fs.Sub can only fail here if that
		// embed itself failed — which would already be a build error, not
		// a runtime one.
		panic("internal/platform/webui: " + err.Error())
	}
	return sub
}
