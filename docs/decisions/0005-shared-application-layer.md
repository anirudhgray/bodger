# ADR-0005 — One application layer; normalise once; and how that is enforced

**Status:** Accepted · 2026-08-31

## Context

`bodger` has four surfaces over one financial core: a CLI, a REST API, a web UI, and an MCP server. The central architectural principle is that business semantics must not be duplicated across them. CLAUDE.md states the sharper version:

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

### Which command fields are raw strings, and why — precisely, not as a blanket policy

Five fields, on write commands, are raw strings all the way to the application layer. Each is raw for one specific, load-bearing reason: **resolving it correctly requires either I/O or clock access that a surface structurally cannot have**, because giving it that access would defeat the reason the surface is banned from parsing in the first place.

```go
type RecordOutflowCommand struct {
    ActorID     UserID
    AccountRef  string  // "HDFC Savings", or a UUID — resolving a name to an ID is a
                         // database query. Only the app layer holds a repository.
    Amount      string  // "800", "1,800.50", "₹800" — becomes minor units using the
                         // RESOLVED currency's exponent. Resolving currency means
                         // walking the precedence ladder, which needs an account
                         // lookup. There is no way to parse this correctly without
                         // that I/O having already happened.
    Currency    string  // "" means: apply the precedence ladder (entry → account →
                         // user → instance). The ladder itself needs account/user/
                         // instance lookups — I/O the surface cannot do.
    CategoryRef string  // same reason as AccountRef.
    Date        string  // "", "today", "yesterday", "2026-08-14", "14/08/2026" —
                         // resolving "today" needs the injected clock and the user's
                         // timezone. Both live only in internal/platform/clock and
                         // config, which surfaces cannot import (see Enforcement).
    Description string
    Notes       string
    Tags        []Tag   // NOT raw. See below.
}
```

Every one of those five is entangled with a lookup or the clock — not with "parsing" in the abstract. A smart constructor that a surface could call directly would need to either perform that I/O itself (impossible: surfaces have no repository) or fake it (a type that validates nothing real, which is worse than an honest string because it looks safe and isn't).

**`Tags` is the counter-example, and it's real, not aspirational.** Tag normalisation — lowercase, strip a leading `#`, kebab-case — is a pure function of the input string. No account lookup, no clock, no ambiguity. So it is **not** a raw string:

```go
// internal/domain — unexported fields; the only way to get one that's valid
type Tag struct{ value string }

func NewTag(raw string) (Tag, error) { /* lowercase, strip #, kebab-case, validate */ }

// internal/app — re-exports the type so a surface can hold one without
// importing internal/domain directly
type Tag = domain.Tag

// re-exported constructor a surface CAN call, because it needs nothing
// the surface doesn't already have
func NewTag(raw string) (Tag, error) { return domain.NewTag(raw) }
```

A CLI flag `--tag reimbursable` or a JSON array `"tags": ["reimbursable"]` is converted to `[]app.Tag` **at the surface**, via `app.NewTag`, before the command is even constructed. If that construction fails, the surface returns the same validation error any other field would. This is the general rule: **a field is typed at the command boundary exactly when a pure function exists from raw input to that field, and raw otherwise.** It isn't a stringly-typed-everything policy; it's a small, closed set of fields that happen to need context only the app layer has.

### Domain types are built only through validating constructors

Independent of the command-boundary question: `Money`, `Date`, and `Tag` all have **unexported fields** and a public constructor that validates on construction — `domain.NewMoney`, `domain.NewDate`, `domain.NewTag`. This means an invalid value (a currency code that isn't real, a `Date{Month: 13}`, a tag that wasn't normalised) cannot exist anywhere in the codebase, including deep inside `internal/app` after a value has already been resolved. It costs nothing and it's unconditional — every call site that used to be able to construct a struct literal now goes through a function that can say no.

`Money` and `Date`'s constructors are **not** the same as `normalize.Amount` / `normalize.DateOf`. `domain.NewDate(2026, 8, 14)` validates a calendar date from already-known, unambiguous integers — it needs no clock and is safe to call anywhere, including tests and fixtures. `normalize.DateOf("today", clk, tz)` is the one that resolves *ambiguous or relative* input, and it is the piece that must stay centralised. The constructor guards validity; the normaliser guards interpretation. Conflating the two is what made the earlier draft of this ADR sound like a blanket "everything is a string" policy when only interpretation-under-ambiguity actually needed to be.

### Normalisation is one package, and it is the only place ambiguous input becomes a domain value

`internal/app/normalize`:

| Input | Normalised by | Rules |
| --- | --- | --- |
| Amount | `normalize.Amount` | Strips grouping separators and symbols; rejects floats; produces `int64` minor units using the resolved currency's exponent ([ADR-0004](0004-multi-currency-and-fx.md)). Needs the resolved currency, so it runs after `normalize.Currency` inside the same handler |
| Currency | `normalize.Currency` | The precedence ladder: entry → account → user → instance |
| Date | `normalize.DateOf` | ISO first, then configured locale formats, then relative keywords; resolved in the **user's** timezone via the injected clock |
| Account / category | `normalize.Ref` | UUID, else exact name, else unique case-insensitive match, else a disambiguation error listing candidates |
| Text | `normalize.Text` | Unicode NFC, trim, collapse internal whitespace, length caps |
| Tags | `app.NewTag` (re-exported `domain.NewTag`) | Lowercase, strip a leading `#`, kebab-case — callable by a surface directly, since it needs no context |

`"today"` and `"yesterday"` are handled centrally by `normalize.DateOf` — so the MCP server gets them for free, the CLI's `--today` flag is unnecessary, and all four surfaces agree on what they mean at 00:30 in `Asia/Kolkata`.

### Time is injected, always

`internal/platform/clock.Clock` is a port. The application layer holds one. Production wires the real clock; tests wire a frozen one. **`time.Now()` appears in exactly one file in the repository**, and CI fails if it appears anywhere else.

This is not only about consistency between surfaces. It is what makes "does the August report include a transaction booked at 23:59 on 31 July in `Asia/Kolkata`?" a deterministic test rather than a thing that passes locally and fails at 18:30 UTC. It is also, concretely, *why* `Date` cannot be a surface-constructible type: the only correct implementation of "what does 'today' mean" requires this clock, and this clock is unreachable from a surface by design.

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

A surface that cannot import `domain` cannot construct a `Money` or a `Date` directly — it can only reach the types the app package chooses to re-export (`app.Tag`), and only through the constructors the app package chooses to expose (`app.NewTag`). This is what makes "parse it in the handler" not a temptation but a compile error for the five entangled fields, while still leaving room for `Tags` to be genuinely typed.

**2 · Banned-symbol check.** `time.Now()` outside `internal/platform/clock` fails. `os.Getenv` outside `internal/platform/config` fails. This is the mechanism that makes `Date` un-typeable at the surface boundary structural rather than conventional — there is no legal way for a surface to obtain the instant "today" is measured against.

**3 · The surface conformance suite.** The one that catches what the others cannot.

One table of raw inputs, each driven through all three in-process surfaces, asserting the resulting command struct is **identical**:

```
case: amount "1,800.50", currency "", date "today", account "hdfc savings"

  CLI:  bodger spend "1,800.50" groceries --account "hdfc savings" --on today
  API:  POST /api/v1/transactions  {"account":"hdfc savings","amount":"1,800.50","date":"today"}
  MCP:  record_outflow{account:"hdfc savings", amount:"1,800.50", date:"today"}

  all three must produce byte-identical RecordOutflowCommand
  and, under a frozen clock, byte-identical results
```

The table covers: empty date, `"today"`, `"yesterday"`, ISO dates, locale-format dates, grouped amounts, currency symbols, absent currency (precedence), account and category resolution by name, ambiguous names, case differences, whitespace, Unicode normalisation, tags with and without `#` (asserting all three surfaces reach the same `app.Tag` values, not just the same string), and the invalid-input error messages.

Cases run under a frozen clock at a **deliberately hostile instant** — `2026-07-31T18:45:00Z`, which is 2026-08-01 in `Asia/Kolkata` — so any surface that resolves "today" itself, or in the wrong zone, produces a different date and fails.

The suite is a single table in `internal/surface/conformance`. **Adding a user-facing operation means adding a row.** A new surface means wiring it into the harness once; every existing row then applies to it.

This is also why the REST API ships in milestone 1 alongside the CLI rather than after it: with one surface the suite has nothing to compare, and the contract would be an untested claim for an entire milestone. It is, if anything, more important now than under a fully-stringly-typed design: it is the thing that verifies the five entangled fields resolve identically, since nothing in the type system can.

**4 · The web UI is checked differently**, since it is TypeScript and cannot join the Go conformance suite. It is constrained instead by construction: its API client is *generated* from the OpenAPI description, so it can only send what the API accepts; and an ESLint rule bans `new Date()`, `Date.now()`, and `Intl.DateTimeFormat` outside a single `src/lib/format.ts` module that is display-only and never produces a value sent to the server. When the UI needs "today", it omits the field and lets the server decide.

## Alternatives considered

**Type every command field, with the surface calling a smart constructor for each.** The design this ADR considered and partly adopted. It works cleanly for fields with no I/O or clock dependency — `Tags` is the shipped example. It does **not** work for `AccountRef`, `CategoryRef`, `Amount`, `Currency`, or `Date`, because a correct constructor for any of them needs a repository lookup or the injected clock, neither of which a surface can be given without undoing the reason surfaces are constrained in the first place. A constructor that faked the check (validated only string shape, not real resolution) would be worse than an honest raw string: it would look type-safe in review while providing none of the guarantee.

**Route the CLI and MCP through HTTP to the API.** Guarantees normalise-once by construction — one process, one normaliser, no possibility of drift. Genuinely tempting, and rejected on product grounds: it means a server must be running before `bodger tx add` works, it makes a local MCP server a network client of a service on the same machine, and it puts serialisation latency in the path of the fast-entry workflow ([ADR-0010](0010-personal-finance-not-accounting-software.md), [`ux-principles.md` §3](../ux-principles.md#3-fast-entry)). In-process composition achieves the same guarantee — all three call the same function — and the conformance suite supplies the proof that HTTP would have supplied structurally.

**Share a normalisation library, but let each surface call it.** The usual compromise. Rejected because it enforces nothing on its own: without the import-graph and banned-symbol checks, a surface could still call `time.Now()`, still default a field before calling the library, still skip a step. What makes normalisation actually centralised is not the string-typing of commands — it's those two checks. Once they exist, a shared library *is* effectively what this ADR describes; the raw-string fields are the visible symptom of the checks, not an independent second mechanism.

**Parse in the surface, pass typed values to the app layer, with no import-graph or banned-symbol enforcement.** The conventional layered design, and what most reviewers would expect. Rejected: parsing is exactly where the divergence happens, and without a mechanical block, a surface handed a `time.Time` parameter type will eventually be handed one computed from `time.Now()` by someone in a hurry.

**Trust the convention; enforce by code review.** Rejected on evidence. Four surfaces, built partly in parallel by separate agents from separate issues, each of which will look locally reasonable. Review catches the violation it happens to look at.

**Generate all four surfaces from one specification.** Maximum consistency, and the surfaces stop being hand-written. Rejected as over-engineering that would make each surface worse — a good CLI, a good REST API, and a good MCP tool set have genuinely different ergonomics, and forcing one shape on all three produces three mediocre ones.

## Consequences

**Good.** A new operation is written once and appears correctly in four surfaces. Divergence fails CI rather than reaching a user. Date and currency semantics are testable at a hostile instant instead of being emergent. Adding a fifth surface — a TUI, a webhook receiver — costs a decode/encode adapter and a conformance harness wiring. Agents working from independent issues cannot silently disagree, because the check is mechanical. The scope of what's genuinely untyped is small and named — five fields per write command, each with a specific reason — rather than a blanket policy, and `Tags` demonstrates the pattern isn't purely defensive: where a field can be typed safely, it is.

**Bad, and worth stating plainly:**

- **Five specific fields will still attract a "shouldn't this be typed?" review comment** — `AccountRef`, `CategoryRef`, `Amount`, `Currency`, `Date` on every write command. It is deliberate, and the answer is now precise rather than a blanket appeal to this ADR: name which of I/O-dependency or clock-dependency applies to the field in question.
- **Type safety on those five fields moves from the compiler to normalisation tests.** The parse layer must be tested hard, because it is the only thing standing between raw input and the domain for exactly those fields.
- **The `Tags` pattern (app-layer re-exported types and constructors) is a second thing contributors need to learn**, alongside "which fields stay raw." Getting a new field's classification wrong in either direction — typing something that secretly needs I/O, or leaving something raw that's actually pure — is a plausible mistake, and there's no automated check for the classification itself, only for its consequences (the import-graph and conformance checks would still catch an *incorrectly* typed field that tried to smuggle I/O through a fake constructor, but wouldn't flag a field that's typed correctly and just adds needless ceremony).
- **Error messages must be generated centrally and rendered per-surface.** A validation failure needs a machine-readable code and field path so the CLI can print it, the API can return a 422 body, MCP can return a tool error, and the web UI can attach it to the right input. Building that shared error vocabulary is real work in M1 — see [ADR-0011](0011-error-model.md).
- **The conformance suite is a standing maintenance cost.** Every new operation adds a row. That is the point, and it will occasionally feel like friction on a small change. It is now doing more work than it would under a fully-stringly-typed design, since it's the only check verifying the five entangled fields' resolution, not just their surface-level shape.
- **The web UI's enforcement is genuinely weaker** — a lint rule and a generated client, not a shared test. It is the surface most likely to drift, and the one to look at first when a number disagrees.
- **In-process composition means multiple processes can open the same SQLite file.** See [ADR-0007](0007-persistence-and-migrations.md).
