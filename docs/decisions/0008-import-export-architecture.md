# ADR-0008 — Import pipeline and canonical export

**Status:** Accepted · 2026-08-31

## Context

Import is a major product requirement: CSV exports, bank statements, credit-card exports, other personal finance applications, with duplicate detection, idempotency, external IDs, provenance, mapping, preview-before-commit, and rollback. Import must be a pipeline rather than source-specific logic embedded in the financial core, and must be extensible to new formats without modifying that core.

Export is first-class, and raises a sharp question: should export → import round-trip, and what does "equivalent" mean? A round-trip guarantee is worth little unless "equivalent" is defined precisely enough to test.

Import is also the highest-risk write path in the application. It is the one operation that creates thousands of rows from data the user did not type, and a bad import that silently duplicates six months of transactions is the worst realistic failure this product has.

## Decision

### Import is a staged pipeline; only the last stage touches the ledger

```
source file
   → parser            (format-specific; the ONLY format-aware stage)
   → ImportRecord      (raw payload + normalised fields, one row per source row)
   → validation
   → mapping           (account, category, currency resolution)
   → duplicate detection
   → user review       ← the pipeline can stop here indefinitely
   → commit            (one database transaction → real Transactions)
```

Everything before `commit` is staged in `import_batch` and `import_record` rows. Nothing before commit affects a balance, a report, or a budget.

This means an import can be started, reviewed a day later, partially corrected, and committed or abandoned, with the ledger untouched throughout.

**A parser's only job is to turn bytes into normalised `ImportRecord` fields.** It is format-aware and domain-ignorant: it does not resolve accounts, does not create transactions, does not know what a posting is. Adding a new format is a new parser and nothing else — this is the extensibility the brief asks for, and it holds because the parser interface is narrow enough to make anything else awkward.

**Field normalisation reuses `internal/app/normalize`.** An import parsing dates or amounts with its own logic would be a fifth surface disagreeing with the other four, which is exactly what [ADR-0005](0005-shared-application-layer.md) exists to prevent. Import is subject to the same rule.

### Duplicate detection is two-tier, and never auto-merges

**Tier 1 — external ID.** If the source provides a transaction ID, `(account_id, external_id)` is unique. An exact match is a definite duplicate and is skipped. This is why deletes are soft ([ADR-0002](0002-authoritative-ledger-and-corrections.md)): a hard delete would let a re-import resurrect a transaction the user removed on purpose.

**Tier 2 — heuristic.** Same account, exact same amount and currency, `booked_date` within ±3 days, and a similar description. A match is flagged `suspected_duplicate` and **surfaced for review**.

The system never silently merges a tier-2 match. It is a suggestion, not a decision — the false-positive case is two genuine identical coffees on the same day, and silently dropping one of those is a wrong balance the user has no way to notice. The user resolves it; the decision is recorded on the `ImportRecord` for auditability.

Transfer detection — recognising that an outflow on one account and an inflow on another are the same movement — uses the same heuristic across accounts and is likewise **proposed, never applied automatically**. Getting this wrong silently converts a real expense into a transfer and removes it from spending reports.

### Commit is atomic; rollback is a first-class operation

Committing a batch writes every transaction in **one database transaction**: all or nothing, never half an import.

Every created transaction carries `import_record_id`, so provenance is queryable in both directions. Rolling back a batch soft-deletes exactly the transactions it created, and a batch remains rollback-able after the fact — "undo that import" is a supported operation, not a manual cleanup.

### Export: a canonical versioned JSON document, plus CSV

Two formats with different jobs, and conflating them is the mistake to avoid:

**Canonical JSON (`bodger.export/v1`)** is the authoritative interchange and backup format. Complete, versioned, deterministic (stable key order, stable row order, no timestamps of the export itself in the payload), and **independent of the database schema** — it describes the domain, so a schema migration does not change the export format, and a format change is a deliberate version bump.

**CSV** is for humans and spreadsheets. Flat, lossy by design, one row per posting with the transaction denormalised across it. Explicitly **not** an interchange format: a split transaction cannot round-trip through it without ambiguity, and pretending otherwise would produce silent data loss on re-import.

### The round-trip guarantee, defined precisely

> `export → import → export` produces a **byte-identical** canonical JSON document, after normalising surrogate IDs and audit timestamps.

"Equivalent" means:

| Must match exactly | May differ |
| --- | --- |
| Every transaction's kind, booked date, description, notes, tags | Surrogate UUIDs (consistently remapped) |
| Every posting's account, signed amount, currency, category | `created_at` / `updated_at` |
| Account names, kinds, currencies, opening balances | Row order in the *database* |
| Category tree structure and kinds | `transaction_revision` history |
| Budgets, budget lines, FX rates | |

This is a CI test on a fixture covering multiple currencies, transfers, splits, refunds, credit cards, imported rows, and month boundaries — not an aspiration in a document. Audit history is explicitly out of scope for round-trip: an export is a statement of what is true, not of how it came to be.

## Alternatives considered

**Parse and write transactions in one pass.** Much simpler. Rejected: it makes preview impossible, makes rollback mean "find and delete the right rows", and mixes format-specific parsing into the ledger write path — exactly what the brief says not to do.

**Auto-merge high-confidence duplicates.** Fewer review decisions. Rejected: the false-positive case (two identical purchases on one day) produces a wrong balance with no signal. An unnecessary review prompt is a much cheaper error than a silently dropped transaction.

**A plugin system for parsers** — external binaries or a scripting host. Rejected as premature. The parser interface is narrow and in-tree parsers are a small PR each; revisit if third parties actually want to ship formats.

**One export format for both humans and machines.** Rejected: CSV cannot represent splits without ambiguity, and a canonical format optimised for spreadsheet paste-ability would be a poor backup. Two formats with clearly separate jobs is honest.

**SQLite file copy as the backup format.** It *is* a perfect backup, and users should absolutely do it. Rejected as the *canonical* export because it is schema-coupled and opaque — it cannot be diffed, inspected, or migrated between versions, and the brief asks for an export independent of the internal schema.

**Round-trip defined as "semantically equivalent", checked by inspection.** Rejected: unfalsifiable. Byte-identical after ID normalisation is a test that either passes or fails.

## Consequences

**Good.** Import is reviewable, reversible, and idempotent. New formats are one parser. The ledger has no format-specific code. Backup is a documented, versioned, diffable file. The round-trip guarantee is a CI test, so it stays true.

**Bad:**

- **Two extra tables and a state machine** for something a naive implementation does in one pass. The complexity is real; it buys preview and rollback.
- **Review is mandatory friction.** A user importing a clean statement still confirms. Mitigated by a summary view and bulk accept, not by skipping the stage.
- **A large import holds the write lock for its commit** ([ADR-0007](0007-persistence-and-migrations.md)). Acceptable for a personal tool.
- **Staged rows accumulate** for abandoned imports. A retention policy is a future issue, not an M5 blocker.
- **Determinism constrains the export writer** — stable ordering and no incidental timestamps. Easy to break accidentally, which is precisely why the byte-identical test exists.
- **CSV import cannot represent splits.** A documented limitation, surfaced in the UI rather than discovered.
