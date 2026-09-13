package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/ports"
)

// TestServer_WhoAmIOverInMemoryTransport proves the whole stack end to
// end (issue #259's "prove the transport end-to-end"): a real
// sdkmcp.Client, talking to a real sdkmcp.Server built by
// Dispatcher.BuildServer with every production tool from Tools()
// registered, over the SDK's own in-memory transport — the same
// client/server wiring `bodger mcp` uses over stdio, minus the process
// boundary.
func TestServer_WhoAmIOverInMemoryTransport(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))

	dispatcher := NewDispatcher(svc, false, nil)
	for _, def := range Tools() {
		dispatcher.Register(def)
	}
	server := dispatcher.BuildServer(&sdkmcp.Implementation{Name: "bodger-test", Version: "test"})

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	defer serverSession.Close()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var whoami *sdkmcp.Tool
	for _, tl := range tools.Tools {
		if tl.Name == "whoami" {
			whoami = tl
		}
	}
	if whoami == nil {
		t.Fatalf("tools/list did not include %q: %+v", "whoami", tools.Tools)
	}
	if whoami.Annotations == nil || !whoami.Annotations.ReadOnlyHint {
		t.Errorf("whoami annotations = %+v, want ReadOnlyHint true (ADR-0013: read tier)", whoami.Annotations)
	}

	result, err := clientSession.CallTool(ctx, &sdkmcp.CallToolParams{Name: "whoami"})
	if err != nil {
		t.Fatalf("CallTool(whoami): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(whoami) result.IsError = true: %+v", result)
	}
	if len(result.Content) == 0 {
		t.Fatal("CallTool(whoami) returned no content")
	}
	text, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("CallTool(whoami) content[0] = %T, want *sdkmcp.TextContent", result.Content[0])
	}
	if !strings.Contains(text.Text, ports.SeededUserID) {
		t.Errorf("CallTool(whoami) text = %q, want it to mention the seeded actor %q", text.Text, ports.SeededUserID)
	}
}

// TestServer_DestructiveToolsAbsentWithoutAllowDestructive proves
// ADR-0013's "an agent introspecting the tool list on a server started
// without the flag does not even see them" over a real tools/list call,
// using a synthetic destructive tool (issue #259 ships no real
// destructive tool yet).
func TestServer_DestructiveToolsAbsentWithoutAllowDestructive(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC))

	dispatcher := NewDispatcher(svc, false, nil)
	dispatcher.Register(ToolDef{
		Name:     "synthetic-destroy",
		Tier:     ports.MCPToolTierDestructive,
		Execute:  syntheticExecute(new(int), false),
		Describe: syntheticDescribe(false),
	})
	server := dispatcher.BuildServer(&sdkmcp.Implementation{Name: "bodger-test", Version: "test"})

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	defer serverSession.Close()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tl := range tools.Tools {
		if tl.Name == "synthetic-destroy" {
			t.Fatalf("tools/list included a destructive tool without --allow-destructive: %+v", tl)
		}
	}
}
