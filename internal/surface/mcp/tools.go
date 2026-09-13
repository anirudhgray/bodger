// Package mcp is bodger's MCP (Model Context Protocol) surface (issue
// #259, ADR-0013): `bodger mcp` over stdio, using the official
// github.com/modelcontextprotocol/go-sdk. Like internal/surface/cli and
// internal/surface/http, it reaches the application layer in-process —
// no HTTP round trip, no credential of its own (ADR-0006) — and every
// tool maps one-to-one onto an application-layer method
// (docs/architecture.md §3: "MCP tools may not... touch the repository
// layer or emit SQL").
//
// This file is the tool registry: every tool this binary ships declares
// its name, JSON schema, ADR-0013 tier, and the app-layer call it wraps.
// Registration hands each ToolDef to a Dispatcher (dispatcher.go) rather
// than wiring it to the SDK's server directly — the dispatcher is what
// actually enforces tiering, annotations, the confirmation-token
// protocol, and audit, so a tool cannot ship without them by
// construction (ADR-0013: "Tiering is enforced by one dispatcher, not
// per-tool checks").
package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// ToolExecuteFunc performs a tool's actual effect: svc is the app-layer
// container every tool reaches, actorID is the resolved acting user
// (ADR-0006: always ports.SeededUserID today), and args is the tool
// call's raw arguments (with any confirmation_token already stripped by
// the dispatcher for a destructive-tier tool).
//
// A business-logic failure (validation, not found, ...) must be rendered
// as a *CallToolResult with IsError set — via errorResult (result.go) —
// and returned with a nil error, per the MCP spec's own split between
// tool errors and protocol errors (ADR-0013). A non-nil error return is
// treated as a protocol-level failure, which the dispatcher never
// expects from a well-behaved tool.
type ToolExecuteFunc func(ctx context.Context, svc *app.Service, actorID string, args json.RawMessage) (*sdkmcp.CallToolResult, error)

// ToolDescribeFunc renders a human-readable description of what Execute
// would do, without doing it — ADR-0013's confirmation protocol: "the
// first call to a destructive-tier tool does not execute it. It returns
// a description of the effect... in the same terms the tool's own result
// would report." Required for a destructive-tier ToolDef; ignored for
// read/write tiers.
type ToolDescribeFunc func(ctx context.Context, svc *app.Service, actorID string, args json.RawMessage) (string, error)

// ToolDef is one MCP tool's registration record: name, schema, declared
// tier, and the app-layer call(s) it wraps. Registering a ToolDef with a
// Dispatcher (Dispatcher.Register) never calls Execute or Describe
// directly — the dispatcher decides when either runs, per the tool's
// tier.
type ToolDef struct {
	// Name is the tool's registered MCP name.
	Name string
	// Description is the tool's human-readable description, shown to a
	// client via tools/list.
	Description string
	// Tier is this tool's ADR-0013 classification, which decides
	// whether it's audited, whether it requires --allow-destructive to
	// even register, and which ToolAnnotations the dispatcher sets.
	Tier ports.MCPToolTier
	// InputSchema is the tool's JSON Schema, in any shape that
	// JSON-marshals to a valid schema object (a map[string]any is
	// simplest, and what every tool in this package uses) — see
	// sdkmcp.Tool.InputSchema's own doc comment.
	InputSchema any
	// Execute performs the tool's actual effect. Called directly for
	// read/write tiers; called for a destructive tier only on the
	// second, token-confirmed call.
	Execute ToolExecuteFunc
	// Describe is required (non-nil) for a destructive-tier tool only —
	// Dispatcher.Register panics at registration time if it's missing,
	// since the confirmation protocol has nothing to show an agent on
	// the first call without it. Ignored for read/write tiers.
	Describe ToolDescribeFunc
}

// Tools returns every tool this binary ships in production. Test-only
// synthetic tools used to exercise the dispatcher's confirmation-token
// and audit paths (issue #259's own scope: "tested against synthetic
// tools in this issue, not real ones") are registered directly against a
// Dispatcher in dispatcher_test.go, never listed here.
//
// Every tool below whoami is issue #260's read-tier wiring: one tool per
// existing read-only app-layer method (account balances, transaction
// list/filter, category spending reports, budget actuals & history, FX
// rate lookup), always allowed and never audited (ADR-0013). Write and
// destructive tools are #261/#262, not this file.
//
// Everything from getBalanceTotalsTool onward is issue #267's extended
// read-tier wiring: balance totals/net worth (balances.go's own
// AccountBalances-derived methods), the M5 analytics suite beyond
// #260's plain category-spending report (cash flow, trends, savings
// rate, top transactions, average transaction size, category trends —
// all in analytics.go), and a single-transaction lookup by ID
// (get_transaction.go), as opposed to #260's list_transactions. Same
// wiring-only shape, same read tier, no new app-layer logic.
func Tools() []ToolDef {
	return []ToolDef{
		whoAmITool(),
		getAccountBalancesTool(),
		listTransactionsTool(),
		getCategoryBreakdownTool(),
		getBudgetActualsTool(),
		getBudgetHistoryTool(),
		listFxRatesTool(),
		getBalanceTotalsTool(),
		getNetWorthOverTimeTool(),
		getCashFlowTool(),
		getTrendsTool(),
		getSavingsRateTool(),
		getTopTransactionsTool(),
		getAverageTransactionSizeTool(),
		getCategoryTrendsTool(),
	}
}
