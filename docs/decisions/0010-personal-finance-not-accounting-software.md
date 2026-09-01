# ADR-0010 — Personal finance, not accounting software

**Status:** Accepted · 2026-08-31

## Context

The brief asks for a *serious* personal finance application — decades of history, multi-currency, import, budgets, analytics — and in the same breath insists it be usable by a normal person with minimal cognitive overhead, with the branding fun and the tool a joy to use.

Those two goals conflict in a specific, predictable way. The correct internal model for money movement **is** accounting-shaped: signed lines that balance, explicit periods, immutable history. Every one of those ideas is a good engineering answer and a bad user-facing concept. The default outcome of building this well, technically, is an application that asks a user what a chart of accounts is.

This is already load-bearing rather than aspirational. Three existing decisions were made on these grounds and would have gone the other way without them:

- [ADR-0002](0002-authoritative-ledger-and-corrections.md) chose mutable transactions with an audit trail over accounting-grade immutability, because someone who typed 800 instead of 8000 wants to fix it, not file a reversal.
- [ADR-0003](0003-transaction-posting-model.md) rejected full double-entry with categories-as-accounts — the most elegant option available — because it puts "Groceries" in every account picker.
- [`data-model.md` §4](../data-model.md#4-accounts) rejected a chart of accounts for the same reason.

All three originally cited a section of `agents/design-docs/initial.md` as their justification — a file that is gitignored, ephemeral, and explicitly meant to be deleted once absorbed. The reasoning behind three architectural decisions was anchored to a document scheduled to disappear. Those citations now point here instead, and this ADR is where the reasoning lives.

## Decision

**Accounting machinery is an implementation detail. It may exist; it may not surface.**

The tie-break rule, which is the operative part:

> When two designs are both technically valid, choose the one that keeps the **common** user workflow simpler — unless doing so compromises the correctness or extensibility of the underlying financial model.

Both halves matter. The first rejects elegant models that leak complexity. The second refuses to buy simplicity with wrong numbers: this is financial software ([ADR-0002](0002-authoritative-ledger-and-corrections.md)), and a simpler design that produces an unauditable balance is not the simpler design, it is the broken one.

Three consequences that bind every surface:

**1 · Vocabulary is a constraint, not a preference.** Users never meet double-entry, debits, credits, journal entries, ledgers, chart of accounts, accounting periods, or postings — not in a label, a flag, a help string, an error, or a tool description. The concrete banned-term table and its replacements are in [`ux-principles.md`](../ux-principles.md).

**2 · Power arrives through progressive disclosure, not through omission.** The answer to "simple but powerful" is *layering*, not a reduced feature set. The common path is short; depth is reachable and never mandatory. Removing capability to achieve simplicity is a violation of this ADR, not an application of it.

**3 · Internal correctness is not negotiable for it.** Postings, invariants, audit trails, and explicit FX policy all stay exactly as designed. They simply never appear in a user-facing string.

## Alternatives considered

**Build an accounting core and expose it directly.** What Ledger, Beancount, and GnuCash do, and they are excellent tools. Rejected: the brief asks for a normal person's application, and this design makes the first five minutes an education in bookkeeping.

**Build an accounting core and hide it behind a simplified UI.** Superficially the same as what was chosen, and meaningfully different. Under this option the domain model is a general accounting system and each surface translates. Rejected because the translation layer is where semantics get invented per-surface — the exact failure [ADR-0005](0005-shared-application-layer.md) exists to prevent — and because the abstraction leaks the moment anything goes wrong, at which point the user meets the accounting model in an error message. Instead the *domain itself* is personal-finance-shaped: categories are not accounts, transfers are a transaction kind rather than an emergent property of balanced lines.

**Simplify by omitting capability** — no splits, no multi-currency, no import. Genuinely simpler, and would ship faster. Rejected: the brief asks for a serious tool, and these are the capabilities a real ledger needs by year two. Progressive disclosure keeps them out of the common path without removing them.

**Treat UX as a per-surface concern, decided when each surface is built.** The status quo of this repo before this ADR. Rejected on evidence from within this session: issue #7's CLI specification had already reached `bodger tx out` — developer jargon in the primary layperson surface — written by the same session that wrote the principle down elsewhere. With four surfaces built partly in parallel by separate agents, "each surface decides" produces four vocabularies.

## Consequences

**Good.** The reasoning behind three existing ADRs survives the deletion of `initial.md`. New feature proposals have something concrete to be tested against. Surfaces share one vocabulary rather than negotiating it four times.

**Bad, and worth stating plainly:**

- **This ADR can be used to argue against almost anything**, since "it adds complexity" is available as an objection to every feature. The tie-break rule is deliberately narrow — it applies when two designs are *both technically valid*, not as a general veto on capability. Consequence 2 exists to block the lazy reading.
- **Vocabulary discipline is only partly enforceable.** [ADR-0005](0005-shared-application-layer.md)'s conformance suite checks that surfaces *behave* identically, not that they *speak* the same way. A banned-term lint ([#11](https://github.com/anirudhgray/bodger/issues/11)) now covers the two surfaces where "user-facing" is unambiguous — the error registry's default messages, and cobra help and flag usage text — and caught a real violation in the root command's help the day it landed. OpenAPI descriptions, MCP tool descriptions, and web UI strings stay a review responsibility: a check with false positives is one people learn to bypass.
- **Some internally natural names become unavailable in user-facing code**, and the split between an internal `Posting` and whatever a surface calls it is friction contributors will feel.
- **"A joy to use" is not testable.** [`ux-principles.md`](../ux-principles.md) converts as much of it as possible into checkable expectations; the residue is a judgement call this ADR cannot make for a reviewer.
- **Progressive disclosure is more design work than either extreme.** A flat simple tool and a flat powerful tool are both easier to build than a layered one.
