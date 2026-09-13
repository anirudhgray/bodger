package app

import (
	"context"
	"encoding/json"

	"github.com/anirudhgray/bodger/internal/ports"
)

// defaultMCPAuditLimit is how many rows ListMCPToolCalls returns when the
// caller doesn't ask for a specific number — `bodger mcp audit`'s own
// default page size.
const defaultMCPAuditLimit = 50

// RecordMCPToolCallCommand is internal/surface/mcp's dispatcher reporting
// one write- or destructive-tier tool invocation it just handled — never
// a user-typed command, and never called by a tool handler itself
// (ADR-0013: "the dispatcher, not the individual tool handler" writes the
// audit record). Arguments is the tool call's raw, as-received JSON;
// RecordMCPToolCall applies mcp_redact.go's redaction list before
// anything is persisted, so the dispatcher never has to know what's safe
// to log.
type RecordMCPToolCallCommand struct {
	// ActorID is the acting user — always ports.SeededUserID today
	// (ADR-0006: no MCP credential of its own), carried as a real field
	// so a future multi-user MCP server needs no schema change.
	ActorID string
	// ToolName is the tool's registered name.
	ToolName string
	// Tier is the tool's declared tier at call time. Never
	// MCPToolTierRead: read-tier calls are never recorded (ADR-0013).
	Tier ports.MCPToolTier
	// Arguments is the call's raw, unredacted JSON arguments (with any
	// confirmation_token already stripped by the dispatcher, since that's
	// call-protocol plumbing, not a tool argument).
	Arguments json.RawMessage
	// ConfirmationToken is the token this call issued or consumed, for a
	// destructive-tier call. Nil for a write-tier call.
	ConfirmationToken *string
	// Result is "ok", or one of internal/platform/errs's registry codes
	// naming why the call failed (ADR-0013's "Tool Execution Errors").
	Result string
}

// RecordMCPToolCall persists one row to mcp_tool_call (ADR-0013,
// migration 00015_add_mcp_tool_call.sql). This is where "what time did
// this happen" is resolved — via the injected clock, once, the same as
// every other timestamp this codebase writes (ADR-0005) — so the mcp
// surface never reads its own copy of "now."
func (s *Service) RecordMCPToolCall(ctx context.Context, cmd RecordMCPToolCallCommand) error {
	if err := requireActorID(cmd.ActorID); err != nil {
		return err
	}

	call := ports.MCPToolCall{
		ID:                s.IDs.NewID(),
		UserID:            cmd.ActorID,
		ToolName:          cmd.ToolName,
		Tier:              cmd.Tier,
		Arguments:         redactMCPArguments(cmd.Arguments),
		ConfirmationToken: cmd.ConfirmationToken,
		Result:            cmd.Result,
		CalledAt:          s.Clock.Now(),
	}
	return s.MCPToolCalls.Create(ctx, cmd.ActorID, call)
}

// ListMCPToolCallsQuery lists actorID's own recorded MCP tool calls,
// most recently called first. Limit defaults to defaultMCPAuditLimit
// when zero or negative.
type ListMCPToolCallsQuery struct {
	ActorID string
	Limit   int
}

// ListMCPToolCallsResult wraps ListMCPToolCalls' rows.
type ListMCPToolCallsResult struct {
	Calls []ports.MCPToolCall
}

// ListMCPToolCalls implements `bodger mcp audit` (ADR-0013) — read-tier
// by construction, since it only ever queries mcp_tool_call.
func (s *Service) ListMCPToolCalls(ctx context.Context, q ListMCPToolCallsQuery) (ListMCPToolCallsResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ListMCPToolCallsResult{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultMCPAuditLimit
	}
	calls, err := s.MCPToolCalls.List(ctx, q.ActorID, limit)
	if err != nil {
		return ListMCPToolCallsResult{}, err
	}
	return ListMCPToolCallsResult{Calls: calls}, nil
}
