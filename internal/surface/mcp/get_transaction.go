package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// getTransactionArgs is get_transaction's arguments.
type getTransactionArgs struct {
	TransactionRef string `json:"transaction_ref"`
}

// getTransactionTool wires app.Service.GetTransaction -- a single
// transaction fetched by ID, as opposed to list_transactions' own
// list/filter (transactions.go) -- onto a read-tier MCP tool. Reuses
// transactionView/transactionViewFrom from transactions.go rather than
// declaring a second, identical rendering of one transaction.
func getTransactionTool() ToolDef {
	return ToolDef{
		Name:        "get_transaction",
		Description: "Get a single recorded transaction by ID.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"transaction_ref": map[string]any{
					"type":        "string",
					"description": "The transaction's ID.",
				},
			},
			"required":             []string{"transaction_ref"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args getTransactionArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.GetTransaction(ctx, app.GetTransactionQuery{
				ActorID:        actorID,
				TransactionRef: args.TransactionRef,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(transactionViewFrom(result))
		},
	}
}
