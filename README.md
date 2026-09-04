# bodger

A personal finance application you actually host yourself. Record what you spend, what comes in, and what moves between your accounts — then find out where it all went.

`bodger` is built around one idea: **one financial core, several thin interfaces over it.** A command line, a REST API, a web UI, and a local MCP server for AI agents all call the same code, so they can never disagree about what your money did.

> **Status: pre-alpha.** The domain model, persistence layer, CLI, and REST API are built and tested; the web UI and MCP server are not yet. See [Status](docs/architecture.md#9-status) for the milestone breakdown. Prebuilt binaries are on the [Releases page](https://github.com/anirudhgray/bodger/releases), or build from source with `make build`; use the CLI or run `bodger serve` against a local SQLite file — see the [user guide](docs/user-guide.md) — but there's no web UI yet.

---

## What it's for

Personal finance for a normal person — not accounting software. You should never need to know what a debit, a journal entry, or a chart of accounts is to use it. That's a deliberate constraint on the engineering, not a marketing line — see [ADR-0010](docs/decisions/0010-personal-finance-not-accounting-software.md).

- **Record money moving.** Spend, earn, transfer between your own accounts. Fast entry, sensible defaults, few required fields.
- **Multi-currency, properly.** Every amount carries its currency. Accounts have their own. Reports convert explicitly, and every converted number tells you which rate it used and when.
- **Understand it.** Balances, income, expenses, net cash flow, spending by category, trends, budgets.
- **Get your history in.** CSV and statement import with preview, duplicate detection, and rollback — nothing touches your ledger until you say so.
- **Get your data out.** A versioned, complete, human-readable export. It's your money; it should never be stuck in here.
- **Self-host it.** One binary, one SQLite file, one `docker compose up`. Back it up by copying a file.

## Interfaces

| | |
| --- | --- |
| **CLI** | `bodger spend 800 groceries --account "HDFC Savings" --on 2026-08-14` |
| **REST API** | `bodger serve` — what the web UI talks to, and what your scripts can too |
| **Web UI** | Dashboards, charts, review workflows. Built into the binary; no separate deployment |
| **MCP server** | `bodger mcp` — let a local AI agent read and record against your own data |

None of these is the "real" one. They are peers over the same application layer, and a [CI test](docs/decisions/0005-shared-application-layer.md#enforcement) proves the same input through any of them produces the same result.

## Documentation

| Document | What's in it |
| --- | --- |
| [Architecture](docs/architecture.md) | Layers, surface boundaries, milestones, current status |
| [Data model](docs/data-model.md) | The financial domain — accounts, transactions, postings, budgets, currency |
| [UX principles](docs/ux-principles.md) | Who this is for, and the expectations every surface is held to |
| [Design system](docs/design-system.md) | The web UI's color, type, spacing, and component primitives |
| [Decision records](docs/decisions/) | Why the load-bearing calls were made the way they were |
| [Contributing](docs/contributing.md) | Toolchain setup, build commands, conventions |
| [User guide](docs/user-guide.md) | Installing, first run, recording transactions, reading balances, the CLI reference |

New here? Read [`docs/data-model.md`](docs/data-model.md) first. The domain model was designed before any interface, deliberately, and everything else makes more sense once you know it.

## Building

Requires Go and Node at the versions pinned in [`.tool-versions`](.tool-versions) (`asdf install` or `mise install`).

```sh
make setup        # install dependencies
make setup-hooks  # activate the repo's git hooks - once per clone
make check        # format, vet, lint, test - exactly what CI runs
make build        # produces bin/bodger
```

Full details in [`docs/contributing.md`](docs/contributing.md).

## Licence

Not yet chosen.
