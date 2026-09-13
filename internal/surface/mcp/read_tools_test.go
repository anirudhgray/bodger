package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/ports"
)

// readToolFixture seeds one checking account, one expense category, one
// outflow transaction against them, one budget with a matching line, and
// one stored FX rate -- enough shared state for every issue #260 read
// tool's own test below to exercise a real, non-empty result over the
// same real SQLite-backed *app.Service newTestService (harness_test.go)
// wires up.
type readToolFixture struct {
	accountID     string
	categoryID    string
	budgetID      string
	transactionID string
}

func seedReadToolFixture(t *testing.T, svc *app.Service) readToolFixture {
	t.Helper()
	ctx := context.Background()

	account, err := svc.CreateAccount(ctx, app.CreateAccountCommand{
		ActorID:  ports.SeededUserID,
		Name:     "Checking",
		Kind:     "bank",
		Currency: "USD",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	category, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{
		ActorID: ports.SeededUserID,
		Name:    "Groceries",
		Kind:    "expense",
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	txn, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID:     ports.SeededUserID,
		AccountRef:  account.Account.ID(),
		Amount:      "42.50",
		Currency:    "USD",
		CategoryRef: category.Category.ID(),
		Date:        "2026-09-10",
		Description: "Farmers market",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	budget, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{
		ActorID:  ports.SeededUserID,
		Name:     "September",
		Currency: "USD",
		StartsOn: "2026-09-01",
		Lines: []app.BudgetLineInput{
			{CategoryRef: category.Category.ID(), Amount: "500.00"},
		},
	})
	if err != nil {
		t.Fatalf("CreateBudget: %v", err)
	}

	seedFxRate(t, svc, "EUR", "USD", "1.10", "2026-09-10", "ecb")

	return readToolFixture{
		accountID:     account.Account.ID(),
		categoryID:    category.Category.ID(),
		budgetID:      budget.Budget.ID(),
		transactionID: txn.Transaction.ID(),
	}
}

// seedFxRate stores rate directly via svc.FxRates -- the same repository
// port ListFxRates itself reads through, never Service.FxProvider (issue
// #135's "never touches the network" contract) -- so a test can seed a
// rate without a real provider call. Mirrors internal/app's own
// unexported seedRate test helper (convert_test.go), reproduced here
// since that one lives in package app_test and isn't importable from this
// package.
func seedFxRate(t *testing.T, svc *app.Service, base, quote, value, date, source string) {
	t.Helper()
	rate, err := fx.NewRate(base, quote, decimal.RequireFromString(value))
	if err != nil {
		t.Fatalf("fx.NewRate(%s, %s, %s): %v", base, quote, value, err)
	}
	parsed, err := time.Parse(time.DateOnly, date)
	if err != nil {
		t.Fatalf("parse date %q: %v", date, err)
	}
	d, err := domain.NewDate(parsed.Year(), parsed.Month(), parsed.Day())
	if err != nil {
		t.Fatalf("domain.NewDate(%q): %v", date, err)
	}
	if err := svc.FxRates.Store(context.Background(), rate, d, source); err != nil {
		t.Fatalf("FxRates.Store: %v", err)
	}
}

// callTool drives the dispatcher directly against every production tool
// from Tools(), the same "no full SDK client/server round trip" shortcut
// dispatcher_test.go's own tests use (Dispatcher.Dispatch's own doc
// comment).
func callTool(t *testing.T, svc *app.Service, name string, args any) json.RawMessage {
	t.Helper()
	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}

	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("json.Marshal(args): %v", err)
		}
		raw = b
	}

	result, err := d.Dispatch(context.Background(), name, raw)
	if err != nil {
		t.Fatalf("Dispatch(%s): %v", name, err)
	}
	text, ok := textContent(result)
	if result.IsError {
		t.Fatalf("Dispatch(%s) result.IsError = true: %s", name, text)
	}
	if !ok {
		t.Fatalf("Dispatch(%s) content[0] wasn't text: %+v", name, result.Content)
	}
	return json.RawMessage(text)
}

// textContent extracts the text of result's first content block —
// every tool in this package renders its result as a single TextContent
// block (jsonResult, result.go's own textResult/errorResult), so this is
// the one place a test needs to know that representation.
func textContent(result *sdkmcp.CallToolResult) (string, bool) {
	if len(result.Content) == 0 {
		return "", false
	}
	tc, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		return "", false
	}
	return tc.Text, true
}

func TestGetAccountBalances_ReturnsSeededAccountUnconverted(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_account_balances", nil)
	var view accountBalancesView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(view.Balances) != 1 {
		t.Fatalf("Balances = %d entries, want 1: %+v", len(view.Balances), view)
	}
	if view.Balances[0].AccountID != fixture.accountID {
		t.Errorf("AccountID = %q, want %q", view.Balances[0].AccountID, fixture.accountID)
	}
	if view.Balances[0].Balance != "-42.50" {
		t.Errorf("Balance = %q, want -42.50 (one 42.50 outflow, no opening balance)", view.Balances[0].Balance)
	}
	if view.Balances[0].Converted != nil {
		t.Errorf("Converted = %+v, want nil (no target currency requested)", view.Balances[0].Converted)
	}
}

func TestGetAccountBalances_InvalidPolicyIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]string{"currency": "USD", "policy": "not-a-policy"})
	result, err := d.Dispatch(context.Background(), "get_account_balances", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unrecognised policy")
	}
}

func TestListTransactions_FiltersByAccount(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "list_transactions", map[string]any{"account": fixture.accountID})
	var view transactionListView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(view.Transactions) != 1 {
		t.Fatalf("Transactions = %d, want 1: %+v", len(view.Transactions), view)
	}
	txn := view.Transactions[0]
	if txn.ID != fixture.transactionID {
		t.Errorf("ID = %q, want %q", txn.ID, fixture.transactionID)
	}
	if txn.Type != "outflow" {
		t.Errorf("Type = %q, want outflow", txn.Type)
	}
	if txn.Amount != "42.50" {
		t.Errorf("Amount = %q, want 42.50", txn.Amount)
	}
	if txn.AccountID != fixture.accountID {
		t.Errorf("AccountID = %q, want %q", txn.AccountID, fixture.accountID)
	}
	if txn.CategoryID != fixture.categoryID {
		t.Errorf("CategoryID = %q, want %q", txn.CategoryID, fixture.categoryID)
	}
}

func TestListTransactions_NoMatchesReturnsEmptyList(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "list_transactions", map[string]any{"description": "no such thing"})
	var view transactionListView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(view.Transactions) != 0 {
		t.Errorf("Transactions = %d, want 0: %+v", len(view.Transactions), view)
	}
}

func TestGetCategoryBreakdown_GroupsSpendingByCategory(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_category_breakdown", map[string]any{
		"currency": "USD",
		"policy":   "current",
	})
	var view categoryBreakdownView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(view.Rows) != 1 {
		t.Fatalf("Rows = %d, want 1: %+v", len(view.Rows), view)
	}
	if view.Rows[0].Category != "Groceries" {
		t.Errorf("Category = %q, want Groceries", view.Rows[0].Category)
	}
	if view.Rows[0].Spending != "42.50" {
		t.Errorf("Spending = %q, want 42.50", view.Rows[0].Spending)
	}
}

func TestGetBudgetActuals_ReportsLineAgainstSpending(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_budget_actuals", map[string]any{"budget_id": fixture.budgetID})
	var view budgetActualsView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.BudgetID != fixture.budgetID {
		t.Errorf("BudgetID = %q, want %q", view.BudgetID, fixture.budgetID)
	}
	if len(view.Lines) != 1 {
		t.Fatalf("Lines = %d, want 1: %+v", len(view.Lines), view)
	}
	if view.Lines[0].Actual != "42.50" {
		t.Errorf("Actual = %q, want 42.50", view.Lines[0].Actual)
	}
	if view.Lines[0].Budgeted != "500.00" {
		t.Errorf("Budgeted = %q, want 500.00", view.Lines[0].Budgeted)
	}
}

func TestGetBudgetActuals_UnknownBudgetIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]string{"budget_id": "does-not-exist"})
	result, err := d.Dispatch(context.Background(), "get_budget_actuals", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unknown budget_id")
	}
}

func TestGetBudgetHistory_ReturnsOnePeriodPerMonth(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_budget_history", map[string]any{
		"budget_id": fixture.budgetID,
		"period":    "2026-09-15",
		"months":    2,
	})
	var view budgetHistoryView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	// The budget starts 2026-09-01, so only its own first month exists —
	// resolveBudgetPeriod/BudgetHistory's own StartsOn clamp
	// (internal/app/budget_actuals.go) stops earlier months from
	// appearing at all, rather than erroring.
	if len(view.Periods) != 1 {
		t.Fatalf("Periods = %d, want 1 (clamped at the budget's StartsOn): %+v", len(view.Periods), view)
	}
	if view.Periods[0].From != "2026-09-01" {
		t.Errorf("Periods[0].From = %q, want 2026-09-01", view.Periods[0].From)
	}
}

func TestListFxRates_ReturnsStoredRate(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "list_fx_rates", map[string]any{
		"from":   "EUR",
		"to":     "USD",
		"policy": "current",
		"amount": "100.00",
	})
	var view fxRateView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Rate != "1.1" {
		t.Errorf("Rate = %q, want 1.1", view.Rate)
	}
	if view.RateSource != "ecb" {
		t.Errorf("RateSource = %q, want ecb", view.RateSource)
	}
	if view.ConvertedAmount != "110.00" {
		t.Errorf("ConvertedAmount = %q, want 110.00", view.ConvertedAmount)
	}
	if view.ConvertedCurrency != "USD" {
		t.Errorf("ConvertedCurrency = %q, want USD", view.ConvertedCurrency)
	}
}

func TestListFxRates_MissingRequiredFieldIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]string{"from": "EUR", "to": "USD"}) // no policy
	result, err := d.Dispatch(context.Background(), "list_fx_rates", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for a missing required policy")
	}
}

// TestReadTools_NeverAudited proves ADR-0013's "read-tier calls are not
// recorded" for every one of issue #260's tools, not just whoami
// (dispatcher_test.go's own TestDispatcher_ReadTierExecutesDirectlyAndIsNotAudited
// covers the synthetic case; this is the same assertion over the real
// ones).
func TestReadTools_NeverAudited(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	callTool(t, svc, "get_account_balances", nil)
	callTool(t, svc, "list_transactions", nil)
	callTool(t, svc, "get_category_breakdown", map[string]any{"currency": "USD", "policy": "current"})
	callTool(t, svc, "get_budget_actuals", map[string]any{"budget_id": fixture.budgetID})
	callTool(t, svc, "get_budget_history", map[string]any{"budget_id": fixture.budgetID})
	callTool(t, svc, "list_fx_rates", map[string]any{"from": "EUR", "to": "USD", "policy": "current"})

	audit, err := svc.ListMCPToolCalls(context.Background(), app.ListMCPToolCallsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListMCPToolCalls: %v", err)
	}
	if len(audit.Calls) != 0 {
		t.Errorf("audit rows = %d, want 0 (ADR-0013: read-tier calls are never recorded): %+v", len(audit.Calls), audit.Calls)
	}
}

// TestReadTools_AllRegisteredAsReadTier proves every issue #260 tool is
// registered read-tier (readOnlyHint: true, over a real tools/list call)
// — server_test.go's own TestServer_WhoAmIOverInMemoryTransport checks
// this for whoami alone; this extends it to the rest.
func TestReadTools_AllRegisteredAsReadTier(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}

	want := []string{
		"get_account_balances",
		"list_transactions",
		"get_category_breakdown",
		"get_budget_actuals",
		"get_budget_history",
		"list_fx_rates",
	}
	for _, name := range want {
		def, ok := d.tools[name]
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		if def.Tier != ports.MCPToolTierRead {
			t.Errorf("tool %q tier = %q, want read", name, def.Tier)
		}
	}
}
