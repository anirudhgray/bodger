# ADR-0001 — Technology stack

**Status:** Accepted · 2026-08-31

## Context

`bodger` is a self-hosted personal finance application with four interfaces over one financial core: a CLI, a REST API, a web UI, and eventually an MCP server. The product requirements, in the order they actually constrain the choice:

- **Self-hosting is first-class.** Home server, VPS, or NAS. Docker Compose is a first-class target; Kubernetes is explicitly out. Easy to back up, easy to upgrade, sensible for one user.
- **Four surfaces must share one implementation**, with business logic in none of them.
- **Correctness over convenience.** This is financial software.
- **Decades of transactions, not millions of users.** Straightforward designs with good indexes; no premature distributed anything.
- **The MCP server runs locally** alongside the platform.

The interesting pressure is that "four surfaces, one core" and "trivial to self-host" pull in opposite directions in most stacks. A typical answer — an API service, a separate CLI package, a separate MCP service, a Node frontend, a Postgres container — is four deployables and two runtimes to satisfy a single user running a NAS.

## Decision

**Go 1.27** for the backend, CLI, and MCP server, compiled to **one static binary** with the web UI's built assets embedded via `embed.FS`. **SQLite** (`modernc.org/sqlite`, pure Go, no cgo) as the database, in the same container. **React + TypeScript + Vite** for the web UI.

| Concern | Choice | Why this and not the obvious alternative |
| --- | --- | --- |
| Language | Go 1.27 | One statically linked binary cross-compiles to arm64 NAS, amd64 VPS, and a developer laptop from one machine. No runtime, no interpreter, no virtualenv on the user's server. Fast enough that no analytics query in this problem domain needs thought. Explicit enough that layer boundaries survive contact with several agents working in parallel. |
| Database | SQLite via `modernc.org/sqlite` | The whole database is one file: back it up with `cp`, restore it by putting it back. No second container, no connection tuning, no separate upgrade path. Handles a personal ledger's decades of transactions without noticing. Pure-Go driver keeps `CGO_ENABLED=0`, which is what keeps cross-compilation and the scratch container trivial. |
| Migrations | goose, SQL files via `embed.FS` | Plain SQL, embedded in the binary, applied on startup. No migration tool to install on the user's server. See [ADR-0007](0007-persistence-and-migrations.md). |
| HTTP routing | stdlib `net/http.ServeMux` | Since Go 1.22 it does method and path-pattern routing, which is the entire reason people reached for chi or gorilla. The handlers here are thin by design ([ADR-0005](0005-shared-application-layer.md)), so a framework would be adding a dependency to save nothing. |
| CLI | cobra | Boring and universal. Subcommands, help, completions, no surprises. |
| MCP | `github.com/modelcontextprotocol/go-sdk` | The official Go SDK. Same language as the app layer, so tools call app methods directly. |
| Web UI | React + TypeScript + Vite | Large ecosystem for the chart-and-table work that dominates M5, and a TypeScript client generated from the API's OpenAPI description keeps the wire contract checked at compile time. Vite's build output is static files, which is what makes embedding work. |
| Money | `int64` minor units + currency | Never floats. See [ADR-0004](0004-multi-currency-and-fx.md). |
| Toolchain pin | `.tool-versions` | `asdf` is already present on the developer's machine; `mise` reads the same file. |
| Task runner | `Makefile` | The repo's existing `.githooks/pre-push` already tells the user to run `make setup-hooks`. CI runs the same targets. |

### Deployment shape

```
docker compose up  →  one container: bodger serve
                      one volume:    /data/bodger.db
```

The web UI is inside the binary. There is no separate frontend container, no reverse proxy required for a basic install, and no database container.

### Surface composition

The CLI, REST API, and MCP server are all subcommands of the same binary and all link the application layer **in-process**. Only the web UI crosses a network boundary, because browsers do that. See [ADR-0005](0005-shared-application-layer.md).

## Alternatives considered

**TypeScript end-to-end (Node + Prisma + Next.js).** One language across the whole stack, and the strongest UI ecosystem. Rejected on the self-hosting constraint: shipping a Node application to someone's NAS means shipping `node_modules`, a runtime version, and a build step, where Go ships a file. The CLI also matters here — a Node CLI has visible startup latency, and fast entry is a product requirement ([ADR-0010](0010-personal-finance-not-accounting-software.md)). Go's weaker frontend story is irrelevant because the frontend is React either way.

**Python (FastAPI + SQLAlchemy).** Excellent for the analytics work in M5. Rejected for the same packaging reason, more acutely — Python deployment on a home server is the least reproducible of the three — plus `Decimal`-vs-`float` money bugs are easy to write and hard to see, where Go's type system makes a `Money` value object that simply cannot be added to another currency.

**Rust (Axum + SQLx).** Best correctness guarantees of the candidates and the same single-binary deployment. Rejected on velocity: this is a large surface area to build, much of it by agents working in parallel, and Rust's compile times and lifetime friction cost more here than its extra safety buys over Go for a domain whose hard parts are semantic (what does "August" mean, which rate converted this) rather than memory-related.

**Postgres instead of SQLite.** More capable, and the right answer at multi-user scale. Rejected as the *default* because it adds a container, a backup story, a connection configuration, and an upgrade path to every self-hosted install, in exchange for capabilities a single-user ledger doesn't use. The repository boundary keeps Postgres an additive adapter rather than a rewrite — see [ADR-0007](0007-persistence-and-migrations.md).

**CLI and MCP as HTTP clients of the API.** Would guarantee normalise-once by construction, since only one process would ever resolve a default. Rejected because it forces a running server before you can record a transaction, turns `bodger tx add` into a network call to localhost, and makes the local MCP server a client of a service on the same machine. In-process composition gets the same guarantee — all three call the same Go function — and [ADR-0005](0005-shared-application-layer.md) supplies the test that proves it.

## Consequences

**Good.** One artefact to build, ship, and version. `cp bodger.db backup.db` is a complete backup. Cross-compilation for a NAS is one `GOOS`/`GOARCH` pair. The CLI starts instantly. Agents working in parallel get a language where layer violations are visible in the import graph and mechanically checkable.

**Bad, and worth stating plainly:**

- **Two languages, two toolchains.** Go and TypeScript, with `make` and CI wrapping both. The web UI is a real sub-project with its own dependency and lint story.
- **Multiple processes on one SQLite file.** `bodger serve` in a container and `bodger tx add` in a terminal can open the same database. WAL mode plus a busy timeout makes this safe at personal-scale concurrency, but it is a real constraint and it is documented in [ADR-0007](0007-persistence-and-migrations.md), not glossed over.
- **SQLite's analytical ceiling.** Fine for a personal ledger; window functions and CTEs are available. If M5's analytics ever outgrow it, that is the trigger for the Postgres adapter, not a surprise.
- **Go's verbosity.** More boilerplate mapping between DTOs and domain types than a dynamic language would need. Accepted: explicitness at layer boundaries is the property this architecture depends on.
- **A generated TypeScript API client is a build-order dependency.** The web build needs the API's OpenAPI description, so `make build` orders those steps.
