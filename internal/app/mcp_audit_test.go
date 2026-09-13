package app_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func TestWhoAmI_ResolvesSeededActor(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC), "UTC")

	result, err := svc.WhoAmI(context.Background(), app.WhoAmIQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("WhoAmI: %v", err)
	}
	if result.ActorID != ports.SeededUserID {
		t.Errorf("ActorID = %q, want %q", result.ActorID, ports.SeededUserID)
	}
}

func TestWhoAmI_RequiresActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC), "UTC")

	_, err := svc.WhoAmI(context.Background(), app.WhoAmIQuery{})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestRecordMCPToolCall_PersistsAndResolvesCalledAtFromClock(t *testing.T) {
	frozenAt := time.Date(2026, time.September, 13, 12, 30, 0, 0, time.UTC)
	svc := newTestService(t, frozenAt, "UTC")

	if err := svc.RecordMCPToolCall(context.Background(), app.RecordMCPToolCallCommand{
		ActorID:   ports.SeededUserID,
		ToolName:  "record_outflow",
		Tier:      ports.MCPToolTierWrite,
		Arguments: json.RawMessage(`{"amount":"10.00"}`),
		Result:    "ok",
	}); err != nil {
		t.Fatalf("RecordMCPToolCall: %v", err)
	}

	result, err := svc.ListMCPToolCalls(context.Background(), app.ListMCPToolCallsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListMCPToolCalls: %v", err)
	}
	if len(result.Calls) != 1 {
		t.Fatalf("Calls = %d, want 1", len(result.Calls))
	}
	call := result.Calls[0]
	if call.ToolName != "record_outflow" || call.Tier != ports.MCPToolTierWrite || call.Result != "ok" {
		t.Errorf("call = %+v", call)
	}
	if !call.CalledAt.Equal(frozenAt) {
		t.Errorf("CalledAt = %v, want the injected clock's instant %v (never a wall-clock read)", call.CalledAt, frozenAt)
	}
	if call.ID == "" {
		t.Error("ID is empty, want a generated ID")
	}
}

func TestRecordMCPToolCall_RedactsSensitiveArgumentsBeforePersisting(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC), "UTC")

	if err := svc.RecordMCPToolCall(context.Background(), app.RecordMCPToolCallCommand{
		ActorID:   ports.SeededUserID,
		ToolName:  "some_tool",
		Tier:      ports.MCPToolTierWrite,
		Arguments: json.RawMessage(`{"note":"hello","nested":{"password":"hunter2"}}`),
		Result:    "ok",
	}); err != nil {
		t.Fatalf("RecordMCPToolCall: %v", err)
	}

	result, err := svc.ListMCPToolCalls(context.Background(), app.ListMCPToolCallsQuery{ActorID: ports.SeededUserID})
	if err != nil {
		t.Fatalf("ListMCPToolCalls: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal([]byte(result.Calls[0].Arguments), &stored); err != nil {
		t.Fatalf("unmarshal stored arguments: %v", err)
	}
	nested, ok := stored["nested"].(map[string]any)
	if !ok {
		t.Fatalf("stored nested = %v, want a map", stored["nested"])
	}
	if nested["password"] != "[redacted]" {
		t.Errorf("nested password = %v, want it redacted at depth", nested["password"])
	}
	if stored["note"] != "hello" {
		t.Errorf("note = %v, want it untouched", stored["note"])
	}
}

func TestListMCPToolCalls_ScopedToActor(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC), "UTC")

	if err := svc.RecordMCPToolCall(context.Background(), app.RecordMCPToolCallCommand{
		ActorID: ports.SeededUserID, ToolName: "t", Tier: ports.MCPToolTierWrite, Result: "ok",
	}); err != nil {
		t.Fatalf("RecordMCPToolCall: %v", err)
	}

	result, err := svc.ListMCPToolCalls(context.Background(), app.ListMCPToolCallsQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListMCPToolCalls: %v", err)
	}
	if len(result.Calls) != 0 {
		t.Errorf("ListMCPToolCalls(other actor) = %d rows, want 0", len(result.Calls))
	}
}
