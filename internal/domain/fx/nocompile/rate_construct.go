// Package nocompile holds a single file that must never compile. It
// exists to document — and let a future developer manually re-verify —
// the invariant that fx.Rate cannot be constructed from outside its
// package, because its fields (base, quote, value) are unexported. The
// only exported ways to produce a Rate are fx.NewRate, fx.IdentityRate,
// and fx.DeriveImpliedRate, all of which validate their inputs.
//
// This mirrors internal/domain/money/nocompile/money_construct.go's
// pattern: a deliberate compile-fail check rather than a runtime test,
// since there's no way to assert "this line doesn't compile" from within
// a test that itself must compile. The //go:build ignore tag below
// excludes this file from `go build ./...`, `go test ./...`,
// `go vet ./...`, and golangci-lint's default run, so it never breaks the
// build on its own.
//
// To manually re-verify the invariant still holds:
//
//  1. Remove the "//go:build ignore" line below.
//  2. Run: go build ./internal/domain/fx/nocompile/...
//  3. Confirm it fails with:
//       cannot refer to unexported field base in struct literal of type fx.Rate
//       cannot refer to unexported field quote in struct literal of type fx.Rate
//       cannot refer to unexported field value in struct literal of type fx.Rate
//  4. Restore the "//go:build ignore" line.
//
// Last manually verified: 2026-09-06, confirmed the build fails with
// exactly those three "cannot refer to unexported field" errors, then
// restored the tag.
//
//go:build ignore

package nocompile

import (
	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain/fx"
)

// This struct literal must fail to compile: base, quote, and value are
// unexported outside the fx package.
var _ = fx.Rate{base: "USD", quote: "INR", value: decimal.NewFromInt(1)}
