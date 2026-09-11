package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/adapters/fxprovider"
	"github.com/anirudhgray/bodger/internal/adapters/sqlite"
	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	"github.com/anirudhgray/bodger/internal/platform/logging"
	"github.com/anirudhgray/bodger/internal/ports"
	clisurface "github.com/anirudhgray/bodger/internal/surface/cli"
)

// newTestFactory wires a clisurface.ServiceFactory to a fresh, migrated
// temp-file SQLite database frozen at frozenAt — the same wiring
// cmd/bodger's real bootstrap does, reproduced here since bootstrap lives
// in package main and can't be imported from this test. It wires a real
// (network-touching) Frankfurter provider — fine for every test that never
// calls `fx rates fetch`; a test that does calls
// newTestFactoryWithFxProvider instead so it never depends on the network.
func newTestFactory(t *testing.T, frozenAt time.Time) clisurface.ServiceFactory {
	t.Helper()
	return newTestFactoryWithFxProvider(t, frozenAt, fxprovider.New("", nil))
}

// newTestFactoryWithFxProvider is newTestFactory with the FX rate provider
// swapped out — for fx_test.go's `fx rates fetch` tests, which need a
// deterministic, in-memory ports.FxRateProvider rather than a real
// network call to Frankfurter.
func newTestFactoryWithFxProvider(t *testing.T, frozenAt time.Time, provider ports.FxRateProvider) clisurface.ServiceFactory {
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
			sqlite.NewUserRepository(db), sqlite.NewSessionRepository(db), sqlite.NewAPITokenRepository(db),
			sqlite.NewFxRateRepository(db), provider,
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

// TestMove_CrossCurrencyRendersImpliedRate is issue #136's CLI surface for
// #133's cross-currency RecordTransfer: moving between two accounts in
// different currencies is accepted (no rejection, unlike before #133), and
// the transaction's implied exchange rate (ledger.Transaction.FxRate) is
// rendered alongside the move, with its source.
func TestMove_CrossCurrencyRendersImpliedRate(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "accounts", "add", "Savings", "--type", "bank", "--currency", "INR")
	mustRun(t, factory, "accounts", "add", "Yen Wallet", "--type", "cash", "--currency", "JPY")

	stdout := mustRun(t, factory, "move", "1000", "--from", "Savings", "--to", "Yen Wallet", "--on", "2026-08-20", "--json")
	var got struct {
		Amount     string `json:"amount"`
		Currency   string `json:"currency"`
		ToAmount   string `json:"to_amount"`
		ToCurrency string `json:"to_currency"`
		Rate       string `json:"rate"`
		RateSource string `json:"rate_source"`
	}
	decodeData(t, stdout, &got)

	// amount/currency report the from-leg (issue #163) — the 1000 INR the
	// command's own positional argument gave. buildTransferPostings
	// applies the same numeric minor-unit magnitude to both legs (1000
	// INR = 100000 minor units at INR's 2-decimal exponent; JPY's
	// 0-decimal exponent means the same 100000 minor units renders as
	// 100000 JPY for the to-leg) — the implied rate this test asserts
	// follows directly from that.
	if got.Amount != "1000.00" || got.Currency != "INR" {
		t.Errorf("amount/currency = %q/%q, want 1000.00/INR", got.Amount, got.Currency)
	}
	if got.ToAmount != "100000" || got.ToCurrency != "JPY" {
		t.Errorf("to_amount/to_currency = %q/%q, want 100000/JPY", got.ToAmount, got.ToCurrency)
	}
	if got.Rate != "1 INR = 100 JPY" {
		t.Errorf("rate = %q, want %q", got.Rate, "1 INR = 100 JPY")
	}
	if got.RateSource != "implied" {
		t.Errorf("rate_source = %q, want %q", got.RateSource, "implied")
	}

	text := mustRun(t, factory, "move", "500", "--from", "Savings", "--to", "Yen Wallet", "--on", "2026-08-21")
	if !strings.Contains(text, "rate: 1 INR = 100 JPY") {
		t.Errorf("plain text output = %q, want a rate line", text)
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

// TestAccounts_RenameKeepsHistory checks the rename lands and that the
// transactions already recorded against the account are still recorded
// against it afterwards — a rename is a relabelling, not a new account.
func TestAccounts_RenameKeepsHistory(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "accounts", "add", "HDFC Savngs", "--type", "bank", "--currency", "INR")
	mustRun(t, factory, "categories", "add", "groceries", "--type", "expense")
	mustRun(t, factory, "spend", "800", "groceries", "--on", "2026-08-10")

	var renamed struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	decodeData(t, mustRun(t, factory, "accounts", "rename", "HDFC Savngs", "HDFC Savings", "--json"), &renamed)
	if renamed.Name != "HDFC Savings" || renamed.Type != "bank" {
		t.Errorf("renamed = %+v", renamed)
	}

	listed := listTransactions(t, factory, "--account", "HDFC Savings")
	if len(listed.Transactions) != 1 || listed.Transactions[0].AccountID != renamed.ID {
		t.Errorf("transactions after the rename = %+v, want the one spend, still on this account", listed.Transactions)
	}
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

// txnView mirrors the JSON shape internal/surface/cli's transactionView
// renders, as a test's-eye view of it: decoding into a struct declared
// here (rather than asserting on substrings of the plain-text output) is
// what makes these tests fail if a field name in the --json envelope
// changes, which is the part scripts depend on.
type txnView struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Date          string   `json:"date"`
	Description   string   `json:"description"`
	Notes         string   `json:"notes"`
	Tags          []string `json:"tags"`
	AccountID     string   `json:"account_id"`
	CategoryID    string   `json:"category_id"`
	FromAccountID string   `json:"from_account_id"`
	ToAccountID   string   `json:"to_account_id"`
	Amount        string   `json:"amount"`
	Currency      string   `json:"currency"`
	ToAmount      string   `json:"to_amount"`
	ToCurrency    string   `json:"to_currency"`
}

type txnListView struct {
	Transactions []txnView `json:"transactions"`
	Limit        int       `json:"limit"`
	Offset       int       `json:"offset"`
}

// seedTransactions records one of everything, on four consecutive days,
// and returns the factory the rest of a transactions test runs against.
func seedTransactions(t *testing.T) clisurface.ServiceFactory {
	t.Helper()
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "accounts", "add", "Cash", "--type", "cash", "--currency", "INR")
	mustRun(t, factory, "accounts", "add", "Bank", "--type", "bank", "--currency", "INR")
	mustRun(t, factory, "categories", "add", "groceries", "--type", "expense")
	mustRun(t, factory, "categories", "add", "salary", "--type", "income")

	mustRun(t, factory, "spend", "800", "groceries", "--account", "Cash", "--on", "2026-08-10")
	mustRun(t, factory, "spend", "200", "groceries", "--account", "Bank", "--on", "2026-08-11")
	mustRun(t, factory, "receive", "150000", "salary", "--account", "Bank", "--on", "2026-08-12")
	mustRun(t, factory, "move", "500", "--from", "Bank", "--to", "Cash", "--on", "2026-08-13")
	return factory
}

func listTransactions(t *testing.T, factory clisurface.ServiceFactory, args ...string) txnListView {
	t.Helper()
	var got txnListView
	decodeData(t, mustRun(t, factory, append([]string{"transactions", "list", "--json"}, args...)...), &got)
	return got
}

// TestTransactions_ListSpeaksTheCLIsOwnVerbs pins the vocabulary decision
// transactions.go's transactionTypeFor documents: a listed transaction is
// described with the same verb that recorded it (spend/receive/move), not
// with the application layer's own kind names.
func TestTransactions_ListSpeaksTheCLIsOwnVerbs(t *testing.T) {
	factory := seedTransactions(t)

	got := listTransactions(t, factory)
	if len(got.Transactions) != 4 {
		t.Fatalf("listed %d transactions, want 4: %+v", len(got.Transactions), got.Transactions)
	}
	// Sorted booked-date descending, so the move (the 13th) comes first
	// and the first spend (the 10th) comes last.
	wantTypes := []string{"move", "receive", "spend", "spend"}
	for i, want := range wantTypes {
		if got.Transactions[i].Type != want {
			t.Errorf("transaction %d type = %q, want %q", i, got.Transactions[i].Type, want)
		}
	}

	move := got.Transactions[0]
	if move.FromAccountID == "" || move.ToAccountID == "" {
		t.Errorf("move = %+v, want both ends named", move)
	}
	if move.AccountID != "" || move.CategoryID != "" {
		t.Errorf("move = %+v, want no single account or category", move)
	}
	if move.Amount != "500.00" || move.Currency != "INR" {
		t.Errorf("move amount/currency = %q/%q, want 500.00/INR", move.Amount, move.Currency)
	}

	spend := got.Transactions[3]
	if spend.AccountID == "" || spend.CategoryID == "" {
		t.Errorf("spend = %+v, want an account and a category", spend)
	}
	if spend.FromAccountID != "" || spend.ToAccountID != "" {
		t.Errorf("spend = %+v, want no move ends", spend)
	}
	// Money spent is stored as a negative amount; the view shows the
	// magnitude, with the direction carried by the type.
	if spend.Amount != "800.00" {
		t.Errorf("spend amount = %q, want 800.00", spend.Amount)
	}
}

func TestTransactions_ListFilters(t *testing.T) {
	factory := seedTransactions(t)

	spends := listTransactions(t, factory, "--type", "spend")
	if len(spends.Transactions) != 2 {
		t.Errorf("--type spend returned %d, want 2", len(spends.Transactions))
	}

	// Both the Cash spend and the move (which lands in Cash) touch Cash.
	cash := listTransactions(t, factory, "--account", "Cash")
	if len(cash.Transactions) != 2 {
		t.Errorf("--account Cash returned %d, want 2", len(cash.Transactions))
	}

	groceries := listTransactions(t, factory, "--category", "groceries")
	if len(groceries.Transactions) != 2 {
		t.Errorf("--category groceries returned %d, want 2", len(groceries.Transactions))
	}

	window := listTransactions(t, factory, "--since", "2026-08-11", "--until", "2026-08-12")
	if len(window.Transactions) != 2 {
		t.Fatalf("--since/--until returned %d, want 2: %+v", len(window.Transactions), window.Transactions)
	}
	if window.Transactions[0].Date != "2026-08-12" || window.Transactions[1].Date != "2026-08-11" {
		t.Errorf("dates = %q/%q, want 2026-08-12/2026-08-11", window.Transactions[0].Date, window.Transactions[1].Date)
	}
}

func TestTransactions_ListRejectsAnUnknownType(t *testing.T) {
	factory := seedTransactions(t)
	_, _, err := run(t, factory, "transactions", "list", "--type", "wire")
	wantErrCode(t, err, errs.InvalidInput)
}

func TestTransactions_ListAcceptsRESTVocabularyAsTypeAlias(t *testing.T) {
	factory := seedTransactions(t)

	spends := listTransactions(t, factory, "--type", "spend")
	outflows := listTransactions(t, factory, "--type", "outflow")
	if len(outflows.Transactions) != len(spends.Transactions) {
		t.Errorf("--type outflow returned %d, want %d (same as --type spend)", len(outflows.Transactions), len(spends.Transactions))
	}

	receives := listTransactions(t, factory, "--type", "receive")
	inflows := listTransactions(t, factory, "--type", "inflow")
	if len(inflows.Transactions) != len(receives.Transactions) {
		t.Errorf("--type inflow returned %d, want %d (same as --type receive)", len(inflows.Transactions), len(receives.Transactions))
	}

	moves := listTransactions(t, factory, "--type", "move")
	transfers := listTransactions(t, factory, "--type", "transfer")
	if len(transfers.Transactions) != len(moves.Transactions) {
		t.Errorf("--type transfer returned %d, want %d (same as --type move)", len(transfers.Transactions), len(moves.Transactions))
	}

	// Output vocabulary doesn't change based on which spelling filtered a
	// transaction in — it's still rendered with this CLI's own verb.
	for _, txn := range outflows.Transactions {
		if txn.Type != "spend" {
			t.Errorf("transaction filtered by --type outflow rendered as %q, want %q", txn.Type, "spend")
		}
	}
}

func TestTransactions_ListPagesWithLimitAndOffset(t *testing.T) {
	factory := seedTransactions(t)

	first := listTransactions(t, factory, "--limit", "2")
	if len(first.Transactions) != 2 || first.Limit != 2 || first.Offset != 0 {
		t.Fatalf("first page = %+v", first)
	}
	second := listTransactions(t, factory, "--limit", "2", "--offset", "2")
	if len(second.Transactions) != 2 || second.Offset != 2 {
		t.Fatalf("second page = %+v", second)
	}
	if first.Transactions[0].ID == second.Transactions[0].ID {
		t.Error("the second page repeats the first page's first transaction")
	}

	// The plain-text renderer offers the next page only when this one came
	// back exactly full, since that's the only hint of more to come.
	full := mustRun(t, factory, "transactions", "list", "--limit", "2")
	if !strings.Contains(full, "--offset 2") {
		t.Errorf("output = %q, want it to point at the next page", full)
	}
	last := mustRun(t, factory, "transactions", "list", "--limit", "3", "--offset", "3")
	if strings.Contains(last, "--offset") {
		t.Errorf("output = %q, want no next-page hint on a page that isn't full", last)
	}
}

func TestTransactions_ListEmptyState(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	stdout := mustRun(t, factory, "transactions", "list")
	if !strings.Contains(stdout, "No transactions found") {
		t.Errorf("stdout = %q, want a friendly empty-state message", stdout)
	}
}

// TestTransactions_EditReplacesEveryField exercises the full-replacement
// contract transactions.go's newTransactionsEditCmd documents, and checks
// the correction actually lands in the balances — the point of ADR-0002's
// correctable ledger, from a user's seat.
func TestTransactions_EditReplacesEveryField(t *testing.T) {
	factory := seedTransactions(t)
	mustRun(t, factory, "categories", "add", "dining", "--type", "expense")

	listed := listTransactions(t, factory, "--type", "spend", "--account", "Cash")
	if len(listed.Transactions) != 1 {
		t.Fatalf("want exactly one Cash spend to edit, got %+v", listed.Transactions)
	}
	id := listed.Transactions[0].ID

	var edited txnView
	decodeData(t, mustRun(t, factory, "transactions", "edit", id,
		"--amount", "950.25", "--description", "Anniversary dinner", "--account", "Cash",
		"--category", "dining", "--on", "2026-08-09", "--note", "split the bill",
		"--tag", "date-night", "--json"), &edited)

	if edited.ID != id {
		t.Errorf("edited a different transaction: %q, want %q", edited.ID, id)
	}
	if edited.Type != "spend" {
		t.Errorf("type = %q, want it to stay %q", edited.Type, "spend")
	}
	if edited.Amount != "950.25" || edited.Date != "2026-08-09" {
		t.Errorf("amount/date = %q/%q, want 950.25/2026-08-09", edited.Amount, edited.Date)
	}
	if edited.Description != "Anniversary dinner" || edited.Notes != "split the bill" {
		t.Errorf("description/notes = %q/%q", edited.Description, edited.Notes)
	}
	if len(edited.Tags) != 1 || edited.Tags[0] != "date-night" {
		t.Errorf("tags = %v, want [date-night]", edited.Tags)
	}
	if edited.CategoryID == "" || edited.CategoryID == listed.Transactions[0].CategoryID {
		t.Errorf("category_id = %q, want the new category's", edited.CategoryID)
	}

	// Cash: -950.25 spent, +500.00 moved in.
	var balances struct {
		Balances []struct {
			Account string `json:"account"`
			Balance string `json:"balance"`
		} `json:"balances"`
	}
	decodeData(t, mustRun(t, factory, "balance", "--json"), &balances)
	for _, b := range balances.Balances {
		if b.Account == "Cash" && b.Balance != "-450.25" {
			t.Errorf("Cash balance = %q, want -450.25 after the correction", b.Balance)
		}
	}
}

func TestTransactions_EditRefusesBothFlagSetsAtOnce(t *testing.T) {
	factory := seedTransactions(t)
	listed := listTransactions(t, factory, "--type", "move")
	id := listed.Transactions[0].ID

	_, _, err := run(t, factory, "transactions", "edit", id,
		"--amount", "500", "--description", "Moved", "--account", "Cash", "--from", "Bank", "--to", "Cash")
	wantErrCode(t, err, errs.InvalidInput)
}

func TestTransactions_EditAMove(t *testing.T) {
	factory := seedTransactions(t)
	listed := listTransactions(t, factory, "--type", "move")
	id := listed.Transactions[0].ID

	var edited txnView
	decodeData(t, mustRun(t, factory, "transactions", "edit", id,
		"--amount", "750", "--description", "Top up Cash", "--from", "Bank", "--to", "Cash",
		"--on", "2026-08-13", "--json"), &edited)

	if edited.Type != "move" || edited.Amount != "750.00" {
		t.Errorf("edited = %+v, want a 750.00 move", edited)
	}
	if edited.FromAccountID != listed.Transactions[0].FromAccountID || edited.ToAccountID != listed.Transactions[0].ToAccountID {
		t.Errorf("from/to = %q/%q, want them unchanged", edited.FromAccountID, edited.ToAccountID)
	}
}

// TestTransactions_EditCrossCurrencyMoveRoundTripPreservesBothLegs is
// issue #163's regression check: `transactions list`'s amount/to_amount
// report the from-leg/to-leg respectively (transactions.go's
// transactionView doc comment), so re-running `transactions edit` with
// those exact values unchanged must reproduce the same transfer, not
// silently reinterpret the to-leg as a new from-leg. Before the fix,
// `transactions list` only reported the to-leg (as "amount"), so this
// same round trip would have turned a 100.00 USD -> 8000.00 INR transfer
// into an 8000.00 USD -> 8000.00 INR one.
func TestTransactions_EditCrossCurrencyMoveRoundTripPreservesBothLegs(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "accounts", "add", "Cash", "--type", "cash", "--currency", "USD")
	mustRun(t, factory, "accounts", "add", "Savings", "--type", "bank", "--currency", "INR")

	mustRun(t, factory, "move", "100", "--from", "Cash", "--to", "Savings", "--to-amount", "8000", "--on", "2026-08-13")

	before := listTransactions(t, factory, "--type", "move")
	if len(before.Transactions) != 1 {
		t.Fatalf("want exactly one move, got %+v", before.Transactions)
	}
	txn := before.Transactions[0]
	if txn.Amount != "100.00" || txn.Currency != "USD" || txn.ToAmount != "8000.00" || txn.ToCurrency != "INR" {
		t.Fatalf("listed move = %+v, want 100.00 USD -> 8000.00 INR", txn)
	}

	// Resubmit exactly what was just listed — the shape a naive "edit"
	// pre-fill would reuse unchanged.
	var edited txnView
	decodeData(t, mustRun(t, factory, "transactions", "edit", txn.ID,
		"--amount", txn.Amount, "--from", "Cash", "--to", "Savings", "--to-amount", txn.ToAmount,
		"--description", txn.Description, "--on", txn.Date, "--json"), &edited)

	if edited.Amount != "100.00" || edited.Currency != "USD" {
		t.Errorf("edited from-leg = %s %s, want 100.00 USD (from-leg corrupted by the round trip)", edited.Amount, edited.Currency)
	}
	if edited.ToAmount != "8000.00" || edited.ToCurrency != "INR" {
		t.Errorf("edited to-leg = %s %s, want 8000.00 INR (to-leg corrupted by the round trip)", edited.ToAmount, edited.ToCurrency)
	}
}

func TestTransactions_DeleteDropsItFromListsAndBalances(t *testing.T) {
	factory := seedTransactions(t)
	listed := listTransactions(t, factory, "--type", "receive")
	if len(listed.Transactions) != 1 {
		t.Fatalf("want exactly one receive, got %+v", listed.Transactions)
	}
	id := listed.Transactions[0].ID

	var deleted txnView
	decodeData(t, mustRun(t, factory, "transactions", "delete", id, "--json"), &deleted)
	if deleted.ID != id {
		t.Errorf("deleted %q, want %q", deleted.ID, id)
	}

	after := listTransactions(t, factory)
	if len(after.Transactions) != 3 {
		t.Errorf("listed %d transactions after the delete, want 3", len(after.Transactions))
	}
	for _, txn := range after.Transactions {
		if txn.ID == id {
			t.Errorf("transaction %q is still listed after being deleted", id)
		}
	}

	// Bank: -200.00 spent, -500.00 moved out, and no salary any more.
	var balances struct {
		Balances []struct {
			Account string `json:"account"`
			Balance string `json:"balance"`
		} `json:"balances"`
	}
	decodeData(t, mustRun(t, factory, "balance", "--json"), &balances)
	for _, b := range balances.Balances {
		if b.Account == "Bank" && b.Balance != "-700.00" {
			t.Errorf("Bank balance = %q, want -700.00 once the salary is deleted", b.Balance)
		}
	}

	// Deleting the same transaction twice can't work — it's gone from
	// every read path, so the second attempt is an ordinary not-found.
	_, _, err := run(t, factory, "transactions", "delete", id)
	wantErrCode(t, err, errs.NotFound)
}

// categoryTreeNode mirrors `categories tree`'s JSON shape: a category's
// own fields inlined, with its children nested underneath.
type categoryTreeNode struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Type     string             `json:"type"`
	ParentID string             `json:"parent_id"`
	Children []categoryTreeNode `json:"children"`
}

// categoryTree runs `categories tree --json` and decodes it into a fresh
// value every time — decoding repeatedly into one variable would let a
// field the next response omits (parent_id, once a category is back at the
// top level) survive from the previous one.
func categoryTree(t *testing.T, factory clisurface.ServiceFactory) []categoryTreeNode {
	t.Helper()
	var roots []categoryTreeNode
	decodeData(t, mustRun(t, factory, "categories", "tree", "--json"), &roots)
	return roots
}

func reparentCategory(t *testing.T, factory clisurface.ServiceFactory, args ...string) categoryTreeNode {
	t.Helper()
	var got categoryTreeNode
	decodeData(t, mustRun(t, factory, append([]string{"categories", "reparent"}, args...)...), &got)
	return got
}

func TestCategories_ReparentAndTree(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "categories", "add", "food", "--type", "expense")
	mustRun(t, factory, "categories", "add", "groceries", "--type", "expense")
	mustRun(t, factory, "categories", "add", "dining", "--type", "expense", "--parent", "food")

	// Two top-level categories to start with, one of which already has a
	// child from --parent at creation time.
	roots := categoryTree(t, factory)
	if len(roots) != 2 {
		t.Fatalf("tree roots = %+v, want food and groceries", roots)
	}

	reparented := reparentCategory(t, factory, "groceries", "--parent", "food", "--json")
	if reparented.Name != "groceries" || reparented.ParentID == "" {
		t.Fatalf("reparented = %+v, want groceries under a parent", reparented)
	}

	roots = categoryTree(t, factory)
	if len(roots) != 1 || roots[0].Name != "food" {
		t.Fatalf("tree roots = %+v, want food alone at the top", roots)
	}
	if len(roots[0].Children) != 2 {
		t.Fatalf("food's children = %+v, want dining and groceries", roots[0].Children)
	}
	if roots[0].Children[0].ParentID != roots[0].ID {
		t.Errorf("child parent_id = %q, want %q", roots[0].Children[0].ParentID, roots[0].ID)
	}

	// The plain-text tree indents each level under the one above.
	text := mustRun(t, factory, "categories", "tree")
	if !strings.Contains(text, "food (expense)\n  ") {
		t.Errorf("tree output = %q, want children indented under their parent", text)
	}

	// Omitting --parent moves a category back to the top level.
	backAtTop := reparentCategory(t, factory, "groceries", "--json")
	if backAtTop.ParentID != "" {
		t.Errorf("parent_id = %q, want it cleared by a reparent with no --parent", backAtTop.ParentID)
	}
	if roots := categoryTree(t, factory); len(roots) != 2 {
		t.Errorf("tree roots = %+v, want groceries back at the top", roots)
	}
}

func TestCategories_ReparentRejectsACycle(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "categories", "add", "food", "--type", "expense")
	mustRun(t, factory, "categories", "add", "dining", "--type", "expense", "--parent", "food")

	_, _, err := run(t, factory, "categories", "reparent", "food", "--parent", "dining")
	if err == nil {
		t.Fatal("want an error for a cycle, got nil")
	}
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCategories_TreeEmptyState(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	stdout := mustRun(t, factory, "categories", "tree")
	if !strings.Contains(stdout, "No categories yet") {
		t.Errorf("stdout = %q, want a friendly empty-state message", stdout)
	}
}

func TestBalance_EmptyInstance(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	stdout := mustRun(t, factory, "balance")
	if !strings.Contains(stdout, "No accounts yet") {
		t.Errorf("stdout = %q, want a friendly empty-state message", stdout)
	}
}

// TestBalance_CurrencyAndPolicyRenderProvenanceAndUnconverted is issue
// #136's balance-conversion surface, end to end: a USD account converts to
// itself as an identity (no rate to show), an INR account converts via a
// stored rate with full ADR-0004 provenance rendered inline, and a EUR
// account with no stored rate is listed under "unconverted" with its
// reason rather than silently dropped from the response.
func TestBalance_CurrencyAndPolicyRenderProvenanceAndUnconverted(t *testing.T) {
	provider := newFakeFxProvider()
	factory := newTestFactoryWithFxProvider(t, mustFrozen(t), provider)

	mustRun(t, factory, "accounts", "add", "Wallet", "--type", "cash", "--currency", "INR", "--opening-balance", "1000")
	mustRun(t, factory, "accounts", "add", "Checking", "--type", "bank", "--currency", "USD", "--opening-balance", "500")
	mustRun(t, factory, "accounts", "add", "Euro Account", "--type", "bank", "--currency", "EUR", "--opening-balance", "200")

	provider.setRate(t, "INR", "USD", "0.0115", mustFxTestDate(t, 2026, time.August, 14))
	mustRun(t, factory, "fx", "rates", "fetch", "--pair", "INR")

	var got struct {
		Balances []struct {
			Account   string `json:"account"`
			Currency  string `json:"currency"`
			Balance   string `json:"balance"`
			Converted *struct {
				Amount     string `json:"amount"`
				Currency   string `json:"currency"`
				Rate       string `json:"rate"`
				RateDate   string `json:"rate_date"`
				RateSource string `json:"rate_source"`
				Stale      bool   `json:"stale"`
				Policy     string `json:"policy"`
			} `json:"converted"`
		} `json:"balances"`
		Unconverted []struct {
			Account string `json:"account"`
			Reason  string `json:"reason"`
		} `json:"unconverted"`
	}
	decodeData(t, mustRun(t, factory, "balance", "--currency", "USD", "--policy", "current", "--json"), &got)

	byAccount := map[string]int{}
	for i, b := range got.Balances {
		byAccount[b.Account] = i
	}

	wallet := got.Balances[byAccount["Wallet"]]
	if wallet.Converted == nil {
		t.Fatalf("Wallet.Converted is nil, want a converted figure")
	}
	if wallet.Converted.Amount != "11.50" || wallet.Converted.Currency != "USD" {
		t.Errorf("Wallet converted = %+v, want 11.50 USD (1000 INR @ 0.0115)", wallet.Converted)
	}
	if wallet.Converted.Rate != "0.0115" || wallet.Converted.RateSource != "fake-provider" || wallet.Converted.Policy != "current" {
		t.Errorf("Wallet converted provenance = %+v", wallet.Converted)
	}
	if wallet.Converted.Stale {
		t.Errorf("Wallet.Converted.Stale = true, want false (rate fetched at today's date)")
	}

	checking := got.Balances[byAccount["Checking"]]
	if checking.Converted == nil || checking.Converted.Amount != "500.00" || checking.Converted.RateSource != "" {
		t.Errorf("Checking converted = %+v, want an identity conversion with no rate source", checking.Converted)
	}

	if len(got.Unconverted) != 1 || got.Unconverted[0].Account != "Euro Account" || got.Unconverted[0].Reason == "" {
		t.Fatalf("unconverted = %+v, want Euro Account with a reason", got.Unconverted)
	}

	// The plain-text rendering points at the fix.
	text := mustRun(t, factory, "balance", "--currency", "USD", "--policy", "current")
	if !strings.Contains(text, "bodger fx rates fetch") {
		t.Errorf("text = %q, want it to point at `bodger fx rates fetch`", text)
	}
	if !strings.Contains(text, "Euro Account") {
		t.Errorf("text = %q, want the unconverted account named", text)
	}
}

// TestBalanceTotals_OverallByCategoryByCurrency is issue #195's totals
// overview, end to end: an INR account and a USD account converted into
// the resolved reporting currency (an identity conversion for the USD
// account, a stored rate for the INR one), and a EUR account with no
// stored rate excluded from "overall"/per-category but still summed raw
// under per-currency and listed under "unconverted".
func TestBalanceTotals_OverallByCategoryByCurrency(t *testing.T) {
	provider := newFakeFxProvider()
	factory := newTestFactoryWithFxProvider(t, mustFrozen(t), provider)

	mustRun(t, factory, "accounts", "add", "Wallet", "--type", "cash", "--currency", "INR", "--opening-balance", "1000")
	mustRun(t, factory, "accounts", "add", "Checking", "--type", "bank", "--currency", "USD", "--opening-balance", "500")
	mustRun(t, factory, "accounts", "add", "Euro Account", "--type", "bank", "--currency", "EUR", "--opening-balance", "200")

	provider.setRate(t, "INR", "USD", "0.0115", mustFxTestDate(t, 2026, time.August, 14))
	mustRun(t, factory, "fx", "rates", "fetch", "--pair", "INR")

	var got struct {
		Currency   string `json:"currency"`
		Overall    string `json:"overall"`
		ByCategory []struct {
			Kind   string `json:"kind"`
			Amount string `json:"amount"`
		} `json:"by_category"`
		ByCurrency []struct {
			Currency string `json:"currency"`
			Amount   string `json:"amount"`
		} `json:"by_currency"`
		Unconverted []struct {
			Account string `json:"account"`
			Reason  string `json:"reason"`
		} `json:"unconverted"`
	}
	decodeData(t, mustRun(t, factory, "balance", "totals", "--currency", "USD", "--policy", "current", "--json"), &got)

	if got.Currency != "USD" {
		t.Errorf("currency = %q, want USD", got.Currency)
	}
	if got.Overall != "511.50" {
		t.Errorf("overall = %q, want 511.50 (1000 INR @ 0.0115 + 500.00 USD; Euro Account excluded, no rate)", got.Overall)
	}

	byKind := map[string]string{}
	for _, c := range got.ByCategory {
		byKind[c.Kind] = c.Amount
	}
	if byKind["cash"] != "11.50" {
		t.Errorf("by_category[cash] = %q, want 11.50", byKind["cash"])
	}
	if byKind["bank"] != "500.00" {
		t.Errorf("by_category[bank] = %q, want 500.00 (Euro Account excluded)", byKind["bank"])
	}

	byCurrency := map[string]string{}
	for _, c := range got.ByCurrency {
		byCurrency[c.Currency] = c.Amount
	}
	if byCurrency["INR"] != "1000.00" || byCurrency["USD"] != "500.00" || byCurrency["EUR"] != "200.00" {
		t.Errorf("by_currency = %+v, want raw INR 1000.00, USD 500.00, EUR 200.00", byCurrency)
	}

	if len(got.Unconverted) != 1 || got.Unconverted[0].Account != "Euro Account" || got.Unconverted[0].Reason == "" {
		t.Fatalf("unconverted = %+v, want Euro Account with a reason", got.Unconverted)
	}

	// The plain-text rendering surfaces the same shortfall.
	text := mustRun(t, factory, "balance", "totals", "--currency", "USD", "--policy", "current")
	if !strings.Contains(text, "Overall: 511.50 USD") {
		t.Errorf("text = %q, want the overall line", text)
	}
	if !strings.Contains(text, "Euro Account") {
		t.Errorf("text = %q, want the unconverted account named", text)
	}
}

// TestBalanceTotals_RequiresPolicy checks --policy's "required" contract:
// leaving it off (unlike plain `balance`, where --currency/--policy are
// optional together) surfaces the app layer's own InvalidInput.
func TestBalanceTotals_RequiresPolicy(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	mustRun(t, factory, "accounts", "add", "Cash", "--type", "cash", "--currency", "USD")

	_, stderr, err := run(t, factory, "balance", "totals")
	if err == nil {
		t.Fatalf("balance totals with no --policy succeeded, want an error; stderr: %s", stderr)
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
	code := clisurface.RenderError(&errBuf, err, true, nil)
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
	code := clisurface.RenderError(&buf, errors.New("boom"), false, nil)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("output = %q, want it to contain the underlying error", buf.String())
	}
}

// TestRenderError_LogsInternalErrorCause proves the gap issue #43 closes:
// an *errs.Error's wrapped cause — the detail ADR-0011 says must reach a
// log and never a user — is actually logged before RenderError prints
// its safe message, rather than silently discarded. The cause text here
// ("sqlite: disk I/O error") stands in for exactly the kind of driver
// detail that must never reach the user-facing message but must reach
// the log in full.
func TestRenderError_LogsInternalErrorCause(t *testing.T) {
	var logBuf bytes.Buffer
	logger := logging.New(&logBuf, slog.LevelInfo)

	cause := errors.New("sqlite: disk I/O error")
	err := errs.New(errs.Internal).Explain("bodger couldn't save that.").Wrap(cause)

	var out bytes.Buffer
	code := clisurface.RenderError(&out, err, false, logger)

	if code != errs.CLIExitCode(errs.Internal) {
		t.Errorf("code = %d, want %d", code, errs.CLIExitCode(errs.Internal))
	}
	if strings.Contains(out.String(), "disk I/O error") {
		t.Errorf("user-facing output leaked the internal cause: %q", out.String())
	}
	if !strings.Contains(out.String(), "bodger couldn't save that.") {
		t.Errorf("user-facing output = %q, want the safe explanation", out.String())
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "disk I/O error") {
		t.Errorf("log output is missing the wrapped cause: %s", logged)
	}
	if !strings.Contains(logged, string(errs.Internal)) {
		t.Errorf("log output is missing the error code: %s", logged)
	}
}
