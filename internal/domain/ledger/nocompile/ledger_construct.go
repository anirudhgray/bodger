// Package nocompile holds a single file that must never compile. It exists
// to document — and let a future developer manually re-verify — the
// invariant that ledger.Account, ledger.Category, ledger.Tag,
// ledger.Posting, and ledger.Transaction cannot be constructed from
// outside their package, because every one of their fields is unexported.
// The only exported way to produce each is its NewXxx constructor, which
// validates the type's invariants.
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
//  2. Run: go build ./internal/domain/ledger/nocompile/...
//  3. Confirm it fails with a "cannot refer to unexported field" error for
//     every field referenced below.
//  4. Restore the "//go:build ignore" line.
//
// Last manually verified: 2026-09-01, confirmed the build fails with
// "cannot refer to unexported field" errors for every field referenced
// below, then restored the tag.
//
//go:build ignore

package nocompile

import "github.com/anirudhgray/bodger/internal/domain/ledger"

// None of these struct literals must compile: every referenced field is
// unexported outside the ledger package.
var (
	_ = ledger.Account{id: "a", userID: "u", name: "n", kind: ledger.AccountKindBank, archivedAt: nil}
	_ = ledger.Category{id: "c", userID: "u", name: "n", kind: ledger.CategoryKindExpense, archivedAt: nil}
	_ = ledger.Tag{value: "x"}
	_ = ledger.Posting{id: "p", accountID: "a", sortOrder: 0}
	_ = ledger.Transaction{id: "t", userID: "u", description: "d"}
)
