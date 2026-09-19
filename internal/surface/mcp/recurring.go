// This file implements issue #281's read-tier MCP wiring for recurring
// rules, scheduled occurrences, and forecasting: list_recurring_rules,
// list_scheduled_occurrences, and get_forecast, wired onto
// internal/app/recurring_rules.go's ListRecurringRules,
// internal/app/recurring_occurrences.go's ListScheduledOccurrences (issue
// #278, extended by #281 itself), and internal/app/forecast.go's Forecast
// (issue #280) — following budgets.go's own read-tier tool conventions.
package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// scheduleView is a RecurringRule's firing pattern, mirroring
// internal/surface/http/recurring.go's own scheduleView field for field —
// only the fields the declared frequency actually uses are populated.
type scheduleView struct {
	Frequency  string `json:"frequency"`
	Interval   int    `json:"interval"`
	Weekday    *int   `json:"weekday,omitempty"`
	DayOfMonth *int   `json:"day_of_month,omitempty"`
	Month      *int   `json:"month,omitempty"`
}

// recurringRuleView is a RecurringRule's wire shape, mirroring
// internal/surface/http/recurring.go's own recurringRuleView field for
// field. Amount is rendered via app.RecurringRuleAmount, since a
// RecurringRule carries no currency of its own and this package may not
// import internal/domain/money to build one itself.
type recurringRuleView struct {
	ID          string       `json:"id"`
	AccountID   string       `json:"account_id"`
	CategoryID  string       `json:"category_id"`
	Amount      string       `json:"amount"`
	Currency    string       `json:"currency"`
	Description string       `json:"description"`
	Schedule    scheduleView `json:"schedule"`
	StartsOn    string       `json:"starts_on"`
	EndsOn      string       `json:"ends_on,omitempty"`
	Archived    bool         `json:"archived"`
	ArchivedAt  string       `json:"archived_at,omitempty"`
}

func recurringRuleViewFrom(r app.RecurringRuleResult) (recurringRuleView, error) {
	rule := r.Rule
	schedule := rule.Schedule()

	v := recurringRuleView{
		ID:          rule.ID(),
		AccountID:   rule.AccountID(),
		CategoryID:  rule.CategoryID(),
		Currency:    r.Currency,
		Description: rule.Description(),
		Schedule: scheduleView{
			Frequency: string(schedule.Frequency()),
			Interval:  schedule.Interval(),
		},
		StartsOn: rule.StartsOn().String(),
		Archived: rule.Archived(),
	}
	if wd, ok := schedule.Weekday(); ok {
		w := int(wd)
		v.Schedule.Weekday = &w
	}
	if dom, ok := schedule.DayOfMonth(); ok {
		v.Schedule.DayOfMonth = &dom
	}
	if m, ok := schedule.Month(); ok {
		mm := int(m)
		v.Schedule.Month = &mm
	}
	if d, ok := rule.EndsOn(); ok {
		v.EndsOn = d.String()
	}
	if d, ok := rule.ArchivedAt(); ok {
		v.ArchivedAt = d.String()
	}

	amount, err := app.RecurringRuleAmount(r.Currency, rule)
	if err != nil {
		return recurringRuleView{}, err
	}
	v.Amount = amount.AmountString()
	return v, nil
}

// recurringRuleResultResult renders r as a jsonResult, or an errorResult
// if recurringRuleViewFrom itself fails — the same shared-render-step
// shape budgetResultResult gives every budget write tool.
func recurringRuleResultResult(r app.RecurringRuleResult) (*sdkmcp.CallToolResult, error) {
	view, err := recurringRuleViewFrom(r)
	if err != nil {
		return errorResult(err), nil
	}
	return jsonResult(view)
}

// listRecurringRulesTool wires app.Service.ListRecurringRules onto a
// read-tier MCP tool.
func listRecurringRulesTool() ToolDef {
	return ToolDef{
		Name:        "list_recurring_rules",
		Description: "List every recurring rule, active and archived alike.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, _ json.RawMessage) (*sdkmcp.CallToolResult, error) {
			result, err := svc.ListRecurringRules(ctx, app.ListRecurringRulesQuery{ActorID: actorID})
			if err != nil {
				return errorResult(err), nil
			}
			views := make([]recurringRuleView, 0, len(result.Rules))
			for _, rule := range result.Rules {
				view, err := recurringRuleViewFrom(app.RecurringRuleResult{Rule: rule, Currency: result.Currencies[rule.ID()]})
				if err != nil {
					return errorResult(err), nil
				}
				views = append(views, view)
			}
			return jsonResult(views)
		},
	}
}

// scheduledOccurrenceView is one ScheduledOccurrence's wire shape,
// mirroring internal/surface/http/recurring.go's own
// scheduledOccurrenceView field for field — no account, no currency, no
// amount, since an occurrence is a projection, never money that moved
// (data-model.md §11).
type scheduledOccurrenceView struct {
	ID             string `json:"id"`
	RuleID         string `json:"rule_id"`
	OccurrenceDate string `json:"occurrence_date"`
	Status         string `json:"status"`
	TransactionID  string `json:"transaction_id,omitempty"`
}

// listScheduledOccurrencesArgs is list_scheduled_occurrences' arguments.
type listScheduledOccurrencesArgs struct {
	RuleID string `json:"rule_id,omitempty"`
	Status string `json:"status,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

// listScheduledOccurrencesTool wires app.Service.ListScheduledOccurrences
// onto a read-tier MCP tool.
func listScheduledOccurrencesTool() ToolDef {
	return ToolDef{
		Name:        "list_scheduled_occurrences",
		Description: "List scheduled occurrences, optionally by rule, status, and date range. An occurrence is a projection -- it never represents money that has actually moved -- until it's materialised, at which point it points at the real transaction it produced.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"rule_id": map[string]any{
					"type":        "string",
					"description": "Only include occurrences of this rule.",
				},
				"status": map[string]any{
					"type":        "string",
					"enum":        []string{"pending", "materialised", "skipped"},
					"description": "Only include occurrences in this status.",
				},
				"from": map[string]any{
					"type":        "string",
					"description": "Only include occurrences on or after this date.",
				},
				"to": map[string]any{
					"type":        "string",
					"description": "Only include occurrences on or before this date.",
				},
			},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args listScheduledOccurrencesArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.ListScheduledOccurrences(ctx, app.ListScheduledOccurrencesQuery{
				ActorID: actorID, RuleID: args.RuleID, Status: args.Status, FromDate: args.From, ToDate: args.To,
			})
			if err != nil {
				return errorResult(err), nil
			}
			views := make([]scheduledOccurrenceView, 0, len(result.Occurrences))
			for _, occ := range result.Occurrences {
				txID, _ := occ.TransactionID()
				views = append(views, scheduledOccurrenceView{
					ID: occ.ID(), RuleID: occ.RuleID(), OccurrenceDate: occ.OccurrenceDate().String(),
					Status: string(occ.Status()), TransactionID: txID,
				})
			}
			return jsonResult(views)
		},
	}
}

// forecastPointView is one period's projected totals (app.ForecastPoint),
// mirroring internal/surface/http/recurring.go's own forecastPointView.
type forecastPointView struct {
	From             string `json:"from"`
	To               string `json:"to"`
	ProjectedInflow  string `json:"projected_inflow"`
	ProjectedOutflow string `json:"projected_outflow"`
	ProjectedNet     string `json:"projected_net"`
}

// unconvertedOccurrenceView names one pending occurrence a forecast's
// conversion couldn't cover (app.UnconvertedOccurrence).
type unconvertedOccurrenceView struct {
	OccurrenceID string `json:"occurrence_id"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	Reason       string `json:"reason"`
}

// forecastView is get_forecast's result shape (app.ForecastResult).
type forecastView struct {
	Currency    string                      `json:"currency"`
	Points      []forecastPointView         `json:"points"`
	Unconverted []unconvertedOccurrenceView `json:"unconverted,omitempty"`
}

func forecastViewFrom(r app.ForecastResult) forecastView {
	v := forecastView{Currency: r.Options.ReportingCurrency, Points: make([]forecastPointView, 0, len(r.Points))}
	for _, p := range r.Points {
		v.Points = append(v.Points, forecastPointView{
			From: p.From.String(), To: p.To.String(),
			ProjectedInflow: p.ProjectedInflow.AmountString(), ProjectedOutflow: p.ProjectedOutflow.AmountString(),
			ProjectedNet: p.ProjectedNet.AmountString(),
		})
	}
	for _, u := range r.Unconverted {
		v.Unconverted = append(v.Unconverted, unconvertedOccurrenceView{
			OccurrenceID: u.OccurrenceID, Amount: u.Amount.AmountString(), Currency: u.Amount.Currency(), Reason: u.Reason,
		})
	}
	return v
}

// getForecastArgs is get_forecast's arguments: ADR-0004's conversion
// options and #194's granularity, plus the required from/to range.
type getForecastArgs struct {
	From string `json:"from"`
	To   string `json:"to"`
	analyticsOptionsArgs
	Granularity string `json:"granularity,omitempty"`
}

// getForecastTool wires app.Service.Forecast onto a read-tier MCP tool.
func getForecastTool() ToolDef {
	return ToolDef{
		Name:        "get_forecast",
		Description: "Get projected inflow/outflow/net from pending scheduled occurrences, bucketed by period. Reads only pending occurrences -- materialised and skipped occurrences are resolved history, not a forecast -- and never touches a balance, budget actual, or any other actuals-only figure.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				map[string]any{
					"from": map[string]any{
						"type":        "string",
						"description": "The inclusive start of the forecast range (required).",
					},
					"to": map[string]any{
						"type":        "string",
						"description": "The inclusive end of the forecast range (required).",
					},
					"granularity": granularitySchemaProperty(),
				},
				analyticsOptionsSchemaProperties(),
			),
			"required":             []string{"from", "to"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args getForecastArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.Forecast(ctx, app.ForecastQuery{
				ActorID: actorID, FromDate: args.From, ToDate: args.To,
				Options: args.options(), Granularity: app.Granularity(args.Granularity),
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(forecastViewFrom(result))
		},
	}
}
