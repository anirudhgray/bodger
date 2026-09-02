// Package webui embeds bodger's web UI build output (issue #58) so
// `bodger serve` can serve it without a second process — ADR-0001's
// single-binary deployment shape, and docs/architecture.md §3's "Ships
// in: Static assets embedded in the binary" for the web UI surface.
//
// dist/ holds whatever `make build-web` last produced there:
// web/vite.config.ts points Vite's build.outDir at this package's dist/
// directory, so `cd web && npm run build` writes straight into it, always
// starting from a clean slate (build.emptyOutDir is deliberately false;
// `make build-web` clears everything under dist/ except .gitkeep itself
// before invoking Vite — see the Makefile). Nothing under dist/ is
// tracked except .gitkeep, an empty marker that exists solely so the
// embed directive below has something to match on a fresh checkout that
// has never run `make build-web` — a real build is otherwise free to empty
// and rewrite dist/ without ever touching a tracked file (the entire
// point: this used to be dist/index.html itself, which made every local
// `make build` show up as a locally modified tracked file).
//
// placeholder/ is the fallback page Dist() serves instead, for exactly
// that never-built-yet case: a small always-tracked page, entirely
// outside the directory Vite owns, so it can never collide with a real
// build's output.
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

// all: includes dist/.gitkeep itself — go:embed's directory walk
// otherwise excludes dot-prefixed entries, and .gitkeep existing on disk
// is the whole reason dist matches anything before a real build has run.
//
//go:embed all:dist
var distFS embed.FS

//go:embed placeholder
var placeholderFS embed.FS

// Dist returns what `bodger serve` should serve for the web UI: the real
// build output rooted at dist/ if `make build-web` has run, or the
// placeholder page otherwise. Either way the caller sees "index.html" and
// "assets/..." paths, never a "dist/" or "placeholder/" prefix.
func Dist() fs.FS {
	dist, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Unreachable: "dist" is a literal directory this file's own
		// go:embed directive names, so fs.Sub can only fail here if that
		// embed itself failed — which would already be a build error, not
		// a runtime one.
		panic("internal/platform/webui: " + err.Error())
	}
	placeholder, err := fs.Sub(placeholderFS, "placeholder")
	if err != nil {
		// Unreachable, same reasoning as above for "placeholder".
		panic("internal/platform/webui: " + err.Error())
	}
	return chooseFS(dist, placeholder)
}

// chooseFS picks dist if a real `make build-web` has run — detected by
// dist/index.html actually existing, since .gitkeep alone doesn't count —
// or placeholder otherwise. Split out from Dist so this decision is
// testable against fake filesystems (webui_test.go) rather than only
// against whatever this package happens to have embedded at build time.
func chooseFS(dist, placeholder fs.FS) fs.FS {
	if _, err := fs.Stat(dist, "index.html"); err == nil {
		return dist
	}
	return placeholder
}
