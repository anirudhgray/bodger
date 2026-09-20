# ADR-0015 — AI-assisted suggestions: typesafe.ai, advisory only

**Status:** Accepted · 2026-09-20

## Context

M10 · Valinor adds the first feature in bodger where a machine proposes a financial decision rather than computing one. [typesafe.ai](https://docs.typesafe.ai/introduction) is the platform it is built on: a decision model (`jev`) that answers **typed questions** — **Choice** (pick one option from a bounded set), **Score** (rate against an ordered rubric), **Noul** (a yes/no probability) — and returns structured values with a calibrated `confidence`, rather than free text something downstream has to parse.

That shape is the reason this is worth doing at all. Every other way of adding "smart categorisation" to a personal-finance app either ships a trained classifier (which needs labelled data bodger does not have, against a category list each user invents and renames at will) or calls a general chat model and parses prose back into an enum. A Choice question over *the actor's own current category list* needs no training, no per-user model, and no parsing — and it cannot return a category that does not exist, because the option set is the request.

Two constraints from the existing system shape everything below, and they pull in opposite directions.

1. **[ADR-0002](0002-authoritative-ledger-and-corrections.md): the ledger is authoritative.** A category is part of a transaction's meaning. Nothing may write one because a model was confident.
2. **[ADR-0012](0012-fx-rate-provider.md): the last external provider was chosen specifically to avoid a key.** Frankfurter won partly *because* it needs "no API key, no signup, no per-instance secret", so self-hosting stays "run the binary". typesafe.ai reintroduces exactly that dependency — an account, a key, and metered credits. This ADR has to reconcile that rather than quietly walk it back.

### The vendor's own design guidance is the spine of this decision

typesafe.ai's ["How to build with System One"](https://docs.typesafe.ai/concepts/how-to-build-with-system-one) states the principle this ADR is organised around: **"code owns the workflow and AI handles narrow, structured decisions"**, and **"use code when you can. It is reliable and cheap."** Deterministic rules, control flow, and side effects stay in code; the model gets atomic, constrained judgements over unstructured text and nothing else.

That is not a stylistic preference here. It is what makes the feature safe to add to a ledger, and — combined with the model's documented limitations below — it decides the shape of the port, the call site, and which half of each question code answers before the model sees it.

### Model limitations that directly constrain this design

The vendor publishes a per-version [known-jaggedness page](https://docs.typesafe.ai/model-jaggedness/jev-1.13), and three entries on it are load-bearing rather than trivia:

- **"Jev reads dates as text, not as ordered quantities."** It cannot reliably tell which of two dates comes first, compute an interval, or verify a date window.
- **"Jev is not a calculator."** It cannot reliably count or compare numeric magnitudes, and performs worse on numeric representations than semantic ones.
- **"Accuracy falls as the state grows with content unrelated to the decision."** Irrelevant context in `state` acts as a distractor.

The first two would silently wreck occurrence matching, which is fundamentally "is this 2026-09-03 debit of ₹42,000 the same money as this occurrence projected for 2026-09-01?" — a date-window and amount comparison. The third decides how rows are batched. Both are resolved below.

### Other vendor facts the design had to accommodate

- **There is no Go SDK.** Python and JavaScript only. [ADR-0001](0001-technology-stack.md) ships a single Go binary, so the adapter is a hand-written HTTP client against `POST https://api.typesafe.ai/v1/systemone` — roughly the size of `internal/adapters/fxprovider`, and no new runtime dependency.
- **64k tokens per request, of which `state` plus the longest single question may be at most 32k.**
- **Questions in one call are evaluated independently and in parallel, and the `state` is charged once** — so several questions *about the same content* cost one state and add no latency.
- **`confidence` is returned on Choice and Score answers only — never on Noul**, which returns a bare probability. Anything that needs a calibrated certainty must ask a Choice.
- **A Choice takes at most 255 options.**
- **`state` must be a string, JSON object, or array of text values**, with objects preferred so each part has a descriptive name.

## Decision

### Suggestions are advisory. Nothing here ever writes to the ledger.

The whole feature is one read-only application-layer method that returns proposals and persists nothing. Every write that follows is an existing, already-human-confirmed use case, called by the user afterwards exactly as they would have called it anyway.

This is not a caveat attached to the design; it is the design. The rest of this ADR is mostly about making it structurally true rather than merely intended.

### The second use case is **occurrence** matching, not "recurring-rule matching"

Issue [#298](https://github.com/anirudhgray/bodger/issues/298) and `architecture.md` §8 both describe the second use case as a suggestion on "`internal/app/recurring_rules.go`'s matching path". **That path does not exist.** `recurring_rules.go` is CRUD only — `Create`/`Update`/`Archive`/`Get`/`List` — and nothing anywhere in `internal/app` or `internal/domain/recurring` matches a transaction against a rule. The premise was wrong, and building to it would have meant inventing a matching concept [ADR-0014](0014-recurring-transactions-scheduling.md) deliberately does not have.

The real gap is one layer down, and it is a genuine bug rather than a missing nicety:

- `GenerateOccurrences` projects **pending `ScheduledOccurrence` rows** for every active rule, and `MaterialiseOccurrence` is the explicit human-confirmed step that turns one into a real transaction.
- `findSuspectedDuplicate` (`internal/app/import_duplicate.go`) searches **committed transactions only** — `s.Transactions.List`. It never looks at pending occurrences.

So when a user imports a bank CSV containing the rent payment that is already sitting as a pending occurrence, nothing notices. They get an imported transaction *and* a still-pending occurrence for the same money, and the duplicate detection ADR-0008 built is structurally incapable of catching it, because the occurrence is not a transaction.

**This ADR therefore re-scopes the second use case to: suggesting that a staged import row is an instance of a pending `ScheduledOccurrence`.** The confirm step already exists (`MaterialiseOccurrence` / `SkipOccurrence`), the candidate set already exists (pending occurrences), and the suggestion lands at the same review step as the category suggestion — so both use cases share one call site, one port, and one failure path.

`architecture.md` §8's M10 description is corrected in the same PR as this ADR. The underlying detection gap is tracked separately as [#301](https://github.com/anirudhgray/bodger/issues/301), because fixing it must not depend on an optional, off-by-default feature.

### Code answers the arithmetic. The model answers only the semantics.

This is the single most important rule in this ADR, and it follows directly from "use code when you can" plus the two jaggedness entries above.

**Dates and amounts are never a model judgement.** The application layer narrows the occurrence candidate set *deterministically* before any request is built, using the comparison logic `findSuspectedDuplicate` already implements — same currency, equal absolute amount, `booked_date` within the existing `duplicateDateWindowDays` window. Only the survivors are offered to the model, and the only question it is asked is the one it is actually good at:

> Of these already date- and amount-eligible occurrences, which one does this row's **description** refer to?

The model never sees the question "is 2026-09-03 within seven days of 2026-09-01?", because it demonstrably cannot answer it. Likewise for categorisation: the row's **amount sign already determines whether an expense or income category applies**, so the app layer filters the option set by `CategoryKind` in code rather than offering both and hoping. A deterministic filter that halves the option set is free accuracy and free spend reduction, and it is exactly what the vendor means by keeping code in control.

If the deterministic filter leaves **no** candidate occurrences for a row, no occurrence question is asked for that row at all. The model is never invited to invent a match out of an empty or irrelevant set.

### One request per row — *not* one request carrying every row

The vendor's batching cookbook reports 12.2× cheaper and 10.0× faster for 13 questions batched into one call, and it is tempting to read that as "put 200 staged rows in one `state`". **That reading is wrong here, and following it would degrade accuracy.**

The cookbook's win comes from many questions about *one document*, where the document is the shared cost. Staged import rows are *independent* records: in a 200-row state, every row's question has 199 rows of unrelated content alongside it — precisely the documented "large irrelevant state" distractor failure. The economics would improve and the answers would get worse.

So the unit of request is **one row**:

- `state` is that row as an object (description, amount, currency, date) — small, entirely relevant.
- `questions` carries the category Choice and, when code left candidates, the occurrence Choice. **Two questions, one state, one round trip, no added latency** — this is where the genuine batching win applies, because both questions are about the same content.

The adapter then issues those per-row requests **concurrently**, through a bounded worker pool, so wall-clock time is set by the pool width rather than the row count. Go makes that the natural shape; it is the reason no contortion is needed to recover the throughput the single-giant-state approach appeared to offer.

The 32k `state` limit stops being a design concern entirely at one row per request — a single transaction row is a few hundred tokens. It remains an adapter-level guard, not a chunking algorithm.

### One port, `ports.SuggestionProvider`, in bodger's vocabulary

Defined in `internal/ports`, consumer-owned, per [ADR-0007](0007-persistence-and-migrations.md).

```go
// CategoryOption is one category the provider may choose from, already
// filtered to the kind the row's amount sign implies.
type CategoryOption struct {
	ID   string
	Name string
}

// OccurrenceOption is one pending occurrence the application layer has
// ALREADY established is date- and amount-eligible for its row. The
// provider is never asked to compare a date or an amount.
type OccurrenceOption struct {
	OccurrenceID string
	Description  string
}

// SuggestionRow is the minimal projection of a staged row — see "What
// leaves the machine". Never an importing.ImportRecord.
type SuggestionRow struct {
	RecordID    string
	Description string
	Amount      money.Money
	Date        domain.Date

	// Categories and OccurrenceCandidates are this row's own bounded
	// option sets, narrowed deterministically by the caller. An empty
	// OccurrenceCandidates means the occurrence question is not asked
	// for this row at all.
	Categories           []CategoryOption
	OccurrenceCandidates []OccurrenceOption
}

// RowSuggestion carries both answers for one row. An empty CategoryID or
// OccurrenceID means "no answer" — not an error, and not a match.
// Confidence is the provider's own, unfiltered: the display threshold is
// application-layer policy, applied above this port.
type RowSuggestion struct {
	RecordID             string
	CategoryID           string
	CategoryConfidence   float64
	OccurrenceID         string
	OccurrenceConfidence float64
}

type SuggestionProvider interface {
	// Suggest answers every row's questions, fanning out concurrently.
	// The returned slice is sparse and unordered; callers index it by
	// RecordID and never assume positional alignment with rows.
	Suggest(ctx context.Context, rows []SuggestionRow) ([]RowSuggestion, error)
}
```

Four properties of that signature are load-bearing:

**It is batch-shaped at the port, per-row on the wire.** A slice in, a slice out; how that becomes requests — one per row, concurrently, bounded — is adapter mechanism the application layer does not express an opinion about. This mirrors `FxRateProvider.FetchRange`, where the port says "this range" and the adapter owns how many HTTP calls that is.

**Each row carries its own option sets**, because the deterministic narrowing above is per-row: two rows in the same batch legitimately have different eligible occurrences and different category kinds.

**One method, not two.** Both questions concern the same row and the same `state`, so splitting them into `SuggestCategories` and `SuggestOccurrenceMatches` would send each row twice and double the latency for no accuracy gain — the vendor's parallel-questions property makes the combined call strictly better here.

**`Choice`, `Score`, `Noul`, `state`, `criteria`, and `jev` appear nowhere in it.** The port speaks about categories and occurrences; the mapping onto typed questions is entirely the adapter's. This follows `FxRateProvider` exactly — that port says `FetchRate(base, quote, date)`, not `GET /v2/rate` — and it is what keeps the vendor swappable. A locally-hosted classifier could satisfy this interface without a line changing above it.

**There is deliberately no `Name()` method**, unlike `FxRateProvider`. That one exists because `fx_rate.source` is a persisted provenance column. Nothing here is persisted, so there is no provenance to carry.

### Call site: one new read-only method, and **not** `StageImport`

`Service.SuggestForImportBatch(ctx, SuggestForImportBatchQuery) (SuggestForImportBatchResult, error)` — a new method in `internal/app`, read-only, writing nothing. It is where the deterministic narrowing happens, and it is the only caller of the port.

**`StageImport` does not call the provider**, and neither does `CommitImportBatch`, `ResolveImportRecord`, or any recurring-rule method. Staging is the write that must always succeed; putting a metered network round trip inside it would make an import fail because a vendor was down, which is precisely the hard dependency ADR-0012 refused. It would also spend credits on every row of every batch, including the ones nobody ever reviews.

Instead the suggestion is a separate, explicitly-invoked action at the review step — the same shape as ADR-0012's FX refresh, where "a refresh is a user pressing a button" rather than something a read path triggers.

Two scoping rules keep the spend proportional to the question actually being asked:

- **Only rows with no resolved category are sent for categorisation.** `buildImportRecord` already resolves `row.CategoryHint` deterministically; a row that matched does not need a model's opinion, and overriding a deterministic match with a probabilistic one would be a regression.
- **Only rows that are not already excluded are sent at all.** A tier-1 exact duplicate is excluded automatically and will never become a transaction.

**Nothing this method returns is persisted.** Not on `ImportRecord`, not in a new table, not in the export. Three reasons: a suggestion is worthless once the underlying category list or occurrence set changes, so a stored one is a stale answer waiting to be trusted; persisting it would require a migration plus [ADR-0008](0008-import-export-architecture.md) export/restore handling for data that carries no financial meaning; and the round-trip test asserts export → import → export is byte-identical, which advisory scores have no business participating in.

What *is* persisted is the user's decision, through the existing category-assignment and `MaterialiseOccurrence` paths — and it is recorded identically to any other manual choice, with no "suggested by AI" marker. That is deliberate. A human confirmed it, so it *is* a manual assignment; a provenance flag would imply the ledger holds a machine's opinion, which after confirmation it does not.

### Confidence never auto-applies, at any value

The vendor documents three bands and recommends acting automatically above 0.9. **bodger does not take that recommendation for any write.** Confidence does exactly one thing: decide what is worth showing.

| Confidence | Behaviour |
| --- | --- |
| < 0.5 | Discarded in the app layer; never reaches a surface |
| ≥ 0.5 | Shown as a suggestion, ranked by confidence |
| > 0.9 | Shown as a suggestion, ranked by confidence. **Identical.** |

Never pre-selected, never applied on commit, never a default a distracted user accepts by pressing enter. There is no threshold anywhere in this system above which a category is assigned without a person choosing it.

Four reasons, in increasing order of how badly they would bite:

1. ADR-0002 makes the ledger authoritative; a silently-applied category is the ledger holding something nobody asserted.
2. An import is a *batch*. A threshold that auto-applies at 0.9 across 200 rows produces 200 plausible, confidently-wrong-in-the-tail categorisations that look reviewed and are not — the same class of failure as ADR-0012's "store the requested date instead of the returned one": no error, no flag, and every report built on it quietly wrong.
3. **The 0.5 cutoff is uncalibrated.** The vendor is explicit that thresholds should be set empirically, "by plotting confidence against accuracy on real data" — which nobody has done for bodger's data, because that data does not exist yet. 0.5 is adopted as the vendor's own band boundary and nothing more. A number chosen that way is fit to decide what is *displayed*; it is not fit to decide what is *written*.
4. **Confidence is calibrated per model version**, and the vendor says so explicitly when recommending that callers pin a version. A display threshold degrades gracefully when the model moves. A writing threshold silently changes meaning under a model upgrade nobody in this repository triggered.

The threshold itself is app-layer policy, not adapter behaviour: the adapter returns whatever confidence it received, and `internal/app` filters. [ADR-0005](0005-shared-application-layer.md) puts every default and policy decision in the application layer, and it makes the threshold testable against the fake without a network.

### Every suggestion says it is a suggestion, and says where it came from

[`ux-principles.md`](../ux-principles.md) §6 already settled the principle this inherits, for FX: **"every converted amount shows its rate, date, and source... this is the user's money, and a figure they can't check is a figure they won't believe."** A converted balance in the CLI renders `source: frankfurter` next to the rate, and the web UI discloses the same provenance. A machine-proposed category is the same kind of claim — something the system derived rather than something the user told it — and it gets the same treatment.

Two things must therefore be visible at the point a suggestion is shown, on every surface:

1. **That it is a suggestion, not a value.** §5 of the same document already committed to this before the feature existed: *"the system never quietly decides something significant... Automatic categorisation, if it ever exists, suggests."* Suggested categories are rendered as a proposal to accept, never as a pre-filled field.
2. **That it came from typesafe.ai**, named plainly, the way `frankfurter` is named plainly. Not "AI", not "smart categorisation", not an unattributed sparkle — the vendor's name, because that is the checkable fact and because a user is entitled to know which third party saw their transaction description.

The model version belongs with the attribution wherever the rate's *date* would belong — available, not shouted. Following §4's progressive-disclosure layering, the common path shows "suggested by typesafe.ai"; `--json` and the REST response carry the precise model identifier and confidence value, the same way `--json` already carries a rate's full provenance. **The raw confidence decimal is not common-path copy** — "0.87" is not checkable by the person reading it, unlike an exchange rate, so it is used for ordering and disclosed on demand rather than printed next to a category name.

**The attribution does not survive confirmation, and that is not an inconsistency.** Once the user accepts a suggested category it is recorded as an ordinary manual assignment with no AI marker, per the call-site section above. The FX analogy breaks here for a real reason: a rate is *permanently part of* the converted figure, so it must travel with it forever, whereas a suggestion is *not part of* the confirmed transaction — the human's decision is. Labelling a confirmed category "AI-assigned" would misstate who asserted it. The transparency obligation is at the moment of decision, where it can still change the decision.

### When there is no key, say so — once, clearly, and never again

An operator who has not configured typesafe.ai gets **no AI features at all**, and the interface must make that legible rather than simply lacking a button nobody knows was supposed to be there.

This cuts against the nearest existing precedent, so the divergence is deliberate and stated: `ux-principles.md` §6 says **"a single-currency user must never meet the currency system at all"** — hide machinery that does not apply. The cases differ in whether anything is actually missing. A single-currency user is not missing a capability; there is genuinely nothing to convert. An unconfigured instance *is* missing a capability that exists, is documented, and that the person may well have read about — and the absence is otherwise indistinguishable from the feature being broken, which is the failure mode #298 asked to avoid.

The resolution is that visibility is bounded, not that it is loud:

- **The import review screen shows one quiet, non-blocking line** where the suggestion affordance would be, saying suggestions are available but not set up on this instance. It is never a modal, never a banner on unrelated screens, and never blocks review — the screen is fully usable, exactly as it is today.
- **Settings is where the detail lives**, including the environment variable an operator sets. An env var name is operator configuration, like `BODGER_DB_PATH`, not the internal jargon CLAUDE.md and §2 ban from user copy — but it belongs in the operator's surface, not in a sentence aimed at someone standing in a shop.
- **The CLI states it where it would have printed a suggestion**, and `--json` carries the machine-readable "not configured" state so a script can branch on it rather than parsing prose.
- **No nagging.** One statement of fact in the one place it is relevant. A self-hoster who has deliberately chosen not to send their data to a third party is making a legitimate choice, not neglecting setup, and the interface must not treat them as having an incomplete installation.

This is the user-facing half of the `Configured: false` result field: a distinct, non-error state exists in the application layer precisely so every surface can render this without inventing its own interpretation of an error code.

### Pin the model to `jev-1.13.0`. Never `jev-latest`.

Following directly from the point above. `jev-latest` is an alias that moves without a release on bodger's side, and it moves the calibration of a number this system makes decisions about. The pinned version is a constant in the adapter, bumped deliberately in its own PR — and a bump means re-reading that version's jaggedness page, since the limitations this design routes around are published per version.

### Credentials and endpoint: instance-level, optional, absent by default

Two values, resolved in `internal/platform/config` — the only package permitted to read the environment (ADR-0005) — following `EnvFxProviderBaseURL`'s existing pattern:

- `BODGER_TYPESAFE_API_KEY` — empty by default. Empty means the feature is off.
- `BODGER_TYPESAFE_BASE_URL` — empty by default, meaning the adapter's own `https://api.typesafe.ai`. Configurable for the same reasons ADR-0012 made the Frankfurter endpoint configurable, and because the JS SDK treats the base URL as ordinary configuration too.

**Instance-level, not per-user.** It is the operator's billing relationship, not a user preference like `ReportingCurrency`. When multi-user arrives ([ADR-0006](0006-authentication-and-multi-user-path.md)) this does not become a per-user column; every actor on an instance shares the operator's key and the operator's spend. Stating that now so M2's successor does not have to guess.

Unlike every other value in `config.Load`, **an absent key is not a validation failure.** Empty is the default and the feature is simply off. A missing key must never prevent the binary from starting, because that would make an optional feature a startup dependency.

The key is never written to SQLite, never included in an export, and never logged. Two concrete obligations follow:

- The adapter must never put the request's headers — or the request itself — into a wrapped error. [ADR-0011](0011-error-model.md)'s cause chain *is* logged at `Error` level, so an error carrying the `Authorization` header is a credential in the log file.
- `api_key` and `typesafe_api_key` are added to `mcpRedactedKeys` (`internal/app/mcp_redact.go`), extending the existing list rather than introducing a second redaction path. The key has no route into MCP arguments today; this is defence in depth against a future tool that takes one, which is exactly what that list's own doc comment asks callers to do.

### What leaves the machine — and what must not

bodger is self-hosted so that a person's financial history stays on their own disk. This feature sends some of it to a third party, and that is the most consequential thing in this ADR.

The adapter receives `[]SuggestionRow` — **never `importing.ImportRecord`.** That type carries `RawPayload`, the entire original CSV line, which for a real bank export routinely includes the account number, running balance, and reference fields that were never needed to guess a category. Passing the domain record would leak all of it on every call, invisibly, and no reviewer would notice because the port would still look reasonable.

| Sent | Not sent |
| --- | --- |
| Description, amount, currency, booked date | `RawPayload` — the raw source row |
| Category names (the kind-filtered option set) | Account names, IDs, or numbers |
| Eligible pending occurrence descriptions | `ExternalID`, running balances, any other parsed column |
| | Any data for an actor other than the one reviewing |
| | Any row other than the one this request is about |

Privacy and accuracy happen to point the same way here, which is worth noticing rather than treating as luck: the vendor's own guidance is to **"send only relevant context"**, because irrelevant state measurably degrades answers. The minimal projection is both the private choice and the accurate one, and the one-row-per-request decision above reinforces it — no row is ever exposed alongside another row's question.

The explicit projection type is what makes this enforceable rather than aspirational: adding a field to `SuggestionRow` is a visible change to a port, reviewed on its own terms.

Because the key defaults to empty, **the privacy posture defaults to closed.** An operator who never sets it has an instance that makes no outbound AI calls and is byte-for-byte as private as it was before M10. This is the reconciliation with ADR-0012: that ADR's requirement was that self-hosting stay zero-friction and the provider never be load-bearing. An optional, off-by-default feature whose absence costs a suggestion and nothing else satisfies both. What it does *not* do is preserve "no key anywhere" as a property of a fully-featured instance, and pretending otherwise would be dishonest — see Consequences.

The user guide documents plainly, in the operator's own words rather than this ADR's, that setting the key sends transaction descriptions and amounts to typesafe.ai.

### Failing gracefully **and informatively**

The suggestion call is not on the path of any write. When it fails, the review screen still renders every staged row and every action on it — ADR-0012's rule that "a network error must never become an error page over data that is sitting in SQLite" applies verbatim.

Graceful is not enough on its own, though: an operator whose key has expired or whose credits have run out must be able to tell that from an outage, or they will debug the wrong thing. So the result distinguishes **why** there are no suggestions, and the surfaces render that distinction:

| Condition | App-layer result | What the operator can tell |
| --- | --- | --- |
| No key configured | **Not an error.** `Configured: false`; no HTTP call is made | "Suggestions aren't set up on this instance" |
| `401` invalid key | `Unavailable`, reason `credential_rejected` | The key is wrong or revoked — an operator action |
| `403` permission denied | `Unavailable`, reason `credential_rejected` | Entitlement or credits — an operator/billing action |
| `429` rate limited | `Unavailable`, reason `throttled` | Transient; try again shortly |
| `529` overloaded, `5xx`, timeout, DNS, refused | `Unavailable`, reason `provider_unreachable` | The vendor, not this instance |
| `422` validation failed | `Internal` | A bodger bug; goes to the log |

Three of those are deliberate and worth defending.

**`401`/`403` are `Unavailable`, not `Unauthenticated`.** ADR-0011's `Unauthenticated` means *the actor* presented no valid credential, and it renders as "You need to sign in before doing that" with HTTP 401. The person reviewing an import is perfectly well authenticated; it is the *operator's instance key* that is wrong or out of credit, which they very likely cannot fix and must certainly not be told to sign in over.

**Out-of-credits cannot be detected precisely, and the ADR does not pretend otherwise.** The published error table lists `401`, `422`, `429`, and `529`; the SDKs additionally map `403` to a permission-denied class. Nothing documents a distinct billing or quota status. So `403` is reported as a credential/entitlement problem — the honest superset — rather than asserting "you are out of credits" on a guess. If the vendor later documents a specific status, this table gains a row; it does not need a redesign.

**`422` is `Internal`, not `InvalidInput`.** A malformed question is bodger constructing a request wrongly. `InvalidInput` would blame the user's CSV for the adapter's bug.

The unconfigured case being a *result field rather than an error* is the load-bearing part: it lets a surface render "AI suggestions aren't set up on this instance" as ordinary state, instead of every surface learning to special-case one error code into a non-error.

**Partial success is a success.** With one request per row, some rows can succeed while others fail. `Suggest` returns the suggestions it obtained plus a count of rows that failed; it returns an error only when *every* row failed, which is the case that actually means "the provider is unavailable". A single row erroring must never discard 199 good answers.

### Retries, timeouts, and the client contract

ADR-0012 bound the Frankfurter adapter to "never retry in a loop", because that service's abuse limiter is undocumented. That reasoning does not transfer wholesale: typesafe.ai documents its rate limits, returns `429` with a server-supplied retry delay, and its own SDKs retry with capped exponential backoff plus jitter, honouring `Retry-After`. Copying the vendor's client behaviour is more correct than inheriting a rule written for a different service's constraints.

So the adapter takes the JS SDK's shape, minus what bodger does not need:

- **At most 2 retries**, only for `429`, `529`, `5xx`, timeouts, and connection failures. Never for `401`, `403`, or `422`, which will not improve by being asked again.
- **Exponential backoff with jitter**, honouring a server-supplied retry delay, capped so a single user action cannot hang.
- **An overall deadline on the whole `Suggest` call**, propagated via `context`, so the bounded worker pool cannot outlive the request that started it.
- **A `User-Agent` identifying bodger and its version**, as the vendor's own clients do.

### Bounding the spend

The application layer caps a single suggestion run at **200 rows**, so a 5,000-row CSV cannot silently become a 5,000-request spend from one click. The cap is a constant, not configuration — an operator who wants a different number is better served by the implementation issue revisiting it with real usage than by a knob nobody knows to turn.

A Choice takes at most **255 options**, and one slot is always reserved for an explicit "none of these" so the model has somewhere honest to put an unmatched row. The `CategoryKind` filter already roughly halves the list, and an actor with more than 254 categories *of a single kind* is well past what this design serves. In that case the app layer **returns no category suggestion rather than truncating**: a truncated option set means the correct answer was never offered, and the model will confidently pick the closest wrong one with no signal that anything was omitted. The vendor's [hierarchical-classification technique](https://docs.typesafe.ai/cookbooks/hierarchical_classification) is the documented way past that ceiling if anyone ever genuinely needs it; it is deliberately not built now, for a limit no realistic personal ledger reaches.

### Testing: mocks in CI, credits never

`make check` and CI must never spend a credit or require a key. Three layers, none of which touch the network by default:

1. **App-layer tests** against a fake `SuggestionProvider`, alongside the existing in-memory repositories — the same role `FxRateProvider`'s fake plays. This is where the 0.5 threshold, the sparse-result indexing, the unconfigured path, the 200-row cap, the 254-option refusal, partial-failure tolerance, and "a provider failure leaves every row reviewable" are proven. **Critically, this is also where the deterministic narrowing is tested** — that a row gets only same-currency, same-amount, in-window occurrence candidates, and only categories of the kind its amount sign implies — because that narrowing is the thing standing between this feature and the model's documented inability to compare dates.
2. **Adapter tests** against a fake `http.RoundTripper`, following `internal/adapters/fxprovider`'s existing `roundTripFunc`/`jsonResponse` helpers exactly — canned JSON for a normal answer, one case per row of the error table, and the retry/backoff policy. This is also where a test pins that `RawPayload` never appears in a serialised request body, and that no request ever carries more than one row, which are the privacy and accuracy rules made executable rather than documented.
3. **Live tests, opt-in and excluded from CI**, behind a `//go:build typesafe_live` tag and skipped unless `BODGER_TYPESAFE_API_KEY` is set, run via their own `make` target. Deliberately minimal — enough to catch the request or response schema drifting out from under the adapter, which no fake can detect, and nothing more. One or two rows, a handful of options, single-digit calls per run.

The build tag is new to this repository; the only existing tags are `//go:build ignore` on the `nocompile` fixtures. It is the right mechanism regardless — a test that costs money and needs a secret must be impossible to run by accident, and a `t.Skip` on an unset environment variable alone would still compile it into the default `go test ./...` run.

### A standalone Go SDK is explicitly out of scope

There is no official Go SDK, and writing one is a genuinely appealing separate project — the JS client is a readable reference for the parts that matter (retry policy, error classification, header contract). **It must not become a dependency of this milestone.** bodger ships a minimal in-repo client covering exactly the one endpoint and three question types it uses, on the `fxprovider` precedent. If a standalone Go SDK later exists, the adapter can adopt it with no change above the port — which is the whole reason the port is expressed in bodger's vocabulary rather than the vendor's.

## Alternatives considered

**One `ports.DecisionProvider` exposing Choice/Score/Noul directly**, with both use cases composing their own questions. This is the option the issue names first, and it is tempting because it is closer to the vendor and would absorb a third use case without a port change. Rejected because it puts *question construction* — the option descriptions, the rubric wording, the instruction text — in `internal/app`. That is vendor-coupled prompt engineering, and ADR-0005 makes the application layer the place that owns orchestration and policy, not provider mechanics. It would also make the port untestable in the way that matters: a fake `DecisionProvider` proves the app can ask a question, not that it asks the *right* one. And it inverts ADR-0007's boundary rule, which exists so the implementer's vocabulary does not become the consumer's.

**Every staged row in one `state`, with one question per row.** The reading the batching cookbook invites, and the design this ADR originally took before the jaggedness page was read closely. Rejected on accuracy: the cookbook's 12.2× saving comes from many questions about *one document*, whereas staged rows are independent records, so a 200-row state makes every question 99.5% distractor content — the documented "accuracy falls as the state grows with content unrelated to the decision" failure. The throughput it appeared to buy is recovered properly by a bounded concurrent worker pool, and the per-row state is also the more private shape.

**Two port methods, one per use case.** Clearer to name, and the shape this ADR first took. Rejected once the request became per-row: both questions concern the same row and the same `state`, and the vendor evaluates questions in one call independently and in parallel at no extra latency. Splitting them would send every row twice and double the wall-clock time to buy nothing but a tidier interface.

**Let the model do the date and amount comparison** — offer every pending occurrence and ask which one matches. Rejected outright on the vendor's own published limitations: jev "reads dates as text, not as ordered quantities" and "is not a calculator". This is the single most seductive mistake available here, because it would *appear* to work — a model asked whether a ₹42,000 debit on 2026-09-03 matches a ₹42,000 occurrence on 2026-09-01 will usually say yes — while failing unpredictably at exactly the boundaries (a near-miss amount, a window edge, a month boundary) where the answer matters and nobody is checking.

**A locally-trained classifier, no external provider.** The option that would have preserved ADR-0012's no-key property completely. Rejected on cold start and on churn: categories are user-defined, renamed, merged and added at will, and a new instance has zero labelled transactions. A classifier would be useless for exactly the user who most needs help — the one who just imported their first year of history — and would need retraining every time someone renames "Groceries" to "Food". Worth revisiting if bodger ever accumulates enough confirmed categorisations per instance to train on, which is precisely the data this feature's confirm step generates. The port is shaped so that adapter could drop in unchanged.

**A general chat model with a JSON-mode prompt.** Rejected for the reason this vendor is interesting at all: a bounded Choice *cannot* return a category that does not exist, while a chat model can and eventually will, leaving the app layer to validate prose against the option set and decide what to do when it does not match. The vendor names this explicitly as an anti-pattern — "generating values outside the schema" — and the calibrated `confidence` the display threshold is built on is not reconstructible from a chat completion.

**Decompose categorisation into atomic yes/no questions**, per the vendor's "probably the most important concept" guidance on question decomposition. Considered seriously and rejected as a misreading: decomposition targets *broad questions hiding several judgements* ("is this spam?" → credentials, sender mismatch, unexpected reward). "Which category does this belong to?" is a single judgement over a bounded set, which is what Choice exists for and what the vendor's own classification cookbooks do. Decomposing it into one Noul per category would also forfeit `confidence`, which Noul does not return.

**Suggest during `StageImport`, so suggestions are ready when review opens.** Better-feeling UX, and rejected on both halves of the dependency posture: it makes a vendor outage able to fail an import, and it spends credits on every row of every batch including those never reviewed. The latency it saves is on an action the user has to arrive at deliberately anyway.

**Persist suggestions on `ImportRecord`.** Would let a review be resumed without re-spending, and would give ADR-0002's audit trail a record of what was proposed. Rejected: it needs a migration and ADR-0008 export/restore handling for data with no financial meaning, it participates in the byte-identical round-trip assertion, and a stored suggestion outlives the category list it was computed against — a stale proposal presented with a confidence number is worse than no proposal.

**Auto-apply above 0.9, as the vendor recommends.** Rejected on all four grounds in the confidence section. The decisive one is that bodger would be making a write against a threshold it has never calibrated, whose calibration is owned by someone else's release process.

**Write a standalone Go SDK first and depend on it.** Rejected as sequencing, not as an idea — see the scope note above. A milestone should not block on a library that does not exist, and the port boundary means adopting one later costs nothing above the adapter.

## Consequences

**Good.** The feature is off until an operator opts in, so an instance that ignores M10 is unchanged in behaviour, dependencies, and privacy. Nothing it produces can reach the ledger without a person choosing it, structurally rather than by convention — the suggestion method writes nothing at all. Every judgement the model makes is one it is documented to be good at, because code answers the dates and the arithmetic first. Every suggestion is attributed to typesafe.ai by name at the point someone acts on it, inheriting the transparency rule FX already established rather than inventing a softer one for AI. The port speaks bodger's vocabulary, so the vendor is replaceable, including by a local model or a future Go SDK. A failure degrades to "no suggestion" with enough detail for an operator to tell a wrong key from an outage. And re-scoping the second use case surfaced a real, pre-existing bug (#301): an imported transaction that duplicates a pending occurrence is invisible to duplicate detection today.

**Bad, and worth stating plainly:**

- **ADR-0012's "no key anywhere" property is gone for anyone who wants this feature.** It survives only as a default. A self-hoster who wants AI suggestions now needs an account, a key, and a billing relationship with a commercial vendor — precisely the setup friction ADR-0012 rejected exchangerate.host and Open Exchange Rates over. The difference is that FX was load-bearing for a core feature and this is not, but that is a difference of degree, and anyone reading ADR-0012 as "bodger does not do vendor keys" is now reading it too broadly.
- **Financial data leaves the machine, which for a self-hosted personal-finance app is a real change in kind.** Defaulting to off and sending a minimal per-row projection reduces the blast radius; it does not eliminate it. Transaction descriptions are not anonymous — merchant names, people's names, and medical or legal providers all routinely appear in them. An operator enabling this is making a privacy decision on behalf of every user on the instance, and under ADR-0006's multi-user path they may not be the same person.
- **One request per row costs more than the batched alternative.** Accuracy was chosen over the cookbook's headline saving, and the honest framing is that this design deliberately spends more money for better answers. At roughly $0.042 per million input tokens a 200-row review is still fractions of a cent, but the scaling is now linear in rows with no state-sharing discount.
- **The spend is real and unmetered from bodger's side.** There is no in-app budget, quota display, or credit-remaining check, because the API exposes none the adapter could read cheaply. The 200-row cap bounds a single click, not a day; an operator can spend more than they expected by reviewing many large imports, and will find out from the vendor's dashboard rather than from bodger.
- **"Out of credits" is a guess dressed as `credential_rejected`.** No documented status distinguishes exhausted credits from a revoked key, so the operator-facing message names both possibilities. That is honest but unsatisfying, and it will read as vague to whoever hits it at 2am.
- **Rate limits are documented as "adjusting dynamically" and may change without notice**, so published limits are not a contract the adapter can rely on — which is why the retry policy is small, bounded, and defers to the server's own retry delay rather than being clever.
- **The model is pinned, so it will go stale.** `jev-1.13.0` gets no improvements until someone bumps it deliberately, and each bump requires re-reading that version's jaggedness page, because this design routes around limitations that are published per version and could change in either direction.
- **The design is built on a published limitations page, which is a dependency on the vendor's candour.** The date and arithmetic workarounds exist because jev-1.13's page says those are weak. An undocumented weakness gets no workaround, and bodger would not know.
- **A build-tagged live test is a test that will rot.** Excluded from CI means nothing runs it, which means schema drift is caught whenever someone remembers, not when it happens. The alternative — running it in CI — trades that for a secret in the repository's CI configuration and a bill attached to every push, which is worse.
- **The 254-option ceiling is a real cliff, not a soft limit**, even after the kind filter halves the list. An actor past it gets no category suggestions at all. That is the right failure — truncation would be worse — but it needs a clear surface message rather than an empty result, or it will read as the feature being broken.
- **The not-configured state is real UI work on four surfaces, for a feature the operator may never enable.** Every surface has to render a third state — suggestions present, suggestions failed, suggestions not set up — and ADR-0012's Frankfurter experience is the warning: getting an ordinary "couldn't reach it" state wrong is what turns a free service's routine flakiness into a broken-looking app. Here the same mistake turns a deliberate privacy choice into a nag.
- **Attribution ends at confirmation, which some readers will call a gap.** There is deliberately no way to ask, later, "which of my categories did a model originally propose?" The reasoning is in the ADR — the human's decision is what the ledger records — but it does mean an operator who suspects a run of bad suggestions has no query that finds them, and would have to reason from the import batch instead.
- **`SuggestionRow` is a projection someone will eventually be tempted to widen.** The privacy rule holds only as long as adding a field to it is treated as the boundary change it is. The adapter tests that assert `RawPayload` never reaches the wire, and that no request carries a second row, are the only things that will actually stop it.
