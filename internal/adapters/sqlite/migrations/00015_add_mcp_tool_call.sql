-- +goose Up

-- ADR-0013 / issue #259: one row per `write`- or `destructive`-tier MCP
-- tool invocation -- "what did an agent do to my ledger", the same reason
-- transaction_revisions (migration 00008) records "what did I change to
-- this transaction". `read`-tier calls are never recorded here (ADR-0013:
-- unbounded query logging has no product use and a real storage cost).
--
-- arguments is a redacted JSON blob (internal/app's RecordMCPToolCall
-- strips anything matching its own redaction list before this table ever
-- sees it) -- deliberately loose, like transaction_revisions.previous_state,
-- since its only reader is `bodger mcp audit`, never a report.
--
-- confirmation_token is set only for a destructive call that went through
-- the confirmation-token protocol (both the issuing call and the
-- confirming one get their own row); null for a write-tier call, which
-- never uses the protocol at all.
--
-- result is "ok" or one of internal/platform/errs's eight registry codes
-- (ADR-0011) -- a business-logic failure the tool reported via
-- CallToolResult.IsError, per the spec's own error-handling split
-- (ADR-0013). A protocol-level error (unknown tool, malformed arguments)
-- never reaches the dispatcher, so it never reaches this table either.
CREATE TABLE mcp_tool_call (
    id                 TEXT NOT NULL PRIMARY KEY,
    user_id            TEXT NOT NULL REFERENCES users (id),
    tool_name          TEXT NOT NULL,
    tier               TEXT NOT NULL CHECK (tier IN ('read', 'write', 'destructive')),
    arguments          TEXT NOT NULL,
    confirmation_token TEXT,
    result             TEXT NOT NULL,
    called_at          TEXT NOT NULL
);

CREATE INDEX idx_mcp_tool_call_user_id_called_at ON mcp_tool_call (user_id, called_at DESC);

-- +goose Down
DROP TABLE mcp_tool_call;
