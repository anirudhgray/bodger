# ADR-0003 — Transactions, postings, transfers, and splits

**Status:** Accepted · 2026-08-31

## Context

The brief asks directly (§5, §8, §34) how transfers and splits should be represented, and warns that transfers must never appear as income or expenses. It also asks (§6) whether categories should be one hierarchy, two typed hierarchies, or tag-like.

These questions have one answer between them, because the shape chosen for transfers decides whether splits need a second mechanism, and whether category assignment lives on the event or on the line.

The constraint that makes this hard is §2a: the model may borrow accounting ideas internally, but the user must never meet debits, credits, journals, or a chart of accounts.

## Decision

### A transaction has one or more postings; a posting has an account, a signed amount, and optionally a category

```
Transaction(id, kind, booked_date, description, …)
  └── Posting(account_id, amount_minor, currency, category_id?)   × 1..n
```

`kind ∈ {outflow, inflow, transfer}` is stored and validated against the posting shape, so reports can filter on it without re-deriving it.

**Signs are real.** Negative means money left the account. There is no separate direction column, and the sign is not a display concern.

**Category lives on the posting, not the transaction.** This is the load-bearing detail: it is what makes splits require no special case anywhere. Every report groups by `posting.category_id` and sums `posting.amount_minor`. A split is just a transaction with more than one posting.

### The four shapes, and their invariants

| Shape | Structure | Invariants |
| --- | --- | --- |
| Outflow | n postings, one account | all amounts negative; all on the same account |
| Inflow | n postings, one account | all amounts positive; all on the same account |
| Split | same as outflow/inflow with n > 1 | as above; no stored total |
| Transfer | exactly 2 postings, 2 distinct accounts | opposite signs; both categories null; same-currency legs sum to zero |

A split's total is never stored. A stored total is a second source of truth waiting to disagree with the sum of its parts.

**Cross-currency transfers are exempt from the zero-sum rule.** ₹20,000 out and $230 in are both facts the user knows; the rate is derived and stored as `fx_rate_used` with `fx_rate_source = 'implied'`. Forcing these legs to balance would either corrupt one of the two amounts or hide the FX cost the user actually paid. See [ADR-0004](0004-multi-currency-and-fx.md).

### Categories: one hierarchy, typed

`Category(id, parent_id?, name, kind ∈ {expense, income})`. Self-referential, arbitrary depth in the schema, two levels in the seed data and UI. Reports roll up the full subtree.

**Transfers carry no category**, enforced in the domain layer rather than by convention.

**Tags** are separate: free-form, many-to-many with `Transaction` (the event, not the line). They handle the cross-cutting concerns a hierarchy cannot — `#japan-trip-2026` spans Food, Transport, and Housing at once, and forcing it into the tree would corrupt the tree.

### Credit cards need no special case

An account with `kind = credit_card` is a liability whose stored balance is normally negative.

- A purchase on the card is an **outflow** on the card account. It is an expense, on the date of the purchase.
- Paying the bill is a **transfer** from the bank account to the card account. It is not an expense; the expense already happened.

Double-counting is structurally impossible: transfer postings carry no category, and income/expense reports filter on `kind`. Only the display sign differs — the UI may render a liability as "you owe ₹42,000" — and that is presentation.

## Alternatives considered

**`from_account_id` / `to_account_id` on the transaction.** Simplest to read; one row per transfer. Rejected: splits then need a separate mechanism, cross-currency transfers need two more amount columns, balance queries need a `UNION` over two columns rather than one `GROUP BY`, and every import and export path branches on which shape it is emitting. Postings pay a small readability cost once and give transfers, splits, multi-currency, imports, exports, and analytics a single uniform code path.

**Two linked single-account transactions.** Model a transfer as two ordinary transactions joined by `transfer_group_id`. Close to the chosen design, but the link is advisory — nothing stops one half being edited or deleted alone, leaving a phantom expense. Postings inside one transaction make the pair atomic by construction.

**Full double-entry with categories as accounts.** Every transaction balances to zero against an expense account. Elegant, and what Ledger and Beancount do. Rejected on §2a: it means a chart of accounts, it means "Groceries" is an account in every account picker, and it means explaining to a normal user why buying groceries is a transfer.

**Category on the transaction, with a separate `splits` table.** Works, but creates two ways to express the same thing (a one-line split versus a categorised transaction) and forces every report to `COALESCE` between them. Category-on-posting collapses both into one.

**Two category hierarchies, expense and income.** Rejected: duplicates every traversal, rollup, and picker. One tree with a `kind` discriminator does the same work.

**Untyped shared category tree.** Rejected: lets "Salary" appear in an expense picker, which is a correctness problem disguised as a UX one.

## Consequences

**Good.** Splits, transfers, refunds, and credit cards all fall out of one structure with no special-casing in reporting. Every analytical query is `SELECT … FROM postings JOIN transactions …`. The export format has one transaction shape. Adding a currency to a transfer leg required no schema change.

**Bad:**

- **A simple ₹800 expense costs two rows.** More joins, more mapping code, slightly more work to read raw SQL during debugging.
- **`kind` is redundant with the posting shape** and can in principle disagree with it. Mitigated by validating one against the other in the domain layer on every write, with tests — but it is denormalisation, chosen because filtering reports on a column beats re-deriving the shape in every query.
- **The invariants live in code, not in the schema.** SQLite cannot express "all postings of an outflow are negative and on one account." The domain layer is the enforcement point, which means a future direct-SQL write path could violate them. Nothing writes to the database except through repositories, and that is a rule the layer check in `make check` enforces.
- **Split transfers are rejected outright.** A transfer with more than two postings is an error. Revisit only with a concrete use case.
- **Transfer fees need a second transaction**, linked by `related_transaction_id`. A `posting.role` discriminator is the forward path if this proves annoying; it is not built.
