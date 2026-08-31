# ADR-0011 — Error model: eight coarse codes and the safe/internal split

**Status:** Accepted · 2026-08-31

## Context

Four surfaces need to report failures: the CLI prints them, the REST API returns them with a status, MCP hands them to an agent, and the web UI attaches them to a field. Under [ADR-0005](0005-shared-application-layer.md) none of those surfaces may *decide* anything, so an error has to arrive from the application layer already carrying everything each surface needs to render it.

Idiomatic Go — `fmt.Errorf("account %q not found", ref)` all the way up — cannot do that. A surface receiving a string cannot choose an HTTP status, cannot decide an exit code, cannot attach the failure to a form field, and cannot tell a user-caused problem from a bug. Every surface then invents its own mapping, which is the exact divergence [ADR-0005](0005-shared-application-layer.md) exists to prevent, on the path users hit when they're already frustrated.

There is also a leak risk that gets worse in financial software. A wrapped driver error carries SQL fragments, file paths, and schema names. Returned verbatim, it's noise to the user and detail an attacker doesn't need.

### The apparent conflict in the project's own rules

CLAUDE.md requires errors to be *"detailed and specific (what failed, which ID, the underlying cause/error chain) since that's necessary for debugging."* It also forbids PII in errors and logs. Those pull against "end-user safe".

They only conflict if "detail" is treated as one thing. It isn't:

| Kind of detail | Goes to the user | Goes to the log |
| --- | --- | --- |
| **Domain detail** — which account, which ID, which field, which value was rejected | **Yes, always.** This is what makes an error actionable, and it's what CLAUDE.md is asking for | Codes and IDs only |
| **Infrastructure detail** — the wrapped cause chain, SQL, driver text, file paths, stack | No | **Yes, in full** |
| **Financial PII** — amounts, payee, description, notes | Only values the user just typed, shown back to them | **Never** |

Nothing is lost for debugging: the cause chain still exists in full, in the log. This is a single-user self-hosted application where the operator *is* the user, reading both the error and the log on the same machine — so being specific about *their own domain* is correct, and being specific about *the database* is neither useful to them nor safe.

## Decision

### One central registry of coarse codes

A small, closed set. Each entry is a stable code string plus a short, human-friendly default message, defined once in `internal/platform/errs`:

| Coarse code | Meaning | HTTP | CLI exit |
| --- | --- | --- | --- |
| `invalid_input` | The request was malformed or failed validation | 422 | 2 |
| `not_found` | The named thing doesn't exist | 404 | 3 |
| `conflict` | Would violate uniqueness or a constraint | 409 | 4 |
| `precondition_failed` | Valid, but not allowed in the current state | 412 | 5 |
| `not_allowed` | The actor may not do this | 403 | 6 |
| `unauthenticated` | No valid credential | 401 | 7 |
| `unavailable` | A dependency is down or data is missing | 503 | 8 |
| `internal` | A bug. Never the user's fault | 500 | 1 |

**That mapping table lives in the registry, not in the surfaces.** Defining it once is the error-path application of [ADR-0005](0005-shared-application-layer.md); a handler choosing its own status code is the same class of defect as a handler parsing its own date.

### The error value

```go
err := errs.New(errs.NotFound).
        Explain("No account called %q.", ref).   // end-user safe, caller-supplied
        With("candidates", names)                // structured, end-user safe
        Wrap(cause)                              // internal only. Never serialised
```

| Field | Reaches the user | Purpose |
| --- | --- | --- |
| `Code` | Yes | One of the eight coarse codes |
| `Message` | Yes | The caller's `Explain` text, else the registry's default for the code |
| `Field` | Yes | Field path, for form and flag attribution |
| `Details` | Yes | Structured, deliberately safe — candidate names, valid options |
| `cause` | **Never** | The wrapped chain. Logged in full |

**`cause` is unexported and has no serialiser.** Leaking it requires writing new code to do so, rather than forgetting to prevent it. Every `internal` error is logged with its full chain.

There is no correlation ID. This is a single-user, single-process, self-hosted application: the person reading the error is the person who can read the log, usually in the same terminal on the same machine. A `Ref` would need plumbing through context into the logger on all four surfaces to save that person a `grep`. Revisit when the web UI means the user isn't looking at the log.

Messages are held to [`ux-principles.md` §6](../ux-principles.md#6-errors-and-telling-the-truth): name what failed, which value, and what to do. *"No account called 'hdcf'. Did you mean 'HDFC Savings'?"* — not *"invalid account reference"*.

### Eight codes, flat

There is no code hierarchy, no dotted nesting, and no promotion mechanism. Eight coarse codes, each with a default message, plus a caller-supplied explanation where the default isn't specific enough. That is the whole model.

A finer-grained code becomes worth adding the moment a real client needs to branch on one — a web UI offering "create it?" on a missing account, say. When that happens, add the code and decide its compatibility story against the actual consumer that would break. Designing a nesting scheme and a prefix-matching guarantee now would be protecting a client that does not exist from a change that has not happened.

### Codes are a public contract

Once shipped, a code is API surface: scripts branch on CLI exit codes, agents branch on MCP error codes, the web UI branches on the wire code. Codes are never renamed or removed, only added and superseded. A registry test enumerates the shipped set and fails on removal, so it takes a deliberate edit rather than a refactor.

## Alternatives considered

**Idiomatic `fmt.Errorf` with sentinel errors and `errors.Is`.** The Go default, and fine for a library. Rejected: sentinels give a surface a boolean, not a rendering — no message, no field path, no status mapping, no safe/internal split. Each surface would rebuild all four, differently.

**Per-surface error handling.** What happens without this ADR. Rejected under [ADR-0005](0005-shared-application-layer.md): four mappings that drift, on the path users hit when something has already gone wrong.

**A nested code hierarchy with promotion** — `not_found.account` under `not_found`, with prefix matching so promoting a code never breaks a client. Considered, and cut as speculative: there are no clients, so there is nothing to keep compatible, and the machinery (nesting rules, a promotion threshold, a documented segment-boundary matching guarantee) is real complexity bought against a hypothetical. Eight flat codes plus a free-text explanation covers every case M1 has. Add specificity when something concrete needs it.

**Design a fine-grained code per failure up front.** Rejected: it guesses which distinctions matter before any surface has needed one, and the guesses become a registry of codes nobody reads. Real usage should decide, and until it does, eight codes plus an explanation is enough.

**HTTP status codes as the canonical code.** Rejected: couples the domain to HTTP, and the CLI and MCP have no statuses. Status is a *rendering* of a code, which is why it lives in the mapping table.

**Return the full cause chain to the user, per a literal reading of CLAUDE.md.** Rejected as a misreading, resolved in Context: the cause chain is preserved in full, in the log. The user gets the domain detail, which is the part they can act on.

**A translated message catalogue from day one.** Rejected as premature — but the registry is precisely the structure that makes i18n a later change rather than a rewrite, since messages already live in one place keyed by code.

## Consequences

**Good.** A surface renders an error without deciding anything. Status codes and exit codes are defined once. Internal detail cannot leak without new code being written to leak it. Eight codes is small enough to hold in your head.

**Bad, and worth stating plainly:**

- **The registry is a shared-file hotspot.** Nearly every feature slice adds a code, so parallel branches will collide there — added to the `orchestrate` hotspot table.
- **It takes discipline that nothing yet enforces.** `fmt.Errorf` is right there, and it will get used. The intended guard is a check that exported app-layer methods return only `*errs.Error`; that isn't built, and until it is this is a review responsibility. Filed as [#12](https://github.com/anirudhgray/bodger/issues/12), alongside the vocabulary lint ([#11](https://github.com/anirudhgray/bodger/issues/11)) — same shape of gap.
- **Eight codes is coarse, and some errors will feel vague.** The explanation text carries the specificity instead, which means it can't be branched on programmatically. That's the accepted trade until a real client needs otherwise.
- **Codes being permanent means a badly-named one is permanent.** Superseding is the only exit, and it leaves both in the registry.
- **No correlation ID** means matching a user-reported error to a log line is a `grep` by time and message rather than an exact lookup. Fine for one user on one machine; the first thing to revisit if that stops being true.
- **Two places to look** when writing an error: the registry for the code, the call site for the explanation. That's the cost of the message not being at the call site.
- **The registry's default messages are user-facing strings** and are therefore subject to [`ux-principles.md` §2](../ux-principles.md#2-vocabulary) — with no automated check until [#11](https://github.com/anirudhgray/bodger/issues/11) lands.
