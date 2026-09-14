package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// importBatchArgs is commit_import's and rollback_import's shared
// argument shape -- a single import batch reference, matching
// internal/surface/http's own ImportBatchRef command field and the CLI's
// "commit <import-id>"/"rollback <import-id>" positional argument.
type importBatchArgs struct {
	ImportID string `json:"import_id"`
}

func importBatchInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"import_id": map[string]any{
				"type":        "string",
				"description": "The import batch's ID.",
			},
		},
		"required":             []string{"import_id"},
		"additionalProperties": false,
	}
}

// importBatchView is this package's shape for one import batch, mirroring
// internal/surface/cli/import.go's own importBatchView field for field.
type importBatchView struct {
	ID              string `json:"id"`
	SourceFormat    string `json:"source_format"`
	Filename        string `json:"filename"`
	TargetAccountID string `json:"target_account_id"`
	Status          string `json:"status"`
}

// importBatchViewFrom takes app.GetImportBatchResult, not a bare domain
// importing.ImportBatch, so this package never has to name that type
// directly -- internal/surface packages never import internal/domain
// directly (§2's layer table); every field below is read off the
// already-typed r.Batch value Go infers from app's own result struct.
func importBatchViewFrom(r app.GetImportBatchResult) importBatchView {
	b := r.Batch
	return importBatchView{
		ID:              b.ID(),
		SourceFormat:    b.SourceFormat(),
		Filename:        b.Filename(),
		TargetAccountID: b.TargetAccountID(),
		Status:          string(b.Status()),
	}
}

// importCommitView is commit_import's result shape, mirroring
// internal/surface/cli/import.go's own importCommitView field for field.
type importCommitView struct {
	Batch        importBatchView   `json:"batch"`
	Transactions []transactionView `json:"transactions"`
}

func importCommitViewFrom(r app.CommitImportBatchResult) importCommitView {
	v := importCommitView{Batch: importBatchViewFrom(app.GetImportBatchResult{Batch: r.Batch})}
	for _, txn := range r.Transactions {
		v.Transactions = append(v.Transactions, transactionViewFrom(app.TransactionResult{Transaction: txn}))
	}
	return v
}

// importRollbackView is rollback_import's result shape, mirroring
// internal/surface/cli/import.go's own importRollbackView field for field.
type importRollbackView struct {
	Batch          importBatchView `json:"batch"`
	TransactionIDs []string        `json:"transaction_ids"`
}

func importRollbackViewFrom(r app.RollbackImportBatchResult) importRollbackView {
	return importRollbackView{
		Batch:          importBatchViewFrom(app.GetImportBatchResult{Batch: r.Batch}),
		TransactionIDs: r.TransactionIDs,
	}
}

// describeCommitImport renders commit_import's confirmation-token
// description. It reports how many staged records are actually ready to
// become transactions -- app.Service.ListImportRecords is an existing
// read-only method (no new app-layer logic added for this), so this can
// state a real count rather than just naming the batch. If the batch
// itself can't be committed (wrong status, unresolved review items),
// GetImportBatch/ListImportRecords still surface enough to describe the
// batch by name; commit_import's own precondition errors are then
// reported the normal way when the confirming call actually reaches
// Execute.
func describeCommitImport(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (string, error) {
	var args importBatchArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}

	batchResult, err := svc.GetImportBatch(ctx, app.GetImportBatchQuery{ActorID: actorID, ImportBatchRef: args.ImportID})
	if err != nil {
		return "", err
	}
	recordsResult, err := svc.ListImportRecords(ctx, app.ListImportRecordsQuery{ActorID: actorID, ImportBatchRef: args.ImportID})
	if err != nil {
		return "", err
	}

	// "ready" is compared as a string, not the domain package's own
	// importing.ImportRecordStatusReady constant: internal/surface
	// packages never import internal/domain directly (§2's layer table),
	// and Status()'s returned domain type still compares fine against a
	// plain string literal.
	ready := 0
	for _, rec := range recordsResult.Records {
		if string(rec.Status()) == "ready" {
			ready++
		}
	}

	return fmt.Sprintf(
		"This will commit import batch %s (file %q, currently %s): %d of %d staged record(s) will become real transactions in your ledger.",
		batchResult.Batch.ID(), batchResult.Batch.Filename(), batchResult.Batch.Status(), ready, len(recordsResult.Records),
	), nil
}

// commitImportTool wires app.Service.CommitImportBatch onto a
// destructive-tier MCP tool -- ADR-0013 names "import commit" explicitly
// as a destructive operation.
func commitImportTool() ToolDef {
	return ToolDef{
		Name:        "commit_import",
		Description: "Commit a staged import: write its cleared records as real transactions. Requires confirmation: call once to see what would be committed, then again with the returned confirmation_token to actually commit it.",
		Tier:        ports.MCPToolTierDestructive,
		InputSchema: importBatchInputSchema(),
		Describe:    describeCommitImport,
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args importBatchArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.CommitImportBatch(ctx, app.CommitImportBatchCommand{ActorID: actorID, ImportBatchRef: args.ImportID})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(importCommitViewFrom(result))
		},
	}
}

// describeRollbackImport renders rollback_import's confirmation-token
// description. It reports how many currently-live transactions the batch
// would soft-delete, via app.Service.ImportBatchTransactions -- an
// existing read-only method, not new app-layer logic -- rather than just
// naming the batch.
func describeRollbackImport(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (string, error) {
	var args importBatchArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}

	batchResult, err := svc.GetImportBatch(ctx, app.GetImportBatchQuery{ActorID: actorID, ImportBatchRef: args.ImportID})
	if err != nil {
		return "", err
	}
	txnsResult, err := svc.ImportBatchTransactions(ctx, app.ImportBatchTransactionsQuery{ActorID: actorID, ImportBatchRef: args.ImportID})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"This will roll back import batch %s (file %q, currently %s): %d transaction(s) it created will be soft-deleted from your ledger.",
		batchResult.Batch.ID(), batchResult.Batch.Filename(), batchResult.Batch.Status(), len(txnsResult.Transactions),
	), nil
}

// rollbackImportTool wires app.Service.RollbackImportBatch onto a
// destructive-tier MCP tool -- ADR-0013 names "rollback" explicitly as a
// destructive operation.
func rollbackImportTool() ToolDef {
	return ToolDef{
		Name:        "rollback_import",
		Description: "Undo a committed import: soft-delete the transactions it created. Requires confirmation: call once to see what would be rolled back, then again with the returned confirmation_token to actually roll it back.",
		Tier:        ports.MCPToolTierDestructive,
		InputSchema: importBatchInputSchema(),
		Describe:    describeRollbackImport,
		Execute: func(ctx context.Context, svc *app.Service, actorID string, raw json.RawMessage) (*sdkmcp.CallToolResult, error) {
			var args importBatchArgs
			if err := decodeArgs(raw, &args); err != nil {
				return errorResult(err), nil
			}

			result, err := svc.RollbackImportBatch(ctx, app.RollbackImportBatchCommand{ActorID: actorID, ImportBatchRef: args.ImportID})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonResult(importRollbackViewFrom(result))
		},
	}
}
