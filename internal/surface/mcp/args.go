package mcp

import (
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// decodeArgs unmarshals raw into dst, treating an empty/absent raw as
// "leave dst at its zero value" -- a tool whose arguments are entirely
// optional (e.g. get_account_balances with nothing set) is called with no
// arguments at all, the same way whoami is called with none. A payload
// that doesn't decode is reported as errs.InvalidInput, rendered by the
// caller via errorResult: the tool's own JSON Schema (InputSchema) is
// advisory to a well-behaved client, not something the dispatcher
// validates against before Execute runs, so a malformed shape still has
// to be caught here.
func decodeArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return errs.New(errs.InvalidInput).Explain("couldn't understand this tool's arguments: %s", err.Error())
	}
	return nil
}

// jsonResult renders v -- one of this package's own JSON-tagged view
// structs, the read-tier mirror of the "view" convention
// internal/surface/cli and internal/surface/http already use to render an
// app-layer result -- as a successful tool call's result: a single
// indented-JSON text block. This package reproduces that convention
// rather than importing either surface's own view types (internal/surface
// packages never import one another, matching how neither imports
// internal/domain directly -- docs/architecture.md §2's layer table has no
// surface-to-surface edge). Plain text (not the SDK's separate
// StructuredContent path) keeps every read-tier tool's result the same
// shape whoami's textResult already established, just carrying richer
// content.
func jsonResult(v any) (*sdkmcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(errs.New(errs.Internal).Wrap(err)), nil
	}
	return textResult(string(data)), nil
}

// mergeSchemaProperties unions any number of JSON Schema "properties"
// fragments into one map, so a tool that takes a shared shape (e.g.
// transactionFilterSchemaProperties) plus a few fields of its own can
// build its InputSchema without repeating the shared fragment's own
// property definitions.
func mergeSchemaProperties(fragments ...map[string]any) map[string]any {
	merged := make(map[string]any)
	for _, fragment := range fragments {
		for k, v := range fragment {
			merged[k] = v
		}
	}
	return merged
}
