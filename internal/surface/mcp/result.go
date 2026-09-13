package mcp

import (
	"errors"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// resultCodeKey is the StructuredContent key errorResult stashes an
// *errs.Error's registry code under, so recordAudit (dispatcher.go) can
// recover which code failed without re-deriving it from rendered text.
const resultCodeKey = "code"

// textResult renders a successful tool call as a single plain-text
// content block — every tool in this package's result shape, since none
// of them (yet) needs structured output.
func textResult(text string) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}},
	}
}

// errorResult renders err as the "Tool Execution Error" half of the
// spec's own split (ADR-0013's Decision, quoting
// https://modelcontextprotocol.io/specification/2025-06-18/server/tools#error-handling):
// a normal CallToolResult with IsError set and a safe, human-readable
// message — never an MCP protocol-level error. When err is an
// *errs.Error, its registry code is also stashed in StructuredContent
// (resultCodeKey) so the dispatcher's audit write can record which code
// failed; any other error is treated as errs.Internal, the same fallback
// internal/surface/cli.RenderError uses for a non-*errs.Error.
func errorResult(err error) *sdkmcp.CallToolResult {
	code := errs.Internal
	message := errs.DefaultMessage(errs.Internal)

	var e *errs.Error
	if errors.As(err, &e) {
		code = e.Code
		message = e.CLIMessage()
	}

	return &sdkmcp.CallToolResult{
		IsError:           true,
		Content:           []sdkmcp.Content{&sdkmcp.TextContent{Text: message}},
		StructuredContent: map[string]any{resultCodeKey: string(code)},
	}
}

// auditResultCode reports the string recordAudit should store in
// mcp_tool_call.result for a completed call (ADR-0013): "ok" for a
// successful result, or the *errs.Error code errorResult stashed in
// StructuredContent for one that failed. A nil result, or one with no
// recognisable code, is recorded as errs.Internal — a tool that set
// IsError without going through errorResult is itself a bug worth a
// generic-but-honest code rather than a silent "ok".
func auditResultCode(result *sdkmcp.CallToolResult) string {
	if result == nil || !result.IsError {
		return "ok"
	}
	if m, ok := result.StructuredContent.(map[string]any); ok {
		if code, ok := m[resultCodeKey].(string); ok && code != "" {
			return code
		}
	}
	return string(errs.Internal)
}
