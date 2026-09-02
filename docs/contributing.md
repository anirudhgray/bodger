# Contributing

How to set up, build, test, and change `bodger`.

> **Note on scope.** The repository currently contains architecture, decision records, and toolchain scaffolding — no application code. The setup and build sections below are real and work today (`make check` and `make build` pass on a fresh clone). Sections describing code layout describe what milestone 1 will create; see [Status](architecture.md#9-status).

---

## Setup

Toolchain versions are pinned in [`.tool-versions`](../.tool-versions) and read by both `asdf` and `mise`. CI reads the same file, so there is one source of truth for what version anything is.

```sh
asdf install          # or: mise install
make setup            # go mod download, and npm ci once web/ exists
make setup-hooks      # activate .githooks - once per clone
```

`make setup-hooks` is not optional. Git does not read hooks from a tracked directory on its own, and [`.githooks/pre-push`](../.githooks/pre-push) is what stops an accidental direct push to `main`.

[`golangci-lint`](https://golangci-lint.run/) is pinned in `.tool-versions` too, at the same version CI uses, so `asdf install` (or `mise install`) gets you a linter that actually matches CI instead of whatever a global install happens to be. `make lint` skips with a notice only if the binary is entirely absent — if one is present but can't parse this repo's `go.mod` (for example, a stale global install built against an older Go than `go.mod` targets), `make lint` fails loudly rather than silently skipping, since that failure is exactly the signal that the wrong version is on `PATH`. Config is [`.golangci.yml`](../.golangci.yml) — close to the defaults on purpose, since the project's real invariants are enforced by tests, not linter rules. **CI pins the linter version**; bump it deliberately in both `.github/workflows/ci.yml` and `.tool-versions` rather than tracking `latest`, so a linter release can't fail an unrelated PR.

---

## Commands

The [`Makefile`](../Makefile) is the build contract. **CI runs `make check` and `make build`, and nothing else** — if a check isn't reachable through a `make` target, it isn't part of the build, and there is no divergent CI-only command list to keep in sync.

| Command | What it does |
| --- | --- |
| `make check` | `fmt-check` + `vet` + `lint` + `test`. Run before every push |
| `make test` | Go tests with `-race`, plus web tests |
| `make fmt` | Format everything in place |
| `make generate` | Regenerate generated files (e.g. `internal/surface/http/openapi.json`) |
| `make build` | Build the web UI, then the binary, into `bin/bodger` |
| `make run` | Run the server locally |
| `make test-cover` | Tests with a coverage profile |
| `make docker-up` | Run the self-hosted stack |
| `make help` | List everything |

`TZ=UTC` is exported by the Makefile and set in CI. Reporting periods are defined in the *user's* timezone and resolved in the application layer ([ADR-0005](decisions/0005-shared-application-layer.md)); pinning the host to UTC means a test that accidentally depends on the machine's timezone fails immediately rather than at 18:30 one evening.

---

## Where code goes

```
cmd/bodger/            single binary: serve, mcp, and all CLI commands
internal/
  domain/              pure financial model. No I/O, no clock, no database
  app/                 use cases. The only layer that knows what "now" is
    normalize/         the ONLY place raw input becomes a domain value
  ports/               repository and provider interfaces
  adapters/sqlite/     persistence + migrations/
  surface/http|cli|mcp adapters. Decode, call one app method, encode
  platform/            clock, config, idgen, logging
web/                   React + Vite. Built assets embedded into the binary
docs/                  architecture, data model, ADRs, guides
```

Read [`architecture.md`](architecture.md) for what each layer is for, and [`data-model.md`](data-model.md) for the domain itself.

### The three rules that get enforced, not just reviewed

`make check` fails on all of these. They are described here so the failure makes sense when you hit it, and in full in [ADR-0005](decisions/0005-shared-application-layer.md).

**1 · Dependencies point inward.** `domain` imports nothing project-local. Surfaces import `app`, never `domain` and never an adapter. An import-graph test enforces it.

**2 · Nothing outside the clock asks what time it is.** `time.Now()` appears in exactly one file. Everything else takes an injected `Clock`. Likewise `os.Getenv` outside `internal/platform/config`.

**3 · Surfaces normalise nothing.** A handler, CLI command, or MCP tool decodes transport input into a command struct, calls one app method, and encodes the result. It does not parse a date, resolve a default, or pick a currency.

That last one is why command struct fields hold **raw strings** rather than parsed types. Resolving any of them needs either a repository lookup (`AccountRef`, `CategoryRef`, `Currency` precedence, and `Amount`, which needs the resolved currency's exponent) or the injected clock (`Date`) — none of which a surface has. [ADR-0005](decisions/0005-shared-application-layer.md) is the long answer. The short version: if the CLI parses a date and the API parses a date, they will eventually disagree about what "today" means at a month boundary — so neither of them parses dates, or anything else that needs context only the app layer has.

Domain types (`Money`, `Date`, `Tag`) still have unexported fields and validating constructors, so an invalid one can't exist anywhere in the codebase. That's separate from the command boundary and applies everywhere.

### Errors leave the app layer with a code

A fourth rule, from [ADR-0011](decisions/0011-error-model.md) rather than ADR-0005, and enforced the same way. **Every error an `internal/app` method returns is an `*errs.Error`** — `errs.New(errs.NotFound).Explain("No account called %q.", ref)` — so that a surface has a status, an exit code, and a message it can show without deciding anything.

`internal/lint` fails `make check` on any `fmt.Errorf` or `errors.New` under `internal/app` that isn't nested inside a `Wrap(...)` call. The boundary it draws:

- **Allowed:** `errs.New(errs.Internal).Wrap(fmt.Errorf("app: %w", err))`, and `.Wrap(err)` generally. A driver error becoming an `*errs.Error`'s cause is the whole point — it's logged in full and never serialised.
- **Not allowed:** the same `fmt.Errorf` returned on its own. That reaches a surface with no code, so no HTTP status and no exit code, and quite possibly a SQL fragment in the text.
- **Not checked:** `internal/domain`, where a pure helper may return a plain error wrapping its own sentinel, and the adapters, whose errors are *meant* to become a cause. The check only reads what it is pointed at, and it is pointed only at `internal/app`.

It is a parse, not a type-checking pass, so it matches on the method name `Wrap` and can't see through a dot-import of `fmt`. That's a deliberate trade — see the doc comments in `internal/lint/errwrap.go`.

The **conformance suite** (`internal/surface/conformance`) drives the same raw input through the CLI, REST API, and MCP and asserts all three produce identical commands, under a frozen clock at an instant deliberately chosen to expose timezone bugs. **Adding a user-facing operation means adding a row to that table.**

---

## Testing

Weighted toward the layer where correctness is decided:

- **Domain** — exhaustive and table-driven. Every invariant in [`data-model.md` §14](data-model.md#14-invariants-that-must-have-tests) needs a test.
- **App** — in-memory repositories, frozen clock, fixed timezone.
- **Persistence** — real temp-file SQLite (not in-memory: it behaves differently around locking and WAL), plus an up→down→up migration cycle per PR.
- **Surfaces** — thin. The conformance suite covers the shared behaviour.
- **Web** — Vitest components; end-to-end kept sparse. UI tests are not where financial correctness is established.

A red test means the code is wrong. Do not adjust an expectation to match current behaviour — if the expectation itself is genuinely wrong, say so and get confirmation first (CLAUDE.md).

**Proving a validating constructor is the only way in.** A domain value object (`Money`, `Date`, `Tag`, `Account`, …) has unexported fields precisely so nothing outside its package can construct one without going through validation. Prove it with a `//go:build ignore` file in a `nocompile` subpackage attempting a keyed struct literal from outside — run it without the tag to confirm the real compiler error, then restore the tag so it's excluded from normal builds and tests. See `internal/domain/money/nocompile`, `internal/domain/nocompile`, or `internal/domain/ledger/nocompile` for the pattern.

---

## Docs describe intent until the code exists

Most of `docs/` was written before any implementation. Where a doc and working code disagree, **the code wins and the doc gets fixed in the same PR** — don't reshape working code to match a paragraph that was speculative when it was written. Say so in the PR description when that happens, so the change is visible rather than silent.

This does not apply to the invariants in [`data-model.md` §14](data-model.md#14-invariants-that-must-have-tests) or to anything an ADR records as a decision: those are requirements, and changing one means updating the ADR deliberately, not incidentally.

## Working on this repo

The conventions in [`CLAUDE.md`](../CLAUDE.md) apply to humans too. The ones that come up most:

- **Never commit to `main`.** Feature branches off `main`, PRs, squash merge.
- **Atomic conventional commits.** Small and coherent. PR titles follow the same format, since they may be squashed.
- **Docs change in the same PR as the code.** The [Status](architecture.md#9-status) section for anything that moves a milestone; the user guide for anything user-visible; this file or an ADR for anything dev-visible. Not a later docs pass.
- **Track deferred work as GitHub issues**, not as a paragraph in a doc. Reference issue numbers in commits and PRs.
- **No ADR numbers or internal paths in user-facing strings** — CLI help, UI labels, prompts. Errors should be specific and detailed about *what* failed, but they don't cite internal documents either.
- **Anything a user reads is held to [`ux-principles.md`](ux-principles.md)** — vocabulary, defaults, error phrasing. §2's banned-term table has no automated check yet, so it's a review responsibility.
- **Return errors from the registry, never `fmt.Errorf`, from an exported app-layer method.** Pick the coarse code, add an explanation if the default isn't specific enough, and `Wrap` the cause so it's logged but never shown. [ADR-0011](decisions/0011-error-model.md) has the codes and the safe/internal split; [Errors leave the app layer with a code](#errors-leave-the-app-layer-with-a-code) has the check that enforces it.

Session notes live in `agents/design-docs/` (gitignored, ephemeral). Anything durable graduates into `docs/`.

### Adding a decision record

Write one when a decision is expensive to reverse, constrains more than one part of the system, or has a defensible alternative a future reader will ask about. Format and index: [`docs/decisions/README.md`](decisions/README.md). Consequences must include the bad ones.

### Adding a migration

Sequential numeric prefix, embedded SQL, with a **tested** `-- +goose Down`. CI runs up→down→up. Migration files are a known parallel-work conflict point — see [ADR-0007](decisions/0007-persistence-and-migrations.md) for why sequential numbering was chosen anyway.

### Regenerating the OpenAPI document

`internal/surface/http/openapi.json` is generated, not hand-written (issue #36): `internal/surface/http/openapi_gen.go`'s `GenerateOpenAPIDocument` builds it by reflecting over that package's own request/response DTOs (`dto.go`, `accounts.go`, …) and walking `routeTable` (`router.go`), using [`kin-openapi`](https://github.com/getkin/kin-openapi)'s `openapi3gen`. After changing a DTO's fields or JSON tags, or adding/changing a route in `routeTable`, run:

```sh
make generate    # or: go generate ./...
```

and commit the result alongside the code change. `openapigen_test.go`'s `TestOpenAPIDocumentMatchesGenerator` fails CI (`make check`) if `openapi.json` on disk doesn't match a fresh run of the generator, so a forgotten `go generate` is caught in CI rather than shipping a stale document — and the same package's `http_test.go` end-to-end tests validate every real response against that same document (`openapi3filter`), so a handler whose response doesn't actually match its declared schema fails there too.

A DTO field's `enum`, `doc`, and `format` struct tags (see `dto.go`'s doc comment) are how the generator learns what a plain Go string actually means on the wire — a closed set of values, prose a type can't carry, or that it's a money amount or a calendar date. Add or adjust these when a new field needs the same treatment, rather than hand-editing `openapi.json` afterwards.
