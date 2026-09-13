package mcp

import "github.com/anirudhgray/bodger/internal/app"

// transactionFilterArgs is ADR-0009's shared transaction-filter shape
// (app.TransactionFilterInput) as MCP tool arguments -- the same
// dimensions ListTransactions and every M5 analytics method share
// verbatim (internal/app/transactions_list.go's own doc comment on
// TransactionFilterInput). list_transactions and get_category_breakdown
// both embed this rather than each declaring the eleven fields
// separately, so the two can never drift on what a filter dimension is
// called or means.
type transactionFilterArgs struct {
	Account     string   `json:"account,omitempty"`
	Category    string   `json:"category,omitempty"`
	Type        string   `json:"type,omitempty"`
	DateFrom    string   `json:"date_from,omitempty"`
	DateTo      string   `json:"date_to,omitempty"`
	Currencies  []string `json:"currencies,omitempty"`
	AmountMin   string   `json:"amount_min,omitempty"`
	AmountMax   string   `json:"amount_max,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	TagMode     string   `json:"tag_mode,omitempty"`
}

// input converts f into app.TransactionFilterInput -- the shape every
// app-layer method that filters transactions actually takes. This is pure
// field renaming, not normalisation: every string here still flows
// through internal/app/normalize exactly once, inside the application
// layer, the same way it already does for the CLI and REST surfaces
// (CLAUDE.md's normalize-once rule).
func (f transactionFilterArgs) input() app.TransactionFilterInput {
	return app.TransactionFilterInput{
		AccountRef:  f.Account,
		CategoryRef: f.Category,
		Kind:        f.Type,
		DateFrom:    f.DateFrom,
		DateTo:      f.DateTo,
		Currencies:  f.Currencies,
		AmountMin:   f.AmountMin,
		AmountMax:   f.AmountMax,
		Description: f.Description,
		Tags:        f.Tags,
		TagMode:     f.TagMode,
	}
}

// transactionFilterSchemaProperties is the JSON Schema "properties"
// fragment matching transactionFilterArgs field for field -- merged into
// a tool's own InputSchema (via mergeSchemaProperties) by every tool that
// embeds transactionFilterArgs.
func transactionFilterSchemaProperties() map[string]any {
	return map[string]any{
		"account": map[string]any{
			"type":        "string",
			"description": "Only include transactions touching this account: an account's ID or unique name.",
		},
		"category": map[string]any{
			"type":        "string",
			"description": "Only include transactions in this category, including anything under it: a category's ID or unique name.",
		},
		"type": map[string]any{
			"type":        "string",
			"enum":        []string{"outflow", "inflow", "transfer"},
			"description": "Only include this kind of transaction.",
		},
		"date_from": map[string]any{
			"type":        "string",
			"description": "Only include transactions booked on or after this date.",
		},
		"date_to": map[string]any{
			"type":        "string",
			"description": "Only include transactions booked on or before this date.",
		},
		"currencies": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Only include transactions in one of these currencies.",
		},
		"amount_min": map[string]any{
			"type":        "string",
			"description": "Only include transactions whose absolute amount is at or above this.",
		},
		"amount_max": map[string]any{
			"type":        "string",
			"description": "Only include transactions whose absolute amount is at or below this.",
		},
		"description": map[string]any{
			"type":        "string",
			"description": "Only include transactions whose description contains this text.",
		},
		"tags": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Only include transactions carrying at least one of these tags (or, with tag_mode \"all\", every one of them).",
		},
		"tag_mode": map[string]any{
			"type":        "string",
			"enum":        []string{"any", "all"},
			"description": "How multiple tags combine. Defaults to \"any\".",
		},
	}
}

// analyticsOptionsArgs is ADR-0004's reporting-currency/conversion-policy
// inputs (app.AnalyticsOptions) as MCP tool arguments -- shared by every
// tool that converts its result into one reporting currency.
type analyticsOptionsArgs struct {
	Currency   string `json:"currency,omitempty"`
	Policy     string `json:"policy,omitempty"`
	PinnedDate string `json:"pinned_date,omitempty"`
}

// options converts o into app.AnalyticsOptions.
func (o analyticsOptionsArgs) options() app.AnalyticsOptions {
	return app.AnalyticsOptions{
		ReportingCurrency: o.Currency,
		Policy:            app.ConversionPolicy(o.Policy),
		PinnedDate:        o.PinnedDate,
	}
}

// analyticsOptionsSchemaProperties is the JSON Schema "properties"
// fragment matching analyticsOptionsArgs field for field.
func analyticsOptionsSchemaProperties() map[string]any {
	return map[string]any{
		"currency": map[string]any{
			"type":        "string",
			"description": "Convert every figure into this currency. Defaults to your configured reporting currency.",
		},
		"policy": map[string]any{
			"type":        "string",
			"enum":        []string{"transaction_date", "current", "pinned"},
			"description": "Which conversion policy to use.",
		},
		"pinned_date": map[string]any{
			"type":        "string",
			"description": "The date to convert at when policy is \"pinned\".",
		},
	}
}
