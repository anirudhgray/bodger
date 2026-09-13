package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// transactionView renders one transaction, mirroring
// internal/surface/cli/transactions.go's own transactionView field for
// field -- see that file's doc comment for why account_id/category_id and
// from_account_id/to_account_id are mutually exclusive, and why
// amount/currency always report a move's from-leg.
type transactionView struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Date          string   `json:"date"`
	Description   string   `json:"description"`
	Notes         string   `json:"notes,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	AccountID     string   `json:"account_id,omitempty"`
	CategoryID    string   `json:"category_id,omitempty"`
	FromAccountID string   `json:"from_account_id,omitempty"`
	ToAccountID   string   `json:"to_account_id,omitempty"`
	Amount        string   `json:"amount"`
	Currency      string   `json:"currency"`
	ToAmount      string   `json:"to_amount,omitempty"`
	ToCurrency    string   `json:"to_currency,omitempty"`
}

func transactionViewFrom(r app.TransactionResult) transactionView {
	t := r.Transaction
	v := transactionView{
		ID:          t.ID(),
		Type:        string(t.Kind()),
		Date:        t.BookedDate().String(),
		Description: t.Description(),
		Notes:       t.Notes(),
	}
	for _, tag := range r.Tags {
		v.Tags = append(v.Tags, tag.String())
	}

	// Which leg is "from" and which is "to" is decided by the amount's
	// sign (the source leg is always the negative one), never by position
	// in the slice -- mirrors transactionViewFrom's own rule in
	// internal/surface/cli/transactions.go.
	for _, p := range t.Postings() {
		switch {
		case v.Type == "transfer" && p.Amount().IsNegative():
			v.FromAccountID = p.AccountID()
			v.Amount = p.Amount().Abs().AmountString()
			v.Currency = p.Currency()
		case v.Type == "transfer":
			v.ToAccountID = p.AccountID()
			v.ToAmount = p.Amount().AmountString()
			v.ToCurrency = p.Currency()
		default:
			v.AccountID = p.AccountID()
			if cid, ok := p.CategoryID(); ok {
				v.CategoryID = cid
			}
			v.Amount = p.Amount().Abs().AmountString()
			v.Currency = p.Currency()
		}
	}
	return v
}

// transactionListView is list_transactions' result shape
// (app.ListTransactionsResult). Limit and Offset are the values the
// application layer actually applied -- an unset or oversized limit is
// defaulted/clamped there -- so an agent paging through a long list knows
// the real page size it got, the same way `transactions list`'s own
// transactionListView does.
type transactionListView struct {
	Transactions []transactionView `json:"transactions"`
	Limit        int               `json:"limit"`
	Offset       int               `json:"offset"`
}

// listTransactionsArgs is list_transactions' arguments: ADR-0009's shared
// filter dimensions plus offset pagination.
type listTransactionsArgs struct {
	transactionFilterArgs
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// listTransactionsTool wires app.Service.ListTransactions onto a
// read-tier MCP tool.
func listTransactionsTool() ToolDef {
	return ToolDef{
		Name:        "list_transactions",
		Description: "List recorded transactions, filtered by account, category, type, date range, currency, amount range, description, or tags.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": mergeSchemaProperties(
				transactionFilterSchemaProperties(),
				map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "How many transactions to return at once. Defaults to 50, capped at 200.",
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "How many matching transactions to skip, for paging through a long list.",
					},
				},
			),
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args listTransactionsArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			filter := args.input()
			result, err := svc.ListTransactions(ctx, app.ListTransactionsQuery{
				ActorID:     actorID,
				AccountRef:  filter.AccountRef,
				CategoryRef: filter.CategoryRef,
				Kind:        filter.Kind,
				DateFrom:    filter.DateFrom,
				DateTo:      filter.DateTo,
				Currencies:  filter.Currencies,
				AmountMin:   filter.AmountMin,
				AmountMax:   filter.AmountMax,
				Description: filter.Description,
				Tags:        filter.Tags,
				TagMode:     filter.TagMode,
				Limit:       args.Limit,
				Offset:      args.Offset,
			})
			if err != nil {
				return errorResult(err), nil
			}

			view := transactionListView{
				Transactions: make([]transactionView, 0, len(result.Transactions)),
				Limit:        result.Limit,
				Offset:       result.Offset,
			}
			for _, t := range result.Transactions {
				view.Transactions = append(view.Transactions, transactionViewFrom(app.TransactionResult{Transaction: t}))
			}
			return jsonResult(view)
		},
	}
}
