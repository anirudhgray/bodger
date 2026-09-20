package app

import (
	"encoding/json"
	"strings"
)

// mcpRedactedKeys is ADR-0013's "documented redaction list" — every JSON
// object key below has its value replaced with mcpRedactedPlaceholder
// before an MCP tool call's arguments are ever persisted to
// mcp_tool_call, so a credential or token an agent happened to pass never
// reaches durable storage. Matched case-insensitively, at any depth: a
// nested object's own "password" field is redacted exactly like a
// top-level one. Extend this list, not each call site, if a future tool
// takes a differently-named sensitive field.
var mcpRedactedKeys = map[string]bool{
	"password":           true,
	"confirmation_token": true,
	"token":              true,
	"api_token":          true,
	"secret":             true,
	"credential":         true,
	"authorization":      true,
	// api_key and typesafe_api_key: ADR-0015's typesafe.ai credential has
	// no route into MCP arguments today — no tool takes one. Listed here
	// anyway as defence in depth against a future tool that does, per this
	// list's own doc comment above.
	"api_key":          true,
	"typesafe_api_key": true,
}

// mcpRedactedPlaceholder replaces every redacted value.
const mcpRedactedPlaceholder = "[redacted]"

// redactMCPArguments renders raw as a JSON string with every value under
// a key in mcpRedactedKeys replaced, at any depth. This is a best-effort
// hygiene pass over arguments a tool's own schema already accepted, not a
// validator: invalid or empty JSON is stored as-is (empty as "{}") rather
// than rejected, since RecordMCPToolCall's caller (the mcp surface's
// dispatcher) already decided the call happened and is worth recording —
// refusing to record it because its arguments don't round-trip would lose
// the one fact the audit table exists to keep.
func redactMCPArguments(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	redactValue(v)
	out, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

// redactValue walks v in place, replacing any map value whose key is in
// mcpRedactedKeys and recursing into every other map value and array
// element.
func redactValue(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if mcpRedactedKeys[strings.ToLower(k)] {
				t[k] = mcpRedactedPlaceholder
				continue
			}
			redactValue(val)
		}
	case []any:
		for _, e := range t {
			redactValue(e)
		}
	}
}
