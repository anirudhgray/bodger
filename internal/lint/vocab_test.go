package lint

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUserFacingVocabulary is the check itself: no banned term from
// docs/ux-principles.md §2 appears in a string this repo shows a person.
//
// It covers the two surfaces issue #11 scopes it to — the error registry's
// default messages, and cobra help and flag usage text — because those are
// the two with the least ambiguity about what "user-facing" means. Tone,
// phrasing, and everything else §2 asks for stay a review responsibility
// (ux-principles.md §8 is honest that they are not mechanisable).
func TestUserFacingVocabulary(t *testing.T) {
	root, err := vocabRepoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	allow, err := loadVocabAllowlist(vocabAllowlistPath)
	if err != nil {
		t.Fatalf("load allowlist: %v", err)
	}

	messages, err := collectRegistryMessages(root)
	if err != nil {
		t.Fatalf("read the error registry: %v", err)
	}
	cobraStrings, err := collectCobraStrings(root)
	if err != nil {
		t.Fatalf("read cobra help strings: %v", err)
	}

	// A check that silently stops finding anything is worse than no
	// check. If a refactor moves the registry's messages or changes how
	// commands are declared, fail here rather than passing on an empty
	// scan.
	if len(messages) == 0 {
		t.Error("found no default messages in internal/platform/errs — the extractor needs updating, not deleting")
	}
	if len(cobraStrings) == 0 {
		t.Error("found no cobra help or flag usage strings — the extractor needs updating, not deleting")
	}

	for _, v := range findVocabViolations(append(messages, cobraStrings...), allow) {
		t.Error(v.String())
	}
}

// TestBannedTermMatching pins the word-boundary rules, which are the whole
// difference between a check people trust and one they learn to work
// around.
func TestBannedTermMatching(t *testing.T) {
	tests := []struct {
		name  string
		term  string
		value string
		want  bool
	}{
		{"plain use", "ledger", "bodger is a personal finance ledger.", true},
		{"case insensitive", "ledger", "Your Ledger", true},
		{"plural", "posting", "two postings were written", true},
		{"past tense", "debit", "the account was debited", true},
		{"phrase", "chart of accounts", "open your chart of accounts", true},
		{"hyphenated term", "double-entry", "this uses double-entry bookkeeping", true},
		{"bare abbreviation", "tx", "record a tx", true},

		// The account type is a real, unrelated use of the substring.
		{"underscore compound", "credit", "account type: bank, cash, credit_card, wallet, investment, loan, or other (required)", false},
		{"longer word", "enum", "enumerate what you have", false},
		{"substring of another word", "nil", "the river Nile", false},
		{"prefix of another word", "credit", "creditor list", false},
		{"unrelated text", "reconcile", "check this against your statement", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := newBannedTerm(tc.term, "").pattern.MatchString(tc.value)
			if got != tc.want {
				t.Errorf("term %q against %q: got %v, want %v", tc.term, tc.value, got, tc.want)
			}
		})
	}
}

// bannedTermsIn runs the whole check over one synthetic Go file and
// returns the terms it flagged, so the tests below can assert on what the
// extractor does and does not consider user-facing.
func bannedTermsIn(t *testing.T, source string) []string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "surface.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	strs, err := collectCobraStrings(dir)
	if err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	var terms []string
	for _, v := range findVocabViolations(strs, vocabAllowlist{}) {
		terms = append(terms, v.term.term)
	}
	return terms
}

// TestVocabularyCheckIgnoresCode is the other half of the boundary §2
// draws: "internal code uses the precise term — `Posting` is a `Posting`
// in internal/domain. The boundary is the string a person reads." Package
// names, identifiers, comments, and struct tags are code, and a check that
// flagged them would be one nobody could keep.
func TestVocabularyCheckIgnoresCode(t *testing.T) {
	const source = `package ledger

import "github.com/spf13/cobra"

// newSpendCmd builds "spend": a single-posting outflow. Every journal
// entry it writes is balanced, and nothing here is a soft delete.
type postingLedger struct {
	Debit string ` + "`json:\"debit\"`" + `
}

func newSpendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spend <amount> <category>",
		Short: "Record money you spent",
	}
	cmd.Flags().String("account", "", "which account this affects")
	return cmd
}
`
	if terms := bannedTermsIn(t, source); len(terms) != 0 {
		t.Errorf("flagged %v in code that no user reads; only user-facing strings are in scope", terms)
	}
}

// TestVocabularyCheckFlagsHelpText covers the case the check exists for:
// issue #7 was originally specified as `bodger tx out`, and review caught
// it rather than CI.
func TestVocabularyCheckFlagsHelpText(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "command name",
			source: `package cli

import "github.com/spf13/cobra"

var cmd = &cobra.Command{Use: "tx out <amount>", Short: "Record money you spent"}
`,
			want: "tx",
		},
		{
			name: "Short help",
			source: `package cli

import "github.com/spf13/cobra"

var cmd = &cobra.Command{Use: "spend", Short: "Add a posting to your ledger"}
`,
			want: "posting",
		},
		{
			name: "Long help, written as a concatenation",
			source: `package cli

import "github.com/spf13/cobra"

var cmd = &cobra.Command{
	Use:  "balance",
	Long: "See what you have in each account, " +
		"reconciled against your statement.",
}
`,
			want: "reconcile",
		},
		{
			name: "flag usage text",
			source: `package cli

import "github.com/spf13/cobra"

func register(cmd *cobra.Command) {
	cmd.Flags().String("since", "", "the accounting period to report on")
}
`,
			want: "accounting period",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			terms := bannedTermsIn(t, tc.source)
			found := false
			for _, term := range terms {
				if term == tc.want {
					found = true
				}
			}
			if !found {
				t.Errorf("flagged %v, want %q among them", terms, tc.want)
			}
		})
	}
}

// TestVocabularyCheckReadsErrorRegistry confirms the registry half of the
// check reads the messages it is meant to, and reads them from
// the real file rather than a fixture — the point of the check is that it
// tracks what actually ships.
func TestVocabularyCheckReadsErrorRegistry(t *testing.T) {
	root, err := vocabRepoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	messages, err := collectRegistryMessages(root)
	if err != nil {
		t.Fatalf("read the error registry: %v", err)
	}
	// Eight coarse codes, each with one default message (ADR-0011).
	if len(messages) != 8 {
		t.Errorf("read %d default messages, want 8 — one per shipped code", len(messages))
	}
	for _, m := range messages {
		if m.value == "" {
			t.Errorf("%s:%d: empty default message", m.file, m.line)
		}
	}
}

func TestVocabAllowlist(t *testing.T) {
	const usage = "account type: bank | cash | credit card (required)"
	dir := t.TempDir()
	path := filepath.Join(dir, "allow.txt")
	contents := "# a comment\n\n  # an indented comment\ncredit|" + usage + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}

	allow, err := loadVocabAllowlist(path)
	if err != nil {
		t.Fatalf("load allowlist: %v", err)
	}

	if !allow.allows("credit", usage) {
		t.Error("the exempted term is still flagged in its exempted string")
	}
	if allow.allows("ledger", usage) {
		t.Error("exempting one term exempted the whole string")
	}
	if allow.allows("credit", "a credit to your account") {
		t.Error("exempting one string exempted the term everywhere")
	}
	if allow.allows("credit", usage+" ") {
		t.Error("the exemption survived an edit to the string it was written for")
	}
}

func TestVocabAllowlistRejectsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allow.txt")
	if err := os.WriteFile(path, []byte("credit\n"), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}
	if _, err := loadVocabAllowlist(path); err == nil {
		t.Error("accepted an entry with no `|` separator")
	}
}

// TestVocabularyCheckScopesItself pins what the check does not read. §2
// applies to every user-facing string, but issue #11 deliberately
// mechanises only the two unambiguous surfaces; the rest stays a review
// responsibility rather than becoming a check with false positives that
// people learn to bypass.
func TestVocabularyCheckScopesItself(t *testing.T) {
	const source = `package http

import "github.com/spf13/cobra"

// openAPIDescription is out of scope: defining "user-facing" across an API
// description precisely enough is the hard part (issue #11).
const openAPIDescription = "The transaction's ledger postings."

var cmd = &cobra.Command{Use: "serve", Short: "Start the REST API server."}
`
	if terms := bannedTermsIn(t, source); len(terms) != 0 {
		t.Errorf("flagged %v outside the two scoped surfaces", terms)
	}
}
