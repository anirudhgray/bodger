# ADR-0007 — Persistence, the repository boundary, and migrations

**Status:** Accepted · 2026-08-31

## Context

Self-hosting must be genuinely easy — Docker Compose, easy to back up, easy to upgrade, sensible for a single user, capable of more — while handling decades of transactions and substantial analytical queries, with correctness as the priority.

[ADR-0001](0001-technology-stack.md) chose SQLite. This ADR covers what that means in practice: the boundary that keeps the choice reversible, how migrations work, and the honest downsides.

## Decision

### SQLite via `modernc.org/sqlite`, one file

Pure-Go driver, so `CGO_ENABLED=0` holds and cross-compilation to an arm64 NAS stays a single command. The database is one file on a mounted volume. Backup is `cp`. Restore is `cp` back.

Connections open with:

| Pragma | Value | Why |
| --- | --- | --- |
| `journal_mode` | `WAL` | Concurrent readers alongside a writer |
| `busy_timeout` | `5000` | Wait rather than fail when the writer is busy |
| `foreign_keys` | `ON` | Off by default in SQLite; referential integrity is not optional in financial software |
| `synchronous` | `NORMAL` | Safe under WAL; `FULL` costs more than it buys at this scale |

Writes use a single connection (`SetMaxOpenConns(1)` on the write pool) because SQLite permits one writer; reads use a separate pooled read-only connection. This is a well-worn Go/SQLite pattern and it removes the `SQLITE_BUSY` class of bug rather than papering over it with retries.

### The repository boundary is what keeps this reversible

`internal/ports` defines repository interfaces; `internal/adapters/sqlite` implements them. The application layer never sees a `*sql.DB`, never sees SQL, and never sees a driver-specific type.

Analytical queries are real SQL inside the adapter, not in-memory aggregation over loaded rows — a "spending by category for 2024" that loads every posting into Go and sums it would be both slow and a second implementation of what the database does well. The *interface* is domain-shaped (`SpendingByCategory(ctx, query) ([]CategoryTotal, error)`); the SQL is an implementation detail.

Postgres, if it ever arrives, is an additive adapter package plus a second migration set. It is not designed for now, not abstracted for now, and no query is written in a lowest-common-denominator dialect to keep it possible. SQLite's window functions and CTEs are used freely where they are the right tool.

### Migrations: goose, embedded SQL, applied on startup

Plain `.sql` files in `internal/adapters/sqlite/migrations`, embedded via `embed.FS`, applied automatically when the server starts.

- **Sequential numeric prefixes** (`00003_add_tags.sql`). Timestamp prefixes avoid collisions between parallel branches but produce an ordering that depends on when someone typed rather than what depends on what, and in a repository where several agents work in parallel, a visible collision that must be resolved is better than an invisible reordering that must be debugged. Migration files are a known conflict hotspot and the `orchestrate` skill names them as one.
- **Every migration has a tested `-- +goose Down`.** CI runs up → down → up on a fresh database for each PR. A down migration that is a lie is worse than none, so this is verified rather than assumed.
- **Applied on startup**, so upgrading a self-hosted install is `docker compose pull && docker compose up -d`. No migration tool on the user's server, no separate step to forget.
- **Backup before migrating.** On startup, if migrations are pending, the server copies the database to `bodger.db.pre-<version>.bak` before applying. Cheap for a personal-scale file, and it is the difference between a bad upgrade being an inconvenience and being a catastrophe.
- SQLite's limited `ALTER TABLE` means some changes require the create-copy-drop-rename dance. Written explicitly in SQL rather than hidden behind a helper, because these are the migrations most worth reading carefully.

### Testing

Repository tests run against a **real temp-file SQLite database**, not an in-memory one and not a mock. In-memory SQLite behaves differently enough around WAL and locking that passing tests would prove less than they appear to. Each test gets a fresh file, migrated up. The suite is fast enough at this scale that this costs nothing worth optimising.

## Alternatives considered

**Postgres from the start.** More capable, better concurrency, no writer-serialisation constraint, and the obvious answer for a multi-user product. Rejected because it adds a container, a backup story, connection configuration, and a version-upgrade path to every self-hosted install, in exchange for capabilities a single-user ledger doesn't exercise. The repository boundary keeps it available. If M5's analytics or a real multi-user deployment ever demand it, that is the trigger.

**`mattn/go-sqlite3` (cgo).** Faster, and the reference driver. Rejected: cgo means a C toolchain for cross-compilation, a non-static binary, and a fatter container. The performance difference is invisible at personal-ledger scale.

**An ORM (GORM, ent).** Less boilerplate for CRUD. Rejected: the interesting queries here are analytical, which is where ORMs are least helpful and most likely to generate something slow that nobody reads. `sqlc`-style generated types over hand-written SQL is a reasonable future refinement; hand-written `database/sql` is the starting point.

**Timestamp-prefixed migrations.** Standard advice for parallel branches. Rejected for the reason above — a visible collision beats a silent reordering when several agents work concurrently.

**Migrate via a separate command or init container.** More control, and the conventional production answer. Rejected against the self-hosting goal: a step the user must remember is a step they will skip, and this is a single-instance application where startup migration has none of the risks it has in a rolling multi-replica deployment.

**In-memory SQLite for tests.** Faster. Rejected: different enough around locking and WAL that it would prove the wrong thing.

## Consequences

**Good.** Backup and restore are file copies. Upgrade is a container pull. One process, one file, no database to operate. Referential integrity is enforced by the database. Cross-compilation stays trivial. Down migrations are tested rather than hoped for.

**Bad:**

- **One writer at a time.** Fine for one user; a genuine ceiling for a busy multi-user instance. WAL and the busy timeout make it invisible at this scale, and it is the loudest signal that Postgres is warranted.
- **Multiple processes can open the same file** — `bodger serve` in a container and `bodger tx add` on the host. WAL handles this correctly, but it requires the file to be reachable from both, which is a documented deployment consideration rather than an accident.
- **Migrations run at startup**, so a failed migration is a failed start. Mitigated by the pre-migration backup and the tested down path, but it means an upgrade can take the service down until the operator intervenes.
- **SQL is dialect-specific.** No lowest-common-denominator restraint is being observed, so a Postgres adapter would mean rewriting the analytical queries, not switching a driver string. That is the accepted price of using the database well.
- **No connection-level multi-tenancy.** Per-user isolation is a query predicate ([ADR-0006](0006-authentication-and-multi-user-path.md)), so a repository method that forgets it is a data leak. Every repository method takes an actor, and this is on the review checklist for new queries.
- **Large imports hold the write lock.** A 10,000-row import commits in one transaction ([ADR-0008](0008-import-export-architecture.md)) and blocks other writes for its duration. Acceptable for a personal tool; batching is available if it isn't.
