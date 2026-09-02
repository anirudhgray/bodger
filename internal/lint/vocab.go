package lint

// This file is the machinery behind the vocabulary check in vocab_test.go:
// docs/ux-principles.md §2's banned-term table, the allowlist that exempts
// a legitimate use, and the extractor that decides which of this repo's
// string literals a person actually reads.
//
// Strings are located by parsing Go source rather than by grepping it,
// for one reason: §2 bans these words in *user-facing strings only*, and
// is explicit that "internal code uses the precise term — `Posting` is a
// `Posting` in internal/domain". A text search cannot tell the difference
// between a cobra Short and the doc comment above it ("a single-posting
// inflow", in internal/surface/cli/entries.go, is correct and must not
// fail the build). Parsing can: the extractors below only ever look at
// string literals in specific syntactic positions, so identifiers,
// package names, and comments are structurally out of reach. The matching
// itself is still a literal term search, not any kind of semantic
// analysis — see issue #11.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// vocabAllowlistPath is the allowlist file, relative to this package's
// directory (which is a test's working directory).
const vocabAllowlistPath = "vocab_allowlist.txt"

// bannedTerm is one row of docs/ux-principles.md §2's table: a word that
// must not reach a user, and what that row says to do instead.
type bannedTerm struct {
	term     string
	guidance string
	pattern  *regexp.Regexp
}

// newBannedTerm compiles the match for one banned term.
//
// Two decisions live in this pattern. Word boundaries count letters,
// digits and underscore as word characters, so the account type
// `credit_card` — a real, unrelated use of the substring "credit" that
// appears in `bodger accounts add --type`'s usage text — does not trip
// the "credit" row. And the optional s/es/ed suffix catches the
// inflections of a banned word ("postings", "debited", "reconciled")
// without needing a table row for each.
func newBannedTerm(term, guidance string) bannedTerm {
	const nonWord = `[^0-9A-Za-z_]`
	const inflection = `(s|es|ed|d)?`
	return bannedTerm{
		term:     term,
		guidance: guidance,
		pattern:  regexp.MustCompile(`(?i)(^|` + nonWord + `)` + regexp.QuoteMeta(term) + inflection + `($|` + nonWord + `)`),
	}
}

// bannedVocabulary is docs/ux-principles.md §2's table. Rows the table
// states as a phrase ("chart of accounts") stay phrases: the individual
// words are fine on their own. Rows whose shorter form already covers the
// longer one are not repeated — "journal" matches "journal entry" too.
var bannedVocabulary = []bannedTerm{
	newBannedTerm("posting", "describe the effect instead; nothing a person reads needs the word"),
	newBannedTerm("entry line", "describe the effect instead; nothing a person reads needs the word"),
	newBannedTerm("journal", "describe the effect instead; nothing a person reads needs the word"),
	newBannedTerm("ledger", `say "your transactions" or "your history"`),
	newBannedTerm("debit", `say "money in" or "money out", or just use the sign`),
	newBannedTerm("credit", `say "money in" or "money out", or just use the sign`),
	newBannedTerm("double-entry", "say nothing; it is an implementation detail"),
	newBannedTerm("balanced entry", "say nothing; it is an implementation detail"),
	newBannedTerm("chart of accounts", `say "your accounts"`),
	newBannedTerm("accounting period", `say "month", "period", or "date range"`),
	newBannedTerm("fiscal period", `say "month", "period", or "date range"`),
	newBannedTerm("reconcile", `say "check against your statement"`),
	newBannedTerm("tx", `say "transaction", or use the verb (spend / receive / move)`),
	newBannedTerm("txn", `say "transaction", or use the verb (spend / receive / move)`),
	newBannedTerm("crud", `say "add", "edit", or "delete"`),
	newBannedTerm("upsert", `say "add", "edit", or "delete"`),
	newBannedTerm("soft delete", `say "add", "edit", or "delete"`),
	newBannedTerm("soft-delete", `say "add", "edit", or "delete"`),
	newBannedTerm("null", `say "not set", "none", or "any"`),
	newBannedTerm("nil", `say "not set", "none", or "any"`),
	newBannedTerm("undefined", `say "not set", "none", or "any"`),
	newBannedTerm("enum", "list the actual options"),
}

// userString is one string literal the check scans, carrying enough
// location for a failure to point at the source line that owns it.
type userString struct {
	file  string // repo-relative
	line  int
	where string // the surface it was read from, e.g. `cobra Short`
	value string
}

// vocabViolation is one banned term found in one user-facing string.
type vocabViolation struct {
	str  userString
	term bannedTerm
}

// String renders a violation as the failure message a contributor sees,
// including the allowlist line that would exempt it — the check is only
// useful if the way out of a false positive is obvious from the failure
// itself rather than from reading this package.
func (v vocabViolation) String() string {
	return fmt.Sprintf(
		"%s:%d: %s says %q, which docs/ux-principles.md §2 bans in user-facing strings — %s\n"+
			"\tstring: %q\n"+
			"\tif this use is legitimate, add this line to internal/lint/%s:\n"+
			"\t\t%s|%s",
		v.str.file, v.str.line, v.str.where, v.term.term, v.term.guidance,
		v.str.value, vocabAllowlistPath, v.term.term, v.str.value,
	)
}

// findVocabViolations returns every banned term appearing in strs that the
// allowlist does not exempt.
func findVocabViolations(strs []userString, allow vocabAllowlist) []vocabViolation {
	var out []vocabViolation
	for _, s := range strs {
		for _, term := range bannedVocabulary {
			if term.pattern.MatchString(s.value) && !allow.allows(term.term, s.value) {
				out = append(out, vocabViolation{str: s, term: term})
			}
		}
	}
	return out
}

// vocabAllowlist maps a banned term to the exact strings allowed to
// contain it.
//
// Exemptions are per term and per exact string on purpose. Exempting a
// whole string would let a second banned word slip into it later, and
// matching loosely would let an allowlist entry outlive the wording that
// justified it — under exact matching, editing an allowlisted string
// re-arms the check for it.
type vocabAllowlist map[string]map[string]bool

func (a vocabAllowlist) allows(term, value string) bool {
	return a[strings.ToLower(term)][value]
}

// loadVocabAllowlist reads the allowlist file at path. Each line is either
// blank, a `#` comment, or an entry of the form `term|exact string`. The
// split is on the first `|` only, so an exempted string may itself contain
// one; a banned term never does.
func loadVocabAllowlist(path string) (vocabAllowlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := vocabAllowlist{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		term, value, ok := strings.Cut(line, "|")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected `term|exact string`, got %q", path, i+1, line)
		}
		term = strings.ToLower(strings.TrimSpace(term))
		if out[term] == nil {
			out[term] = map[string]bool{}
		}
		out[term][value] = true
	}
	return out, nil
}

// collectRegistryMessages returns the default user-facing message of every
// row in internal/platform/errs's registry (ADR-0011): the `message:`
// field of each entry literal. These are the strings a surface prints when
// the application layer does not supply a more specific explanation, so
// they are read by users of all four surfaces at once.
func collectRegistryMessages(root string) ([]userString, error) {
	path := filepath.Join(root, "internal", "platform", "errs", "registry.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	var out []userString
	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		if key, ok := kv.Key.(*ast.Ident); !ok || key.Name != "message" {
			return true
		}
		if value, ok := literalStringValue(kv.Value); ok {
			out = append(out, userString{
				file:  relativeTo(root, path),
				line:  fset.Position(kv.Pos()).Line,
				where: "an error registry default message",
				value: value,
			})
		}
		return true
	})
	return out, nil
}

// cobraHelpFields are the cobra.Command fields whose contents a user reads
// — in `--help` output, or (Use) as the command they type.
var cobraHelpFields = map[string]string{
	"Use":     "a command name",
	"Short":   "a command's Short help",
	"Long":    "a command's Long help",
	"Example": "a command's Example help",
}

// collectCobraStrings returns every cobra help string and flag usage
// string declared anywhere under root.
//
// It walks the whole tree rather than a fixed directory because a cobra
// command is user-facing wherever it is declared: today `serve` lives in
// internal/surface/http and the root command in cmd/bodger, and CLI
// commands are added in parallel branches. Scanning by syntax rather than
// by location means a new command is covered the moment it is written,
// without anyone remembering to extend a list. Nothing else is picked up:
// only cobra.Command literals and flag-set calls are inspected, so
// OpenAPI descriptions and web UI strings — out of scope for issue #11 —
// stay out of scope even though they live under the same tree.
func collectCobraStrings(root string) ([]userString, error) {
	var out []userString
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipVocabScanDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		// Test files declare cobra commands as fixtures (see
		// internal/surface/cli/cli_test.go), and nobody reads those.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel := relativeTo(root, path)
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CompositeLit:
				if isCobraCommandLit(node) {
					out = append(out, cobraHelp(fset, rel, node)...)
				}
			case *ast.CallExpr:
				if s, ok := flagUsage(fset, rel, node); ok {
					out = append(out, s)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// skipVocabScanDir reports whether a directory is not this repo's own Go
// source. Dot-directories matter most: .claude/worktrees holds other
// agents' checkouts of other branches, which must never decide whether
// this one builds.
func skipVocabScanDir(name string) bool {
	switch name {
	case "web", "node_modules", "vendor", "bin", "dist", "testdata":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// isCobraCommandLit reports whether lit is a `cobra.Command{...}` literal.
func isCobraCommandLit(lit *ast.CompositeLit) bool {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Command" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "cobra"
}

// cobraHelp returns the user-readable fields of one cobra.Command literal.
func cobraHelp(fset *token.FileSet, file string, lit *ast.CompositeLit) []userString {
	var out []userString
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		where, ok := cobraHelpFields[key.Name]
		if !ok {
			continue
		}
		if value, ok := literalStringValue(kv.Value); ok {
			out = append(out, userString{
				file:  file,
				line:  fset.Position(kv.Pos()).Line,
				where: where,
				value: value,
			})
		}
	}
	return out
}

// flagUsage returns the usage text of a flag declared on a cobra command's
// flag set — `cmd.Flags().StringVar(&f.on, "on", "", "the date this
// happened")`, or the `Bool`/`StringArrayVar`/`…P` variants. Every pflag
// declaration takes its usage string last regardless of which variant it
// is, so the last argument is the one to read, and the flag's own name is
// the first string literal among the arguments.
func flagUsage(fset *token.FileSet, file string, call *ast.CallExpr) (userString, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return userString{}, false
	}
	recv, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return userString{}, false
	}
	recvSel, ok := recv.Fun.(*ast.SelectorExpr)
	if !ok {
		return userString{}, false
	}
	switch recvSel.Sel.Name {
	case "Flags", "PersistentFlags", "LocalFlags", "InheritedFlags":
	default:
		return userString{}, false
	}

	usage, ok := literalStringValue(call.Args[len(call.Args)-1])
	if !ok {
		return userString{}, false
	}
	where := "a flag's usage text"
	for _, arg := range call.Args {
		if name, ok := literalStringValue(arg); ok {
			where = "--" + name + "'s usage text"
			break
		}
	}
	return userString{
		file:  file,
		line:  fset.Position(call.Pos()).Line,
		where: where,
		value: usage,
	}, true
}

// literalStringValue returns the value of a string literal expression,
// including a raw (backquoted) one and a concatenation of literals, which
// is how a Long help string long enough to wrap is usually written.
func literalStringValue(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return value, true
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, lok := literalStringValue(e.X)
		right, rok := literalStringValue(e.Y)
		if !lok || !rok {
			return "", false
		}
		return left + right, true
	}
	return "", false
}

func relativeTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
