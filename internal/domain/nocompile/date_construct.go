// Package nocompile holds a single file that must never compile. It exists
// to document — and let a future developer manually re-verify — the
// invariant that domain.Date cannot be constructed from outside its
// package, because its fields (year, month, day) are unexported. The only
// exported way to produce a Date is domain.NewDate, which validates the
// calendar date.
//
// This is a deliberate compile-fail check rather than a runtime test:
// there's no way to assert "this line doesn't compile" from within a test
// that itself must compile. The //go:build ignore tag below excludes this
// file from `go build ./...`, `go test ./...`, `go vet ./...`, and
// golangci-lint's default run, so it never breaks the build on its own.
//
// To manually re-verify the invariant still holds:
//
//  1. Remove the "//go:build ignore" line below.
//  2. Run: go build ./internal/domain/nocompile/...
//  3. Confirm it fails with:
//       cannot refer to unexported field year in struct literal of type domain.Date
//       cannot refer to unexported field month in struct literal of type domain.Date
//       cannot refer to unexported field day in struct literal of type domain.Date
//  4. Restore the "//go:build ignore" line.
//
// Last manually verified: 2026-08-31, confirmed the build fails with
// exactly those "cannot refer to unexported field" errors, then restored
// the tag.
//
//go:build ignore

package nocompile

import (
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
)

// This struct literal must fail to compile: year, month, and day are
// unexported outside the domain package.
var _ = domain.Date{year: 2026, month: time.August, day: 14}
