# ADR-0002 — Authoritative ledger, derived balances, and corrections

**Status:** Accepted · 2026-08-31

## Context

Two product requirements bear directly on this. Derived aggregates must not become the authoritative source of financial history, and the effect of editing a historical entry on reporting and auditability must be understood before an edit operation is offered.

Both are easy to violate accidentally. A `balance` column on `accounts` is the natural thing to write and the thing that silently goes wrong — a failed update, a concurrent write, or a bug in one code path and the stored number no longer matches the transactions, with no way to tell which one lied. Likewise, a plain `UPDATE` on a transaction is the natural edit and destroys the answer to "what did this report say last month?"

The countervailing pressure is the product itself ([ADR-0010](0010-personal-finance-not-accounting-software.md)): this is personal finance for a normal person. Someone who typed 800 instead of 8000 wants to fix it, not to record a compensating reversal entry. Accounting-grade immutability is correct and would make the product worse.

## Decision

### Transactions and their postings are the only authoritative record

Every number the system reports — balances, category totals, net cash flow, budget actuals, savings rate, net worth — is computed from postings at query time and is never the source of truth.

```
balance(account, as_of) = opening_balance
                        + Σ posting.amount_minor
                          where posting.account_id = account
                            and transaction.booked_date <= as_of
                            and transaction.deleted_at is null
```

`accounts.opening_balance` is a *declared starting point*, not a cached aggregate: the user asserts "this account had ₹50,000 on 2026-01-01" so they need not import a decade of history. It never changes as transactions arrive.

Cached or incrementally-maintained balances are permitted later **only** as a measured performance optimisation, and only behind a test asserting the cached value equals the recomputed value on a representative fixture. Until such a test exists, the cache does not.

### Transactions are mutable, with an append-only audit trail

An edit is an `UPDATE`, plus an insert into `transaction_revision`:

| Field | Notes |
| --- | --- |
| `id`, `transaction_id` | |
| `revised_at` | timestamptz |
| `actor` | user ID, or `import:<batch_id>`, or `cli`, or `mcp` |
| `previous_state` | JSON snapshot of the transaction and its postings before the change |
| `reason` | optional free text |

Deletion sets `deleted_at`. Rows are never physically removed. This matters for more than sentiment: an import's `external_id` must still deduplicate against a transaction the user deleted, or re-importing the same statement resurrects it.

`transaction_revision` is **never read by analytics**. It answers "what did I change?" and supports debugging; it is not an event log the system replays.

### Refunds and reversals are ordinary transactions

A refund is an inflow whose posting carries the *same category* as the original outflow, with `related_transaction_id` pointing at it. No special entity, no special reporting rule: a ₹2,000 shirt refunded leaves ₹0 in Clothing because −2000 + 2000 = 0, and the original purchase is untouched. A user who returned something did not un-buy it.

## Alternatives considered

**Full immutability; corrections are reversal entries.** What real accounting systems do, and the most auditable option. Rejected on [ADR-0010](0010-personal-finance-not-accounting-software.md): it forces journal-entry thinking onto someone fixing a typo, and it makes the transaction list confusing — three rows where the user remembers one purchase.

**Event sourcing over the ledger.** Complete history, time-travel queries for free. Rejected as disproportionate. It buys reproducibility this model already has (postings are themselves the durable facts) and costs projection machinery, replay performance, and schema-evolution pain, for a single-user application.

**Mutable with no history.** Simplest, and what most personal-finance apps ship. Rejected against §26: "how did this report change?" becomes unanswerable, and an import that silently rewrites existing transactions is undetectable. A JSON snapshot per edit is a cheap price for that.

**Hard deletes.** Rejected specifically because of import deduplication — a deleted transaction whose `external_id` is gone comes back on the next import of the same statement.

## Consequences

**Good.** Reports are reproducible from first principles, and any disagreement between two numbers is a query bug rather than a data-integrity mystery. Editing feels normal. Import re-runs are idempotent against deleted rows. The audit trail is available if the user ever asks what changed.

**Bad:**

- **Every query joins `postings` to `transactions`** and filters `deleted_at is null`. A query that forgets is wrong. The repository layer owns those predicates so surfaces cannot omit them, and this is on the review checklist for any new query.
- **Soft-deleted rows accumulate.** Unbounded, in a single-user database — acceptable at this scale, but a `purge` operation with an explicit retention window is a plausible future issue, not a defect to fix now.
- **`transaction_revision` grows without limit** for a user who edits heavily, storing full JSON snapshots. Pruning is deferred until someone actually notices.
- **No cached balances means recomputation on every request.** Correct by construction; if it ever becomes slow, the fix is a measured cache with the equality test above, not a `balance` column added under time pressure.
