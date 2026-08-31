package errs_test

import (
	"regexp"
	"testing"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// shipped pins the exact set of coarse codes this repository has ever
// shipped: wire string, HTTP status, and CLI exit code, per ADR-0011's
// mapping table. It is referenced entirely through the exported Go
// constants, so deleting or renaming a constant is a compile error here —
// and TestRegistry_ShippedCodes below checks the registry still has an
// entry for every one of them, with the exact wire string and mapping, so
// a registry edit that drifts from this list fails at test time instead of
// silently. Codes are a public contract (ADR-0011): scripts branch on CLI
// exit codes, agents branch on MCP error codes. This test is what makes
// changing one a deliberate edit in two places rather than a refactor in
// one.
var shipped = []struct {
	code errs.Code
	wire string
	http int
	cli  int
}{
	{errs.InvalidInput, "invalid_input", 422, 2},
	{errs.NotFound, "not_found", 404, 3},
	{errs.Conflict, "conflict", 409, 4},
	{errs.PreconditionFailed, "precondition_failed", 412, 5},
	{errs.NotAllowed, "not_allowed", 403, 6},
	{errs.Unauthenticated, "unauthenticated", 401, 7},
	{errs.Unavailable, "unavailable", 503, 8},
	{errs.Internal, "internal", 500, 1},
}

func TestRegistry_ShippedCodes(t *testing.T) {
	if got, want := len(errs.Codes()), len(shipped); got != want {
		t.Fatalf("errs.Codes() has %d entries, want %d — a code was added to "+
			"the registry without being pinned in this test, or removed "+
			"without this test being updated", got, want)
	}

	seenMessages := make(map[string]errs.Code, len(shipped))

	for _, row := range shipped {
		t.Run(row.wire, func(t *testing.T) {
			if string(row.code) != row.wire {
				t.Errorf("code constant = %q, want %q", row.code, row.wire)
			}
			if got := errs.HTTPStatus(row.code); got != row.http {
				t.Errorf("HTTPStatus(%s) = %d, want %d", row.wire, got, row.http)
			}
			if got := errs.CLIExitCode(row.code); got != row.cli {
				t.Errorf("CLIExitCode(%s) = %d, want %d", row.wire, got, row.cli)
			}

			msg := errs.DefaultMessage(row.code)
			if msg == "" {
				t.Fatalf("code %s has an empty default message", row.wire)
			}
			if dup, ok := seenMessages[msg]; ok {
				t.Errorf("code %s has the same default message as %s: %q", row.wire, dup, msg)
			}
			seenMessages[msg] = row.code
		})
	}
}

// bannedTerms mirrors the "Never say" column of ux-principles.md §2's
// vocabulary table. It is duplicated here deliberately (that document has
// no automated check yet — see issue #11 — this test covers only the
// registry's own default messages) rather than parsed from the doc, so a
// doc-formatting change can't silently disable the check.
var bannedTerms = []string{
	"posting", "entry line", "journal entry", "journal", "ledger",
	"debit", "credit", "double-entry", "balanced entry",
	"chart of accounts", "accounting period", "fiscal period",
	"reconcile", "tx", "txn", "crud", "upsert", "soft delete",
	"null", "nil", "undefined", "enum", "enum value",
}

func TestRegistry_MessagesContainNoBannedVocabulary(t *testing.T) {
	for _, row := range shipped {
		msg := errs.DefaultMessage(row.code)
		for _, term := range bannedTerms {
			pattern := `(?i)\b` + regexp.QuoteMeta(term) + `\b`
			if regexp.MustCompile(pattern).MatchString(msg) {
				t.Errorf("code %s default message contains banned term %q: %q", row.wire, term, msg)
			}
		}
	}
}
