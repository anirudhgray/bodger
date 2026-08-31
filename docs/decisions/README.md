# Decision records

One file per load-bearing architectural decision. Numbered sequentially, never renumbered, never deleted — a decision that stops being true gets a new ADR that supersedes it, and the old one is marked `Superseded by ADR-XXXX` so the reasoning stays readable.

Not every choice needs one. An ADR is warranted when a decision is expensive to reverse, when it constrains work in more than one part of the system, or when the obvious alternative is defensible enough that a future reader will ask why it wasn't taken.

Format: **Status · Context · Decision · Alternatives considered · Consequences.** Consequences must include the bad ones; an ADR listing only benefits isn't recording a decision, it's advertising one.

| ADR | Decision | Status |
| --- | --- | --- |
| [0001](0001-technology-stack.md) | Technology stack: Go, SQLite, React, single binary | Accepted |
| [0002](0002-authoritative-ledger-and-corrections.md) | Transactions authoritative, balances derived, mutable with audit trail | Accepted |
| [0003](0003-transaction-posting-model.md) | Transactions and postings; transfers, splits, categories | Accepted |
| [0004](0004-multi-currency-and-fx.md) | Money representation, currency precedence, FX conversion policies | Accepted |
| [0005](0005-shared-application-layer.md) | One application layer; normalise once; how it is enforced | Accepted |
| [0006](0006-authentication-and-multi-user-path.md) | Single-user first, multi-user shaped; sessions and API tokens | Accepted |
| [0007](0007-persistence-and-migrations.md) | SQLite, repository boundary, goose migrations | Accepted |
| [0008](0008-import-export-architecture.md) | Staged import pipeline; canonical versioned export | Accepted |
| [0009](0009-query-and-analytics-model.md) | One filter and analytics model shared by every surface | Accepted |
