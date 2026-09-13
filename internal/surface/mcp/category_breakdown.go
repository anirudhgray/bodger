package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// categoryBreakdownRowView is one top-level category's totals, mirroring
// internal/surface/cli/report.go's own categoryBreakdownRowView field for
// field.
type categoryBreakdownRowView struct {
	Category string `json:"category"`
	Spending string `json:"spending"`
	Income   string `json:"income"`
	Net      string `json:"net"`
}

// categoryBreakdownView is get_category_breakdown's result shape
// (app.CategoryBreakdownResult).
type categoryBreakdownView struct {
	Currency    string                     `json:"currency"`
	Rows        []categoryBreakdownRowView `json:"rows"`
	Unconverted []unconvertedPostingView   `json:"unconverted,omitempty"`
}

func categoryBreakdownViewFrom(r app.CategoryBreakdownResult) categoryBreakdownView {
	v := categoryBreakdownView{
		Currency:    r.Options.ReportingCurrency,
		Rows:        make([]categoryBreakdownRowView, 0, len(r.Rows)),
		Unconverted: unconvertedPostingViewsFrom(r.Unconverted),
	}
	for _, row := range r.Rows {
		name := "Uncategorized"
		if row.Category != nil {
			name = row.Category.Name()
		}
		v.Rows = append(v.Rows, categoryBreakdownRowView{
			Category: name,
			Spending: row.Spending.AmountString(),
			Income:   row.Income.AmountString(),
			Net:      row.Net.AmountString(),
		})
	}
	return v
}

// getCategoryBreakdownArgs is get_category_breakdown's arguments:
// ADR-0009's shared filter dimensions plus ADR-0004's conversion options.
type getCategoryBreakdownArgs struct {
	transactionFilterArgs
	analyticsOptionsArgs
}

// getCategoryBreakdownTool wires app.Service.CategoryBreakdown -- the
// category-spending report `bodger report category-breakdown` already
// exposes -- onto a read-tier MCP tool.
func getCategoryBreakdownTool() ToolDef {
	return ToolDef{
		Name:        "get_category_breakdown",
		Description: "Get spending and income grouped by top-level category, over a filtered set of transactions, converted into one reporting currency.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				transactionFilterSchemaProperties(),
				analyticsOptionsSchemaProperties(),
			),
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args getCategoryBreakdownArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.CategoryBreakdown(ctx, app.CategoryBreakdownQuery{
				ActorID: actorID,
				Filter:  args.input(),
				Options: args.options(),
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(categoryBreakdownViewFrom(result))
		},
	}
}
