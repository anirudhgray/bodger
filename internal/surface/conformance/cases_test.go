package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// kind is which of the three record verbs a case exercises.
type kind string

const (
	outflow  kind = "outflow"
	inflow   kind = "inflow"
	transfer kind = "transfer"
)

// conformanceCase is one row of this suite's table: a raw input, driven
// through both surfaces' real entry points, that must produce an
// identical observable result — or, for a case with wantErrCode set, an
// identical error.
//
// docs/contributing.md's "adding an operation means adding a row" applies
// here: a fourth record verb, or a fifth field either surface accepts,
// extends this struct and dispatch() below, not the harness.
type conformanceCase struct {
	name string
	kind kind

	amount   string
	date     string
	account  string // outflow, inflow
	category string // outflow, inflow

	fromAccount string // transfer
	toAccount   string // transfer
	toAmount    string // transfer, optional (issue #159)

	notes string
	tags  []string

	// wantErrCode, when non-zero, is the *errs.Error code both surfaces
	// must fail this case with. A case with wantErrCode set is not
	// compared for a successful result at all; instead, both surfaces'
	// error field path (the "field" JSON key CLI's --json and HTTP's
	// error envelope both carry) must also agree with each other —
	// asserted generically rather than against a value hardcoded per
	// case, since which exact field path a shared application-layer
	// error carries is that layer's decision, not this table's to
	// predict.
	wantErrCode errs.Code
}

// cases is the table itself. Coverage follows issue #9's list: empty
// date, "today", "yesterday", an ISO date, a locale-format date, a
// grouped amount, a currency symbol, absent currency (the precedence
// ladder — both surfaces omit it here, since internal/surface/cli's
// spend/receive/move have no --currency flag at all, so there is no
// input shape in which a real CLI invocation could supply an entry-level
// currency to conflict with this), account and category resolution by
// name, an ambiguous name, a case difference, whitespace, Unicode
// normalisation (in notes, which — unlike description — both surfaces
// let a caller set directly), tags with and without "#", and
// invalid-input error messages. See harness_test.go's seed for the
// account/category fixtures these resolve against.
var cases = []conformanceCase{
	{
		name:     "empty date resolves to today in the actor's timezone",
		kind:     outflow,
		amount:   "500",
		date:     "",
		account:  "HDFC Savings",
		category: "groceries",
	},
	{
		name:     "today resolves in the actor's timezone, not the process's",
		kind:     outflow,
		amount:   "500",
		date:     "today",
		account:  "HDFC Savings",
		category: "groceries",
	},
	{
		name:     "yesterday resolves in the actor's timezone, not the process's",
		kind:     outflow,
		amount:   "500",
		date:     "yesterday",
		account:  "HDFC Savings",
		category: "groceries",
	},
	{
		name:     "ISO date",
		kind:     outflow,
		amount:   "500",
		date:     "2026-01-15",
		account:  "HDFC Savings",
		category: "groceries",
	},
	{
		name:     "locale-format date (day/month/year)",
		kind:     outflow,
		amount:   "500",
		date:     "15/01/2026",
		account:  "HDFC Savings",
		category: "groceries",
	},
	{
		name:     "grouped amount with thousands separators",
		kind:     outflow,
		amount:   "1,800.50",
		date:     "2026-01-15",
		account:  "HDFC Savings",
		category: "groceries",
	},
	{
		name:     "amount carrying the resolved currency's own symbol",
		kind:     outflow,
		amount:   "₹1,800.50",
		date:     "2026-01-15",
		account:  "HDFC Savings", // INR account: the entry has no explicit currency, so it resolves to INR and ₹ is this amount's own symbol
		category: "groceries",
	},
	{
		name:     "account resolved by exact name",
		kind:     outflow,
		amount:   "500",
		date:     "2026-01-15",
		account:  "Cash",
		category: "groceries",
	},
	{
		name:     "account resolved case-insensitively",
		kind:     outflow,
		amount:   "500",
		date:     "2026-01-15",
		account:  "hdfc savings",
		category: "groceries",
	},
	{
		name:     "account resolved with leading/trailing whitespace trimmed",
		kind:     outflow,
		amount:   "500",
		date:     "2026-01-15",
		account:  "  HDFC Savings  ",
		category: "groceries",
	},
	{
		name:     "category resolved case-insensitively",
		kind:     outflow,
		amount:   "500",
		date:     "2026-01-15",
		account:  "Cash",
		category: "GROCERIES",
	},
	{
		name:     "note carrying combining-character Unicode normalises to NFC",
		kind:     outflow,
		amount:   "500",
		date:     "2026-01-15",
		account:  "Cash",
		category: "groceries",
		notes:    "Café receipt", // "e" + combining acute accent (U+0301)
	},
	{
		name:     "tags with and without a leading #",
		kind:     outflow,
		amount:   "500",
		date:     "2026-01-15",
		account:  "Cash",
		category: "groceries",
		tags:     []string{"#Weekend Trip", "urgent"},
	},
	{
		name:     "inflow (receive) resolves identically to outflow",
		kind:     inflow,
		amount:   "2500",
		date:     "2026-01-15",
		account:  "Cash",
		category: "salary",
	},
	{
		name:        "transfer between two accounts of the same currency",
		kind:        transfer,
		amount:      "1000",
		date:        "2026-01-15",
		fromAccount: "HDFC Savings",
		toAccount:   "ICICI Checking",
	},
	{
		// A cross-currency transfer (issue #133/#136/#137): both surfaces
		// must derive and render the same implied exchange rate and
		// source for the same raw input — see comparableKeys' "rate"/
		// "rate_source" entries below, which is what actually asserts
		// that agreement rather than just amount/currency. toAmount is
		// deliberately omitted here: this covers the fallback (issue
		// #159) where the to-leg reuses amount's raw digits, reinterpreted
		// in the to-currency — both surfaces must still agree on that
		// trivial 1:1-ratio rate. The following case covers a real,
		// independently-given to-amount instead.
		name:        "cross-currency transfer renders its implied rate identically",
		kind:        transfer,
		amount:      "100",
		date:        "2026-01-15",
		fromAccount: "Cash",         // USD
		toAccount:   "HDFC Savings", // INR
	},
	{
		// Same pair, but with an independent to-leg amount (issue
		// #159): both surfaces must derive and render the same *real*
		// implied rate (not the trivial 1:1 the case above exercises)
		// from the two independently-given amounts.
		name:        "cross-currency transfer with an independent to-amount renders its implied rate identically",
		kind:        transfer,
		amount:      "100",
		toAmount:    "8000",
		date:        "2026-01-15",
		fromAccount: "Cash",         // USD
		toAccount:   "HDFC Savings", // INR
	},
	{
		name:        "ambiguous account name matches more than one candidate",
		kind:        outflow,
		amount:      "500",
		date:        "2026-01-15",
		account:     "wallet", // case-insensitively matches both seeded "Wallet" and "WALLET"
		category:    "groceries",
		wantErrCode: errs.InvalidInput,
	},
	{
		name:        "unknown account name",
		kind:        outflow,
		amount:      "500",
		date:        "2026-01-15",
		account:     "Nonexistent Account",
		category:    "groceries",
		wantErrCode: errs.NotFound,
	},
	{
		name:        "unrecognised date",
		kind:        outflow,
		amount:      "500",
		date:        "not a date",
		account:     "Cash",
		category:    "groceries",
		wantErrCode: errs.InvalidInput,
	},
	{
		name:        "amount with more fractional digits than the currency allows",
		kind:        outflow,
		amount:      "10.999",
		date:        "2026-01-15",
		account:     "Cash",
		category:    "groceries",
		wantErrCode: errs.InvalidInput,
	},
	{
		name:        "tag with no letters or digits normalises to nothing",
		kind:        outflow,
		amount:      "500",
		date:        "2026-01-15",
		account:     "Cash",
		category:    "groceries",
		tags:        []string{"!!!"},
		wantErrCode: errs.InvalidInput,
	},
}

// TestConformance runs every case in the table through all three
// surfaces against one shared, seeded database, and asserts they agree.
//
// Manually re-verified that this catches a surface resolving a date
// itself instead of delegating to internal/app/normalize.DateOf (issue
// #9's fourth "done when" demonstration, alongside
// TestImportGraph_DetectsViolations and
// TestBannedSymbols_DetectsViolations for the other three): temporarily
// changed internal/surface/http/transactions.go's createTransaction to
// pass a hardcoded Date instead of body.Date, ran this suite, and
// confirmed every date-sensitive case failed — "today"/"yesterday"/empty
// cases on a date mismatch, and the invalid-date case on HTTP silently
// succeeding instead of rejecting "not a date" — then reverted the
// change.
//
// Re-verified the same way for the MCP leg (issue #263), the same day:
// temporarily commented out passing Date: args.Date in
// internal/surface/mcp/transactions_write.go's recordOutflowTool (so
// RecordOutflowCommand.Date was always its zero value), ran this suite,
// and confirmed 12 of 22 cases failed specifically on the MCP leg — 11
// date-sensitive outflow cases ("yesterday", the ISO/locale/grouped/
// symbol-amount/case-insensitive/whitespace/Unicode/tags cases, all of
// which book on a non-"today" date) on a plain date mismatch against
// CLI, and the invalid-date case on MCP silently succeeding instead of
// rejecting "not a date" — then reverted the change. (This also
// surfaced a real, unrelated bug this session fixed separately:
// internal/surface/mcp/transactions.go's transactionViewFrom never
// rendered rate/rate_source for a cross-currency transfer, unlike
// moveViewFrom and dto.go's own transactionView — see that file's git
// history.) Not left as a permanent fixture the way the import-graph and
// banned-symbol violations are, because there is no way to break "a
// surface resolves its own date" without editing a real surface's
// production code, which would leave a real bug in the tree between test
// runs rather than a self-contained fixture.
//
// Last manually verified: 2026-09-02 for the CLI/HTTP legs, confirmed 4
// of 19 cases failed with exactly this shape, then restored
// internal/surface/http/transactions.go. 2026-09-14 for the MCP leg,
// confirmed 12 of 22 cases failed with exactly this shape, then restored
// internal/surface/mcp/transactions_write.go.
func TestConformance(t *testing.T) {
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.seed()

			cliOut, cliErr := h.runCLI(tc.cliArgs()...)
			httpStatus, httpDecoded := h.runHTTP(tc.httpMethod(), tc.httpPath(), tc.httpBody())
			mcpResult := h.runMCP(context.Background(), tc.mcpToolName(), tc.mcpArgs())

			if tc.wantErrCode != "" {
				if cliErr == nil {
					t.Fatalf("CLI: expected error code %s, got success (output: %s)", tc.wantErrCode, cliOut)
				}
				if httpStatus < 400 {
					t.Fatalf("HTTP: expected an error status, got %d (body: %v)", httpStatus, httpDecoded)
				}
				if !mcpResult.IsError {
					t.Fatalf("MCP: expected an error result, got success: %+v", mcpResult)
				}
				gotCLI := h.cliErrorCode(cliErr)
				gotHTTP := h.httpErrorCode(httpDecoded)
				gotMCP := h.mcpErrorCode(mcpResult)
				if gotCLI != tc.wantErrCode {
					t.Errorf("CLI error code = %s, want %s", gotCLI, tc.wantErrCode)
				}
				if gotHTTP != tc.wantErrCode {
					t.Errorf("HTTP error code = %s, want %s", gotHTTP, tc.wantErrCode)
				}
				if gotMCP != tc.wantErrCode {
					t.Errorf("MCP error code = %s, want %s", gotMCP, tc.wantErrCode)
				}
				cliField := h.cliErrorField(cliErr)
				httpField := h.httpErrorField(httpDecoded)
				if cliField != httpField {
					t.Errorf("CLI and HTTP disagree on which field this error attaches to: CLI %q, HTTP %q", cliField, httpField)
				}
				return
			}

			if cliErr != nil {
				t.Fatalf("CLI: unexpected error: %v (output: %s)", cliErr, cliOut)
			}
			if httpStatus >= 300 {
				t.Fatalf("HTTP: unexpected error status %d: %v", httpStatus, httpDecoded)
			}
			if mcpResult.IsError {
				t.Fatalf("MCP: unexpected error result: %+v", mcpResult)
			}

			cliData := decodeEnvelope(t, cliOut)
			httpData := h.httpData(httpDecoded)
			mcpData := h.mcpData(mcpResult)

			cliFields := comparableFields(cliData)
			httpFields := comparableFields(httpData)
			mcpFields := comparableFields(mcpData)

			cliJSON, _ := json.MarshalIndent(cliFields, "", "  ")
			httpJSON, _ := json.MarshalIndent(httpFields, "", "  ")
			mcpJSON, _ := json.MarshalIndent(mcpFields, "", "  ")
			if string(cliJSON) != string(httpJSON) {
				t.Errorf("CLI and HTTP disagree on the stored transaction:\nCLI:\n%s\nHTTP:\n%s", cliJSON, httpJSON)
			}
			if string(cliJSON) != string(mcpJSON) {
				t.Errorf("CLI and MCP disagree on the stored transaction:\nCLI:\n%s\nMCP:\n%s", cliJSON, mcpJSON)
			}
		})
	}
}

// cliArgs builds the argv runCLI executes for c — the same command a
// person would actually type, per docs/ux-principles.md §7.
func (c conformanceCase) cliArgs() []string {
	var args []string
	switch c.kind {
	case outflow:
		args = []string{"spend", c.amount, c.category, "--account", c.account}
	case inflow:
		args = []string{"receive", c.amount, c.category, "--account", c.account}
	case transfer:
		args = []string{"move", c.amount, "--from", c.fromAccount, "--to", c.toAccount}
	}
	if c.kind == transfer && c.toAmount != "" {
		args = append(args, "--to-amount", c.toAmount)
	}
	if c.date != "" {
		args = append(args, "--on", c.date)
	}
	if c.notes != "" {
		args = append(args, "--note", c.notes)
	}
	for _, tag := range c.tags {
		args = append(args, "--tag", tag)
	}
	return args
}

// mcpToolName and mcpArgs build runMCP's call for c — record_outflow,
// record_inflow, or record_transfer (internal/surface/mcp/transactions_write.go),
// the write-tier tools #261 already shipped for these three verbs.
func (c conformanceCase) mcpToolName() string {
	switch c.kind {
	case outflow:
		return "record_outflow"
	case inflow:
		return "record_inflow"
	case transfer:
		return "record_transfer"
	}
	return ""
}

func (c conformanceCase) mcpArgs() map[string]any {
	if c.kind == transfer {
		return map[string]any{
			"from_account": c.fromAccount,
			"to_account":   c.toAccount,
			"amount":       c.amount,
			"to_amount":    c.toAmount,
			"date":         c.date,
			// Same default as httpBody's transfer branch — see its own
			// comment for why description is never a point of
			// divergence between surfaces.
			"description": fmt.Sprintf("Transfer from %s to %s", c.fromAccount, c.toAccount),
			"notes":       c.notes,
			"tags":        c.tags,
		}
	}
	return map[string]any{
		"account":     c.account,
		"category":    c.category,
		"amount":      c.amount,
		"date":        c.date,
		"description": c.category, // same default as httpBody's entry branch
		"notes":       c.notes,
		"tags":        c.tags,
	}
}

// httpMethod, httpPath, and httpBody build runHTTP's request for c —
// POST /api/v1/transactions for an outflow/inflow, POST
// /api/v1/transfers for a transfer, per internal/surface/http's own
// route table.
func (c conformanceCase) httpMethod() string { return "POST" }

func (c conformanceCase) httpPath() string {
	if c.kind == transfer {
		return "/api/v1/transfers"
	}
	return "/api/v1/transactions"
}

func (c conformanceCase) httpBody() any {
	if c.kind == transfer {
		return map[string]any{
			"from_account": c.fromAccount,
			"to_account":   c.toAccount,
			"amount":       c.amount,
			"to_amount":    c.toAmount,
			"date":         c.date,
			// The same default internal/surface/cli/move.go's newMoveCmd
			// builds, so description is never a point of divergence
			// between the two surfaces' results — it isn't one of the
			// fields comparableFields even looks at, but the application
			// layer still requires it non-empty.
			"description": fmt.Sprintf("Transfer from %s to %s", c.fromAccount, c.toAccount),
			"notes":       c.notes,
			"tags":        c.tags,
		}
	}
	txType := transactionTypeOutflow
	if c.kind == inflow {
		txType = transactionTypeInflow
	}
	return map[string]any{
		"type":     txType,
		"account":  c.account,
		"category": c.category,
		"amount":   c.amount,
		"date":     c.date,
		// The same default internal/surface/cli/entries.go's
		// runEntryCommand uses (the category positional does double duty
		// as the description) — see httpBody's transfer branch above for
		// why this is safe to hardcode rather than compare.
		"description": c.category,
		"notes":       c.notes,
		"tags":        c.tags,
	}
}

const (
	transactionTypeOutflow = "outflow"
	transactionTypeInflow  = "inflow"
)

// decodeEnvelope decodes runCLI's stdout — {"data": ...} on success, per
// internal/surface/cli's own --json envelope — failing the test outright
// if it isn't shaped like one.
func decodeEnvelope(t *testing.T, output string) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode CLI --json output: %v (output: %s)", err, output)
	}
	if envelope.Data == nil {
		t.Fatalf("CLI --json output has no \"data\": %s", output)
	}
	return envelope.Data
}

// comparableKeys is the subset of each surface's response shape that
// records what was actually stored, independent of which surface stored
// it: internal/surface/cli's entryView/moveView and
// internal/surface/http's transactionView all use these same field
// names for these same values (see their doc comments), so no
// translation table is needed between the two — only ID, type-specific
// label fields (CLI's "account"/"category"/"from"/"to", which echo back
// the raw text a person typed rather than a resolved value), and
// description (deliberately excluded — see conformanceCase.httpBody) are
// asymmetric, and none of those describe what was normalised and stored.
//
// "rate" and "rate_source" (issue #137) are a cross-currency transfer's
// implied exchange rate and its provenance — both surfaces derive and
// render them identically (CLI's moveView, HTTP's transactionView), so a
// divergence here would mean the two surfaces disagree on the actual
// derived rate, not just on how they format it.
//
// "to_amount" and "to_currency" (issue #163) are a transfer's to-leg,
// alongside "amount"/"currency" for its from-leg — both surfaces derive
// and render them identically, so a divergence here would mean the two
// surfaces disagree on which leg is which, not just on formatting.
var comparableKeys = []string{
	"date", "amount", "currency", "to_amount", "to_currency",
	"account_id", "category_id", "from_account_id", "to_account_id",
	"notes", "tags",
	"rate", "rate_source",
}

// comparableFields projects data down to comparableKeys, so a field only
// one surface's view struct happens to also carry (or that one surface
// omitted via omitempty and the other didn't) never causes a false
// mismatch.
func comparableFields(data map[string]any) map[string]any {
	out := make(map[string]any, len(comparableKeys))
	for _, k := range comparableKeys {
		if v, ok := data[k]; ok {
			out[k] = v
		}
	}
	return out
}
