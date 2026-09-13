package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// fxRateView is list_fx_rates' result shape (app.ListFxRatesResult),
// echoing the query's own from/to since the result itself doesn't carry
// them -- mirrors internal/surface/cli/fx.go's own fxRateView, split into
// separate amount/currency fields (rather than that file's
// pre-formatted "amount currency" strings) since a tool result is read
// by a program, not printed to a terminal.
type fxRateView struct {
	From              string `json:"from"`
	To                string `json:"to"`
	Rate              string `json:"rate"`
	RateDate          string `json:"rate_date"`
	RateSource        string `json:"rate_source"`
	Stale             bool   `json:"stale"`
	Policy            string `json:"policy"`
	Amount            string `json:"amount,omitempty"`
	AmountCurrency    string `json:"amount_currency,omitempty"`
	ConvertedAmount   string `json:"converted_amount,omitempty"`
	ConvertedCurrency string `json:"converted_currency,omitempty"`
}

func fxRateViewFrom(from, to string, r app.ListFxRatesResult) fxRateView {
	v := fxRateView{
		From:       from,
		To:         to,
		Rate:       r.Rate.Value().String(),
		RateDate:   r.RateDate.String(),
		RateSource: r.RateSource,
		Stale:      r.Stale,
		Policy:     string(r.Policy),
	}
	if r.Converted != nil {
		v.Amount = r.Converted.ConvertedFrom.AmountString()
		v.AmountCurrency = r.Converted.ConvertedFrom.Currency()
		v.ConvertedAmount = r.Converted.Amount.AmountString()
		v.ConvertedCurrency = r.Converted.Amount.Currency()
	}
	return v
}

// listFxRatesArgs is list_fx_rates' arguments, mirroring
// app.ListFxRatesQuery field for field.
type listFxRatesArgs struct {
	From            string `json:"from"`
	To              string `json:"to"`
	Policy          string `json:"policy"`
	TransactionDate string `json:"transaction_date,omitempty"`
	PinnedDate      string `json:"pinned_date,omitempty"`
	Amount          string `json:"amount,omitempty"`
}

// listFxRatesTool wires app.Service.ListFxRates -- issue #135's pure,
// stored-data-only FX read (it never calls Service.FxProvider, so a call
// never triggers a network fetch) -- onto a read-tier MCP tool.
func listFxRatesTool() ToolDef {
	return ToolDef{
		Name:        "list_fx_rates",
		Description: "Look up the exchange rate between two currencies from bodger's own stored rates, optionally converting an amount. Never fetches a new rate from the network.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"from": map[string]any{
					"type":        "string",
					"description": "The currency to look up a rate for.",
				},
				"to": map[string]any{
					"type":        "string",
					"description": "The currency it's quoted against.",
				},
				"policy": map[string]any{
					"type":        "string",
					"enum":        []string{"transaction_date", "current", "pinned"},
					"description": "Which conversion policy to use.",
				},
				"transaction_date": map[string]any{
					"type":        "string",
					"description": "The transaction date to look the rate up at. Required when policy is \"transaction_date\".",
				},
				"pinned_date": map[string]any{
					"type":        "string",
					"description": "The pinned date to look the rate up at. Required when policy is \"pinned\".",
				},
				"amount": map[string]any{
					"type":        "string",
					"description": "An amount in from's currency to convert. Optional -- omit for just the rate itself.",
				},
			},
			"required":             []string{"from", "to", "policy"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args listFxRatesArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.ListFxRates(ctx, app.ListFxRatesQuery{
				From:            args.From,
				To:              args.To,
				Policy:          app.ConversionPolicy(args.Policy),
				TransactionDate: args.TransactionDate,
				PinnedDate:      args.PinnedDate,
				Amount:          args.Amount,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(fxRateViewFrom(args.From, args.To, result))
		},
	}
}
