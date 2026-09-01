package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/adapters/sqlite"
	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	clisurface "github.com/anirudhgray/bodger/internal/surface/cli"
)

// newTestFactory wires a clisurface.ServiceFactory to a fresh, migrated
// temp-file SQLite database frozen at frozenAt — the same wiring
// cmd/bodger's real bootstrap does, reproduced here since bootstrap lives
// in package main and can't be imported from this test.
func newTestFactory(t *testing.T, frozenAt time.Time) clisurface.ServiceFactory {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bodger.db")
	clk := clock.NewFrozen(frozenAt)

	db, err := sqlite.Open(clk, path)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})
	if err := db.MigrateUp(context.Background()); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}

	return func(ctx context.Context) (*app.Service, func() error, error) {
		svc, err := app.NewService(
			clk, config.Defaults, idgen.New(),
			sqlite.NewAccountRepository(db), sqlite.NewCategoryRepository(db),
			sqlite.NewTransactionRepository(db), sqlite.NewTagRepository(db),
		)
		return svc, func() error { return nil }, err
	}
}

// run builds a brand-new root command tree around factory and executes
// args against it, the way a fresh `bodger` process invocation would. A
// fresh tree per call matters: cobra flag variables are bound once at
// command construction and don't reset between Execute calls on the same
// tree, so reusing one root across calls within a test would leak flag
// values from an earlier call the way two real, separate CLI invocations
// never could.
func run(t *testing.T, factory clisurface.ServiceFactory, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "bodger", SilenceUsage: true, SilenceErrors: true}
	clisurface.Register(root, factory)

	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func mustRun(t *testing.T, factory clisurface.ServiceFactory, args ...string) string {
	t.Helper()
	stdout, stderr, err := run(t, factory, args...)
	if err != nil {
		t.Fatalf("run(%v): %v (stderr: %s)", args, err, stderr)
	}
	return stdout
}

func decodeData(t *testing.T, stdout string, v any) {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("decode envelope: %v (stdout: %s)", err, stdout)
	}
	if err := json.Unmarshal(envelope.Data, v); err != nil {
		t.Fatalf("decode data: %v (stdout: %s)", err, stdout)
	}
}

func wantErrCode(t *testing.T, err error, code errs.Code) {
	t.Helper()
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want an *errs.Error with code %s", err, code)
	}
	if e.Code != code {
		t.Errorf("err code = %s, want %s (message: %s)", e.Code, code, e.Message)
	}
}

const frozenInstant = "2026-08-14T12:00:00Z"

func mustFrozen(t *testing.T) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, frozenInstant)
	if err != nil {
		t.Fatalf("parse frozen instant: %v", err)
	}
	return ts
}

// TestSpend_DefaultsAccountWhenExactlyOne is the CLI's counterpart to
// issue #7's own "done when" check: a user with one account and no --on
// flag can record an expense with just an amount and a category, and it
// books to today's date via the injected clock.
func TestSpend_DefaultsAccountWhenExactlyOne(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "accounts", "add", "Cash", "--type", "cash", "--currency", "INR")
	mustRun(t, factory, "categories", "add", "groceries", "--type", "expense")

	stdout := mustRun(t, factory, "spend", "1800.50", "groceries", "--json")

	var got struct {
		Type        string `json:"type"`
		Date        string `json:"date"`
		Description string `json:"description"`
		Account     string `json:"account"`
		Category    string `json:"category"`
		Amount      string `json:"amount"`
		Currency    string `json:"currency"`
	}
	decodeData(t, stdout, &got)

	if got.Type != "spend" {
		t.Errorf("type = %q, want %q", got.Type, "spend")
	}
	if got.Date != "2026-08-14" {
		t.Errorf("date = %q, want %q (today under the frozen clock)", got.Date, "2026-08-14")
	}
	if got.Account != "Cash" {
		t.Errorf("account = %q, want %q (the only account, defaulted)", got.Account, "Cash")
	}
	if got.Category != "groceries" {
		t.Errorf("category = %q, want %q", got.Category, "groceries")
	}
	if got.Amount != "1800.50" {
		t.Errorf("amount = %q, want %q", got.Amount, "1800.50")
	}
	if got.Currency != "INR" {
		t.Errorf("currency = %q, want %q", got.Currency, "INR")
	}
	// Judgment call (see entries.go's newSpendCmd doc comment): with no
	// description flag in this issue's scope, the category positional
	// also serves as the entry's description.
	if got.Description != "groceries" {
		t.Errorf("description = %q, want %q (defaults to the category text)", got.Description, "groceries")
	}
}

func TestSpend_NoAccountsYet(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "categories", "add", "groceries", "--type", "expense")

	_, _, err := run(t, factory, "spend", "10", "groceries")
	wantErrCode(t, err, errs.InvalidInput)
}

func TestSpend_AmbiguousAccountRequiresExplicitFlag(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "accounts", "add", "Cash", "--type", "cash")
	mustRun(t, factory, "accounts", "add", "Bank", "--type", "bank")
	mustRun(t, factory, "categories", "add", "groceries", "--type", "expense")

	_, _, err := run(t, factory, "spend", "10", "groceries")
	wantErrCode(t, err, errs.InvalidInput)

	// Naming the account explicitly still works.
	stdout := mustRun(t, factory, "spend", "10", "groceries", "--account", "Cash", "--json")
	var got struct {
		Account string `json:"account"`
	}
	decodeData(t, stdout, &got)
	if got.Account != "Cash" {
		t.Errorf("account = %q, want %q", got.Account, "Cash")
	}
}

func TestReceive_WithExplicitAccountAndDate(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "accounts", "add", "HDFC Savings", "--type", "bank", "--currency", "INR")
	mustRun(t, factory, "categories", "add", "salary", "--type", "income")

	stdout := mustRun(t, factory, "receive", "150000", "salary", "--account", "HDFC Savings", "--on", "2026-08-01", "--json")
	var got struct {
		Type    string `json:"type"`
		Date    string `json:"date"`
		Amount  string `json:"amount"`
		Account string `json:"account"`
	}
	decodeData(t, stdout, &got)
	if got.Type != "receive" || got.Date != "2026-08-01" || got.Amount != "150000.00" || got.Account != "HDFC Savings" {
		t.Errorf("got %+v", got)
	}
}

func TestMove_FromAndToAreCorrectRegardlessOfPostingOrder(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "accounts", "add", "Savings", "--type", "bank", "--currency", "INR")
	mustRun(t, factory, "accounts", "add", "Checking", "--type", "bank", "--currency", "INR")

	stdout := mustRun(t, factory, "move", "20000", "--from", "Savings", "--to", "Checking", "--on", "2026-08-20", "--json")
	var got struct {
		Type        string `json:"type"`
		Date        string `json:"date"`
		Description string `json:"description"`
		From        string `json:"from"`
		To          string `json:"to"`
		Amount      string `json:"amount"`
		Currency    string `json:"currency"`
	}
	decodeData(t, stdout, &got)
	if got.Type != "move" {
		t.Errorf("type = %q, want %q", got.Type, "move")
	}
	if got.From != "Savings" || got.To != "Checking" {
		t.Errorf("from/to = %q/%q, want Savings/Checking", got.From, got.To)
	}
	if got.Amount != "20000.00" || got.Currency != "INR" {
		t.Errorf("amount/currency = %q/%q, want 20000.00/INR", got.Amount, got.Currency)
	}
	// Judgment call (see move.go's newMoveCmd doc comment): with no
	// description flag, the description defaults to a sentence built
	// from the raw --from/--to text.
	want := "Transfer from Savings to Checking"
	if got.Description != want {
		t.Errorf("description = %q, want %q", got.Description, want)
	}

	balances := mustRun(t, factory, "balance", "--on", "2026-08-20", "--json")
	var balResult struct {
		Balances []struct {
			Account string `json:"account"`
			Balance string `json:"balance"`
		} `json:"balances"`
	}
	decodeData(t, balances, &balResult)
	balanceByAccount := map[string]string{}
	for _, b := range balResult.Balances {
		balanceByAccount[b.Account] = b.Balance
	}
	if balanceByAccount["Savings"] != "-20000.00" {
		t.Errorf("Savings balance = %q, want -20000.00", balanceByAccount["Savings"])
	}
	if balanceByAccount["Checking"] != "20000.00" {
		t.Errorf("Checking balance = %q, want 20000.00", balanceByAccount["Checking"])
	}
}

func TestAccounts_AddListArchiveSetOpeningBalance(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))

	addOut := mustRun(t, factory, "accounts", "add", "HDFC Savings", "--type", "bank", "--currency", "INR",
		"--opening-balance", "1000", "--institution", "HDFC Bank", "--json")
	var added struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		Institution string `json:"institution"`
	}
	decodeData(t, addOut, &added)
	if added.Name != "HDFC Savings" || added.Type != "bank" || added.Institution != "HDFC Bank" {
		t.Errorf("got %+v", added)
	}

	listOut := mustRun(t, factory, "accounts", "list", "--json")
	var list []struct {
		Name     string `json:"name"`
		Archived bool   `json:"archived"`
	}
	decodeData(t, listOut, &list)
	if len(list) != 1 || list[0].Name != "HDFC Savings" || list[0].Archived {
		t.Fatalf("list = %+v", list)
	}

	setOut := mustRun(t, factory, "accounts", "set-opening-balance", "HDFC Savings", "2000", "--on", "2026-01-01", "--json")
	var set struct {
		OpeningBalance     string `json:"opening_balance"`
		OpeningBalanceDate string `json:"opening_balance_date"`
	}
	decodeData(t, setOut, &set)
	if set.OpeningBalance != "2000.00" || set.OpeningBalanceDate != "2026-01-01" {
		t.Errorf("got %+v", set)
	}

	archiveOut := mustRun(t, factory, "accounts", "archive", "HDFC Savings", "--json")
	var archived struct {
		Archived bool `json:"archived"`
	}
	decodeData(t, archiveOut, &archived)
	if !archived.Archived {
		t.Error("archived.Archived = false, want true")
	}
}

func TestAccounts_AddMissingTypeFails(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	_, stderr, err := run(t, factory, "accounts", "add", "Cash")
	if err == nil {
		t.Fatal("want an error for a missing --type, got nil")
	}
	if !strings.Contains(err.Error(), "type") {
		t.Errorf("err = %v, want it to mention the missing --type flag", err)
	}
	_ = stderr
}

func TestCategories_AddListRenameArchive(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))

	mustRun(t, factory, "categories", "add", "groceries", "--type", "expense")

	listOut := mustRun(t, factory, "categories", "list", "--json")
	var list []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	decodeData(t, listOut, &list)
	if len(list) != 1 || list[0].Name != "groceries" || list[0].Type != "expense" {
		t.Fatalf("list = %+v", list)
	}

	renameOut := mustRun(t, factory, "categories", "rename", "groceries", "food", "--json")
	var renamed struct {
		Name string `json:"name"`
	}
	decodeData(t, renameOut, &renamed)
	if renamed.Name != "food" {
		t.Errorf("renamed.Name = %q, want %q", renamed.Name, "food")
	}

	archiveOut := mustRun(t, factory, "categories", "archive", "food", "--json")
	var archived struct {
		Archived bool `json:"archived"`
	}
	decodeData(t, archiveOut, &archived)
	if !archived.Archived {
		t.Error("archived.Archived = false, want true")
	}
}

func TestBalance_EmptyInstance(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	stdout := mustRun(t, factory, "balance")
	if !strings.Contains(stdout, "No accounts yet") {
		t.Errorf("stdout = %q, want a friendly empty-state message", stdout)
	}
}

// TestJSONError_UsesSharedEnvelope checks that an application-layer error
// surfaced through --json uses the {"error": {...}} shape RenderError
// documents, with the registered code and field intact — the same
// envelope every command shares, not a one-off per command.
func TestJSONError_UsesSharedEnvelope(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "categories", "add", "groceries", "--type", "expense")

	stdout, _, err := run(t, factory, "spend", "10", "groceries", "--account", "does-not-exist", "--json")
	if err == nil {
		t.Fatal("want an error for an unknown account, got nil")
	}
	wantErrCode(t, err, errs.NotFound)

	// The command's own RunE returns the error to cobra; rendering it as
	// the {"error": ...} envelope is main.go's job (via
	// clisurface.RenderError), so exercise that explicitly here too.
	var errBuf bytes.Buffer
	code := clisurface.RenderError(&errBuf, err, true)
	if code != errs.CLIExitCode(errs.NotFound) {
		t.Errorf("exit code = %d, want %d", code, errs.CLIExitCode(errs.NotFound))
	}
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Field   string `json:"field"`
		} `json:"error"`
	}
	if err := json.Unmarshal(errBuf.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v (body: %s)", err, errBuf.String())
	}
	if envelope.Error.Code != string(errs.NotFound) {
		t.Errorf("error.code = %q, want %q", envelope.Error.Code, errs.NotFound)
	}
	if envelope.Error.Message == "" {
		t.Error("error.message is empty")
	}
	_ = stdout
}

func TestRenderError_NonErrsErrorFallsBackToExitOne(t *testing.T) {
	var buf bytes.Buffer
	code := clisurface.RenderError(&buf, errors.New("boom"), false)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("output = %q, want it to contain the underlying error", buf.String())
	}
}
