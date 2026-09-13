package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// syntheticExecute is a tiny ToolExecuteFunc for the synthetic tools this
// file registers directly against a Dispatcher — issue #259's own scope:
// the confirmation-token and audit paths are exercised against synthetic
// tools here, never a real financial one (those are #260/#261/#262).
func syntheticExecute(calls *int, fail bool) ToolExecuteFunc {
	return func(_ context.Context, _ *app.Service, _ string, _ json.RawMessage) (*sdkmcp.CallToolResult, error) {
		*calls++
		if fail {
			return errorResult(errs.New(errs.InvalidInput).Explain("synthetic failure")), nil
		}
		return textResult("done"), nil
	}
}

func syntheticDescribe(fail bool) ToolDescribeFunc {
	return func(_ context.Context, _ *app.Service, _ string, args json.RawMessage) (string, error) {
		if fail {
			return "", errs.New(errs.InvalidInput).Explain("bad describe args")
		}
		return fmt.Sprintf("would act on %s", string(args)), nil
	}
}

func TestDispatcher_ReadTierExecutesDirectlyAndIsNotAudited(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, false, nil)

	var calls int
	d.Register(ToolDef{
		Name:    "synthetic-read",
		Tier:    ports.MCPToolTierRead,
		Execute: syntheticExecute(&calls, false),
	})

	result, err := d.Dispatch(context.Background(), "synthetic-read", nil)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.IsError {
		t.Errorf("result.IsError = true, want false")
	}
	if calls != 1 {
		t.Errorf("Execute called %d times, want 1", calls)
	}

	audit, err := svc.ListMCPToolCalls(context.Background(), app.ListMCPToolCallsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListMCPToolCalls: %v", err)
	}
	if len(audit.Calls) != 0 {
		t.Errorf("read-tier call was audited: %+v, want no rows (ADR-0013: read-tier calls are never recorded)", audit.Calls)
	}
}

func TestDispatcher_WriteTierExecutesAndAudits(t *testing.T) {
	svc, clk := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, false, nil)

	var calls int
	d.Register(ToolDef{
		Name:    "synthetic-write",
		Tier:    ports.MCPToolTierWrite,
		Execute: syntheticExecute(&calls, false),
	})

	result, err := d.Dispatch(context.Background(), "synthetic-write", json.RawMessage(`{"amount":"12.00"}`))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.IsError {
		t.Errorf("result.IsError = true, want false")
	}
	if calls != 1 {
		t.Errorf("Execute called %d times, want 1", calls)
	}

	audit, err := svc.ListMCPToolCalls(context.Background(), app.ListMCPToolCallsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListMCPToolCalls: %v", err)
	}
	if len(audit.Calls) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(audit.Calls))
	}
	row := audit.Calls[0]
	if row.ToolName != "synthetic-write" || row.Tier != ports.MCPToolTierWrite || row.Result != "ok" {
		t.Errorf("audit row = %+v", row)
	}
	if row.ConfirmationToken != nil {
		t.Errorf("ConfirmationToken = %v, want nil for a write-tier call", *row.ConfirmationToken)
	}
	if row.Arguments != `{"amount":"12.00"}` {
		t.Errorf("Arguments = %q, want the call's own arguments", row.Arguments)
	}
	if !row.CalledAt.Equal(clk.Now()) {
		t.Errorf("CalledAt = %v, want the injected clock's instant %v", row.CalledAt, clk.Now())
	}
}

func TestDispatcher_WriteTierFailure_AuditsErrorCode(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, false, nil)

	var calls int
	d.Register(ToolDef{
		Name:    "synthetic-write-fails",
		Tier:    ports.MCPToolTierWrite,
		Execute: syntheticExecute(&calls, true),
	})

	result, err := d.Dispatch(context.Background(), "synthetic-write-fails", nil)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true")
	}

	audit, err := svc.ListMCPToolCalls(context.Background(), app.ListMCPToolCallsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListMCPToolCalls: %v", err)
	}
	if len(audit.Calls) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(audit.Calls))
	}
	if audit.Calls[0].Result != string(errs.InvalidInput) {
		t.Errorf("Result = %q, want %q", audit.Calls[0].Result, errs.InvalidInput)
	}
}

func TestDispatcher_WriteTierRedactsSensitiveArguments(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, false, nil)

	var calls int
	d.Register(ToolDef{
		Name:    "synthetic-write-secret",
		Tier:    ports.MCPToolTierWrite,
		Execute: syntheticExecute(&calls, false),
	})

	_, err := d.Dispatch(context.Background(), "synthetic-write-secret", json.RawMessage(`{"note":"hi","password":"hunter2"}`))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	audit, err := svc.ListMCPToolCalls(context.Background(), app.ListMCPToolCallsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListMCPToolCalls: %v", err)
	}
	if len(audit.Calls) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(audit.Calls))
	}
	var stored map[string]any
	if err := json.Unmarshal([]byte(audit.Calls[0].Arguments), &stored); err != nil {
		t.Fatalf("unmarshal stored arguments: %v", err)
	}
	if stored["password"] != "[redacted]" {
		t.Errorf("stored password = %v, want it redacted", stored["password"])
	}
	if stored["note"] != "hi" {
		t.Errorf("stored note = %v, want it untouched", stored["note"])
	}
}

// TestDispatcher_DestructiveTier_ConfirmationTokenProtocol drives the
// whole confirmation-token exchange ADR-0013 specifies against a
// synthetic destructive tool: the first call describes the effect and
// issues a token without executing anything, and only a second call
// carrying that exact token executes.
func TestDispatcher_DestructiveTier_ConfirmationTokenProtocol(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, true, nil)

	var calls int
	d.Register(ToolDef{
		Name:     "synthetic-destroy",
		Tier:     ports.MCPToolTierDestructive,
		Execute:  syntheticExecute(&calls, false),
		Describe: syntheticDescribe(false),
	})

	args := json.RawMessage(`{"target":"txn-1"}`)

	// First call: no token yet -- must not execute.
	first, err := d.Dispatch(context.Background(), "synthetic-destroy", args)
	if err != nil {
		t.Fatalf("Dispatch (issue): %v", err)
	}
	if calls != 0 {
		t.Fatalf("Execute called %d times after the issuing call, want 0", calls)
	}
	if first.IsError {
		t.Fatalf("issuing call result.IsError = true, want false: %+v", first)
	}
	structured, ok := first.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("issuing call StructuredContent = %T, want map[string]any", first.StructuredContent)
	}
	token, ok := structured["confirmation_token"].(string)
	if !ok || token == "" {
		t.Fatalf("issuing call did not return a confirmation_token: %+v", structured)
	}

	// Second call: correct token, same arguments -- must execute exactly once.
	confirmArgs, err := json.Marshal(map[string]any{"target": "txn-1", "confirmation_token": token})
	if err != nil {
		t.Fatalf("marshal confirm args: %v", err)
	}
	second, err := d.Dispatch(context.Background(), "synthetic-destroy", confirmArgs)
	if err != nil {
		t.Fatalf("Dispatch (confirm): %v", err)
	}
	if second.IsError {
		t.Fatalf("confirming call result.IsError = true, want false: %+v", second)
	}
	if calls != 1 {
		t.Fatalf("Execute called %d times after the confirming call, want 1", calls)
	}

	// The audit trail should show both legs.
	audit, err := svc.ListMCPToolCalls(context.Background(), app.ListMCPToolCallsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListMCPToolCalls: %v", err)
	}
	if len(audit.Calls) != 2 {
		t.Fatalf("audit rows = %d, want 2 (issue + confirm)", len(audit.Calls))
	}
	for _, row := range audit.Calls {
		if row.ConfirmationToken == nil || *row.ConfirmationToken != token {
			t.Errorf("audit row %+v does not carry the issued token %q", row, token)
		}
	}
}

func TestDispatcher_DestructiveTier_RejectsReusedToken(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, true, nil)

	var calls int
	d.Register(ToolDef{
		Name:     "synthetic-destroy",
		Tier:     ports.MCPToolTierDestructive,
		Execute:  syntheticExecute(&calls, false),
		Describe: syntheticDescribe(false),
	})

	issued, err := d.Dispatch(context.Background(), "synthetic-destroy", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Dispatch (issue): %v", err)
	}
	token := issued.StructuredContent.(map[string]any)["confirmation_token"].(string)

	confirmArgs, _ := json.Marshal(map[string]any{"confirmation_token": token})
	if _, err := d.Dispatch(context.Background(), "synthetic-destroy", confirmArgs); err != nil {
		t.Fatalf("Dispatch (first confirm): %v", err)
	}
	if calls != 1 {
		t.Fatalf("Execute called %d times after first confirm, want 1", calls)
	}

	// Reusing the same token a second time must be rejected, not executed
	// again.
	replay, err := d.Dispatch(context.Background(), "synthetic-destroy", confirmArgs)
	if err != nil {
		t.Fatalf("Dispatch (replay): %v", err)
	}
	if !replay.IsError {
		t.Fatalf("replaying a consumed token succeeded, want it rejected: %+v", replay)
	}
	if calls != 1 {
		t.Fatalf("Execute called %d times after replaying a consumed token, want still 1", calls)
	}
}

func TestDispatcher_DestructiveTier_RejectsMismatchedArguments(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, true, nil)

	var calls int
	d.Register(ToolDef{
		Name:     "synthetic-destroy",
		Tier:     ports.MCPToolTierDestructive,
		Execute:  syntheticExecute(&calls, false),
		Describe: syntheticDescribe(false),
	})

	issued, err := d.Dispatch(context.Background(), "synthetic-destroy", json.RawMessage(`{"target":"txn-1"}`))
	if err != nil {
		t.Fatalf("Dispatch (issue): %v", err)
	}
	token := issued.StructuredContent.(map[string]any)["confirmation_token"].(string)

	// Confirming with a different "target" than what was described must
	// be rejected -- ADR-0013's "attached to a call whose arguments don't
	// match what it was issued for."
	confirmArgs, _ := json.Marshal(map[string]any{"target": "txn-2", "confirmation_token": token})
	result, err := d.Dispatch(context.Background(), "synthetic-destroy", confirmArgs)
	if err != nil {
		t.Fatalf("Dispatch (confirm): %v", err)
	}
	if !result.IsError {
		t.Fatalf("confirming with changed arguments succeeded, want it rejected: %+v", result)
	}
	if calls != 0 {
		t.Fatalf("Execute called %d times, want 0 (mismatched arguments must never execute)", calls)
	}
}

func TestDispatcher_DestructiveTier_RejectsExpiredToken(t *testing.T) {
	svc, clk := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, true, nil)

	var calls int
	d.Register(ToolDef{
		Name:     "synthetic-destroy",
		Tier:     ports.MCPToolTierDestructive,
		Execute:  syntheticExecute(&calls, false),
		Describe: syntheticDescribe(false),
	})

	issued, err := d.Dispatch(context.Background(), "synthetic-destroy", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Dispatch (issue): %v", err)
	}
	token := issued.StructuredContent.(map[string]any)["confirmation_token"].(string)

	clk.Advance(confirmationTokenTTL + time.Minute)

	confirmArgs, _ := json.Marshal(map[string]any{"confirmation_token": token})
	result, err := d.Dispatch(context.Background(), "synthetic-destroy", confirmArgs)
	if err != nil {
		t.Fatalf("Dispatch (confirm): %v", err)
	}
	if !result.IsError {
		t.Fatalf("confirming with an expired token succeeded, want it rejected: %+v", result)
	}
	if calls != 0 {
		t.Fatalf("Execute called %d times, want 0 (an expired token must never execute)", calls)
	}
}

func TestDispatcher_DestructiveTier_DescribeFailureIsNotExecutedOrTokenIssued(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, true, nil)

	var calls int
	d.Register(ToolDef{
		Name:     "synthetic-destroy-bad-describe",
		Tier:     ports.MCPToolTierDestructive,
		Execute:  syntheticExecute(&calls, false),
		Describe: syntheticDescribe(true),
	})

	result, err := d.Dispatch(context.Background(), "synthetic-destroy-bad-describe", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !result.IsError {
		t.Fatalf("Describe failure did not produce an error result: %+v", result)
	}
	if calls != 0 {
		t.Fatalf("Execute called %d times, want 0", calls)
	}
}

func TestDispatcher_Register_PanicsOnDestructiveToolWithoutDescribe(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, true, nil)

	defer func() {
		if recover() == nil {
			t.Fatal("Register did not panic for a destructive tool with no Describe")
		}
	}()
	d.Register(ToolDef{
		Name: "synthetic-destroy-no-describe",
		Tier: ports.MCPToolTierDestructive,
		Execute: func(context.Context, *app.Service, string, json.RawMessage) (*sdkmcp.CallToolResult, error) {
			return textResult("done"), nil
		},
	})
}

// TestDispatcher_Register_SkipsDestructiveToolsWithoutAllowDestructive is
// ADR-0013's "refuses to register any destructive-tier tool at all
// unless the server was started with --allow-destructive... so an agent
// introspecting the tool list... does not even see them."
func TestDispatcher_Register_SkipsDestructiveToolsWithoutAllowDestructive(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	d := NewDispatcher(svc, false, nil)

	d.Register(ToolDef{
		Name:     "synthetic-destroy",
		Tier:     ports.MCPToolTierDestructive,
		Execute:  syntheticExecute(new(int), false),
		Describe: syntheticDescribe(false),
	})

	if _, err := d.Dispatch(context.Background(), "synthetic-destroy", nil); err == nil {
		t.Fatal("Dispatch found a destructive tool registered without --allow-destructive, want it absent")
	}

	server := d.BuildServer(&sdkmcp.Implementation{Name: "test", Version: "0"})
	if server == nil {
		t.Fatal("BuildServer returned nil")
	}
}
