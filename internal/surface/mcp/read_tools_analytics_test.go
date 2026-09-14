package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// This file covers issue #267's extended read-tier analytics tools: one
// success path and at least one validation-error path per tool, the same
// style read_tools_test.go's own #260 coverage uses, reusing
// seedReadToolFixture/callTool from that file (same package).
// get_transaction, #267's other tool, is covered by
// get_transaction_test.go.

func TestGetBalanceTotals_ReturnsOverall(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_balance_totals", map[string]any{"currency": "USD", "policy": "current"})
	var view balanceTotalsView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Overall != "-42.50" {
		t.Errorf("Overall = %q, want -42.50 (one 42.50 outflow, no opening balance)", view.Overall)
	}
	if view.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", view.Currency)
	}
}

func TestGetBalanceTotals_InvalidPolicyIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]string{"currency": "USD", "policy": "not-a-policy"})
	result, err := d.Dispatch(context.Background(), "get_balance_totals", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unrecognised policy")
	}
}

func TestGetNetWorthOverTime_ReturnsPoints(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_net_worth_over_time", map[string]any{
		"from":     "2026-09-01",
		"to":       "2026-09-30",
		"currency": "USD",
		"policy":   "current",
	})
	var view netWorthOverTimeView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(view.Points) != 1 {
		t.Fatalf("Points = %d, want 1 (month granularity over a single month): %+v", len(view.Points), view)
	}
	if view.Points[0].Amount != "-42.50" {
		t.Errorf("Points[0].Amount = %q, want -42.50", view.Points[0].Amount)
	}
}

func TestGetNetWorthOverTime_MissingToIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]string{"from": "2026-09-01", "currency": "USD", "policy": "current"})
	result, err := d.Dispatch(context.Background(), "get_net_worth_over_time", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for a missing \"to\"")
	}
}

func TestGetCashFlow_ReturnsPoint(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_cash_flow", map[string]any{
		"from":     "2026-09-01",
		"to":       "2026-09-30",
		"currency": "USD",
		"policy":   "current",
	})
	var view cashFlowView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(view.Points) != 1 {
		t.Fatalf("Points = %d, want 1: %+v", len(view.Points), view)
	}
	if view.Points[0].Outflow != "42.50" {
		t.Errorf("Points[0].Outflow = %q, want 42.50", view.Points[0].Outflow)
	}
}

func TestGetCashFlow_InvalidGranularityIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]string{"currency": "USD", "policy": "current", "granularity": "fortnight"})
	result, err := d.Dispatch(context.Background(), "get_cash_flow", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unrecognised granularity")
	}
}

func TestGetTrends_ReturnsCurrentAndPrevious(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_trends", map[string]any{"currency": "USD", "policy": "current"})
	var view trendsView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Current.Outflow != "42.50" {
		t.Errorf("Current.Outflow = %q, want 42.50 (frozen clock is within September 2026)", view.Current.Outflow)
	}
}

func TestGetTrends_InvalidPolicyIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]string{"currency": "USD", "policy": "not-a-policy"})
	result, err := d.Dispatch(context.Background(), "get_trends", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unrecognised policy")
	}
}

func TestGetSavingsRate_ReturnsNegativeRate(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_savings_rate", map[string]any{
		"date_from": "2026-09-01",
		"date_to":   "2026-09-30",
		"currency":  "USD",
		"policy":    "current",
	})
	var view savingsRateView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Income != "0.00" {
		t.Errorf("Income = %q, want 0.00 (no inflow seeded)", view.Income)
	}
	if view.Rate != nil {
		t.Errorf("Rate = %v, want nil (income is zero, an undefined ratio)", *view.Rate)
	}
}

func TestGetSavingsRate_InvalidPolicyIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]string{"currency": "USD", "policy": "not-a-policy"})
	result, err := d.Dispatch(context.Background(), "get_savings_rate", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unrecognised policy")
	}
}

func TestGetTopTransactions_ReturnsSeededOutflow(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_top_transactions", map[string]any{"currency": "USD", "policy": "current"})
	var view topTransactionsView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(view.Rows) != 1 {
		t.Fatalf("Rows = %d, want 1: %+v", len(view.Rows), view)
	}
	if view.Rows[0].TransactionID != fixture.transactionID {
		t.Errorf("Rows[0].TransactionID = %q, want %q", view.Rows[0].TransactionID, fixture.transactionID)
	}
	if view.Rows[0].Amount != "-42.50" {
		t.Errorf("Rows[0].Amount = %q, want -42.50 (signed, an outflow)", view.Rows[0].Amount)
	}
}

func TestGetTopTransactions_LimitTooLargeIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]any{"currency": "USD", "policy": "current", "limit": 500})
	result, err := d.Dispatch(context.Background(), "get_top_transactions", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for a limit above the maximum")
	}
}

func TestGetAverageTransactionSize_ReturnsOverallAverage(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_average_transaction_size", map[string]any{"currency": "USD", "policy": "current"})
	var view averageTransactionSizeView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.Overall.Count != 1 {
		t.Fatalf("Overall.Count = %d, want 1: %+v", view.Overall.Count, view)
	}
	if view.Overall.Average != "42.50" {
		t.Errorf("Overall.Average = %q, want 42.50 (magnitude, not signed)", view.Overall.Average)
	}
}

func TestGetAverageTransactionSize_InvalidPolicyIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]string{"currency": "USD", "policy": "not-a-policy"})
	result, err := d.Dispatch(context.Background(), "get_average_transaction_size", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unrecognised policy")
	}
}

func TestGetCategoryTrends_ReportsCurrentSpending(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_category_trends", map[string]any{"currency": "USD", "policy": "current"})
	var view categoryTrendsView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(view.Rows) != 1 {
		t.Fatalf("Rows = %d, want 1: %+v", len(view.Rows), view)
	}
	if view.Rows[0].Category != "Groceries" {
		t.Errorf("Rows[0].Category = %q, want Groceries", view.Rows[0].Category)
	}
	if view.Rows[0].Current.Spending != "42.50" {
		t.Errorf("Rows[0].Current.Spending = %q, want 42.50", view.Rows[0].Current.Spending)
	}
}

func TestGetCategoryTrends_InvalidGranularityIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]string{"currency": "USD", "policy": "current", "granularity": "fortnight"})
	result, err := d.Dispatch(context.Background(), "get_category_trends", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unrecognised granularity")
	}
}

// TestAnalyticsReadTools_NeverAudited proves ADR-0013's "read-tier calls
// are not recorded" for issue #267's analytics tools, mirroring
// read_tools_test.go's own TestReadTools_NeverAudited for #260's.
func TestAnalyticsReadTools_NeverAudited(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	seedReadToolFixture(t, svc)

	callTool(t, svc, "get_balance_totals", map[string]any{"currency": "USD", "policy": "current"})
	callTool(t, svc, "get_net_worth_over_time", map[string]any{"from": "2026-09-01", "to": "2026-09-30", "currency": "USD", "policy": "current"})
	callTool(t, svc, "get_cash_flow", map[string]any{"currency": "USD", "policy": "current"})
	callTool(t, svc, "get_trends", map[string]any{"currency": "USD", "policy": "current"})
	callTool(t, svc, "get_savings_rate", map[string]any{"currency": "USD", "policy": "current"})
	callTool(t, svc, "get_top_transactions", map[string]any{"currency": "USD", "policy": "current"})
	callTool(t, svc, "get_average_transaction_size", map[string]any{"currency": "USD", "policy": "current"})
	callTool(t, svc, "get_category_trends", map[string]any{"currency": "USD", "policy": "current"})

	audit, err := svc.ListMCPToolCalls(context.Background(), app.ListMCPToolCallsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListMCPToolCalls: %v", err)
	}
	if len(audit.Calls) != 0 {
		t.Errorf("audit rows = %d, want 0 (ADR-0013: read-tier calls are never recorded): %+v", len(audit.Calls), audit.Calls)
	}
}

// TestAnalyticsReadTools_AllRegisteredAsReadTier proves every issue #267
// analytics tool is registered read-tier, mirroring read_tools_test.go's
// own TestReadTools_AllRegisteredAsReadTier for #260's.
func TestAnalyticsReadTools_AllRegisteredAsReadTier(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}

	want := []string{
		"get_balance_totals",
		"get_net_worth_over_time",
		"get_cash_flow",
		"get_trends",
		"get_savings_rate",
		"get_top_transactions",
		"get_average_transaction_size",
		"get_category_trends",
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
