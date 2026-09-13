package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// This file covers issue #267's get_transaction tool: a single
// transaction fetched by ID, distinct from list_transactions' own
// list/filter (read_tools_test.go).

func TestGetTransaction_ReturnsSeededTransaction(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	raw := callTool(t, svc, "get_transaction", map[string]any{"transaction_ref": fixture.transactionID})
	var view transactionView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if view.ID != fixture.transactionID {
		t.Errorf("ID = %q, want %q", view.ID, fixture.transactionID)
	}
	if view.Type != "outflow" {
		t.Errorf("Type = %q, want outflow", view.Type)
	}
	if view.Amount != "42.50" {
		t.Errorf("Amount = %q, want 42.50", view.Amount)
	}
	if view.AccountID != fixture.accountID {
		t.Errorf("AccountID = %q, want %q", view.AccountID, fixture.accountID)
	}
}

func TestGetTransaction_UnknownRefIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]string{"transaction_ref": "does-not-exist"})
	result, err := d.Dispatch(context.Background(), "get_transaction", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for an unknown transaction_ref")
	}
}

func TestGetTransaction_MissingRefIsAToolError(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))

	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}
	raw, _ := json.Marshal(map[string]string{})
	result, err := d.Dispatch(context.Background(), "get_transaction", raw)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for a missing transaction_ref")
	}
}

// TestGetTransaction_NeverAudited proves ADR-0013's "read-tier calls are
// not recorded" for get_transaction, mirroring read_tools_test.go's own
// TestReadTools_NeverAudited for #260's tools.
func TestGetTransaction_NeverAudited(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	fixture := seedReadToolFixture(t, svc)

	callTool(t, svc, "get_transaction", map[string]any{"transaction_ref": fixture.transactionID})

	audit, err := svc.ListMCPToolCalls(context.Background(), app.ListMCPToolCallsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListMCPToolCalls: %v", err)
	}
	if len(audit.Calls) != 0 {
		t.Errorf("audit rows = %d, want 0 (ADR-0013: read-tier calls are never recorded): %+v", len(audit.Calls), audit.Calls)
	}
}

// TestGetTransaction_RegisteredAsReadTier proves get_transaction is
// registered read-tier, mirroring read_tools_test.go's own
// TestReadTools_AllRegisteredAsReadTier for #260's tools.
func TestGetTransaction_RegisteredAsReadTier(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		d.Register(def)
	}

	def, ok := d.tools["get_transaction"]
	if !ok {
		t.Fatalf("tool %q not registered", "get_transaction")
	}
	if def.Tier != ports.MCPToolTierRead {
		t.Errorf("tool %q tier = %q, want read", "get_transaction", def.Tier)
	}
}
