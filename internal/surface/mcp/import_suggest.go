// This file implements issue #306's read-tier MCP wiring for
// SuggestForImportBatch (internal/app/import_suggest.go, issue #305) —
// ADR-0015's advisory category/occurrence suggestions for a staged
// import's still-unresolved rows. Read tier because the method writes
// nothing at all: a suggestion is a proposal an agent (or the human
// behind it) accepts through the existing commit/edit/
// resolve-occurrence-match tools, exactly as it would without this one.
package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// suggestionSource is what every typesafe.ai suggestion this package
// renders is attributed to (ADR-0015, docs/ux-principles.md §6). A
// literal, not something read off app.ImportRowSuggestion:
// ports.SuggestionProvider deliberately carries no Name() (nothing it
// returns is persisted, so there's no provenance column to fill), and an
// instance has only ever one configured provider for a suggestion to
// have come from.
const suggestionSource = "typesafe.ai"

// suggestedCategoryView is one staged row's surviving category
// suggestion (app.SuggestedCategory) — a proposal, never a value.
type suggestedCategoryView struct {
	CategoryID string  `json:"category_id"`
	Confidence float64 `json:"confidence"`
}

// suggestedOccurrenceView is one staged row's surviving occurrence-match
// suggestion (app.SuggestedOccurrence).
type suggestedOccurrenceView struct {
	OccurrenceID string  `json:"occurrence_id"`
	Confidence   float64 `json:"confidence"`
}

// importRowSuggestionView is one staged row's suggestion(s)
// (app.ImportRowSuggestion) — Category and/or Occurrence are omitted
// when there was nothing worth showing for that half of the row.
type importRowSuggestionView struct {
	RecordID   string                   `json:"record_id"`
	Source     string                   `json:"source"`
	Category   *suggestedCategoryView   `json:"category,omitempty"`
	Occurrence *suggestedOccurrenceView `json:"occurrence,omitempty"`
}

func importRowSuggestionViewFrom(s app.ImportRowSuggestion) importRowSuggestionView {
	v := importRowSuggestionView{RecordID: s.RecordID, Source: suggestionSource}
	if s.Category != nil {
		v.Category = &suggestedCategoryView{CategoryID: s.Category.CategoryID, Confidence: s.Category.Confidence}
	}
	if s.Occurrence != nil {
		v.Occurrence = &suggestedOccurrenceView{OccurrenceID: s.Occurrence.OccurrenceID, Confidence: s.Occurrence.Confidence}
	}
	return v
}

// importSuggestionsView is get_import_suggestions' result shape
// (app.SuggestForImportBatchResult).
type importSuggestionsView struct {
	Configured             bool                      `json:"configured"`
	Suggestions            []importRowSuggestionView `json:"suggestions"`
	TooManyCategoryOptions []string                  `json:"too_many_category_options,omitempty"`
	RowsSuggested          int                       `json:"rows_suggested"`
	RowsFailed             int                       `json:"rows_failed"`
}

func importSuggestionsViewFrom(r app.SuggestForImportBatchResult) importSuggestionsView {
	v := importSuggestionsView{
		Configured:             r.Configured,
		TooManyCategoryOptions: r.TooManyCategoryOptions,
		RowsSuggested:          r.RowsSuggested,
		RowsFailed:             r.RowsFailed,
	}
	for _, s := range r.Suggestions {
		v.Suggestions = append(v.Suggestions, importRowSuggestionViewFrom(s))
	}
	return v
}

// getImportSuggestionsArgs is get_import_suggestions' arguments: a
// single import batch reference, matching importBatchArgs'
// (import_destructive.go) own shape for commit_import/rollback_import.
type getImportSuggestionsArgs struct {
	ImportID string `json:"import_id"`
}

// getImportSuggestionsTool wires app.Service.SuggestForImportBatch onto a
// read-tier MCP tool. Always allowed and never audited (ADR-0013): unlike
// commit_import/rollback_import, this writes nothing, so it needs no
// confirmation-token protocol and no --allow-destructive gate.
func getImportSuggestionsTool() ToolDef {
	return ToolDef{
		Name: "get_import_suggestions",
		Description: "Get advisory category and pending-occurrence-match suggestions, from typesafe.ai, for a staged " +
			"import's still-unresolved rows. This writes nothing: a suggestion is a proposal to accept yourself " +
			"through the existing commit/edit_transaction/resolve-occurrence-match tools, never applied " +
			"automatically. If this instance has no typesafe.ai key configured, \"configured\" is false and " +
			"\"suggestions\" is empty — not an error.",
		Tier:        ports.MCPToolTierRead,
		InputSchema: importBatchInputSchema(),
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args getImportSuggestionsArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.SuggestForImportBatch(ctx, app.SuggestForImportBatchQuery{
				ActorID: actorID, ImportBatchRef: args.ImportID,
			})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(importSuggestionsViewFrom(result))
		},
	}
}
