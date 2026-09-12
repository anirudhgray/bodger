// Package nocompile holds a single file that must never compile. It exists
// to document — and let a future developer manually re-verify — the
// invariant that importing.ImportBatch, importing.ImportRecord, and
// importing.DuplicateMatch cannot be constructed from outside their
// package, because every one of their fields is unexported. The only
// exported ways to produce each are NewImportBatch, NewImportRecord, and
// NewDuplicateMatch, all of which validate their inputs.
//
// This mirrors internal/domain/ledger/nocompile's pattern: a deliberate
// compile-fail check rather than a runtime test, since there's no way to
// assert "this line doesn't compile" from within a test that itself must
// compile. The //go:build ignore tag below excludes this file from
// `go build ./...`, `go test ./...`, `go vet ./...`, and golangci-lint's
// default run, so it never breaks the build on its own.
//
// To manually re-verify the invariant still holds:
//
//  1. Remove the "//go:build ignore" line below.
//  2. Run: go build ./internal/domain/importing/nocompile/...
//  3. Confirm it fails with a "cannot refer to unexported field" error for
//     every field referenced below.
//  4. Restore the "//go:build ignore" line.
//
// Last manually verified: 2026-09-12, confirmed the build fails with
// "cannot refer to unexported field" errors for every field referenced
// below, then restored the tag.
//
//go:build ignore

package nocompile

import "github.com/anirudhgray/bodger/internal/domain/importing"

// None of these struct literals must compile: every referenced field is
// unexported outside the importing package.
var (
	_ = importing.ImportBatch{id: "b", userID: "u", status: importing.ImportBatchStatusStaged}
	_ = importing.ImportRecord{id: "r", userID: "u", importBatchID: "b", status: importing.ImportRecordStatusPending}
	_ = importing.DuplicateMatch{tier: importing.DuplicateMatchTierExact, matchedTransactionID: "t"}
)
