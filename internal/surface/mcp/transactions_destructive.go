package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// deleteTransactionArgs is delete_transaction's arguments -- a single
// transaction ID, the same reference shape edit_transaction's own
// transaction_id field uses (transactions_write.go).
type deleteTransactionArgs struct {
	TransactionID string `json:"transaction_id"`
}

func deleteTransactionInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"transaction_id": map[string]any{
				"type":        "string",
				"description": "The transaction's ID.",
			},
		},
		"required":             []string{"transaction_id"},
		"additionalProperties": false,
	}
}

// describeDeleteTransaction renders delete_transaction's confirmation-token
// description (ADR-0013: "in the same terms the tool's own result would
// report"), by looking the transaction up read-only via
// app.Service.GetTransaction first -- the same lookup EditTransaction and
// DeleteTransaction themselves make internally, exposed here as its own
// read so Describe never has to guess at what it's about to delete.
// GetTransaction's own validation (a blank transaction_id, an unknown ID)
// is relied on rather than repeated here.
func describeDeleteTransaction(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (string, error) {
	var args deleteTransactionArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}

	result, err := svc.GetTransaction(ctx, app.GetTransactionQuery{ActorID: actorID, TransactionRef: args.TransactionID})
	if err != nil {
		return "", err
	}
	view := transactionViewFrom(result)
	return fmt.Sprintf(
		"This will delete the %s transaction %s: %q for %s %s on %s. This is a soft delete -- it can be reversed only by a database-level restore, not by this tool.",
		view.Type, view.ID, view.Description, view.Amount, view.Currency, view.Date,
	), nil
}

// deleteTransactionTool wires app.Service.DeleteTransaction (soft delete,
// ADR-0002) onto a destructive-tier MCP tool.
func deleteTransactionTool() ToolDef {
	return ToolDef{
		Name:        "delete_transaction",
		Description: "Soft-delete a transaction. Requires confirmation: call once to see what would be deleted, then again with the returned confirmation_token to actually delete it.",
		Tier:        ports.MCPToolTierDestructive,
		InputSchema: deleteTransactionInputSchema(),
		Describe:    describeDeleteTransaction,
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args deleteTransactionArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.DeleteTransaction(ctx, app.DeleteTransactionCommand{ActorID: actorID, TransactionRef: args.TransactionID})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(transactionViewFrom(result))
		},
	}
}
