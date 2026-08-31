# ADR-0009 — One query, filter, and analytics model

**Status:** Accepted · 2026-08-31

## Context

Filtering is a fundamental capability shared by every surface, across roughly a dozen dimensions — date range, account, account kind, category and its subtree, direction, transfer, currency, amount range, payee, tags, import source. The architecture must avoid separate filtering logic per surface.

Analytics is a long list of metrics, and charting requires multiple interfaces to request the same analytical data with the same semantics.

The failure mode is specific and common: the web UI grows a rich filter panel that builds query parameters the CLI cannot express, then the CLI grows its own flags with subtly different semantics (does `--category Food` include subcategories?), and eventually the same question asked two ways gives two answers. This is the [ADR-0005](0005-shared-application-layer.md) problem again, in the read path.

## Decision

### One `TransactionFilter` struct, defined in the application layer

Every surface builds the same struct. It is the only way to express "which transactions", and it is used identically by transaction listing, every analytics method, every chart, and every export.

```go
type TransactionFilter struct {
    DateFrom, DateTo   string   // raw; normalised by app ("2026-08", "last-90-days", ISO)
    AccountRefs        []string
    AccountKinds       []string
    CategoryRefs       []string
    IncludeSubcategories bool   // default true
    Kinds              []string // outflow | inflow | transfer
    Currencies         []string
    AmountMin, AmountMax string
    Description        string   // substring, case-insensitive
    Tags               []string
    TagMode            string   // any | all
    ImportBatchRefs    []string
    IncludeDeleted     bool     // default false
}
```

Raw strings, normalised by the app layer, for exactly the reasons in [ADR-0005](0005-shared-application-layer.md). `"last-90-days"` and `"2026-08"` resolve against the injected clock in the user's timezone — so all four surfaces agree on what "this month" means, including at 00:30 on the first.

**Semantics are fixed once, in the filter, not per surface:**

- Multiple values within one dimension are **OR**. Different dimensions are **AND**.
- `CategoryRefs` includes the subtree by default. `Food` means Groceries, Restaurants, and Delivery — that is what users mean, and defaulting the other way produces reports that quietly under-count.
- `Kinds` defaults to `[outflow, inflow]` for analytics and to all three for listing. Transfers are excluded from spending and income totals by construction ([ADR-0003](0003-transaction-posting-model.md)).
- Date bounds are **inclusive** at both ends.
- Amounts compare on **absolute** value, so `--min 1000` means "at least ₹1,000" regardless of direction.

### Analytics are application methods, not endpoints

Each metric is a method on the app layer taking a filter, a grouping, and a currency policy, returning a structured result. `/api/v1/analytics/...`, `bodger report ...`, an MCP `analyze_spending` tool, and a chart in the web UI all call the *same method*.

```go
SpendingByCategory(ctx, ActorID, filter, opts) (CategoryBreakdown, error)
CashFlow(ctx, ActorID, filter, GroupByMonth, opts) (TimeSeries, error)
AccountBalances(ctx, ActorID, asOf, opts) (BalanceSet, error)
BudgetPerformance(ctx, ActorID, budgetRef, period, opts) (BudgetReport, error)
```

`opts` carries the reporting currency and the conversion policy ([ADR-0004](0004-multi-currency-and-fx.md)), and the result carries them back so the caller can render provenance.

### Results are chart-shaped, and the surfaces do no maths

An analytics result is a structured series with labelled dimensions and typed monetary values — the shape a chart library, a CLI table, and a JSON response all consume without further computation.

**The web UI does not sum, convert, roll up, or bucket.** If a number appears in a chart, the server computed it. The moment a total is computed in TypeScript, the CLI and MCP cannot produce it and the product has a second financial model ([ADR-0005](0005-shared-application-layer.md) again). The browser formats server-supplied minor-unit integers for display, and does nothing else with them.

### Pagination and determinism

List queries are cursor-paginated (opaque cursor over a stable sort key) rather than offset-paginated, so a page boundary does not shift when a transaction is inserted mid-scroll. Sort order is always fully specified — `(booked_date DESC, created_at DESC, id DESC)` — because a query with a non-deterministic tiebreak produces a different answer on two runs and is untestable.

Analytics results are not paginated; they are aggregates. A grouping that would produce an unbounded number of buckets (`group by description`) is not offered.

## Alternatives considered

**A general query language** — GraphQL, or a DSL over transactions. Maximum flexibility, and it would let the UI ask for anything. Rejected as over-engineering with a real cost: an open query surface makes it impossible to guarantee that two surfaces asking "the same" question get the same answer, which is the exact property this ADR exists to protect. A closed set of filter dimensions and named metrics is more constrained and more trustworthy.

**Per-surface filter types**, each mapped to a shared query internally. Rejected: the mapping *is* the divergence — that is where `IncludeSubcategories` defaults differently in the CLI than in the UI, and nobody notices until two reports disagree.

**Analytics as SQL views.** Fast, and pushes work to the database. Rejected as the primary interface: views cannot take a currency conversion policy, cannot express the precedence rules, and would live in the persistence adapter rather than the app layer. Views may be used *inside* the adapter as an implementation detail.

**Compute analytics in the browser from a raw transaction feed.** Very responsive, and enables client-side re-slicing without a round trip. Rejected as a direct violation of the one architectural principle: it puts financial logic where only one surface can reach it, and currency conversion in the browser would need the rate table in the browser.

**Offset pagination.** Simpler. Rejected: shifting page boundaries during scroll is a real bug in a list that is inserted into.

## Consequences

**Good.** A new filter dimension is added once and every surface gains it. A new metric is one app method and appears in the API, CLI, and MCP together. The same question asked through any surface returns the same answer, and there is a test that says so. Chart-shaped results keep the web UI genuinely thin.

**Bad:**

- **The filter struct is wide and will grow.** Every surface has to expose a lot of options, and the CLI in particular ends up with many flags. Better than the alternative, but it is real surface area.
- **A closed metric set means new questions need backend work.** A user who wants a metric nobody anticipated cannot compose it from primitives; they file an issue. Accepted in exchange for the guarantee above — and the canonical export ([ADR-0008](0008-import-export-architecture.md)) is the escape hatch for genuinely bespoke analysis.
- **Some analytics queries will be expensive** as history grows. Indexes on `(user_id, booked_date)`, `(account_id)`, and `(category_id)` are the first answer; materialisation is a measured last resort, subject to [ADR-0002](0002-authoritative-ledger-and-corrections.md)'s rule that a cached aggregate needs a test proving it equals the recomputed value.
- **Cursor pagination is harder to implement** than offset and cannot jump to page N. The UI uses infinite scroll, which is what it wanted anyway.
- **Raw-string filter fields** carry the same "shouldn't this be typed?" objection as [ADR-0005](0005-shared-application-layer.md)'s commands, and the same answer.
