package mcp

import (
	"context"
	"encoding/json"
	"fmt"

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

// fetchedRateView renders one app.FetchedRate. Mirrors
// internal/surface/cli/fx.go's and internal/surface/http/fx.go's own
// fetchedRateView field-for-field, reproduced here rather than imported
// (internal/surface packages never import one another).
type fetchedRateView struct {
	Pair   string `json:"pair"`
	Rate   string `json:"rate"`
	Date   string `json:"date"`
	Source string `json:"source"`
}

func fetchedRateViewFrom(f app.FetchedRate) fetchedRateView {
	return fetchedRateView{
		Pair:   fmt.Sprintf("%s/%s", f.Rate.Base(), f.Rate.Quote()),
		Rate:   f.Rate.Value().String(),
		Date:   f.Date.String(),
		Source: f.Source,
	}
}

// fxFetchView is fetch_fx_rates' result shape (app.FetchFxRatesResult).
// Mirrors internal/surface/cli/fx.go's and internal/surface/http/fx.go's
// own fxFetchView.
type fxFetchView struct {
	ReportingCurrency string            `json:"reporting_currency"`
	Fetched           []fetchedRateView `json:"fetched"`
}

func fxFetchViewFrom(r app.FetchFxRatesResult) fxFetchView {
	v := fxFetchView{ReportingCurrency: r.ReportingCurrency}
	for _, f := range r.Fetched {
		v.Fetched = append(v.Fetched, fetchedRateViewFrom(f))
	}
	return v
}

// fetchFxRatesArgs is fetch_fx_rates' arguments, mirroring
// app.FetchFxRatesCommand field for field (aside from ActorID, resolved
// by the dispatcher/harness like every other tool). Every field is
// optional -- see FetchFxRatesCommand's own doc comments for what an
// empty value means for each.
type fetchFxRatesArgs struct {
	Pairs []string `json:"pairs,omitempty"`
	Quote string   `json:"quote,omitempty"`
	From  string   `json:"from,omitempty"`
	To    string   `json:"to,omitempty"`
}

// fetchFxRatesTool wires app.Service.FetchFxRates -- issue #135's one
// explicit, network-touching, store-writing FX action -- onto a
// write-tier MCP tool (issue #272). Unlike list_fx_rates, this does call
// Service.FxProvider and writes to fx_rates, so it's registered write,
// not read; it's additive (new rate rows) and reversible in the same
// sense any other write is, not a deletion or hard-to-reverse operation,
// so it's write, not destructive either. Argument and result shapes
// mirror internal/surface/cli/fx.go's "fx rates fetch" command and
// internal/surface/http/fx.go's "POST /api/v1/fx/rates/fetch" handler
// field for field, the same wiring-only pattern list_fx_rates already
// used for its own sibling read.
func fetchFxRatesTool() ToolDef {
	return ToolDef{
		Name:        "fetch_fx_rates",
		Description: "Fetch and store exchange rates from the configured provider. With no pairs, fetches every currency pair actually in use across your accounts and transactions, quoted against your reporting currency, at today's date. Pass pairs to restrict the fetch to specific base currencies (add quote to quote them against a currency other than your reporting currency), and from/to together for a historical backfill instead of just today.",
		Tier:        ports.MCPToolTierWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pairs": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Restrict the fetch to these base currencies, quoted against quote (or your reporting currency). Omit to fetch every in-use pair (quote is ignored in that case).",
				},
				"quote": map[string]any{
					"type":        "string",
					"description": "Quote currency for every pairs entry, instead of your reporting currency. Ignored when pairs is omitted.",
				},
				"from": map[string]any{
					"type":        "string",
					"description": "Backfill range start date, inclusive (requires \"to\").",
				},
				"to": map[string]any{
					"type":        "string",
					"description": "Backfill range end date, inclusive (requires \"from\").",
				},
			},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args fetchFxRatesArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.FetchFxRates(ctx, app.FetchFxRatesCommand{
				ActorID: actorID,
				Pairs:   args.Pairs,
				Quote:   args.Quote,
				From:    args.From,
				To:      args.To,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(fxFetchViewFrom(result))
		},
	}
}
