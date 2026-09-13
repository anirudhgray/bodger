package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// writeToolFixture extends readToolFixture with a second account, so
// record_transfer's tests have two distinct accounts to move money
// between.
type writeToolFixture struct {
	readToolFixture
	secondAccountID string
}

func seedWriteToolFixture(t *testing.T, svc *app.Service) writeToolFixture {
	t.Helper()
	base := seedReadToolFixture(t, svc)

	savings, err := svc.CreateAccount(context.Background(), app.CreateAccountCommand{
		ActorID:  ports.SeededUserID,
		Name:     "Savings",
		Kind:     "bank",
		Currency: "USD",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	return writeToolFixture{readToolFixture: base, secondAccountID: savings.Account.ID()}
}

func TestRecordOutflow_CreatesTransaction(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "record_outflow", map[string]any{
		"account":     fixture.accountID,
		"category":    fixture.categoryID,
		"amount":      "12.34",
		"description": "Coffee",
	})
	var view transactionView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Type != "outflow" {
		t.Errorf("Type = %q, want outflow", view.Type)
	}
	if view.Amount != "12.34" {
		t.Errorf("Amount = %q, want 12.34", view.Amount)
	}
	if view.AccountID != fixture.accountID {
		t.Errorf("AccountID = %q, want %q", view.AccountID, fixture.accountID)
	}
	if view.CategoryID != fixture.categoryID {
		t.Errorf("CategoryID = %q, want %q", view.CategoryID, fixture.categoryID)
	}
}

func TestRecordOutflow_UnknownAccountIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]any{
		"account":     "does-not-exist",
		"amount":      "12.34",
		"description": "Coffee",
	})
	result, err := d.Dispatch(context.Background(), "record_outflow", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unknown account")
	}
}

func TestRecordInflow_CreatesTransaction(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "record_inflow", map[string]any{
		"account":     fixture.accountID,
		"amount":      "500.00",
		"description": "Paycheck",
	})
	var view transactionView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Type != "inflow" {
		t.Errorf("Type = %q, want inflow", view.Type)
	}
	if view.Amount != "500.00" {
		t.Errorf("Amount = %q, want 500.00", view.Amount)
	}
}

func TestRecordInflow_InvalidAmountIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]any{
		"account":     fixture.accountID,
		"amount":      "not-a-number",
		"description": "Paycheck",
	})
	result, err := d.Dispatch(context.Background(), "record_inflow", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unparseable amount")
	}
}

func TestRecordTransfer_MovesMoneyBetweenAccounts(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedWriteToolFixture(t, svc)

	raw := callTool(t, svc, "record_transfer", map[string]any{
		"from_account": fixture.accountID,
		"to_account":   fixture.secondAccountID,
		"amount":       "100.00",
		"description":  "Move to savings",
	})
	var view transactionView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Type != "transfer" {
		t.Errorf("Type = %q, want transfer", view.Type)
	}
	if view.FromAccountID != fixture.accountID {
		t.Errorf("FromAccountID = %q, want %q", view.FromAccountID, fixture.accountID)
	}
	if view.ToAccountID != fixture.secondAccountID {
		t.Errorf("ToAccountID = %q, want %q", view.ToAccountID, fixture.secondAccountID)
	}
	if view.Amount != "100.00" {
		t.Errorf("Amount = %q, want 100.00", view.Amount)
	}
}

func TestRecordTransfer_SameAccountIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]any{
		"from_account": fixture.accountID,
		"to_account":   fixture.accountID,
		"amount":       "100.00",
		"description":  "Move to self",
	})
	result, err := d.Dispatch(context.Background(), "record_transfer", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for a transfer to the same account")
	}
}

func TestEditTransaction_ReplacesFields(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "edit_transaction", map[string]any{
		"transaction_id": fixture.transactionID,
		"account":        fixture.accountID,
		"category":       fixture.categoryID,
		"amount":         "99.99",
		"description":    "Farmers market (corrected)",
	})
	var view transactionView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Amount != "99.99" {
		t.Errorf("Amount = %q, want 99.99", view.Amount)
	}
	if view.Description != "Farmers market (corrected)" {
		t.Errorf("Description = %q, want %q", view.Description, "Farmers market (corrected)")
	}
}

func TestEditTransaction_UnknownTransactionIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]any{
		"transaction_id": "does-not-exist",
		"account":        fixture.accountID,
		"amount":         "99.99",
		"description":    "Doesn't matter",
	})
	result, err := d.Dispatch(context.Background(), "edit_transaction", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unknown transaction_id")
	}
}

func TestCreateBudget_CreatesBudgetWithLines(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	category, err := svc.CreateCategory(context.Background(), app.CreateCategoryCommand{
		ActorID: ports.SeededUserID, Name: "Rent", Kind: "expense",
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	raw := callTool(t, svc, "create_budget", map[string]any{
		"name":      "October",
		"currency":  "USD",
		"starts_on": "2026-10-01",
		"lines": []map[string]any{
			{"category": category.Category.ID(), "amount": "1200.00"},
		},
	})
	var view budgetView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Name != "October" {
		t.Errorf("Name = %q, want October", view.Name)
	}
	if len(view.Lines) != 1 {
		t.Fatalf("Lines = %d, want 1: %+v", len(view.Lines), view)
	}
	if view.Lines[0].Amount != "1200.00" {
		t.Errorf("Lines[0].Amount = %q, want 1200.00", view.Lines[0].Amount)
	}
}

func TestCreateBudget_MissingNameIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]any{"currency": "USD"})
	result, err := d.Dispatch(context.Background(), "create_budget", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for a missing name")
	}
}

func TestUpdateBudget_ReplacesNameAndStartsOn(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "update_budget", map[string]any{
		"budget_id": fixture.budgetID,
		"name":      "September (renamed)",
		"starts_on": "2026-09-01",
	})
	var view budgetView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Name != "September (renamed)" {
		t.Errorf("Name = %q, want %q", view.Name, "September (renamed)")
	}
}

func TestUpdateBudget_UnknownBudgetIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]any{"budget_id": "does-not-exist", "name": "Whatever"})
	result, err := d.Dispatch(context.Background(), "update_budget", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unknown budget_id")
	}
}

func TestAddBudgetLine_AddsNewLine(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)
	category, err := svc.CreateCategory(context.Background(), app.CreateCategoryCommand{
		ActorID: ports.SeededUserID, Name: "Transport", Kind: "expense",
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	raw := callTool(t, svc, "add_budget_line", map[string]any{
		"budget_id": fixture.budgetID,
		"category":  category.Category.ID(),
		"amount":    "50.00",
	})
	var view budgetView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(view.Lines) != 2 {
		t.Fatalf("Lines = %d, want 2: %+v", len(view.Lines), view)
	}
}

func TestAddBudgetLine_DuplicateCategoryIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]any{
		"budget_id": fixture.budgetID,
		"category":  fixture.categoryID, // seedReadToolFixture already gave this category a line
		"amount":    "10.00",
	})
	result, err := d.Dispatch(context.Background(), "add_budget_line", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for a duplicate category line")
	}
}

func TestUpdateBudgetLine_ChangesAmount(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	getRaw := callTool(t, svc, "get_budget_actuals", map[string]any{"budget_id": fixture.budgetID})
	var actuals budgetActualsView
	if err := json.Unmarshal(getRaw, &actuals); err != nil {
		t.Fatalf("unmarshal get_budget_actuals result: %v", err)
	}
	lineID := actuals.Lines[0].LineID

	raw := callTool(t, svc, "update_budget_line", map[string]any{
		"budget_id": fixture.budgetID,
		"line_id":   lineID,
		"amount":    "750.00",
	})
	var view budgetView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Lines[0].Amount != "750.00" {
		t.Errorf("Lines[0].Amount = %q, want 750.00", view.Lines[0].Amount)
	}
}

func TestUpdateBudgetLine_UnknownLineIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]any{
		"budget_id": fixture.budgetID,
		"line_id":   "does-not-exist",
		"amount":    "10.00",
	})
	result, err := d.Dispatch(context.Background(), "update_budget_line", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unknown line_id")
	}
}

func TestRemoveBudgetLine_RemovesLine(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	getRaw := callTool(t, svc, "get_budget_actuals", map[string]any{"budget_id": fixture.budgetID})
	var actuals budgetActualsView
	if err := json.Unmarshal(getRaw, &actuals); err != nil {
		t.Fatalf("unmarshal get_budget_actuals result: %v", err)
	}
	lineID := actuals.Lines[0].LineID

	raw := callTool(t, svc, "remove_budget_line", map[string]any{
		"budget_id": fixture.budgetID,
		"line_id":   lineID,
	})
	var view budgetView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(view.Lines) != 0 {
		t.Errorf("Lines = %d, want 0: %+v", len(view.Lines), view)
	}
}

func TestRemoveBudgetLine_UnknownLineIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]any{
		"budget_id": fixture.budgetID,
		"line_id":   "does-not-exist",
	})
	result, err := d.Dispatch(context.Background(), "remove_budget_line", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unknown line_id")
	}
}

func TestArchiveBudget_ArchivesBudget(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "archive_budget", map[string]any{"budget_id": fixture.budgetID})
	var view budgetView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !view.Archived {
		t.Errorf("Archived = false, want true")
	}
	if view.ArchivedAt == "" {
		t.Errorf("ArchivedAt is empty, want set")
	}
}

func TestArchiveBudget_UnknownBudgetIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]any{"budget_id": "does-not-exist"})
	result, err := d.Dispatch(context.Background(), "archive_budget", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unknown budget_id")
	}
}

// TestWriteTools_AreAuditedAndRegisteredAsWriteTier proves every issue
// #261 tool is both registered write-tier (readOnlyHint: false,
// destructiveHint: false) and recorded in mcp_tool_call (ADR-0013: "write
// and destructive activity is recorded... not read").
func TestWriteTools_AreAuditedAndRegisteredAsWriteTier(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}

	want := []string{
		"record_outflow",
		"record_inflow",
		"record_transfer",
		"edit_transaction",
		"create_budget",
		"update_budget",
		"add_budget_line",
		"update_budget_line",
		"remove_budget_line",
		"archive_budget",
	}
	for _, name := range want {
		def, ok := d.tools[name]
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		if def.Tier != ports.MCPToolTierWrite {
			t.Errorf("tool %q tier = %q, want write", name, def.Tier)
		}
	}

	callTool(t, svc, "record_outflow", map[string]any{
		"account": fixture.accountID, "amount": "1.00", "description": "audit check",
	})

	audit, err := svc.ListMCPToolCalls(context.Background(), app.ListMCPToolCallsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListMCPToolCalls: %v", err)
	}
	if len(audit.Calls) != 1 {
		t.Fatalf("audit rows = %d, want 1 (ADR-0013: write-tier calls are recorded): %+v", len(audit.Calls), audit.Calls)
	}
	if audit.Calls[0].ToolName != "record_outflow" {
		t.Errorf("ToolName = %q, want record_outflow", audit.Calls[0].ToolName)
	}
	if audit.Calls[0].Result != "ok" {
		t.Errorf("Result = %q, want ok", audit.Calls[0].Result)
	}
}
