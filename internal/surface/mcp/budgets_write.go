package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// budgetLineView is one budget line's wire shape, mirroring
// internal/surface/http/budgets.go's own budgetLineView field for field.
type budgetLineView struct {
	ID         string `json:"id"`
	CategoryID string `json:"category_id"`
	Amount     string `json:"amount"`
	Rollover   bool   `json:"rollover"`
}

// budgetView is create_budget's, update_budget's, archive_budget's, and
// every budget-line tool's result shape (app.BudgetResult), mirroring
// internal/surface/http/budgets.go's own budgetView field for field --
// the same aggregate shape, since there is no separate "just the lines"
// tool result any more than there's a separate REST endpoint for it.
type budgetView struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	PeriodType string           `json:"period_type"`
	Currency   string           `json:"currency"`
	StartsOn   string           `json:"starts_on"`
	Archived   bool             `json:"archived"`
	ArchivedAt string           `json:"archived_at,omitempty"`
	Lines      []budgetLineView `json:"lines"`
}

func budgetViewFrom(r app.BudgetResult) (budgetView, error) {
	b := r.Budget
	v := budgetView{
		ID:         b.ID(),
		Name:       b.Name(),
		PeriodType: string(b.PeriodType()),
		Currency:   b.Currency(),
		StartsOn:   b.StartsOn().String(),
		Archived:   b.Archived(),
		Lines:      make([]budgetLineView, 0, len(b.Lines())),
	}
	if d, ok := b.ArchivedAt(); ok {
		v.ArchivedAt = d.String()
	}
	for _, l := range b.Lines() {
		amount, err := app.BudgetLineAmount(b.Currency(), l)
		if err != nil {
			return budgetView{}, err
		}
		v.Lines = append(v.Lines, budgetLineView{
			ID:         l.ID(),
			CategoryID: l.CategoryID(),
			Amount:     amount.AmountString(),
			Rollover:   l.Rollover(),
		})
	}
	return v, nil
}

// budgetResultResult renders r as a jsonResult, or an errorResult if
// budgetViewFrom itself fails (BudgetLineAmount's own currency-parse
// error -- unreachable in practice, since every Budget already validated
// its own currency at construction, but handled the same defensive way
// internal/surface/http's own handlers do). Shared by every budget write
// tool below so the render step isn't repeated five times.
func budgetResultResult(r app.BudgetResult) (*sdkmcp.CallToolResult, error) {
	view, err := budgetViewFrom(r)
	if err != nil {
		return errorResult(err), nil
	}
	return jsonResult(view)
}

// budgetLineInputArgs is one line of create_budget's optional initial
// "lines" array, mirroring internal/surface/http's budgetLineRequest.
type budgetLineInputArgs struct {
	Category string `json:"category"`
	Amount   string `json:"amount"`
	Rollover bool   `json:"rollover,omitempty"`
}

func budgetLineInputsFrom(args []budgetLineInputArgs) []app.BudgetLineInput {
	inputs := make([]app.BudgetLineInput, 0, len(args))
	for _, a := range args {
		inputs = append(inputs, app.BudgetLineInput{CategoryRef: a.Category, Amount: a.Amount, Rollover: a.Rollover})
	}
	return inputs
}

// budgetLineInputSchema is the JSON Schema fragment for one
// budgetLineInputArgs entry, shared by create_budget's "lines" array items
// and add_budget_line's own top-level properties.
func budgetLineInputSchema() map[string]any {
	return map[string]any{
		"category": map[string]any{
			"type":        "string",
			"description": "The category this line plans an amount for: a category's ID or unique name.",
		},
		"amount": map[string]any{
			"type":        "string",
			"description": "The planned amount, in the budget's own currency.",
		},
		"rollover": map[string]any{
			"type":        "boolean",
			"description": "Carry an unspent (or overspent) amount into the next period. Defaults to false.",
		},
	}
}

// createBudgetArgs is create_budget's arguments, mirroring
// internal/surface/http's createBudgetRequest field for field. period_type
// is absent -- a budget's period type is always monthly today
// (budgeting.PeriodTypeMonthly's own doc comment).
type createBudgetArgs struct {
	Name     string                `json:"name"`
	Currency string                `json:"currency,omitempty"`
	StartsOn string                `json:"starts_on,omitempty"`
	Lines    []budgetLineInputArgs `json:"lines,omitempty"`
}

// createBudgetTool wires app.Service.CreateBudget onto a write-tier MCP
// tool.
func createBudgetTool() ToolDef {
	return ToolDef{
		Name:        "create_budget",
		Description: "Create a new monthly budget, optionally with an initial batch of category lines.",
		Tier:        ports.MCPToolTierWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "The budget's name.",
				},
				"currency": map[string]any{
					"type":        "string",
					"description": "Defaults to your reporting currency, then your instance default.",
				},
				"starts_on": map[string]any{
					"type":        "string",
					"description": "The date the budget's periods are computed from. Defaults to today.",
				},
				"lines": map[string]any{
					"type":        "array",
					"description": "An initial batch of lines to create alongside the budget. Optional -- lines can also be added afterward via add_budget_line.",
					"items": map[string]any{
						"type":                 "object",
						"properties":           budgetLineInputSchema(),
						"required":             []string{"category", "amount"},
						"additionalProperties": false,
					},
				},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args createBudgetArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{
				ActorID:  actorID,
				Name:     args.Name,
				Currency: args.Currency,
				StartsOn: args.StartsOn,
				Lines:    budgetLineInputsFrom(args.Lines),
			})
			if err != nil {
				return errorResult(err), nil
			}
			return budgetResultResult(result)
		},
	}
}

// updateBudgetArgs is update_budget's arguments, mirroring
// internal/surface/http's patchBudgetRequest: a full replacement of both
// editable fields (app.UpdateBudgetCommand's own doc comment -- omitting
// starts_on resolves it to today, not to the budget's existing value).
type updateBudgetArgs struct {
	BudgetID string `json:"budget_id"`
	Name     string `json:"name"`
	StartsOn string `json:"starts_on,omitempty"`
}

// updateBudgetTool wires app.Service.UpdateBudget onto a write-tier MCP
// tool.
func updateBudgetTool() ToolDef {
	return ToolDef{
		Name:        "update_budget",
		Description: "Update a budget's name and starts_on. This is a full replacement of both fields -- resend the current starts_on if only the name is changing, since omitting it resolves to today.",
		Tier:        ports.MCPToolTierWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"budget_id": map[string]any{
					"type":        "string",
					"description": "The budget's ID.",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "The budget's name.",
				},
				"starts_on": map[string]any{
					"type":        "string",
					"description": "Defaults to today when omitted -- not to the budget's existing starts_on.",
				},
			},
			"required":             []string{"budget_id", "name"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args updateBudgetArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.UpdateBudget(ctx, app.UpdateBudgetCommand{
				ActorID:  actorID,
				BudgetID: args.BudgetID,
				Name:     args.Name,
				StartsOn: args.StartsOn,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return budgetResultResult(result)
		},
	}
}

// addBudgetLineArgs is add_budget_line's arguments, mirroring
// internal/surface/http's budgetLineRequest plus the target budget_id.
type addBudgetLineArgs struct {
	BudgetID string `json:"budget_id"`
	budgetLineInputArgs
}

// addBudgetLineTool wires app.Service.AddBudgetLine onto a write-tier MCP
// tool.
func addBudgetLineTool() ToolDef {
	return ToolDef{
		Name:        "add_budget_line",
		Description: "Add one new category line to an existing budget.",
		Tier:        ports.MCPToolTierWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				map[string]any{
					"budget_id": map[string]any{
						"type":        "string",
						"description": "The budget's ID.",
					},
				},
				budgetLineInputSchema(),
			),
			"required":             []string{"budget_id", "category", "amount"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args addBudgetLineArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.AddBudgetLine(ctx, app.AddBudgetLineCommand{
				ActorID:     actorID,
				BudgetID:    args.BudgetID,
				CategoryRef: args.Category,
				Amount:      args.Amount,
				Rollover:    args.Rollover,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return budgetResultResult(result)
		},
	}
}

// updateBudgetLineArgs is update_budget_line's arguments. A line's
// category is fixed once created (app.UpdateBudgetLineCommand's own doc
// comment), so only amount and rollover are editable here.
type updateBudgetLineArgs struct {
	BudgetID string `json:"budget_id"`
	LineID   string `json:"line_id"`
	Amount   string `json:"amount"`
	Rollover bool   `json:"rollover,omitempty"`
}

// updateBudgetLineTool wires app.Service.UpdateBudgetLine onto a
// write-tier MCP tool.
func updateBudgetLineTool() ToolDef {
	return ToolDef{
		Name:        "update_budget_line",
		Description: "Update an existing budget line's amount and rollover flag. A line's category can't be changed -- remove it and add a new one instead.",
		Tier:        ports.MCPToolTierWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"budget_id": map[string]any{
					"type":        "string",
					"description": "The budget's ID.",
				},
				"line_id": map[string]any{
					"type":        "string",
					"description": "The line's ID.",
				},
				"amount": map[string]any{
					"type":        "string",
					"description": "The planned amount, in the budget's own currency.",
				},
				"rollover": map[string]any{
					"type":        "boolean",
					"description": "Carry an unspent (or overspent) amount into the next period. Defaults to false.",
				},
			},
			"required":             []string{"budget_id", "line_id", "amount"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args updateBudgetLineArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.UpdateBudgetLine(ctx, app.UpdateBudgetLineCommand{
				ActorID:  actorID,
				BudgetID: args.BudgetID,
				LineID:   args.LineID,
				Amount:   args.Amount,
				Rollover: args.Rollover,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return budgetResultResult(result)
		},
	}
}

// removeBudgetLineArgs is remove_budget_line's arguments.
type removeBudgetLineArgs struct {
	BudgetID string `json:"budget_id"`
	LineID   string `json:"line_id"`
}

// removeBudgetLineTool wires app.Service.RemoveBudgetLine onto a
// write-tier MCP tool.
func removeBudgetLineTool() ToolDef {
	return ToolDef{
		Name:        "remove_budget_line",
		Description: "Remove one line from an existing budget.",
		Tier:        ports.MCPToolTierWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"budget_id": map[string]any{
					"type":        "string",
					"description": "The budget's ID.",
				},
				"line_id": map[string]any{
					"type":        "string",
					"description": "The line's ID.",
				},
			},
			"required":             []string{"budget_id", "line_id"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args removeBudgetLineArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.RemoveBudgetLine(ctx, app.RemoveBudgetLineCommand{
				ActorID:  actorID,
				BudgetID: args.BudgetID,
				LineID:   args.LineID,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return budgetResultResult(result)
		},
	}
}

// archiveBudgetArgs is archive_budget's arguments.
type archiveBudgetArgs struct {
	BudgetID string `json:"budget_id"`
}

// archiveBudgetTool wires app.Service.ArchiveBudget onto a write-tier MCP
// tool.
//
// Tier decision (issue #261's own scope: "decide whether ArchiveBudget is
// write or destructive tier... record the decision either way rather than
// defaulting silently"): this is write, not destructive. ADR-0013 scopes
// "destructive" to deletion, bulk edits, import commit, and rollback --
// operations that either remove data or are otherwise hard to undo through
// the ordinary CRUD surface. Archiving a budget does neither:
// app.ArchiveBudget's own doc comment describes it as "stops a budget from
// appearing in current listings and creation flows going forward, while
// leaving it and its lines fully queryable" -- the same "hides from
// pickers, keeps history" contract accounts already use as an ordinary
// write. It's also fully reversible: unlike a soft-deleted transaction (no
// "undelete" use case exists) or a committed import (ADR-0008's own
// import-commit semantics), an archived budget can be brought back into
// active use the same way it left -- update_budget's caller can pass a
// budgeting.WithArchivedAt-clearing follow-up once that "unarchive" use
// case exists, and even before it does, the archived flag is a metadata
// change with no data-loss to gate behind a confirmation token.
func archiveBudgetTool() ToolDef {
	return ToolDef{
		Name:        "archive_budget",
		Description: "Archive a budget: it stops appearing in current listings and creation flows, but its history (lines, actuals, past periods) stays fully queryable.",
		Tier:        ports.MCPToolTierWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"budget_id": map[string]any{
					"type":        "string",
					"description": "The budget's ID.",
				},
			},
			"required":             []string{"budget_id"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args archiveBudgetArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.ArchiveBudget(ctx, app.ArchiveBudgetCommand{
				ActorID:  actorID,
				BudgetID: args.BudgetID,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return budgetResultResult(result)
		},
	}
}
