package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// accountBalanceView is one account's computed balance, mirroring
// internal/surface/cli/balance.go's balanceEntryView field for field.
type accountBalanceView struct {
	AccountID string               `json:"account_id"`
	Account   string               `json:"account"`
	Currency  string               `json:"currency"`
	Balance   string               `json:"balance"`
	Archived  bool                 `json:"archived"`
	Converted *convertedAmountView `json:"converted,omitempty"`
}

// accountBalancesView is get_account_balances' result shape
// (app.AccountBalancesResult).
type accountBalancesView struct {
	AsOf        string                   `json:"as_of"`
	Balances    []accountBalanceView     `json:"balances"`
	Unconverted []unconvertedBalanceView `json:"unconverted,omitempty"`
}

func accountBalancesViewFrom(r app.AccountBalancesResult) accountBalancesView {
	v := accountBalancesView{
		AsOf:        r.AsOf.String(),
		Balances:    make([]accountBalanceView, 0, len(r.Balances)),
		Unconverted: unconvertedBalanceViewsFrom(r.Unconverted),
	}
	for _, b := range r.Balances {
		entry := accountBalanceView{
			AccountID: b.Account.ID(),
			Account:   b.Account.Name(),
			Currency:  b.Balance.Currency(),
			Balance:   b.Balance.AmountString(),
			Archived:  b.Account.Archived(),
		}
		if b.Converted != nil {
			converted := convertedAmountViewFrom(*b.Converted)
			entry.Converted = &converted
		}
		v.Balances = append(v.Balances, entry)
	}
	return v
}

// accountBalancesArgs is get_account_balances' arguments: an optional
// as_of date, plus ADR-0004's optional currency/policy/pinned_date
// conversion trio -- leaving currency empty keeps every balance in its
// own account currency, unconverted, exactly like
// `bodger balance` with no --currency.
type accountBalancesArgs struct {
	AsOf string `json:"as_of,omitempty"`
	analyticsOptionsArgs
}

// getAccountBalancesTool wires app.Service.AccountBalances -- the "what do
// I have available?" read every account-balance display in this codebase
// already calls (docs/ux-principles.md §1) -- onto a read-tier MCP tool.
func getAccountBalancesTool() ToolDef {
	return ToolDef{
		Name:        "get_account_balances",
		Description: "Get every account's balance as of a date, optionally converted into one reporting currency.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				map[string]any{
					"as_of": map[string]any{
						"type":        "string",
						"description": "Compute balances as of this date. Defaults to today.",
					},
				},
				analyticsOptionsSchemaProperties(),
			),
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args accountBalancesArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			opts := args.options()
			result, err := svc.AccountBalances(ctx, app.AccountBalancesQuery{
				ActorID:        actorID,
				AsOf:           args.AsOf,
				TargetCurrency: opts.ReportingCurrency,
				Policy:         opts.Policy,
				PinnedDate:     opts.PinnedDate,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(accountBalancesViewFrom(result))
		},
	}
}
