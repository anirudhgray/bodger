package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// budgetLineActualsView is one budget line's plan-vs-actual, mirroring
// internal/surface/cli/budgets.go's own budgetLineActualsView field for
// field.
type budgetLineActualsView struct {
	LineID      string  `json:"line_id"`
	CategoryID  string  `json:"category_id"`
	Budgeted    string  `json:"budgeted"`
	Actual      string  `json:"actual"`
	Remaining   string  `json:"remaining"`
	Utilisation float64 `json:"utilisation"`
}

// budgetOverallActualsView is every line's plan-vs-actual summed into one
// figure for the whole budget (app.BudgetOverallActuals).
type budgetOverallActualsView struct {
	Budgeted    string  `json:"budgeted"`
	Actual      string  `json:"actual"`
	Remaining   string  `json:"remaining"`
	Utilisation float64 `json:"utilisation"`
}

// budgetActualsView is get_budget_actuals' result shape
// (app.BudgetActualsResult).
type budgetActualsView struct {
	BudgetID    string                   `json:"budget_id"`
	Currency    string                   `json:"currency"`
	From        string                   `json:"from"`
	To          string                   `json:"to"`
	AsOf        string                   `json:"as_of"`
	Overall     budgetOverallActualsView `json:"overall"`
	Lines       []budgetLineActualsView  `json:"lines"`
	Unconverted []unconvertedPostingView `json:"unconverted,omitempty"`
}

func budgetActualsViewFrom(r app.BudgetActualsResult) budgetActualsView {
	v := budgetActualsView{
		BudgetID: r.Budget.ID(),
		Currency: r.Budget.Currency(),
		From:     r.From.String(),
		To:       r.To.String(),
		AsOf:     r.AsOf.String(),
		Overall: budgetOverallActualsView{
			Budgeted:    r.Overall.Budgeted.AmountString(),
			Actual:      r.Overall.Actual.AmountString(),
			Remaining:   r.Overall.Remaining.AmountString(),
			Utilisation: r.Overall.Utilisation,
		},
		Lines:       make([]budgetLineActualsView, 0, len(r.Lines)),
		Unconverted: unconvertedPostingViewsFrom(r.Unconverted),
	}
	for _, l := range r.Lines {
		v.Lines = append(v.Lines, budgetLineActualsView{
			LineID:      l.Line.ID(),
			CategoryID:  l.Line.CategoryID(),
			Budgeted:    l.Budgeted.AmountString(),
			Actual:      l.Actual.AmountString(),
			Remaining:   l.Remaining.AmountString(),
			Utilisation: l.Utilisation,
		})
	}
	return v
}

// budgetActualsArgs is get_budget_actuals' arguments.
type budgetActualsArgs struct {
	BudgetID string `json:"budget_id"`
	Period   string `json:"period,omitempty"`
}

// getBudgetActualsTool wires app.Service.BudgetActuals onto a read-tier
// MCP tool.
func getBudgetActualsTool() ToolDef {
	return ToolDef{
		Name:        "get_budget_actuals",
		Description: "Get one budget's plan-vs-actual for a single period.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"budget_id": map[string]any{
					"type":        "string",
					"description": "The budget's ID.",
				},
				"period": map[string]any{
					"type":        "string",
					"description": "Any date within the target month. Defaults to the current month.",
				},
			},
			"required":             []string{"budget_id"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args budgetActualsArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.BudgetActuals(ctx, app.BudgetActualsQuery{
				ActorID:  actorID,
				BudgetID: args.BudgetID,
				Period:   args.Period,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(budgetActualsViewFrom(result))
		},
	}
}

// budgetHistoryView is get_budget_history's result shape
// (app.BudgetHistoryResult): one budgetActualsView per month, oldest
// first.
type budgetHistoryView struct {
	BudgetID string              `json:"budget_id"`
	Periods  []budgetActualsView `json:"periods"`
}

func budgetHistoryViewFrom(r app.BudgetHistoryResult) budgetHistoryView {
	v := budgetHistoryView{BudgetID: r.Budget.ID(), Periods: make([]budgetActualsView, 0, len(r.Periods))}
	for _, p := range r.Periods {
		v.Periods = append(v.Periods, budgetActualsViewFrom(p))
	}
	return v
}

// budgetHistoryArgs is get_budget_history's arguments.
type budgetHistoryArgs struct {
	BudgetID string `json:"budget_id"`
	Period   string `json:"period,omitempty"`
	Months   int    `json:"months,omitempty"`
}

// getBudgetHistoryTool wires app.Service.BudgetHistory onto a read-tier
// MCP tool.
func getBudgetHistoryTool() ToolDef {
	return ToolDef{
		Name:        "get_budget_history",
		Description: "Get one budget's plan-vs-actual repeated over a range of consecutive calendar months.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"budget_id": map[string]any{
					"type":        "string",
					"description": "The budget's ID.",
				},
				"period": map[string]any{
					"type":        "string",
					"description": "Any date within the most recent month to include. Defaults to the current month.",
				},
				"months": map[string]any{
					"type":        "integer",
					"description": "How many consecutive months to include. Defaults to 6.",
				},
			},
			"required":             []string{"budget_id"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args budgetHistoryArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.BudgetHistory(ctx, app.BudgetHistoryQuery{
				ActorID:  actorID,
				BudgetID: args.BudgetID,
				Period:   args.Period,
				Months:   args.Months,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(budgetHistoryViewFrom(result))
		},
	}
}
