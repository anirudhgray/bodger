# ADR-0005 — One application layer; normalise once; and how that is enforced

**Status:** Accepted · 2026-08-31

## Context

`bodger` has four surfaces over one financial core: a CLI, a REST API, a web UI, and an MCP server. The brief's central architectural principle (§33) is that business semantics must not be duplicated across them. CLAUDE.md states the sharper version:

> Normalize input and resolve time-/environment-dependent values ("now", locale, etc.) once, in the service/application layer — never independently in each UI or API surface. […] every surface must inherit the same normalization instead of reimplementing its own version, which is what lets two surfaces silently diverge on the same underlying data.

**This is the decision most likely to be violated, and the violation is nearly invisible.** Nobody sets out to write a second date parser. What happens is smaller and entirely reasonable-looking:

- The CLI adds a `--today` convenience flag and calls `time.Now()` to implement it.
- The REST handler defaults a missing `date` field to today, because the request is otherwise valid.
- An MCP tool accepts `"yesterday"` from an agent, because agents talk like that.
- The web UI sends `new Date().toISOString()`, in the browser's timezone rather than the user's.

Each of those is three lines and passes review. Together they are four different answers to "what date is this transaction?", diverging at month boundaries and across timezones — in an application whose reports are defined by month boundaries. With two surfaces this is a bug someone eventually finds. With four, and with several agents building them in parallel from separate issues, it is the default outcome unless something mechanical prevents it.

A document saying "don't do that" does not prevent it. This ADR specifies a contract *and* the enforcement that makes the contract real.

## Decision

### One application layer, four thin adapters

```
   CLI          REST API        MCP server         Web UI
    │               │                │                │
    │               │                │      (browser — HTTP only)
    │               │                │                │
    │               └────────────────┴────────────────┘
    │                                │
    └───────────────┬────────────────┘
                    │   decode transport → command struct. Nothing else.
      ──────────────▼──────────────────────────────────
                 internal/app
       normalisation · defaults · currency precedence
       · "now" · timezone · authorisation · transactions
      ──────────────┬──────────────────────────────────
                    │
              internal/domain
```

`internal/app` exposes one method per user intent. Each takes a **command or query struct** and returns a **result struct**. There are no loose parameters, so adding a field is a compile-time event at every call site rather than a silent default somewhere.

The CLI, REST API, and MCP server call these methods **in-process** — the same Go function, not a re-implementation and not an HTTP round trip. The web UI is the only surface that crosses a network boundary, and it reaches the same methods through the REST API.

### The adapter contract

A surface may do exactly three things:

1. **Decode** transport input into a command struct — JSON body, cobra flags, MCP tool arguments.
2. **Call** one application method.
3. **Encode** the result — JSON, a table, a tool response.

A surface may **not**:

- Call `time.Now()`, or otherwise decide what "today" is.
- Parse a date, an amount, or a currency into a domain value.
- Resolve any default — currency, account, category, period, page size.
- Read the user's timezone or locale.
- Read an environment variable.
- Import `internal/domain` or any adapter package.
- Contain any `if` that is not about presentation.

### Commands carry raw input, not parsed values

This is the mechanism that makes the contract enforceable rather than aspirational. Command structs hold **the user's raw strings**:

```go
type RecordOutflowCommand struct {
    ActorID     UserID
    AccountRef  string  // "HDFC Savings", or a UUID — app resolves which
    Amount      string  // "800", "1,800.50", "₹800" — app parses
    Currency    string  // "" means: apply the precedence ladder
    CategoryRef string  // name or UUID
    Date        string  // "", "today", "yesterday", "2026-08-14", "14/08/2026"
    Description string
    Notes       string
    Tags        []string
}
```

An empty `Date` means "the app decides", and the app is the only thing that *can* decide, because it holds the injected clock and the user's timezone. A surface cannot helpfully fill it in, because it has neither.

`"today"` and `"yesterday"` are handled centrally — so the MCP server gets them for free, the CLI's `--today` flag is unnecessary, and all four surfaces agree on what they mean at 00:30 in `Asia/Kolkata`.

Normalisation is one package, `internal/app/normalize`, and it is the only place raw input becomes a domain value:

| Input | Normalised by | Rules |
| --- | --- | --- |
| Amount | `normalize.Amount` | Strips grouping separators and symbols; rejects floats; produces `int64` minor units using the currency's exponent ([ADR-0004](0004-multi-currency-and-fx.md)) |
| Currency | `normalize.Currency` | The precedence ladder: entry → account → user → instance |
| Date | `normalize.Date` | ISO first, then configured locale formats, then relative keywords; resolved in the **user's** timezone via the injected clock |
| Account / category | `normalize.Ref` | UUID, else exact name, else unique case-insensitive match, else a disambiguation error listing candidates |
| Text | `normalize.Text` | Unicode NFC, trim, collapse internal whitespace, length caps |
| Tags | `normalize.Tag` | Lowercase, strip a leading `#`, kebab-case |

### Time is injected, always

`internal/platform/clock.Clock` is a port. The application layer holds one. Production wires the real clock; tests wire a frozen one. **`time.Now()` appears in exactly one file in the repository**, and CI fails if it appears anywhere else.

This is not only about consistency between surfaces. It is what makes "does the August report include a transaction booked at 23:59 on 31 July in `Asia/Kolkata`?" a deterministic test rather than a thing that passes locally and fails at 18:30 UTC.

### Authorisation lives in the app layer

Every command and query carries an `ActorID`. The app layer decides what that actor may touch. No surface makes an authorisation decision, because then there would be four authorisation models. See [ADR-0006](0006-authentication-and-multi-user-path.md).

## Enforcement

The contract above is worth as much as its enforcement. All of this runs in `make check`, which is exactly what CI runs.

**1 · Import-graph check.** A build-failing test walks the package graph and asserts:

| Package | May import |
| --- | --- |
| `internal/domain` | nothing project-local |
| `internal/app` | `domain`, `ports`, `platform` |
| `internal/adapters/*` | `ports`, `domain`, `platform` |
| `internal/surface/*` | `app`, `platform` — **not** `domain`, **not** adapters |

A surface that cannot import `domain` cannot construct a `Money` or a `Date`, which makes "parse it in the handler" not a temptation but a compile error.

**2 · Banned-symbol check.** `time.Now()` outside `internal/platform/clock` fails. `os.Getenv` outside `internal/platform/config` fails.

**3 · The surface conformance suite.** The one that catches what the others cannot.

One table of raw inputs, each driven through all three in-process surfaces, asserting the resulting command struct is **identical**:

```
case: amount "1,800.50", currency "", date "today", account "hdfc savings"

  CLI:  bodger tx out --account "hdfc savings" --amount "1,800.50" --date today
  API:  POST /api/v1/transactions  {"account":"hdfc savings","amount":"1,800.50","date":"today"}
  MCP:  record_outflow{account:"hdfc savings", amount:"1,800.50", date:"today"}

  all three must produce byte-identical RecordOutflowCommand
  and, under a frozen clock, byte-identical results
```

The table covers: empty date, `"today"`, `"yesterday"`, ISO dates, locale-format dates, grouped amounts, currency symbols, absent currency (precedence), account and category resolution by name, ambiguous names, case differences, whitespace, Unicode normalisation, tags with and without `#`, and the invalid-input error messages.

Cases run under a frozen clock at a **deliberately hostile instant** — `2026-07-31T18:45:00Z`, which is 2026-08-01 in `Asia/Kolkata` — so any surface that resolves "today" itself, or in the wrong zone, produces a different date and fails.

The suite is a single table in `internal/surface/conformance`. **Adding a user-facing operation means adding a row.** A new surface means wiring it into the harness once; every existing row then applies to it.

This is also why the REST API ships in milestone 1 alongside the CLI rather than after it: with one surface the suite has nothing to compare, and the contract would be an untested claim for an entire milestone.

**4 · The web UI is checked differently**, since it is TypeScript and cannot join the Go conformance suite. It is constrained instead by construction: its API client is *generated* from the OpenAPI description, so it can only send what the API accepts; and an ESLint rule bans `new Date()`, `Date.now()`, and `Intl.DateTimeFormat` outside a single `src/lib/format.ts` module that is display-only and never produces a value sent to the server. When the UI needs "today", it omits the field and lets the server decide.

## Alternatives considered

**Route the CLI and MCP through HTTP to the API.** Guarantees normalise-once by construction — one process, one normaliser, no possibility of drift. Genuinely tempting, and rejected on product grounds: it means a server must be running before `bodger tx add` works, it makes a local MCP server a network client of a service on the same machine, and it puts serialisation latency in the path of the fast-entry workflow the brief cares about (§32). In-process composition achieves the same guarantee — all three call the same function — and the conformance suite supplies the proof that HTTP would have supplied structurally.

**Share a normalisation library, but let each surface call it.** The usual compromise. Rejected because it enforces nothing: a surface can still call `time.Now()`, still default a field before calling the library, still skip a step. The bug isn't that surfaces normalise *differently* — it's that they normalise *at all*. Raw-input command structs remove the opportunity rather than documenting against it.

**Parse in the surface, pass typed values to the app layer.** The conventional layered design, and what most reviewers would expect. Rejected: parsing is exactly where the divergence happens. Handing the app layer a `time.Time` means the surface already decided what "today" meant, in whatever zone it happened to be in.

**Trust the convention; enforce by code review.** Rejected on evidence. Four surfaces, built partly in parallel by separate agents from separate issues, each of which will look locally reasonable. Review catches the violation it happens to look at.

**Generate all four surfaces from one specification.** Maximum consistency, and the surfaces stop being hand-written. Rejected as over-engineering that would make each surface worse — a good CLI, a good REST API, and a good MCP tool set have genuinely different ergonomics, and forcing one shape on all three produces three mediocre ones.

## Consequences

**Good.** A new operation is written once and appears correctly in four surfaces. Divergence fails CI rather than reaching a user. Date and currency semantics are testable at a hostile instant instead of being emergent. Adding a fifth surface — a TUI, a webhook receiver — costs a decode/encode adapter and a conformance harness wiring. Agents working from independent issues cannot silently disagree, because the check is mechanical.

**Bad, and worth stating plainly:**

- **Command structs full of strings look wrong** to anyone who reasonably expects typed parameters at a layer boundary. `Amount string` will attract a "shouldn't this be a `Money`?" review comment on roughly every PR. It is deliberate, this ADR is the answer, and the answer needs repeating.
- **Type safety moves from the compiler to normalisation tests.** The parse layer must be tested hard, because it is now the only thing standing between raw input and the domain.
- **Error messages must be generated centrally and rendered per-surface.** A validation failure needs a machine-readable code and field path so the CLI can print it, the API can return a 422 body, MCP can return a tool error, and the web UI can attach it to the right input. Building that shared error vocabulary is real work in M1, not an afterthought.
- **The conformance suite is a standing maintenance cost.** Every new operation adds a row. That is the point, and it will occasionally feel like friction on a small change.
- **The web UI's enforcement is genuinely weaker** — a lint rule and a generated client, not a shared test. It is the surface most likely to drift, and the one to look at first when a number disagrees.
- **In-process composition means multiple processes can open the same SQLite file.** See [ADR-0007](0007-persistence-and-migrations.md).
