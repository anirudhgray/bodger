// Package nocompile holds a single file that must never compile. It exists
// to document — and let a future developer manually re-verify — the
// invariant that money.Money cannot be constructed from outside its
// package, because its fields (amountMinor, currency) are unexported. The
// only exported way to produce a Money is money.NewMoney, which validates
// the currency code.
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
//  2. Run: go build ./internal/domain/money/nocompile/...
//  3. Confirm it fails with:
//       cannot refer to unexported field amountMinor in struct literal of type money.Money
//       cannot refer to unexported field currency in struct literal of type money.Money
//  4. Restore the "//go:build ignore" line.
//
// Last manually verified: 2026-08-31, confirmed the build fails with
// exactly those two "cannot refer to unexported field" errors, then
// restored the tag.
//
//go:build ignore

package nocompile

import "github.com/anirudhgray/bodger/internal/domain/money"

// This struct literal must fail to compile: amountMinor and currency are
// unexported outside the money package.
var _ = money.Money{amountMinor: 100, currency: "USD"}
