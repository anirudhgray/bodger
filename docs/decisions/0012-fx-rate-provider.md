# ADR-0012 — FX rate provider: Frankfurter

**Status:** Accepted · 2026-09-06

## Context

[ADR-0004](0004-multi-currency-and-fx.md) specifies everything about FX except where the rates come from. It defines the durable `fx_rate` record, the three conversion policies, the staleness ladder, and the rule that missing rates fail loudly — and then deliberately says only that rates "come from an external provider that must not become a hard dependency". M4 is the milestone that has to make that real, and the provider adapter ([#131](https://github.com/anirudhgray/bodger/issues/131)) cannot be written against an unnamed service.

ADR-0004 and [ADR-0001](0001-technology-stack.md) between them constrain the candidate set harder than a general "which FX API is good" survey would suggest:

1. **Historical, per-date lookups are mandatory, not a nice-to-have.** `transaction_date` is the *default* policy for historical reports. A provider that only serves "latest" cannot answer the default question.
2. **Base and quote are both variable.** The reporting currency is the third rung of ADR-0004's precedence ladder and is user-configured; account and entry currencies are whatever the user has. Nothing in this system may assume USD or EUR on either side of a pair.
3. **The provider must not be load-bearing for reads.** The app is fully usable with no network. Only *fetching new* rates needs connectivity.
4. **It has to survive self-hosting.** ADR-0001 ships a single binary onto someone's own machine. A provider that requires each self-hoster to sign up for an account, hold a per-instance secret, and stay inside a commercial free tier makes the setup story materially worse and makes the project's correctness contingent on a vendor's continued generosity.
5. **The refresh is batch-shaped.** [#135](https://github.com/anirudhgray/bodger/issues/135) refreshes *every currency pair currently in use* against the reporting currency as one user action. Rate limits are therefore a product constraint, not an ops footnote: a free tier of a hundred requests a month cannot host a loop over pairs and dates.

## Decision

### Frankfurter, via its v2 API

[Frankfurter](https://frankfurter.dev) (`https://api.frankfurter.dev`, `/v2`). MIT-licensed, open source, free, **no API key, no signup, no quota**. It aggregates published reference rates from roughly 84 institutional and central-bank sources — the ECB among them — covering 201 currencies with history back to 1948, and ships as a Docker image (`lineofflight/frankfurter`) that a self-hoster can run privately against a mounted SQLite volume.

| ADR-0004 constraint | How Frankfurter meets it |
| --- | --- |
| Historical per-date lookups | `?date=YYYY-MM-DD` on every rate endpoint; also a `from`/`to` time series |
| Arbitrary base **and** quote | `?base=` is a free parameter; see below — no triangulation needed |
| Not a hard dependency | Only the explicit fetch path calls it; stored rates serve every read |
| Self-hostable | MIT + Docker image + no key; the hosted endpoint is a convenience, not a lock-in |
| Batch-shaped refresh | `?quotes=` takes a comma list; one request covers every pair for a date |

### It quotes arbitrary base/quote pairs directly. The adapter does not triangulate.

This is the fact the issue exists to settle, and the answer is the good one. `base` is an ordinary query parameter, not a plan feature:

```
GET /v2/rate/INR/USD?date=2026-01-15
→ {"date":"2026-01-15","base":"INR","quote":"USD","rate":0.01109}

GET /v2/rates?base=INR&quotes=USD,JPY,BHD&date=2026-01-17
→ [{"date":"2026-01-17","base":"INR","quote":"BHD","rate":0.00415},
   {"date":"2026-01-17","base":"INR","quote":"JPY","rate":1.7496},
   {"date":"2026-01-17","base":"INR","quote":"USD","rate":0.01105}]
```

So the `FxRateProvider` adapter is a pass-through. **No EUR or USD pivot, no dividing one rate by another, no compounding two roundings into one stored number, no pivot-currency edge case when the pivot is itself one side of the pair.** That last one is worth naming: triangulating INR→USD through USD is a special case someone always forgets to write, and not needing it removes a class of bug rather than testing for it.

The port signature ([#131](https://github.com/anirudhgray/bodger/issues/131)) still takes an arbitrary base, quote, and date. If this provider is ever replaced by a single-base one, triangulation becomes *that* adapter's private problem and nothing above the port changes.

### The date in the response is authoritative. The date in the request is not.

Frankfurter answers a dated request with **the most recent rate at or before that date**, and tells you which date that was in the row itself. Observed:

```
GET /v2/rate/INR/ALL?date=2026-01-17   → 200 {"date":"2026-01-16", ... }
GET /v2/rate/EUR/USD?date=2026-01-18   → 200 {"date":"2026-01-18", ... }
```

Two currencies, same request shape, different answers — the walk-back is per pair, depends on which institutions had published, and is unbounded in size. A batch request for one date routinely returns **rows with several different dates in it**.

The rule this forces, and it is not optional: **`fx_rate.rate_date` is taken from the response row, never from the request.** Filing a Friday rate under Saturday's date would defeat ADR-0004's staleness ladder in the worst possible way — a rate the system was supposed to flag `stale`, or refuse, gets stored looking exact, and every report built on it is quietly wrong while looking checkable. This is precisely the silent-conversion failure ADR-0004 exists to prevent, arriving through the front door.

Consequences that follow, for [#130](https://github.com/anirudhgray/bodger/issues/130)/[#131](https://github.com/anirudhgray/bodger/issues/131)/[#135](https://github.com/anirudhgray/bodger/issues/135):

- A refresh of N pairs for one date may write N rows at up to N distinct `rate_date` values. That is fine — `(base, quote, rate_date, source)` is the primary key — but the write path must not assume one date per batch.
- The provider's walk-back is *its* fallback, not bodger's. ADR-0004's staleness window still applies, and it applies to the **returned** date: a successful 200 does not mean a rate exists for the date that was asked for.
- Every observed walk-back was backwards. The adapter should nonetheless reject a returned date **later** than the requested one rather than store it, because ADR-0004 forbids nearest-neighbour search into the future and a provider change should surface as an error rather than as a silently forward-dated rate.

### `source` is `frankfurter`, not `ecb`

ADR-0004's illustrative payload shows `"rate_source": "ecb"`. That was written before a provider was chosen and should be read as a placeholder. The value bodger stores is **`frankfurter`**.

The default response is a *blend* across every institution that published for that pair — around fifty of them for a liquid pair on a given day, visible via `?expand=providers`. Labelling that blend `ecb` would be a provenance lie, and `source` is part of `fx_rate`'s primary key precisely so that provenance is not a guess.

Frankfurter also accepts `?providers=ECB` to pin a single institution. **Not used, deliberately**, and the cost of pinning is easy to see:

```
GET /v2/rates?base=EUR&quotes=USD&date=2026-01-18                 → date 2026-01-18
GET /v2/rates?base=EUR&quotes=USD&date=2026-01-18&providers=ECB   → date 2026-01-16
```

The ECB does not publish on a Sunday; the blend does, because someone else did. Pinning trades 201 currencies and weekend coverage for a determinism ADR-0004 already provides another way — by storing rates permanently rather than re-deriving them. If someone later needs single-institution rates, the `source` column distinguishes the two populations already, so it is a new `source` value and not a migration.

Worth stating plainly, since it looks alarming and is not: **the blend is not frozen.** Institutions backfill, so re-fetching 2026-01-17 next month may return a slightly different number than it returned today. ADR-0004 is already right about this — the stored record is authoritative and is kept forever — which is why a report re-run in five years reproduces, and why "re-fetch and compare" is not a way to verify a stored rate.

### Rate limits: no quota, and a whole refresh is one request per date

Frankfurter's own statement: *"There are no quotas. Requests are rate-limited to prevent abuse, but there are no monthly or daily caps."* A self-hosted instance has no application-level limit at all.

Because `quotes` takes a comma list, [#135](https://github.com/anirudhgray/bodger/issues/135)'s "refresh every pair in use" is **one GET per distinct `rate_date`**, not one per pair:

| Fetch | Requests |
| --- | --- |
| `current` refresh, any number of pairs | 1 |
| Backfill `transaction_date` rates for one specific day | 1 |
| Backfill a year of daily dates, naively | ~365 |
| Backfill a year via `?from=&to=&quotes=` | 1 response, chunked as needed |

The abuse limiter is undocumented and therefore must be assumed tight. The fetch path is bound by this ADR to: **serialise requests** (no unbounded parallel fan-out over pairs or dates), keep the request rate low and bounded, set a client timeout, and never retry in a loop on failure. The time-series form is the right primitive for any backfill and should be preferred over a per-date loop the moment one is needed.

None of this is on a read path. A refresh is a user pressing a button.

### Offline and staleness

ADR-0004's guarantee is unchanged and the provider choice does not weaken it: stored rates serve every historical report with no network, and only fetching new rates needs connectivity. Two provider-specific facts sharpen how that must be presented.

**The public instance is not highly available.** Third-party monitoring puts `api.frankfurter.dev` around 90% uptime over a 90-day window, with a P95 latency near five seconds. For a free, no-key service that is a fair trade, and for bodger it is nearly harmless — it is only ever reachable from the explicit refresh — but it means **a failed refresh is a routine outcome, not an exception**. Every surface must render it as "couldn't reach the rate service; your stored rates are unaffected", with the stored data still on screen. A network error must never become an error page over data that is sitting in SQLite. A self-hoster who wants better than that runs their own Frankfurter, which is the point of it being self-hostable.

**Coverage gaps are real but smaller than expected.** The blend covers weekends for liquid pairs, because some contributing institutions publish daily. Thin pairs still walk back, and any pair walks back over a global holiday. ADR-0004's staleness window (default 7 days) is what absorbs this, applied to the returned date per the rule above.

### Error shapes the adapter can rely on

[#131](https://github.com/anirudhgray/bodger/issues/131) needs "provider unreachable" and "no data for this pair/date" to be distinguishable. They are, by status code — no heuristics on error strings:

| Response | Means | Adapter behaviour |
| --- | --- | --- |
| `200`, row date == requested | Exact rate | Store at the row's date |
| `200`, row date < requested | Provider walked back; no rate for the requested date | Store at the **row's** date; ADR-0004's staleness window decides usability |
| `404` | Valid currencies, requested date outside coverage — future date, or before this pair's history | Typed *no data for this pair/date*. **Not** `unavailable` |
| `422` | Unknown or unsupported currency code, or a bad parameter | `invalid_input` ([ADR-0011](0011-error-model.md)) |
| `5xx`, timeout, DNS, connection refused | Provider unreachable | `unavailable` |

Verified against the live API: `/v2/rate/EUR/XYZ?date=2026-01-15` → 422; `/v2/rate/EUR/INR?date=1950-01-04` → 404; `/v2/rate/EUR/USD?date=2030-01-04` → 404.

### Rates arrive as JSON numbers. Decode them as text.

`{"rate":0.01109}` is a JSON *number*, and a JSON number is a `float64` in `encoding/json` by default. ADR-0004 forbids floats anywhere — "not in the database, not in memory, not on the wire". The adapter therefore decodes with `json.Number` (a `json.Decoder` with `UseNumber()`, or a `json.Number`-typed field) and builds the decimal from its **string** form. Never through `float64`, not even transiently.

This is the one boundary in the system where the outside world hands us a float, so it is the one place the rule has to be enforced by construction rather than by convention, and it deserves a test that would fail if someone "simplified" the field to `float64`.

Observed precision is five to six significant figures — the provider's, not something bodger chooses.

### No API key, and the base URL is configuration

**Frankfurter needs no key, no token, and no signup.** There is no secret to commit, no secret to leak, and no per-self-hoster onboarding step. That is a selection criterion in its own right, not a bonus, and it is a large part of why it wins over better-funded alternatives.

What *is* configuration is the endpoint, because a self-hoster who wants volume, latency, or independence runs `lineofflight/frankfurter` themselves:

- `fx.provider_base_url`, defaulting to `https://api.frankfurter.dev`. Pointing it at a private instance is a config line, not a code change.
- A self-hosted Frankfurter also needs no credential *from bodger*. Its own optional per-institution keys (FRED, Banco de México, Bank of Thailand, and so on) are environment variables on **that** container. They are never bodger's secrets, never in bodger's config, and never in this repository.

Should a future provider require a key, this ADR sets the shape in advance so nobody improvises it: environment variable or config file only, read once at startup into config alongside every other setting, **never a literal in source, never a committed default, never logged and never echoed in a config dump**. ADR-0011 already keeps infrastructure detail out of user-facing errors, and the logger already forbids it. Adopting a keyed provider is an amendment to this ADR, not an implementation detail of an adapter.

## Alternatives considered

**exchangerate.host.** Broad coverage, historical endpoints, a familiar name, and the other seed candidate. Rejected on its free tier, which is **100 requests per month and excludes source-currency switching** — free is USD-source only. Both halves are disqualifying: 100 requests does not survive a single backfill, and a fixed USD source would force triangulation through USD for every non-USD pair, compounding rounding and doubling the request count against that same tiny quota. Paid tiers start around $15/month. A self-hosted personal-finance app should not require its user to hold a commercial API subscription to see last August in their own currency, and a free tier this small is not a usable fallback for the ones who won't.

**Open Exchange Rates.** Long-running, well-regarded, genuinely good history, and the most credible commercial option. Free tier: 1,000 requests/month, hourly updates, **USD base only** — non-USD base currency and the conversion endpoint begin at the $12/month Developer plan. Rejected for the same two reasons as above, with the base restriction the sharper one: it turns "does the provider quote arbitrary pairs?" from a property into a paywall, and would make triangulation a permanent fixture of the adapter for exactly the users least likely to be paying — the ones whose reporting currency isn't USD. The better quota does not change the shape of the problem.

**The ECB reference-rate XML feed, directly.** No intermediary, no third-party availability to depend on, and the most authoritative single source in the seed set. Rejected on three counts: about 30 currencies, EUR-base only (triangulation again, for a user whose currencies are INR and USD), and two different feeds — 90-day and full-history — with their own parsing and revision edge cases. Frankfurter is a thin MIT-licensed wrapper over precisely this feed plus 83 more sources; self-hosting it gets the same independence without bodger owning an XML importer, and keeps the option of `?providers=ECB` if pure-ECB provenance is ever wanted.

**Pin to `providers=ECB` rather than take the blend.** Tempting: every stored rate would be attributable to one named institution, which reads better in an audit trail than "a blend". Rejected as the default because it costs 201 currencies down to 47 and loses weekend coverage (demonstrated above) to buy a reproducibility that ADR-0004 already delivers by storing rates permanently. Available later as a distinct `source` value, without a migration.

**Ship two or three provider implementations up front to prove the abstraction.** Rejected as speculative, on the same reasoning [ADR-0011](0011-error-model.md) used against a code hierarchy: the port exists because ADR-0004 requires the provider not to be a hard dependency, not because there is a second real requirement. The second implementation is the in-memory fake the tests use, and that is enough to keep the port honest. A third gets written when someone actually needs one.

**Let the user paste rates in by hand instead of choosing a provider at all.** Rejected: it is not a provider decision, it is the absence of one, and [#135](https://github.com/anirudhgray/bodger/issues/135) explicitly scopes `fx_rates` as fetch-populated. Manual entry can be added later without contradicting anything here — it would arrive as its own `source` value, which is what that column is for.

## Consequences

**Good.** No key, no signup, no quota, no billing relationship, and nothing a self-hoster has to register for — the setup story stays "run the binary". Arbitrary base and quote means no triangulation code, no pivot-currency edge cases, and no compounded rounding in a stored rate. A full refresh of every in-use pair is one HTTP request. "Unreachable" and "no data" are distinguishable by status code. The provider is MIT-licensed and self-hostable, so the escape hatch from every remaining downside below is a Docker command.

**Bad, and worth stating plainly:**

- **Availability is roughly 90%, not four nines.** A failed refresh is a routine event that every surface has to render gracefully, over data that is still perfectly readable. Designing that "couldn't refresh, here's what you have" state is real UI work, and getting it wrong turns a free service's ordinary flakiness into a broken-looking app.
- **P95 latency near five seconds** means the refresh action needs a visible progress state and a client timeout chosen with that number in mind — not a spinner that looks hung.
- **It is a small open-source project, so there is a bus factor.** Mitigated by MIT licensing, the Docker image, and the fact that the durable part is the central banks rather than the wrapper — and by ADR-0004 storing rates forever, so an outage of any length costs *new* rates, never old ones. It is still a real dependency on a volunteer service.
- **The blend is not reproducible from the provider.** Re-fetching an old date can return a slightly different number as institutions backfill, and two instances that fetched the same date at different times can legitimately hold different rates for it. Bodger's stored copy being authoritative is the right answer, but it means re-fetching is not a way to verify a stored rate.
- **`source = 'frankfurter'` is less specific than a central bank's name.** The audit trail says which *service*, not which institution. `?expand=providers` could store the full breakdown later; it is deliberately not stored now, because that is ~50 rows per pair per day of provenance nobody has asked to read.
- **Fiat only.** No crypto (`/v2/currency/BTC` is a 404), no metals, no securities. Anything that isn't an ISO 4217 currency needs a different provider and a different ADR.
- **The response-date rule is a permanent trap for the next person.** Storing the requested date instead of the returned one is a one-character-looking mistake that produces confidently wrong historical reports and no error. It needs a test in the adapter suite that fails on that specific substitution, not just a comment.
- **Precision is the provider's, at five to six significant figures**, which is below what ADR-0004's decimal type can hold. Fine for personal finance, but it is the honest answer when someone eventually reports a converted total off by a few minor units at large amounts.
