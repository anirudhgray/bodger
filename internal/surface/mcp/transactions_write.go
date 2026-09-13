package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// recordEntryArgs is record_outflow's and record_inflow's shared argument
// shape -- one posting against a single account, mirroring
// internal/surface/http's createTransactionRequest field for field (the
// "account"/"category" naming for an ID-or-unique-name reference matches
// this package's own transactionFilterArgs).
type recordEntryArgs struct {
	Account     string   `json:"account"`
	Category    string   `json:"category,omitempty"`
	Amount      string   `json:"amount"`
	Currency    string   `json:"currency,omitempty"`
	Date        string   `json:"date,omitempty"`
	Description string   `json:"description"`
	Notes       string   `json:"notes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// recordEntrySchemaProperties is the JSON Schema "properties" fragment
// matching recordEntryArgs field for field -- shared by record_outflow and
// record_inflow, which differ only in which app-layer method they call and
// which way the money moves.
func recordEntrySchemaProperties() map[string]any {
	return map[string]any{
		"account": map[string]any{
			"type":        "string",
			"description": "The account this affects: an account's ID or unique name.",
		},
		"category": map[string]any{
			"type":        "string",
			"description": "The category this belongs to: a category's ID or unique name.",
		},
		"amount": map[string]any{
			"type":        "string",
			"description": "Always positive; the tool itself says which way the money moves.",
		},
		"currency": map[string]any{
			"type":        "string",
			"description": "Defaults to the account's own currency.",
		},
		"date": map[string]any{
			"type":        "string",
			"description": "When this happened. Defaults to today.",
		},
		"description": map[string]any{
			"type":        "string",
			"description": "What this was for.",
		},
		"notes": map[string]any{
			"type":        "string",
			"description": "A free-text note to attach.",
		},
		"tags": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Tags to attach.",
		},
	}
}

func recordEntryInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           recordEntrySchemaProperties(),
		"required":             []string{"account", "amount", "description"},
		"additionalProperties": false,
	}
}

// recordOutflowTool wires app.Service.RecordOutflow -- money leaving one
// account, optionally attributed to a category -- onto a write-tier MCP
// tool.
func recordOutflowTool() ToolDef {
	return ToolDef{
		Name:        "record_outflow",
		Description: "Record money leaving an account, e.g. a purchase or a bill payment.",
		Tier:        ports.MCPToolTierWrite,
		InputSchema: recordEntryInputSchema(),
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args recordEntryArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
				ActorID:     actorID,
				AccountRef:  args.Account,
				Amount:      args.Amount,
				Currency:    args.Currency,
				CategoryRef: args.Category,
				Date:        args.Date,
				Description: args.Description,
				Notes:       args.Notes,
				Tags:        args.Tags,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(transactionViewFrom(result))
		},
	}
}

// recordInflowTool wires app.Service.RecordInflow -- money arriving in one
// account, optionally attributed to a category -- onto a write-tier MCP
// tool.
func recordInflowTool() ToolDef {
	return ToolDef{
		Name:        "record_inflow",
		Description: "Record money arriving in an account, e.g. a paycheck or a refund.",
		Tier:        ports.MCPToolTierWrite,
		InputSchema: recordEntryInputSchema(),
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args recordEntryArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.RecordInflow(ctx, app.RecordInflowCommand{
				ActorID:     actorID,
				AccountRef:  args.Account,
				Amount:      args.Amount,
				Currency:    args.Currency,
				CategoryRef: args.Category,
				Date:        args.Date,
				Description: args.Description,
				Notes:       args.Notes,
				Tags:        args.Tags,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(transactionViewFrom(result))
		},
	}
}

// recordTransferArgs is record_transfer's arguments, mirroring
// internal/surface/http's createTransferRequest field for field -- there is
// no currency field (the from-leg's amount is always parsed in
// from_account's own currency; see app.RecordTransferCommand's doc
// comment).
type recordTransferArgs struct {
	FromAccount string   `json:"from_account"`
	ToAccount   string   `json:"to_account"`
	Amount      string   `json:"amount"`
	ToAmount    string   `json:"to_amount,omitempty"`
	Date        string   `json:"date,omitempty"`
	Description string   `json:"description"`
	Notes       string   `json:"notes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// recordTransferTool wires app.Service.RecordTransfer -- a movement of
// money between two of the actor's own accounts -- onto a write-tier MCP
// tool.
func recordTransferTool() ToolDef {
	return ToolDef{
		Name:        "record_transfer",
		Description: "Record a transfer of money from one account to another.",
		Tier:        ports.MCPToolTierWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"from_account": map[string]any{
					"type":        "string",
					"description": "The account money leaves: an account's ID or unique name.",
				},
				"to_account": map[string]any{
					"type":        "string",
					"description": "The account money arrives in: an account's ID or unique name.",
				},
				"amount": map[string]any{
					"type":        "string",
					"description": "Always positive, in the from-account's own currency.",
				},
				"to_amount": map[string]any{
					"type":        "string",
					"description": "The to-leg's own amount, in the to-account's own currency. Omit to reuse amount's raw digits, reinterpreted in the to-currency.",
				},
				"date": map[string]any{
					"type":        "string",
					"description": "When this happened. Defaults to today.",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "What this was for.",
				},
				"notes": map[string]any{
					"type":        "string",
					"description": "A free-text note to attach.",
				},
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Tags to attach.",
				},
			},
			"required":             []string{"from_account", "to_account", "amount", "description"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args recordTransferArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.RecordTransfer(ctx, app.RecordTransferCommand{
				ActorID:        actorID,
				FromAccountRef: args.FromAccount,
				ToAccountRef:   args.ToAccount,
				Amount:         args.Amount,
				ToAmount:       args.ToAmount,
				Date:           args.Date,
				Description:    args.Description,
				Notes:          args.Notes,
				Tags:           args.Tags,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(transactionViewFrom(result))
		},
	}
}

// editTransactionArgs is edit_transaction's arguments, mirroring
// internal/surface/http's editTransactionRequest field for field. This is
// full-replacement, not a partial patch (app.EditTransactionCommand's own
// doc comment): a caller changing one field re-sends every other field's
// current value. account/category apply when editing an outflow or
// inflow; from_account/to_account/to_amount apply when editing a
// transfer -- whichever pair doesn't match the transaction's own kind is
// ignored by the application layer, not by this tool.
type editTransactionArgs struct {
	TransactionID string   `json:"transaction_id"`
	Account       string   `json:"account,omitempty"`
	Category      string   `json:"category,omitempty"`
	Currency      string   `json:"currency,omitempty"`
	FromAccount   string   `json:"from_account,omitempty"`
	ToAccount     string   `json:"to_account,omitempty"`
	ToAmount      string   `json:"to_amount,omitempty"`
	Amount        string   `json:"amount"`
	Date          string   `json:"date,omitempty"`
	Description   string   `json:"description"`
	Notes         string   `json:"notes,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// editTransactionTool wires app.Service.EditTransaction onto a write-tier
// MCP tool.
func editTransactionTool() ToolDef {
	return ToolDef{
		Name:        "edit_transaction",
		Description: "Replace an existing transaction's editable fields. This is a full replacement: resend every field's current value, not just the one that's changing.",
		Tier:        ports.MCPToolTierWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"transaction_id": map[string]any{
					"type":        "string",
					"description": "The transaction's ID.",
				},
				"account": map[string]any{
					"type":        "string",
					"description": "An account's ID or unique name. Read when editing an outflow or an inflow.",
				},
				"category": map[string]any{
					"type":        "string",
					"description": "A category's ID or unique name. Read when editing an outflow or an inflow.",
				},
				"currency": map[string]any{
					"type":        "string",
					"description": "Read when editing an outflow or an inflow.",
				},
				"from_account": map[string]any{
					"type":        "string",
					"description": "An account's ID or unique name. Read when editing a transfer.",
				},
				"to_account": map[string]any{
					"type":        "string",
					"description": "An account's ID or unique name. Read when editing a transfer.",
				},
				"to_amount": map[string]any{
					"type":        "string",
					"description": "The to-leg's own amount, in the to-account's own currency. Read when editing a transfer; omit to reuse amount's raw digits, reinterpreted in the to-currency.",
				},
				"amount": map[string]any{
					"type":        "string",
					"description": "Always positive; the transaction's own type says which way the money moves.",
				},
				"date": map[string]any{
					"type":        "string",
					"description": "When this happened. Defaults to today.",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "What this was for.",
				},
				"notes": map[string]any{
					"type":        "string",
					"description": "A free-text note to attach.",
				},
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Tags to attach.",
				},
			},
			"required":             []string{"transaction_id", "amount", "description"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args editTransactionArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.EditTransaction(ctx, app.EditTransactionCommand{
				ActorID:        actorID,
				TransactionRef: args.TransactionID,
				AccountRef:     args.Account,
				Currency:       args.Currency,
				CategoryRef:    args.Category,
				FromAccountRef: args.FromAccount,
				ToAccountRef:   args.ToAccount,
				ToAmount:       args.ToAmount,
				Amount:         args.Amount,
				Date:           args.Date,
				Description:    args.Description,
				Notes:          args.Notes,
				Tags:           args.Tags,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(transactionViewFrom(result))
		},
	}
}
