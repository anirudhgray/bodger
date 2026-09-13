# ADR-0013 — MCP server design: tiering, confirmation, and audit

**Status:** Accepted · 2026-09-13

## Context

[`docs/architecture.md` §3](../architecture.md#3-surface-boundaries) already fixes the shape MCP tools must take, deliberately, ahead of the milestone that builds them:

- MCP tools map one-to-one onto app-layer methods. They never touch the repository layer or emit SQL.
- Three tiers, classified explicitly: **read** (always allowed), **write** (allowed, audited), **destructive** (deletion, bulk edits, import commit, rollback — requires explicit confirmation, disabled by default).
- The MCP server reaches the application layer the same way the CLI does: an in-process call, no HTTP round trip, no credential of its own. [ADR-0006](0006-authentication-and-multi-user-path.md) already settled this — filesystem permissions on the database file are the boundary for local CLI and MCP access, same as they are for the CLI today.

What that leaves open, and what this ADR decides, is everything mechanical: where in the code the tiers are actually enforced, what "requires explicit confirmation" means as a protocol an agent can follow, whether write/destructive activity is recorded anywhere durable, and how the surface-conformance suite ([ADR-0005](0005-shared-application-layer.md), architecture.md §6) picks up MCP as its third leg rather than staying a CLI/HTTP-only check forever, which is itself already a documented gap.

An MCP server is a different trust boundary from the CLI even though both are in-process and unauthenticated at the transport level: a human typing `bodger tx delete` decided to do that, in that moment. An agent calling a `delete_transaction` tool is acting on an instruction that may be several steps removed from anything the human actually reviewed. The tiering exists precisely for this gap, and the mechanics below exist to make the gap real rather than decorative.

## Decision

### Tiering is enforced by one dispatcher, not per-tool checks

Every tool is registered in `internal/surface/mcp/tools.go` with a declared tier (`read`, `write`, `destructive`) alongside its name, schema, and the app-layer method it calls. Registration does not call that method directly; it hands the method to a single dispatcher that every tool call passes through. The dispatcher, not the individual tool handler, is what:

- refuses to register any `destructive`-tier tool at all unless the server was started with `--allow-destructive` (see below) — so an agent introspecting the tool list on a server started without the flag does not even see them, rather than seeing them and being refused at call time,
- writes the audit record (see below) for every `write` and `destructive` call, before and after invocation,
- runs the confirmation protocol for `destructive` calls.

A future tool is added by registering it with a tier; it cannot accidentally skip enforcement by being written differently from its neighbours, because there is no path to the app layer that bypasses the dispatcher.

### A tool's tier is also expressed as the spec's own tool annotations, not just bodger-internal metadata

The MCP spec defines a standard `annotations` object on every tool returned from `tools/list` — `readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`, plus a display `title`. This is the mechanism the spec actually gives a client for knowing a tool is destructive; the bodger-internal tier above is what bodger's own dispatcher enforces, but a client has no visibility into that unless it's also stated in the vocabulary the client understands. The dispatcher sets these directly from a tool's declared tier at registration time — `read` → `readOnlyHint: true`; `write` → `readOnlyHint: false, destructiveHint: false`; `destructive` → `readOnlyHint: false, destructiveHint: true` — so the two never drift apart, and a compliant client (Claude Desktop, Claude Code, and others already do this) can drive its own confirmation UI off `destructiveHint` without bodger inventing a parallel signal. The Go SDK's tool registration already carries an annotations struct for exactly this, so setting it accurately is close to free.

### Destructive tools are disabled by default, at startup, the same way M1 refused an insecure bind

`bodger mcp` does not register any `destructive`-tier tool unless started with `--allow-destructive`. This is the same shape ADR-0006 already chose for the non-loopback-bind-without-auth case: an explicit, named opt-in rather than a runtime prompt that's easy to click through without reading. A destructive-tier tool absent from the list is a stronger signal to a calling agent (and to whoever configured it) than one present but gated, and it means a read/write-only deployment carries zero destructive-tool surface area to reason about at all.

### Confirmation has two layers: the client's own UI, and a dispatcher-level backstop that doesn't trust the client at all

The first layer is the `destructiveHint` annotation above, plus the spec's own guidance that clients **should** prompt for confirmation on operations it flags as destructive. This is the intended, spec-native mechanism, and it's the one a human actually sees rendered as a real approval dialog in a compliant client — bodger doesn't have to build any UI for it.

The MCP spec's elicitation capability (a server asking the connected client to collect structured input mid-call) was considered as the mechanism for this instead, and rejected: elicitation is specified for gathering data the server needs to proceed (a missing field, a choice among options), and the spec explicitly says a server **must not** use it to collect sensitive information — a yes/no "are you sure you want to delete this" is exactly the kind of action-gate it isn't designed to be, and treating it as one would be relying on an off-label use of the capability for the one thing that most needs to be right.

Because the dispatcher cannot assume every connected client actually renders a confirmation prompt on `destructiveHint` — a script driving an MCP client programmatically has no UI to show one in — the dispatcher runs its own gate underneath the annotation, uniformly, regardless of what the client does: the first call to a `destructive`-tier tool does not execute it. It returns a description of the effect (in the same terms the tool's own result would report — which transaction, which import batch, which snapshot) and a short-lived, single-use `confirmation_token`. The tool only executes when called again with that token attached, and the token is rejected if reused, expired, or attached to a call whose arguments don't match what it was issued for. A boolean `confirm: true` argument on a single call was considered and rejected (see below) — the token exists specifically so that "confirm" cannot be something an agent can pass reflexively without any actual pause.

This backstop is handled once, in the dispatcher, not per destructive tool, and applies whether or not the calling client has any confirmation UI of its own — annotations are the signal a good client acts on; the token is what bodger enforces regardless of whether the client is a good client.

### Write and destructive activity is recorded in a durable, queryable audit log — not just a debug-level log line

A new table, `mcp_tool_call` (next sequential migration prefix per [ADR-0007](0007-persistence-and-migrations.md)), records one row per `write` or `destructive` tool invocation: `id`, `user_id`, `tool_name`, `tier`, `arguments` (JSON, with a documented redaction list so no credential or token value is ever persisted), `confirmation_token` (nullable — set only for a destructive call that used the token protocol), `result` (`ok` or an `*errs.Error` code), and `called_at` (timestamptz, UTC, via the injected clock per architecture.md §6).

`result` follows the spec's own split between protocol errors and tool-execution errors. A business-logic failure (validation, not-found, insufficient permission) is not a JSON-RPC error — it's a normal `CallToolResult` with `isError: true` and a text explanation, and that's what a non-`ok` `result` in this table records. A malformed call the schema itself rejects (unknown tool, arguments that don't match the declared schema) is a protocol-level JSON-RPC error instead, raised before the dispatcher runs at all — it never reaches this table, since no tool invocation happened for it to describe. The Go SDK already separates these two paths (`CallToolResult.IsError` vs. a returned protocol error); the dispatcher's job is only to write the row on the former, not reimplement the split.

`read`-tier calls are not recorded here — read access is always allowed and unbounded query logging of every balance lookup an agent makes has no product use and a real storage cost. This mirrors `transaction_revision` (data-model.md §7): that table answers "what did I change to this transaction," recorded because editing is mutable and needs a trail; this table answers "what did an agent do to my ledger," recorded for the same reason a mutable, agent-driven write path needs one. Neither is read by analytics.

A `bodger mcp audit` CLI command (read-tier by construction — it only queries this table) lists recent entries, so the answer to "what did the agent I connected last week actually do" doesn't require reading raw SQLite.

### The conformance suite's third leg drives cases through MCP in-process, no subprocess

`internal/surface/conformance` gains an MCP dispatch path alongside its existing CLI and HTTP ones, using the official SDK's in-memory client/server transport rather than spawning `bodger mcp` as a subprocess — the suite already runs the CLI and HTTP paths in-process against the same test harness, and an in-memory MCP transport keeps that property rather than introducing a slower, flakier subprocess boundary for the third leg alone. Only cases exercising a `write`-tier record verb (outflow/inflow/transfer, matching the suite's existing coverage) gain an MCP assertion; `read`-tier tools have no existing case-table entries to extend, and adding a table of read-only cases is left to whichever issue first needs one, not invented here speculatively.

## Alternatives considered

**Route MCP through the REST API over HTTP instead of an in-process dispatcher.** Rejected already, for the whole server, in [ADR-0005](0005-shared-application-layer.md) and architecture.md §3: it would require an HTTP server to be running before a local agent could read a balance, and would make the MCP server a network client of a service usually sharing its machine. Nothing about tiering or confirmation changes that answer.

**A `confirm: true` boolean argument on the same tool call, instead of a separate call with a token.** Simplest to implement. Rejected: nothing stops an agent (or the system prompt driving it) from always passing `confirm: true`, which makes "requires confirmation" a documentation claim rather than a mechanism. A separate call, with a token an agent cannot fabricate, is what makes the pause real.

**Using MCP elicitation as the confirmation mechanism itself.** Considered first, since it's the capability in the spec that sounds like it fits. Rejected: elicitation is specified for the server to request structured input it needs to proceed, and the spec explicitly says it must not be used to collect sensitive information — a destructive yes/no gate isn't the form-filling use case it's designed for, and building the one safety-critical mechanism in this ADR on an off-label use of the spec is exactly the kind of thing to avoid.

**Relying solely on `destructiveHint` and the client's own confirmation UI, with no dispatcher-level gate.** Spec-native and requires no bodger-side state. Rejected as the *only* mechanism: nothing guarantees a connected client actually implements a confirmation prompt — a script driving an MCP client has no UI to show one in — and bodger's own safety net shouldn't depend on trusting every client's implementation quality. Annotations remain the signal a good client acts on; the token gate is what's enforced either way.

**Log write/destructive activity to the existing structured application log instead of a database table.** Cheaper — no migration, no new repository. Rejected: the log is rotated/operational and not meant to answer a durable "what has this agent done to my finances" question days or weeks later; a table queryable by actor and time, parallel to `transaction_revision`, is worth the one small migration.

**No default-off gate for destructive tools — ship them enabled, confirmation-only.** Rejected: it means every `bodger mcp` invocation carries destructive capability unless the user remembers to configure otherwise, the same shape ADR-0006 already rejected for network exposure. An explicit flag makes the safer state the default state.

**Enforce tiers inside each tool handler rather than a shared dispatcher.** Keeps registration flat. Rejected: it means every new tool re-implements (or forgets to implement) the audit write and the confirmation check, exactly the kind of per-surface drift ADR-0005 exists to prevent one layer up.

## Consequences

**Good.** Tiering, confirmation, and audit are enforced structurally in one place; a new tool cannot ship without them by construction. The destructive-disabled-by-default flag gives an operator a single, legible switch. The audit table gives a real answer to "what did the agent do," queryable without touching SQLite directly. The conformance suite closes a gap that was already documented as owed.

**Bad:**

- **`mcp_tool_call` is a new table with its own migration and repository** — small, but it is new persistence surface for something that isn't ledger data.
- **Destructive tools disabled by default will frustrate an agent workflow that wants zero friction for, say, import rollback.** Deliberate, same trade-off ADR-0006 already made for the non-loopback bind case: the annoyed case is the safe default working as intended.
- **The confirmation-token protocol needs the dispatcher to hold short-lived state** (issued tokens, their expiry, and the arguments they were issued against) — in-memory is sufficient since a token's lifetime is a single MCP session, but it is state the dispatcher didn't need before.
- **A destructive tool call now always takes two round trips**, even against a client with a perfectly good confirmation UI already honouring `destructiveHint`. Accepted: the alternative is trusting client behaviour bodger can't verify, for the one category of operation where being wrong is expensive.
- **No rate limiting**, which the spec's security guidance lists as a server `MUST`. Deliberately out of scope: it's aimed at multi-tenant/remote servers under load or abuse from an untrusted network client; bodger's MCP server is single-user, local, stdio-only, and already gated by filesystem permissions per ADR-0006 — the same reasoning that exempts it from the spec's OAuth/token-passthrough guidance, which targets remote HTTP transports bodger doesn't have.
