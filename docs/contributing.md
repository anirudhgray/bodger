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

The [`Makefile`](../Makefile) is the build contract. **CI runs `make check-go`, `make check-web`, and `make build`, and nothing else** — if a check isn't reachable through a `make` target, it isn't part of the build, and there is no divergent CI-only command list to keep in sync. `check-go`/`check-web` are each gated in CI ([`ci.yml`](../.github/workflows/ci.yml)) on whether a commit actually touched that side ([`dorny/paths-filter`](https://github.com/dorny/paths-filter)) — a web-only PR doesn't pay for a Go job and vice versa. `make check` runs both unconditionally; run that locally before pushing.

| Command | What it does |
| --- | --- |
| `make check` | `check-go` + `check-web`. Run before every push |
| `make check-go` | `fmt-check-go` + `vet` + `lint-go` + `test-go` |
| `make check-web` | `fmt-check-web` + `lint-web` + `test-web` |
| `make test` | Go tests with `-race`, plus web tests |
| `make fmt` | Format everything in place |
| `make generate` | Regenerate generated files (`internal/surface/http/openapi.json`, then `web/src/lib/api-types.ts` from it) |
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

The **conformance suite** (`internal/surface/conformance`) drives the same raw input through the CLI and REST API's real entry points — MCP joins in M8 — and asserts both produce an identical *observable result*: the same stored transaction fields, or the same error code and field, under a frozen clock at an instant deliberately chosen to expose timezone bugs (`2026-07-31T18:45:00Z`, already the next calendar day in the actor's timezone). It asserts on results, not on the internal command struct each surface builds — comparing structs would mean giving each surface a decode step callable outside its own transport, purely for the test's benefit. **Adding a user-facing operation means adding a row to that table.**

### Logging

`internal/platform/logging.New` builds bodger's one structured logger (`log/slog`, JSON output) — `cmd/bodger` constructs it exactly once, in `main`, and passes it down to every surface (issue #43). Its doc comment states the rule mechanically unenforced elsewhere: no PII, no amounts, no payee or description text, ever. What it *is* for is the other half of ADR-0011's safe/internal split: `*errs.Error` implements `slog.LogValuer`, so `logger.Error(msg, "error", err)` automatically expands into the code, message, field, and — when one was wrapped — the full `cause` chain a driver error or `fmt.Errorf` carries. `internal/surface/cli.RenderError` and `internal/surface/http`'s `respondError` both log through this before rendering the safe message, so the detail ADR-0011 says must never reach the user still reaches somewhere the self-hoster running the instance can read it.

**Destination: stderr, always.** Simplest option, and consistent with `bodger serve`'s existing "listening on ..." line, which already goes there. A rotating log file was considered for the long-running `bodger serve` process specifically — but it needs either a small rotation helper or a dependency like `lumberjack`, which is an [ADR-0001](decisions/0001-technology-stack.md) dependency-policy call this issue didn't want to make unilaterally. Deferred; if a self-hoster's stderr redirection genuinely isn't enough, file rotation is worth a real ADR at that point, not a silent addition.

**Level, and why the CLI and `bodger serve` share one default.** `internal/platform/config.Config.LogLevel` ("debug", "info", "warn", or "error"; default `"info"`) is layered the same way `HTTPBindAddr` is — `BODGER_LOG_LEVEL` overrides the instance default, validated structurally at load time. The CLI (one-shot) and `bodger serve` (long-running) could reasonably want different defaults, but right now the only thing either surface logs through this is an `*errs.Error`'s cause chain, always at Error level — which a different Info/Warn default wouldn't change. A single shared default is the honest reflection of that; revisit if either surface grows its own routine Info/Debug logging that should differ.

### The vocabulary check

`internal/lint` fails the build when a word from [`ux-principles.md` §2](ux-principles.md#2-vocabulary)'s banned-term table reaches a string a person reads. The conformance suite checks that surfaces *behave* alike; this checks that they *speak* alike, which is otherwise invisible in review because every individual string looks fine.

It reads two places, chosen for having no ambiguity about what "user-facing" means:

- every default message in the error registry (`internal/platform/errs`), and
- every cobra `Use`, `Short`, `Long`, and `Example`, and every flag's usage text, wherever a command is declared.

Everything else — OpenAPI descriptions, MCP tool descriptions, web UI strings — stays a review responsibility. Defining "user-facing" precisely enough across those to avoid false positives is the hard part, and a check people learn to bypass is worse than no check.

Strings are found by parsing Go source, not by grepping it, because §2 bans these words in user-facing strings *only*: `Posting` is a `Posting` in `internal/domain`, and the doc comment above `newReceiveCmd` may say "a single-posting inflow". Identifiers, package names, and comments are structurally out of the check's reach.

Matching is case-insensitive, whole-word, and covers the obvious inflections (`postings`, `debited`). Letters, digits, and underscore count as word characters, so the account type `credit_card` does not trip the `credit` row.

**To exempt a genuine false positive**, add a line to [`internal/lint/vocab_allowlist.txt`](../internal/lint/vocab_allowlist.txt):

```
term|the exact string that may contain it
```

The failure message prints the exact line to paste. An entry exempts one term in one exact string, so editing that string re-arms the check for it — an exemption expires with the wording that justified it. Add one only for a use of the word that has nothing to do with what §2 means by it; if the term means what the table says it means, the string is wrong, not the check.

---

## Testing

Weighted toward the layer where correctness is decided:

- **Domain** — exhaustive and table-driven. Every invariant in [`data-model.md` §14](data-model.md#14-invariants-that-must-have-tests) needs a test.
- **App** — in-memory repositories, frozen clock, fixed timezone.
- **Persistence** — real temp-file SQLite (not in-memory: it behaves differently around locking and WAL), plus an up→down→up migration cycle per PR.
- **Surfaces** — thin. The conformance suite covers the shared behaviour.
- **Web** — Vitest components; end-to-end kept deliberately sparse. UI tests are not where financial correctness is established.

A red test means the code is wrong. Do not adjust an expectation to match current behaviour — if the expectation itself is genuinely wrong, say so and get confirmation first (CLAUDE.md).

**The web e2e smoke test** (`web/e2e/smoke.spec.ts`, [Playwright](https://playwright.dev), issue #65) is the one deliberately sparse exception: log in, record a transaction, see the updated balance, log out — the golden path, proving the real pieces integrate, not a substitute for each screen's own Vitest tests (which mock the API). It runs against a real build of the production binary, not `npm run dev`: `web/e2e/global-setup.ts` runs `npm run build` and a real `go build` (the same artefacts `make build` produces — ADR-0001, one binary serves the API and the embedded static UI), seeds a temp SQLite database with a password and one account/category via the CLI (`bodger auth set-password`, `bodger accounts add`, `bodger categories add`), starts that binary on a fixed loopback port over plain HTTP, and tears it down after. It deliberately does not use `npm run dev`: that dev server serves over real HTTPS via `vite-plugin-mkcert`, which installs a local CA through an interactive `sudo` prompt on first run (`web/vite.config.ts`'s own comment; issues #86/#85) — unusable in CI or any unattended run. `npm run test` (and so `make test`/`make check`) runs it automatically as its last step; run it alone with `cd web && npm run test:e2e`. It needs Chromium installed once per machine: `cd web && npx playwright install --with-deps chromium` (CI does this before `make check-web`, caching the downloaded browser so only a genuinely new Chromium version pays for a fresh download).

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
- **Anything a user reads is held to [`ux-principles.md`](ux-principles.md)** — vocabulary, defaults, error phrasing. §2's banned-term table is checked automatically in error registry messages and cobra help text ([the vocabulary check](#the-vocabulary-check)); everywhere else it's still a review responsibility.
- **Anything the web UI renders is held to [`design-system.md`](design-system.md)** — color, type, spacing, radius, and which `web/src/components/ui` primitive to reach for. Add a new primitive with `cd web && npx shadcn@latest add <component>`, not by hand-rolling one that already exists in the registry.
- **Return errors from the registry, never `fmt.Errorf`, from an exported app-layer method.** Pick the coarse code, add an explanation if the default isn't specific enough, and `Wrap` the cause so it's logged but never shown. [ADR-0011](decisions/0011-error-model.md) has the codes and the safe/internal split; [Errors leave the app layer with a code](#errors-leave-the-app-layer-with-a-code) has the check that enforces it.

Session notes live in `agents/design-docs/` (gitignored, ephemeral). Anything durable graduates into `docs/`.

Cutting an actual release (tagging, the changelog, GoReleaser) is [`docs/releasing.md`](releasing.md) — a separate concern from day-to-day contribution.

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

### Regenerating the frontend's API types

`web/src/lib/api-types.ts` is generated from `internal/surface/http/openapi.json`, not hand-written (issue #83) — the same discipline one level further down the chain: `web/src/lib/api.ts` and `web/src/lib/settings.ts` alias their `Account`/`Category`/`Transaction`/`ApiToken`/etc. types and `type` enums from it (`import type { components } from './api-types'; type Account = components['schemas']['Account']`) instead of hand-transcribing the DTO shape, so a field or enum value that doesn't actually exist on the wire is a `tsc` error rather than a silent guess. `make generate` regenerates both files in order — `openapi.json` first, then `api-types.ts` from it:

```sh
make generate    # or, from web/: npm run generate
```

and commit the result alongside the code change. `npm run check:api-types` ([`openapi-typescript`](https://www.npmjs.com/package/openapi-typescript)'s own `--check` flag: regenerate in memory and diff against the checked-in file) runs as part of `npm run test`, so `make check`/CI fails if `api-types.ts` doesn't match a fresh run of the generator — the same regenerate-and-diff freshness check `openapigen_test.go` does for `openapi.json` itself, extended one step further down. `api-types.ts` is excluded from `npm run fmt`/`fmt:check` (`.prettierignore`) since it's generated output, not hand-formatted code. This is compile-time types only — no frontend runtime schema validation.
