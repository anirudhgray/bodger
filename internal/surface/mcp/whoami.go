package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// whoAmITool is issue #259's one trivial, read-tier tool: it proves the
// stdio transport, the dispatcher, and the tool registry work end to end
// without touching any financial data. It takes no arguments and maps
// onto app.Service.WhoAmI, the same "one tool, one app-layer method" rule
// every future tool follows.
func whoAmITool() ToolDef {
	return ToolDef{
		Name:        "whoami",
		Description: "Report the identity bodger's MCP server is acting as.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, _ json.RawMessage) (*sdkmcp.CallToolResult, error) {
			result, err := svc.WhoAmI(ctx, app.WhoAmIQuery{ActorID: actorID})
			if err != nil {
				return errorResult(err), nil
			}
			return textResult(fmt.Sprintf("You are acting as %s.", result.ActorID)), nil
		},
	}
}
