package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// Dispatcher is ADR-0013's "one dispatcher, not per-tool checks": every
// registered tool's call is routed through it, and it — not the tool's
// own handler — is what enforces tiering, sets the MCP spec's tool
// annotations, runs the destructive-tier confirmation protocol, and
// writes the write/destructive audit trail. A tool cannot skip any of
// this by being written differently from its neighbours, because there
// is no path from a registered tool to the app layer that bypasses
// Dispatch.
type Dispatcher struct {
	svc              *app.Service
	logger           *slog.Logger
	allowDestructive bool
	tools            map[string]ToolDef
	tokens           *tokenStore
}

// NewDispatcher constructs a Dispatcher over svc. allowDestructive is
// ADR-0013's --allow-destructive gate: when false, Register silently
// refuses to add any destructive-tier tool at all, so it never appears in
// tools/list. logger may be nil; when non-nil, a failure to write an
// audit row is logged (never returned to the caller — a broken audit
// write must not also break the tool call it's trying to describe).
func NewDispatcher(svc *app.Service, allowDestructive bool, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		svc:              svc,
		logger:           logger,
		allowDestructive: allowDestructive,
		tools:            make(map[string]ToolDef),
		tokens:           newTokenStore(svc.Clock, confirmationTokenTTL),
	}
}

// Register adds def to the dispatcher's tool table.
//
// It panics if def.Tier is MCPToolTierDestructive and def.Describe is
// nil: the confirmation protocol has nothing to show an agent on a
// destructive tool's first call without one, and this is a programming
// error in the tool's own registration, not a runtime condition to
// recover from — the same "fail at construction, not at the first call
// that needed it" shape internal/app.NewService's missingDependency
// panics-via-error for a nil repository.
//
// A destructive-tier tool is otherwise skipped entirely (not merely
// registered-but-refused-at-call-time) when the dispatcher wasn't
// constructed with allowDestructive — ADR-0013: "so an agent
// introspecting the tool list on a server started without the flag does
// not even see them."
func (d *Dispatcher) Register(def ToolDef) {
	if def.Tier == ports.MCPToolTierDestructive {
		if def.Describe == nil {
			panic("mcp: destructive-tier tool " + def.Name + " must declare Describe")
		}
		if !d.allowDestructive {
			return
		}
	}
	d.tools[def.Name] = def
}

// BuildServer constructs the sdkmcp.Server every registered tool is
// wired into, with each call routed through Dispatch and each tool's
// annotations set from its declared tier.
func (d *Dispatcher) BuildServer(impl *sdkmcp.Implementation) *sdkmcp.Server {
	server := sdkmcp.NewServer(impl, nil)
	for name, def := range d.tools {
		server.AddTool(&sdkmcp.Tool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: def.InputSchema,
			Annotations: annotationsForTier(def.Tier),
		}, d.handlerFor(name, def))
	}
	return server
}

// annotationsForTier sets the MCP spec's standard tool annotations from
// tier, per ADR-0013: read -> readOnlyHint: true; write ->
// readOnlyHint: false, destructiveHint: false; destructive ->
// readOnlyHint: false, destructiveHint: true. Kept as the one place this
// mapping is made, so bodger's internal tier and the spec's own
// vocabulary can never drift apart.
func annotationsForTier(tier ports.MCPToolTier) *sdkmcp.ToolAnnotations {
	no, yes := false, true
	switch tier {
	case ports.MCPToolTierRead:
		return &sdkmcp.ToolAnnotations{ReadOnlyHint: true}
	case ports.MCPToolTierWrite:
		return &sdkmcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &no}
	case ports.MCPToolTierDestructive:
		return &sdkmcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &yes}
	default:
		return nil
	}
}

// handlerFor closes over name and def and returns the sdkmcp.ToolHandler
// BuildServer wires up for name — a thin adapter from the SDK's
// ToolHandler shape onto Dispatch, the entry point tests drive directly
// too.
func (d *Dispatcher) handlerFor(name string, def ToolDef) sdkmcp.ToolHandler {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return d.dispatch(ctx, name, def, req.Params.Arguments)
	}
}

// Dispatch routes one call to the registered tool name through this
// dispatcher's tiering, confirmation, and audit logic — the same path
// BuildServer wires every registered tool's handler through. Exported so
// a test can drive the dispatcher directly against a synthetic ToolDef
// (issue #259's own scope: the confirmation-token and audit paths are
// "tested against synthetic tools in this issue, not real ones") without
// a full SDK client/server round trip; production code only ever reaches
// this via BuildServer's handlers.
func (d *Dispatcher) Dispatch(ctx context.Context, name string, args json.RawMessage) (*sdkmcp.CallToolResult, error) {
	def, ok := d.tools[name]
	if !ok {
		return nil, fmt.Errorf("mcp: no such tool %q", name)
	}
	return d.dispatch(ctx, name, def, args)
}

// dispatch is Dispatch and handlerFor's shared implementation. actorID is
// always ports.SeededUserID — bodger's MCP server carries no credential
// of its own (ADR-0006), the same in-process trust boundary the CLI
// already uses.
func (d *Dispatcher) dispatch(ctx context.Context, name string, def ToolDef, rawArgs json.RawMessage) (*sdkmcp.CallToolResult, error) {
	actorID := ports.SeededUserID

	switch def.Tier {
	case ports.MCPToolTierRead:
		return def.Execute(ctx, d.svc, actorID, rawArgs)

	case ports.MCPToolTierWrite:
		result, err := def.Execute(ctx, d.svc, actorID, rawArgs)
		d.recordAudit(ctx, actorID, name, def.Tier, rawArgs, nil, result, err)
		return result, err

	case ports.MCPToolTierDestructive:
		return d.dispatchDestructive(ctx, actorID, name, def, rawArgs)

	default:
		return nil, fmt.Errorf("mcp: tool %q has unrecognised tier %q", name, def.Tier)
	}
}

// dispatchDestructive implements ADR-0013's confirmation-token protocol.
// A call with no confirmation_token issues one and describes the effect
// without executing anything; a call carrying a valid, matching,
// unexpired token executes exactly once, since consume burns the token
// regardless of outcome. Both legs are audited — the issuing call with
// the token it just minted, the confirming call with the token it
// consumed — so mcp_tool_call shows the whole exchange, not just the
// side that actually mutated data.
func (d *Dispatcher) dispatchDestructive(ctx context.Context, actorID, name string, def ToolDef, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
	token, businessArgs, err := splitConfirmationToken(raw)
	if err != nil {
		result := errorResult(fmt.Errorf("mcp: %s: %w", name, err))
		d.recordAudit(ctx, actorID, name, def.Tier, raw, nil, result, nil)
		return result, nil
	}

	if token == "" {
		description, err := def.Describe(ctx, d.svc, actorID, businessArgs)
		if err != nil {
			result := errorResult(err)
			d.recordAudit(ctx, actorID, name, def.Tier, businessArgs, nil, result, nil)
			return result, nil
		}

		issued := d.tokens.issue(name, businessArgs)
		result := textResult(fmt.Sprintf(
			"%s\n\nThis action has not been performed. Call %s again with confirmation_token=%q within %s to proceed.",
			description, name, issued, confirmationTokenTTL,
		))
		result.StructuredContent = map[string]any{
			"confirmation_token": issued,
			"effect":             description,
		}
		d.recordAudit(ctx, actorID, name, def.Tier, businessArgs, &issued, result, nil)
		return result, nil
	}

	if verr := d.tokens.consume(name, token, businessArgs); verr != nil {
		result := errorResult(verr)
		d.recordAudit(ctx, actorID, name, def.Tier, businessArgs, &token, result, nil)
		return result, nil
	}

	result, err := def.Execute(ctx, d.svc, actorID, businessArgs)
	d.recordAudit(ctx, actorID, name, def.Tier, businessArgs, &token, result, err)
	return result, err
}

// recordAudit writes one mcp_tool_call row via the app layer
// (app.Service.RecordMCPToolCall) for every write- or destructive-tier
// call — never for read. A failure to write the audit row is logged, not
// returned: the tool's own result has already been decided by the time
// this runs, and a broken audit write shouldn't also break the call it's
// describing (the same "log, don't fail the caller's result" shape
// internal/surface/cli.closeQuietly uses for a close error).
func (d *Dispatcher) recordAudit(ctx context.Context, actorID, name string, tier ports.MCPToolTier, args json.RawMessage, token *string, result *sdkmcp.CallToolResult, callErr error) {
	if tier == ports.MCPToolTierRead {
		return
	}

	var resultCode string
	if callErr != nil {
		// A non-nil error escaping a tool's Execute is a protocol-level
		// failure the dispatcher never expects from a well-behaved tool
		// (ToolDef's own doc comment) — recorded as Internal rather than
		// silently as "ok", since something genuinely went wrong even
		// though it isn't a business-logic failure a tool reported.
		resultCode = string(errs.Internal)
	} else {
		resultCode = auditResultCode(result)
	}

	if err := d.svc.RecordMCPToolCall(ctx, app.RecordMCPToolCallCommand{
		ActorID:           actorID,
		ToolName:          name,
		Tier:              tier,
		Arguments:         args,
		ConfirmationToken: token,
		Result:            resultCode,
	}); err != nil && d.logger != nil {
		d.logger.Error("mcp: failed to record tool call audit row", "tool", name, "error", err)
	}
}
