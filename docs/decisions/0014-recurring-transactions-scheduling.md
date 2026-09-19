# ADR-0014 — Recurring transactions: the schedule type, occurrence generation, and the balance firewall

**Status:** Accepted · 2026-09-16

## Context

[`docs/data-model.md` §11](../data-model.md#11-recurring-activity) already fixes the shape recurring activity must take, deliberately, ahead of the milestone that builds it:

- Three strictly separate things — a **`RecurringRule`** (a template; money has never moved), a **`ScheduledOccurrence`** (a projection of one firing of that rule on a date; money has still never moved), and a **`Transaction`** (money moved). "Conflating any two of them is a correctness bug."
- Only the transaction contributes to balances, reports, or budget actuals. Occurrences may appear in forecasts and reminders, "always visually and structurally distinguished from actuals."
- A rule's schedule is an "RFC 5545 `RRULE` subset: monthly-on-day-N, weekly, yearly."
- Materialising an occurrence creates a real transaction and links back.

What that leaves open, and what this ADR decides, is everything mechanical: what the supported subset actually is and how it is represented in Go and in the schema, what a rule's firing dates *are* on the dates where a naive reading of RFC 5545 is ambiguous or wrong for this product (a monthly-on-the-31st rule in February), where "what date is it" is resolved, how far ahead occurrences are generated and whether they are persisted at all, and — the one that is expensive to retrofit — what makes it *structurally* impossible for a projection to end up inside a balance.

That last point is why this is decided now, in the milestone's first issue, rather than when the first forecast screen is built. [ADR-0002](0002-authoritative-ledger-and-corrections.md) makes every balance a recomputation over postings; the cheapest way to keep forecast money out of a balance forever is for a projection to have no posting, no account, and no amount-in-a-currency at all — a property that is free to establish now and a migration to establish later.

Recurring **transfers** are out of scope for this milestone: a rule posts to exactly one account/category pair. Split transfers are already deferred in [`data-model.md` §13](../data-model.md#13-deliberately-deferred), and a transfer rule would need a second account reference that nothing else in this design carries.

## Decision

### The schedule is a structured domain value, not a stored `RRULE` string

`internal/domain/recurring.Schedule` is a closed, validated value object with a `Frequency` discriminator (`weekly`, `monthly`, `yearly`), an `Interval` (≥ 1), and exactly the positional fields that frequency needs: a weekday for `weekly`, a day-of-month for `monthly`, a month plus day-of-month for `yearly`. It is constructed only through `NewWeeklySchedule`/`NewMonthlySchedule`/`NewYearlySchedule`, each of which rejects an out-of-range value, and it persists as ordinary typed columns (`frequency`, `interval_count`, `weekday`, `day_of_month`, `month_of_year`) with a `CHECK` constraint asserting that exactly the right subset is populated for the declared frequency.

Storing a raw `RRULE` string instead was the obvious alternative and is rejected for three reasons:

1. **A string column can hold grammar the system cannot evaluate.** `FREQ=DAILY;BYDAY=MO,WE;BYSETPOS=-1` is a perfectly valid `RRULE` and there is no code in bodger that can compute its firings. A row that no evaluator can interpret is a stored value that is *wrong* in a way nothing catches until a forecast silently produces nothing. The struct makes those states unrepresentable, and the `CHECK` constraint makes them unwritable.
2. **It would require a parser and a validator before the first rule can be created**, and that parser would necessarily be a partial implementation of RFC 5545 that accepts a subset — i.e. the same decision as this one, taken twice, with a string as the intermediate representation.
3. **bodger's monthly semantics deliberately differ from RFC 5545's** (see the next section). Persisting an `RRULE` string would claim conformance the implementation does not have, for the one case where the difference is most visible.

`Schedule.String()` renders the canonical `RRULE` text for the subset (`FREQ=MONTHLY;INTERVAL=1;BYMONTHDAY=31`) for display, debugging, and a future export format. It is a derived rendering, one-way: the structured columns are canonical, and a parser in the other direction is deliberately not built until an issue actually needs to read one back.

`UNTIL` and `COUNT` are not part of the schedule. A rule's end is `ends_on` on the rule itself, so there is exactly one place that answers "when does this stop" rather than two that can disagree.

### The supported subset, stated exhaustively

| Supported | Not supported |
| --- | --- |
| `FREQ=WEEKLY` with one `BYDAY` weekday | `FREQ=DAILY`, `HOURLY`, `MINUTELY`, `SECONDLY` |
| `FREQ=MONTHLY` with one `BYMONTHDAY` day (1–31) | `BYDAY` lists (`MO,WE,FR`), ordinal weekdays (`-1FR`, `2TU`) |
| `FREQ=YEARLY` with one `BYMONTH` + one `BYMONTHDAY` | `BYSETPOS`, `BYWEEKNO`, `BYYEARDAY`, `WKST`, `EXDATE`/`RDATE` |
| `INTERVAL` ≥ 1 on any of the three | More than one value in any `BY*` part |
| | `UNTIL`/`COUNT` (expressed as the rule's `ends_on`) |

`INTERVAL` is included because it is one integer and it is what makes fortnightly pay and quarterly premiums expressible without a fourth frequency; everything else in the right column is excluded because no workflow in this milestone needs it and each one adds a genuinely different evaluation path. The subset can be widened later by adding a frequency constructor and a column; it cannot be widened by accident, which is the point.

### A short month clamps to its last day; it never skips, and the anchor never drifts

A monthly-on-the-31st rule fires on **28 February** (29 in a leap year), **30 April**, and **31 May**. A yearly rule anchored on 29 February fires on 28 February in a non-leap year.

This is a deliberate, documented deviation from RFC 5545, whose `BYMONTHDAY=31` skips months that have no 31st. The reason is that bodger's rules describe money that actually moves: a rent standing order, a salary, an EMI on the last day of the month. Skipping February would mean a forecast that silently omits a payment the user will certainly make, which is exactly the class of quiet wrongness this project's data model exists to prevent. An omitted occurrence is worse than a clamped one, because the clamped one is visible and correctable and the omitted one is not.

Two properties follow, and both have tests:

- **Clamping is per-occurrence, never persisted back into the rule.** The rule stores day-of-month 31 forever. February clamps to the 28th and March returns to the 31st. A design where the clamped date became the new anchor would walk a rule backwards through the calendar a day at a time, which is the classic bug in this area.
- **Clamping never produces a duplicate.** Because each firing is computed from the anchor month rather than from the previous firing, two consecutive months can never resolve to the same date.

### "What date is it" is resolved once, in the application layer, and the domain never asks

`ScheduledOccurrence.occurrence_date` is a `domain.Date` — a calendar date with no time and no timezone, the same type and the same reasoning as `transactions.booked_date` ([`data-model.md` §9](../data-model.md#9-dates-periods-and-time)). All schedule arithmetic is therefore integer calendar arithmetic, performed against `time.UTC` internally purely as a calendar, and it **cannot** hit a DST boundary: there is no instant to shift, no zone to shift it in, and no wall-clock hour that can happen twice or not at all. A weekly rule crossing a spring-forward or fall-back weekend advances by exactly seven days like any other week, and the same computation returns identical dates under `TZ=UTC` and `TZ=Asia/Kolkata`.

The one place a timezone genuinely enters is deciding *which* occurrences are due — that is, what "today" is. That resolves exactly once, in `internal/app`, from `Service.Clock` and the actor's configured timezone, through the same `normalize.DateOf` path every other user-entered date already takes ([ADR-0005](0005-shared-application-layer.md), CLAUDE.md's normalise-once rule). Concretely:

- `internal/domain/recurring` imports no clock and has no `Now()`. Every evaluation method takes the window it should evaluate over as explicit `domain.Date` arguments.
- The generation use case (issue #278) receives an already-resolved "today" and derives its horizon from that. No repository method takes a clock, an instant, or a "now" argument; no surface resolves a date for itself and passes it in.

This is the same shape the FX rate-fetch and budget-period work already hold to, and it is the property the surface-conformance suite exists to keep true.

### Occurrences are persisted ahead of time, by an explicit, idempotent generation call — never a background job

An occurrence carries state a user creates (`skipped`) or a materialisation creates (`materialised`, plus the resulting `transaction_id`), so it has to be durable. Generating the whole projection on demand and persisting only the acted-on ones would mean two different code paths producing the list a user sees, which is the sort of divergence this codebase spends its enforcement budget preventing.

So: **occurrences are rows, generated ahead, over a bounded horizon.**

- The generation window is `[rule.starts_on, today + 12 months]`, clipped by `ends_on`, and an archived rule generates nothing. Twelve months is chosen so a yearly rule produces at least one row and a forecast has a full cycle to draw; a weekly rule produces ~52 rows, which is nothing at personal-ledger scale.
- Generation starts at the rule's own `starts_on` rather than at today, so a rule backdated to an existing standing order surfaces its past firings for review rather than pretending they never fired. The cost — a long-backdated rule generating a large batch in one call — is accepted, with `starts_on` as the only knob; issue #278 may add an explicit `from` override if it proves annoying in practice.
- **Generation is idempotent.** `scheduled_occurrences` has `UNIQUE (rule_id, occurrence_date)`, so re-running generation over an overlapping window can neither duplicate a row nor resurrect one the user skipped. This is enforced by the schema, not by the caller remembering to check.
- **There is no scheduler, no cron, and no background goroutine**, matching the choice [ADR-0004](0004-multi-currency-and-fx.md) already made for FX rate fetching: bodger is a single binary a user runs, and an action that writes rows is always an action someone took.
- When a rule's schedule changes, its *pending* future occurrences are discarded and regenerated. `ScheduledOccurrenceRepository.DeletePending` is scoped to exactly that — pending rows, one rule, on or after a date — so a regeneration cannot delete a materialised or skipped one even if a later caller gets the filter wrong.

### A `ScheduledOccurrence` cannot reach a balance, structurally

This is the invariant the whole three-entity split exists for, and it is enforced by construction rather than by convention:

- **An occurrence has no account, no currency, and no `Money`.** Its entire field set is `id`, `rule_id`, `occurrence_date`, `status`, and an optional `transaction_id`. There is no arithmetic anyone could perform on it that would produce a balance-shaped number, and no join key from it to an account.
- **Neither type can produce a `ledger.Posting`.** `internal/domain/recurring` does not import `internal/domain/ledger`, and balances are defined ([`data-model.md` §4](../data-model.md#4-accounts)) purely as a sum over postings. Only `internal/app`'s materialisation use case (issue #279) constructs a transaction, and at that point the *transaction* is what a balance reads — the occurrence merely records that it happened.
- **No repository method on either interface returns a posting, a transaction, or a monetary value**, and no balance or analytics query joins either table. `scheduled_occurrences.transaction_id` is a one-way provenance pointer *to* `transactions`; nothing traverses it in the other direction.
- A rule's `amount_minor` is a plain integer in its account's currency, the same single-source-of-truth shape `budgeting.BudgetLine` uses. A rule carries no currency column that could disagree with its account's.
- The direction of the money comes from the category's `kind` (`expense` → outflow, `income` → inflow), so a rule needs no `kind` of its own and cannot declare one that contradicts its category.

A persistence test asserts the blunt version of this: with rules and pending occurrences present, the `postings` table is empty and the transaction repository returns nothing.

### Status is a closed enum with a real state machine

`pending` → `materialised` (carrying the resulting transaction ID) or `pending` → `skipped`, and nothing else. A materialised occurrence must have a `transaction_id`; a pending or skipped one must not — enforced in the constructor and again by a `CHECK ((status = 'materialised') = (transaction_id IS NOT NULL))` in the schema, so the two states cannot drift apart through a direct write. Transitions return a copy rather than mutating, the same shape `importing.ImportBatch`/`ImportRecord` already use for ADR-0008's pipeline.

### Actor scoping goes through the rule, not a duplicated `user_id`

`recurring_rules` carries `user_id`; `scheduled_occurrences` does not. Every actor-scoped occurrence query joins its rule, exactly the way a balance query reaches `postings` through `transactions` — postings carry no `user_id` either, for the same reason. A duplicated owner column on the child is a second source of truth for ownership that can disagree with the first, and the disagreement would be invisible until it mattered.

## Alternatives considered

**Store a raw RFC 5545 `RRULE` string.** Standard, interoperable, one column. Rejected — see the three reasons above; the decisive one is that a string column can hold rules no evaluator in this system can compute, and the schema cannot stop it.

**Implement the full RFC 5545 recurrence grammar (or vendor a library that does).** Rejected: the grammar's hard parts (`BYSETPOS`, ordinal weekdays, `WKST`-sensitive weekly expansion, `EXDATE`) serve calendaring, not a personal ledger, and every one of them is an evaluation path that would have to be correct, tested, and rendered in a UI for a case nobody has asked for. The subset can grow one frequency at a time when a real workflow needs it.

**Skip the month when a monthly-on-day-N rule has no day N, per RFC 5545's own `BYMONTHDAY` semantics.** Strictly more standards-conformant. Rejected: the rules this models are rent, salary, and instalments, which are paid in February. A forecast that quietly drops a payment is a worse failure than one that shows it on the 28th, and "quietly wrong" is precisely the failure mode [`data-model.md` §1](../data-model.md#1-guiding-constraints) is written against.

**Roll a short month forward into the 1st of the next month instead of clamping back.** Rejected: it moves an occurrence across a reporting-period boundary, which would make a monthly forecast show two firings in March and none in February — a worse distortion than a date that is off by up to three days within the correct month.

**Compute occurrences entirely on demand and persist nothing.** No generation call, no horizon, no idempotency to get right. Rejected: `skipped` and `materialised` are user-created state that has to live somewhere, so occurrences would have to be *partly* persisted — which means one code path for the projected ones and another for the stored ones, producing the same list. Two implementations of one list is exactly the drift ADR-0005 exists to prevent.

**Generate occurrences from a background scheduler.** What most finance apps do. Rejected: bodger is a single binary someone runs, frequently not running at all when a rule fires, and a background writer would make "why did this row appear" unanswerable from the audit trail. The same reasoning ADR-0004 applied to FX fetching applies here, and an explicit generation call keeps every write attributable to an action.

**Give `scheduled_occurrences` its own `user_id`.** Faster actor-scoped queries by one join. Rejected: a second owner column for a child row is a second truth about ownership; `postings` sets the precedent for scoping through the parent, and an index on `(status, occurrence_date)` plus the rule's own `user_id` index makes the join cheap at this scale.

**Put `account_id` on the occurrence so a forecast can group by account without joining.** Rejected outright: it is the single change that would give a projection a join key into an account, which is the first half of every way forecast money ends up in a balance. The join through the rule costs nothing and the absence of the column is load-bearing.

## Consequences

**Good.** The set of expressible schedules is exactly the set the system can evaluate, in Go and in SQL both. A monthly rule behaves the way a person with a standing order expects, in every month, with no drift. Nothing in the recurring subsystem can ask what time it is, so the normalise-once rule holds by construction rather than by review. Generation is idempotent and attributable, and re-running it is always safe. A projection has no path to a balance that doesn't run through a real transaction first.

**Bad:**

- **bodger's monthly semantics are not RFC 5545's.** Anything that eventually imports or exports a real `RRULE` will have to state this difference explicitly, and a `BYMONTHDAY=31` rule imported from a calendar app would fire in February here and not there. Deliberate, and the reason the stored form is not an `RRULE` string.
- **A 12-month horizon means a forecast cannot look further ahead than a year** without a second generation call, and a rule created today shows nothing beyond next year even though its schedule is unbounded. Extending the horizon is a constant change plus a regeneration, not a redesign.
- **A rule backdated years produces a large batch of pending occurrences in one call**, all of which the user then has to skip or materialise. Accepted for now; an explicit `from` override on generation is the escape hatch if it bites.
- **Two new tables and two new repositories** for something that is not ledger data, the same cost [ADR-0013](0013-mcp-server-design.md) accepted for `mcp_tool_call`.
- **Regenerating after a schedule edit throws away pending rows**, so anything a user might later attach to a pending occurrence (a note, a reminder preference) would not survive an edit. Nothing attaches to one today; a later feature that wants to will need to say what an edit means for it.
- **No recurring transfers**, so a monthly savings sweep between two of the user's own accounts cannot be expressed as a rule yet. It needs a second account reference and a second posting, and it is deferred rather than half-built.
