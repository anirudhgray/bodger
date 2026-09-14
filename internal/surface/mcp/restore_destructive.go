package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// restoreSnapshotArgs is restore_snapshot's arguments: the backup document
// itself, carried inline as a JSON value rather than a filesystem path --
// unlike `bodger restore [file]` (internal/surface/cli/restore.go), an MCP
// client has no access to this process's own filesystem, so the document
// has to travel as part of the tool call.
type restoreSnapshotArgs struct {
	Document json.RawMessage `json:"document"`
}

func restoreSnapshotInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"document": map[string]any{
				"type":        "object",
				"description": "The bodger.export/v1 backup document to restore, exactly as produced by `bodger export json` or the export_json tool.",
			},
		},
		"required":             []string{"document"},
		"additionalProperties": false,
	}
}

// restoreSnapshotView is restore_snapshot's result shape, mirroring
// internal/surface/cli/restore.go's own restoreSnapshotView field for
// field.
type restoreSnapshotView struct {
	Accounts     int `json:"accounts"`
	Categories   int `json:"categories"`
	Transactions int `json:"transactions"`
	Budgets      int `json:"budgets"`
}

func restoreSnapshotViewFrom(r app.RestoreSnapshotResult) restoreSnapshotView {
	return restoreSnapshotView{Accounts: r.Accounts, Categories: r.Categories, Transactions: r.Transactions, Budgets: r.Budgets}
}

// restoreDocumentCounts is the minimal shape this file peeks at the
// document with to describe a restore's effect before doing it -- just
// the top-level arrays' lengths, via a plain encoding/json decode of the
// caller's own argument. This is not app-layer validation: a malformed or
// invalid document (a bad date, a dangling reference, a wrong format
// version) is still only ever caught the normal way, inside
// app.Service.RestoreSnapshot itself, when the confirming call actually
// reaches Execute. Describe only reports what it can safely read off the
// document's own shape, per the issue's own scope: "describe using
// whatever identifying information is available in the args themselves
// rather than fabricating counts you'd have to add new app logic to get."
type restoreDocumentCounts struct {
	Accounts     []json.RawMessage `json:"accounts"`
	Categories   []json.RawMessage `json:"categories"`
	Transactions []json.RawMessage `json:"transactions"`
	Budgets      []json.RawMessage `json:"budgets"`
}

// describeRestoreSnapshot renders restore_snapshot's confirmation-token
// description. It tries to report how many accounts/categories/
// transactions/budgets the document contains (a plain top-level count,
// not a validated one); if the document doesn't even parse as an object
// with those array fields, it falls back to a generic description rather
// than failing Describe outright -- the actual validation (format
// version, structural correctness) always happens in
// app.Service.RestoreSnapshot on the confirming call.
func describeRestoreSnapshot(_ context.Context, _ *app.Service, _ string, raw json.RawMessage) (string, error) {
	var args restoreSnapshotArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	if len(args.Document) == 0 {
		return "", errs.New(errs.InvalidInput).Explain("A backup document is required.").Field("document")
	}

	var counts restoreDocumentCounts
	if err := json.Unmarshal(args.Document, &counts); err != nil {
		return "This will wipe and replace every account, category, transaction, and budget you have with the contents of the supplied document. This cannot be undone.", nil
	}

	return fmt.Sprintf(
		"This will wipe and replace every account, category, transaction, and budget you have with the contents of the supplied document: "+
			"%d account(s), %d category(-ies), %d transaction(s), %d budget(s). This cannot be undone -- your current data is replaced, not merged.",
		len(counts.Accounts), len(counts.Categories), len(counts.Transactions), len(counts.Budgets),
	), nil
}

// restoreSnapshotTool wires app.Service.RestoreSnapshot onto a
// destructive-tier MCP tool -- the same wipe-and-replace operation
// `bodger restore` gates behind --yes, gated here instead behind
// ADR-0013's confirmation-token protocol.
func restoreSnapshotTool() ToolDef {
	return ToolDef{
		Name:        "restore_snapshot",
		Description: "Replace everything you have with a JSON backup document. Requires confirmation: call once to see what would be replaced, then again with the returned confirmation_token to actually restore it.",
		Tier:        ports.MCPToolTierDestructive,
		InputSchema: restoreSnapshotInputSchema(),
		Describe:    describeRestoreSnapshot,
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args restoreSnapshotArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}
			if len(args.Document) == 0 {
				return errorResult(errs.New(errs.InvalidInput).Explain("A backup document is required.").Field("document")), nil
			}

			result, err := svc.RestoreSnapshot(ctx, app.RestoreSnapshotQuery{ActorID: actorID, Document: args.Document})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(restoreSnapshotViewFrom(result))
		},
	}
}
