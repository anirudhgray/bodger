package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// testFactory adapts an already-built *app.Service into the
// ServiceFactory shape Register expects, for a test that wants a single
// shared Service across multiple command invocations (mirroring
// mustAccountFixtureAs and friends elsewhere in this codebase).
func testFactory(svc *app.Service) ServiceFactory {
	return func(context.Context) (*app.Service, func() error, error) {
		return svc, func() error { return nil }, nil
	}
}

func runRoot(t *testing.T, factory ServiceFactory, args ...string) (stdout string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "bodger", SilenceUsage: true, SilenceErrors: true}
	Register(root, factory, nil)

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), err
}

func TestMCPAuditCmd_ListsRecordedCalls(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	factory := testFactory(svc)

	if err := svc.RecordMCPToolCall(context.Background(), app.RecordMCPToolCallCommand{
		ActorID:   ports.SeededUserID,
		ToolName:  "synthetic-write",
		Tier:      ports.MCPToolTierWrite,
		Arguments: json.RawMessage(`{"amount":"5.00"}`),
		Result:    "ok",
	}); err != nil {
		t.Fatalf("RecordMCPToolCall: %v", err)
	}

	stdout, err := runRoot(t, factory, "mcp", "audit")
	if err != nil {
		t.Fatalf("run mcp audit: %v", err)
	}
	if !strings.Contains(stdout, "synthetic-write") {
		t.Errorf("mcp audit output = %q, want it to mention the recorded tool", stdout)
	}
	if !strings.Contains(stdout, "ok") {
		t.Errorf("mcp audit output = %q, want it to mention the recorded result", stdout)
	}
}

func TestMCPAuditCmd_NoCallsYet(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))
	factory := testFactory(svc)

	stdout, err := runRoot(t, factory, "mcp", "audit")
	if err != nil {
		t.Fatalf("run mcp audit: %v", err)
	}
	if !strings.Contains(stdout, "No MCP tool calls recorded yet.") {
		t.Errorf("mcp audit output = %q, want the empty-state message", stdout)
	}
}

// TestMCPCmd_AllowDestructiveFlagIsRegistered guards against the
// --allow-destructive flag silently disappearing from `bodger mcp` — a
// full RunE invocation would block on stdio, so this only checks the
// flag exists and defaults to false.
func TestMCPCmd_AllowDestructiveFlagIsRegistered(t *testing.T) {
	root := &cobra.Command{Use: "bodger"}
	Register(root, testFactory(nil), nil)

	mcpCmd, _, err := root.Find([]string{"mcp"})
	if err != nil {
		t.Fatalf("root.Find(mcp): %v", err)
	}
	flag := mcpCmd.Flags().Lookup("allow-destructive")
	if flag == nil {
		t.Fatal("mcp command has no --allow-destructive flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--allow-destructive default = %q, want %q (disabled by default, ADR-0013)", flag.DefValue, "false")
	}
}
