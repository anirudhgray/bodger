# ADR-0004 — Money representation, currency precedence, and FX conversion

**Status:** Accepted · 2026-08-31

## Context

Multi-currency is a first-class requirement, not a later feature. Accounts have currencies, entries may override them, the user has a reporting currency, and reports must be presentable in a selected currency. FX rates come from an external provider that must not become a hard dependency.

The question that actually decides correctness: when viewing a historical report, should conversion use the transaction-date rate, today's rate, or an explicitly chosen one — and whichever the answer, these must never be silently mixed.

That last part is the crux. "How much did I spend in August?" and "what is my portfolio worth right now?" want *different* rates over the same data, and a system with one implicit answer will be wrong for one of the questions without ever saying so.

## Decision

### Money is an integer count of minor units plus a currency code

```
Money { amount_minor int64; currency string }
```

Never a float, anywhere: not in the database, not in memory, not on the wire. JSON serialises amounts as **strings** (`"1500.00"`) alongside the currency and its minor-unit exponent, because a JSON number is a float in most parsers and `0.1 + 0.2` is the oldest bug in financial software.

The **exponent comes from the `currency` table**, not from an assumed 2. JPY is 0, INR and USD are 2, BHD and KWD are 3. This is the difference between ¥1,500 and ¥15, and it is a test in the domain suite.

Adding two `Money` values of different currencies is an **error, never an implicit conversion**. Conversion is an explicit operation that takes a rate and returns a value that remembers which rate it used.

### Currency precedence resolves once

```
explicit entry currency  →  account default currency  →  user reporting currency  →  instance default currency
```

Resolved in `internal/app/normalize`, and nowhere else. No handler, command, tool, or component may implement this ladder — see [ADR-0005](0005-shared-application-layer.md), which exists largely because of rules like this one.

### FX rates are a durable record, not a cache

`fx_rate(base, quote, rate_date, rate, source, fetched_at)`, primary key `(base, quote, rate_date, source)`. `rate` is a fixed-point decimal stored as text, not a float.

Calling this a cache would invite eviction, and evicting a rate silently changes a historical report. Rates are kept forever. A report run today and re-run in five years produces the same numbers, whether or not the provider still exists or has since revised its history.

### Conversion policy is explicit on every query, and travels with every result

Three policies, named in the query and echoed in the response:

| Policy | Rate used | For |
| --- | --- | --- |
| `transaction_date` | The rate on each transaction's `booked_date` | **Default for historical reports.** Reproducible; "August spending" never changes after August |
| `current` | The most recent rate available | **Default for present-tense questions** — current balances, net worth |
| `pinned(date)` | The rate on one specified date | Comparing periods on a common basis |

Every converted figure the API, CLI, or MCP server emits carries its provenance:

```json
{
  "amount": "1150.00", "currency": "USD",
  "converted_from": { "amount": "100000.00", "currency": "INR" },
  "rate": "0.011500", "rate_date": "2026-08-14",
  "rate_source": "ecb", "policy": "transaction_date"
}
```

There is no such thing as an unlabelled converted amount in this system. A number whose rate the user cannot see is a number they cannot check.

**Mixed-policy aggregates are forbidden.** A single total is computed under exactly one policy. If some transactions in a range have no available rate, the response reports the shortfall explicitly (`"unconverted": [...]`) rather than silently omitting them or falling back to a different policy for those rows.

### Missing rates degrade loudly

Rate lookup order: exact `rate_date` → most recent earlier rate within a configurable staleness window (default 7 days), flagged as `stale` → **fail with a named error**. No silent 1.0, no nearest-neighbour search into the future, no dropping the row from the total.

The application is fully usable with no network. Rates already stored serve every historical report; only fetching *new* rates needs connectivity.

### Cross-currency transfers record both legs

Both posting amounts are authoritative (the user knows both), the implied rate is derived and stored on the transaction with `source = 'implied'`, and the legs do not sum to zero. See [ADR-0003](0003-transaction-posting-model.md). The gap between the implied rate and the market rate is a real cost the user paid, and the model shows it rather than smoothing it away.

## Alternatives considered

**Store every amount pre-converted to the reporting currency.** Fast reports, no conversion at query time. Rejected outright: it makes a derived value authoritative ([ADR-0002](0002-authoritative-ledger-and-corrections.md)), and changing the reporting currency would require rewriting the ledger.

**Decimal type instead of integer minor units.** Correct, and what a database with a real `NUMERIC` type would encourage. Rejected because SQLite has no decimal type, so it would mean text-encoded decimals with parsing on every read, and because integer minor units make every arithmetic operation exact and trivially fast. Fixed-point decimal *is* used for FX rates, where sub-minor-unit precision is required.

**Float64 with careful rounding.** Rejected without discussion. This is financial software.

**One implicit conversion policy — always transaction-date.** Simpler API. Rejected because "what are my balances worth today?" is a real and common question that transaction-date conversion answers wrongly, and users would not be able to tell.

**Always convert at current rates.** Rejected because it makes every historical report change every day, which destroys reproducibility and makes month-over-month comparison meaningless.

**Treat the rate table as a cache with TTL eviction.** Rejected: evicting a rate silently changes a historical report.

**Fall back to 1.0 or skip unconvertible rows.** Rejected. Both produce a total that is wrong and looks fine. Failing loudly is the only honest option in financial software.

## Consequences

**Good.** Historical reports are reproducible indefinitely and offline. Every converted number is auditable down to the rate that produced it. Rounding errors cannot accumulate. Changing reporting currency is a query parameter, not a migration. FX cost on transfers is visible rather than hidden.

**Bad:**

- **The API is more verbose.** Every monetary field is an object with a currency, and converted ones carry five more fields. Deliberate: the alternative is a bare number nobody can verify.
- **Every surface must render provenance somewhere.** The web UI needs a way to show "converted at 0.0115 on 14 Aug" without cluttering a table — probably a tooltip or a detail row. That is real design work, deferred to M3.
- **Policy is a parameter on nearly every analytics query**, and picking the right default per endpoint is a judgement call that must be made explicitly rather than inherited.
- **Multi-currency reports can fail** where a single-currency one would succeed, when rates are missing. Correct, but it is a failure mode the UI must handle gracefully rather than as an error page.
- **The rate table grows forever.** Trivial at daily granularity for a handful of currency pairs; a non-issue at this scale.
- **Minor-unit exponents must be right in the seed data.** A wrong exponent is a 100× error. It is seeded from ISO 4217 and tested.
