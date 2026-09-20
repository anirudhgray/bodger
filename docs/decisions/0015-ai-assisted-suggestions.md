# ADR-0015 — AI-assisted suggestions: typesafe.ai, advisory only

**Status:** Accepted · 2026-09-20

## Context

M10 · Valinor adds the first feature in bodger where a machine proposes a financial decision rather than computing one. [typesafe.ai](https://docs.typesafe.ai/introduction) is the platform it is built on: a decision model (`jev`) that answers **typed questions** — **Choice** (pick one option from a bounded set), **Score** (rate against an ordered rubric), **Noul** (a yes/no probability) — and returns structured values with a calibrated `confidence`, rather than free text something downstream has to parse.

That shape is the reason this is worth doing at all. Every other way of adding "smart categorisation" to a personal-finance app either ships a trained classifier (which needs labelled data bodger does not have, against a category list each user invents and renames at will) or calls a general chat model and parses prose back into an enum. A Choice question over *the actor's own current category list* needs no training, no per-user model, and no parsing — and it cannot return a category that does not exist, because the option set is the request.

Two constraints from the existing system shape everything below, and they pull in opposite directions.

1. **[ADR-0002](0002-authoritative-ledger-and-corrections.md): the ledger is authoritative.** A category is part of a transaction's meaning. Nothing may write one because a model was confident.
2. **[ADR-0012](0012-fx-rate-provider.md): the last external provider was chosen specifically to avoid a key.** Frankfurter won partly *because* it needs "no API key, no signup, no per-instance secret", so self-hosting stays "run the binary". typesafe.ai reintroduces exactly that dependency — an account, a key, and metered credits. This ADR has to reconcile that rather than quietly walk it back.

Four facts about the vendor, verified against its documentation, constrain the design harder than a general "should we add AI" discussion would:

- **There is no Go SDK.** Python and JavaScript only. [ADR-0001](0001-technology-stack.md) ships a single Go binary, so the adapter is a hand-written HTTP client against `POST https://api.typesafe.ai/v1/systemone` — roughly the same size as `internal/adapters/fxprovider`, and no new runtime dependency.
- **64k tokens per request, of which `state` plus the longest single question may be at most 32k.** A batch of staged rows is not unbounded input; something has to chunk.
- **Questions in one call are independent and the `state` is charged once.** The vendor's own measurement of batching 13 questions over one document is *12.2× cheaper and 10.0× faster* than 13 single-question calls. Batching is therefore the default shape, not an optimisation.
- **`confidence` is returned on Choice and Score answers only — never on Noul**, which returns a bare probability. Anything that wants a calibrated certainty must ask a Choice.

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

`architecture.md` §8's M10 description is corrected in the same PR as this ADR.

### One port, `ports.SuggestionProvider`, in bodger's vocabulary

Defined in `internal/ports`, consumer-owned, per [ADR-0007](0007-persistence-and-migrations.md).

```go
// CategoryOption / OccurrenceOption are the bounded candidate sets.
type CategoryOption struct {
	ID   string
	Name string
	Kind string // disambiguates a same-named expense and income category
}

type OccurrenceOption struct {
	OccurrenceID string
	Description  string
	Amount       money.Money
	Date         domain.Date
}

// SuggestionRow is the minimal projection of a staged row — see
// "What leaves the machine" below. Never an importing.ImportRecord.
type SuggestionRow struct {
	RecordID    string
	Description string
	Amount      money.Money
	Date        domain.Date
}

type CategorySuggestion struct {
	RecordID   string
	CategoryID string
	Confidence float64 // 0..1, the provider's own, unfiltered
}

type OccurrenceSuggestion struct {
	RecordID     string
	OccurrenceID string
	Confidence   float64
}

type SuggestionProvider interface {
	SuggestCategories(ctx context.Context, rows []SuggestionRow, options []CategoryOption) ([]CategorySuggestion, error)
	SuggestOccurrenceMatches(ctx context.Context, rows []SuggestionRow, options []OccurrenceOption) ([]OccurrenceSuggestion, error)
}
```

Three properties of that signature are load-bearing:

**It is batch-shaped, like `FetchRange` and unlike `FetchRate`.** A slice of rows in, a slice of suggestions out, one provider round trip. A per-row method would multiply the request count by the batch size and re-send the option set every time — the exact thing the vendor's own batching measurement says not to do.

**The returned slice is sparse.** A row the provider had no answer for simply has no entry. Callers index by `RecordID`; they never assume positional alignment with the input.

**`Choice`, `Score`, `Noul`, `state`, `criteria`, and `jev` appear nowhere in it.** The port speaks about categories and occurrences; the mapping onto typed questions is entirely the adapter's. This follows `FxRateProvider` exactly — that port says `FetchRate(base, quote, date)`, not `GET /v2/rate` — and it is what keeps the vendor swappable. A locally-hosted classifier could satisfy this interface without a line changing above it.

**There is deliberately no `Name()` method**, unlike `FxRateProvider`. That one exists because `fx_rate.source` is a persisted provenance column. Nothing here is persisted, so there is no provenance to carry.

### Call site: one new read-only method, and **not** `StageImport`

`Service.SuggestForImportBatch(ctx, SuggestForImportBatchQuery) (SuggestForImportBatchResult, error)` — a new method in `internal/app`, read-only, writing nothing.

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

Three reasons, in increasing order of how badly they would bite:

1. ADR-0002 makes the ledger authoritative; a silently-applied category is the ledger holding something nobody asserted.
2. An import is a *batch*. A threshold that auto-applies at 0.9 across 200 rows produces 200 plausible, confidently-wrong-in-the-tail categorisations that look reviewed and are not — the same class of failure as ADR-0012's "store the requested date instead of the returned one": no error, no flag, and every report built on it quietly wrong.
3. **Confidence is calibrated per model version**, and the vendor says so explicitly when recommending that callers pin a version if they have calibrated thresholds against one. A threshold that only *filters display* degrades gracefully when the model moves. A threshold that *writes* silently changes meaning under a model upgrade nobody in this repository triggered.

The 0.5 cutoff itself is app-layer policy, not adapter behaviour: the adapter returns whatever confidence it received, and `internal/app` filters. [ADR-0005](0005-shared-application-layer.md) puts every default and policy decision in the application layer, and it makes the threshold testable against the fake without a network.

### Pin the model to `jev-1.13.0`. Never `jev-latest`.

Following directly from the point above. `jev-latest` is an alias that moves without a release on bodger's side, and it moves the calibration of a number this system makes decisions about. The pinned version is a constant in the adapter, bumped deliberately in its own PR.

### Credentials: instance-level, optional, and absent by default

`BODGER_TYPESAFE_API_KEY`, resolved in `internal/platform/config` — the only package permitted to read the environment (ADR-0005) — following `EnvFxProviderBaseURL`'s existing pattern.

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
| Category names and kinds (the option set) | Account names, IDs, or numbers |
| Pending occurrence descriptions, amounts, dates | `ExternalID`, running balances, any other parsed column |
| | Any data for an actor other than the one reviewing |

The explicit projection type is what makes this enforceable rather than aspirational: adding a field to `SuggestionRow` is a visible change to a port, reviewed on its own terms.

Because the key defaults to empty, **the privacy posture defaults to closed.** An operator who never sets it has an instance that makes no outbound AI calls and is byte-for-byte as private as it was before M10. This is the reconciliation with ADR-0012: that ADR's requirement was that self-hosting stay zero-friction and the provider never be load-bearing. An optional, off-by-default feature whose absence costs a suggestion and nothing else satisfies both. What it does *not* do is preserve "no key anywhere" as a property of a fully-featured instance, and pretending otherwise would be dishonest — see Consequences.

The user guide documents plainly, in the operator's own words rather than this ADR's, that setting the key sends transaction descriptions and amounts to typesafe.ai.

### Failure and latency: never blocks, and `Unavailable` almost always

The suggestion call is not on the path of any write. When it fails, the review screen still renders every staged row and every action on it — ADR-0012's rule that "a network error must never become an error page over data that is sitting in SQLite" applies verbatim.

| Condition | App-layer result |
| --- | --- |
| No key configured | **Not an error.** Result carries `Configured: false` and no suggestions; no HTTP call is made |
| `401` invalid key | `Unavailable` |
| Insufficient credits, or any other unlisted 4xx | `Unavailable` |
| `429` rate limited, `529` overloaded | `Unavailable`. Not retried in a loop (ADR-0012's rule) |
| `5xx`, timeout, DNS, connection refused | `Unavailable` |
| `422` validation failed | `Internal` |

Two of those are deliberate and worth defending.

**`401` is `Unavailable`, not `Unauthenticated`.** ADR-0011's `Unauthenticated` means *the actor* presented no valid credential, and it renders as "You need to sign in before doing that" with HTTP 401. The person reviewing an import is perfectly well authenticated; it is the *operator's instance key* that is wrong, which they very likely cannot fix and must certainly not be told to sign in over. `Unavailable`'s "This isn't available right now" is the honest message. The specific cause goes in the internal chain for the operator's log, per ADR-0011's safe/internal split.

**`422` is `Internal`, not `InvalidInput`.** A malformed question is bodger constructing a request wrongly. `InvalidInput` would blame the user's CSV for the adapter's bug.

The unconfigured case being a *result field rather than an error* is the load-bearing part: it lets a surface render "AI suggestions aren't set up on this instance" as ordinary state, instead of every surface learning to special-case one error code into a non-error.

A client timeout is set explicitly, and the request is never retried in a loop.

### Bounding the spend

The adapter chunks to respect the 32k `state`-plus-longest-question limit; that is a vendor mechanism and belongs nowhere else. Above it, the application layer caps a single suggestion run at **200 rows**, so a 5,000-row CSV cannot silently become a twenty-five-request spend from one click. The cap is a constant, not configuration — an operator who wants a different number is better served by the implementation issue revisiting it with real usage than by a knob nobody knows to turn.

A Choice question takes at most **255 options**. One slot is always reserved for an explicit "none of these" option, so the effective ceiling is 254 categories or 254 pending occurrences. Over that, the app layer **returns no suggestions rather than truncating the list**: a truncated option set means the correct answer was never offered, and the model will confidently pick the closest wrong one with no signal that anything was omitted.

### Testing: mocks in CI, credits never

`make check` and CI must never spend a credit or require a key. Three layers, none of which touch the network by default:

1. **App-layer tests** against a fake `SuggestionProvider`, alongside the existing in-memory repositories — the same role `FxRateProvider`'s fake plays. This is where the 0.5 threshold, the sparse-result indexing, the unconfigured path, the 200-row cap, the 254-option refusal, and "a provider failure leaves every row reviewable" are proven.
2. **Adapter tests** against a fake `http.RoundTripper`, following `internal/adapters/fxprovider`'s existing `roundTripFunc`/`jsonResponse` helpers exactly — canned JSON for a normal answer, and one case per row of the error table above. This is also where a test pins that `RawPayload` never appears in a serialised request body, which is the privacy rule made executable rather than documented.
3. **Live tests, opt-in and excluded from CI**, behind a `//go:build typesafe_live` tag and skipped unless `BODGER_TYPESAFE_API_KEY` is set, run via their own `make` target. Deliberately minimal — enough to catch the request or response schema drifting out from under the adapter, which no fake can detect, and nothing more. One or two rows, a handful of options, single-digit calls per run.

The build tag is new to this repository; the only existing tags are `//go:build ignore` on the `nocompile` fixtures. It is the right mechanism regardless — a test that costs money and needs a secret must be impossible to run by accident, and a `t.Skip` on an unset environment variable alone would still compile it into the default `go test ./...` run.

## Alternatives considered

**One `ports.DecisionProvider` exposing Choice/Score/Noul directly**, with both use cases composing their own questions. This is the option the issue names first, and it is tempting because it is closer to the vendor and would absorb a third use case without a port change. Rejected because it puts *question construction* — the option descriptions, the rubric wording, the instruction text — in `internal/app`. That is vendor-coupled prompt engineering, and ADR-0005 makes the application layer the place that owns orchestration and policy, not provider mechanics. It would also make the port untestable in the way that matters: a fake `DecisionProvider` proves the app can ask a question, not that it asks the *right* one. And it inverts ADR-0007's boundary rule, which exists so the implementer's vocabulary does not become the consumer's.

**One port method returning both suggestion kinds in a single call.** Genuinely attractive on the vendor's own economics: both use cases run over the same rows at the same moment, `state` is charged once, and a mixed `questions` map is explicitly supported. Rejected after costing it. At $0.042 per million input tokens, a 200-row batch is on the order of 20k tokens, so sending the rows twice costs roughly **$0.0008 extra per import review**. That is not a number worth deforming an interface over. Two methods named for what they do are clearer, independently testable, and independently skippable — an actor with no pending occurrences never makes the second call at all. If the economics ever change, merging them is an adapter-and-port change with no call-site redesign.

**A locally-trained classifier, no external provider.** The option that would have preserved ADR-0012's no-key property completely. Rejected on cold start and on churn: categories are user-defined, renamed, merged and added at will, and a new instance has zero labelled transactions. A classifier would be useless for exactly the user who most needs help — the one who just imported their first year of history — and would need retraining every time someone renames "Groceries" to "Food". Worth revisiting if bodger ever accumulates enough confirmed categorisations per instance to train on, which is precisely the data this feature's confirm step generates. The port is shaped so that adapter could drop in unchanged.

**A general chat model with a JSON-mode prompt.** Rejected for the reason this vendor is interesting at all: a bounded Choice *cannot* return a category that does not exist, while a chat model can and eventually will, leaving the app layer to validate prose against the option set and decide what to do when it does not match. The calibrated `confidence` is also not reconstructible from a chat completion, and it is what the display threshold is built on.

**Suggest during `StageImport`, so suggestions are ready when review opens.** Better-feeling UX, and rejected on both halves of the dependency posture: it makes a vendor outage able to fail an import, and it spends credits on every row of every batch including those never reviewed. The latency it saves is on an action the user has to arrive at deliberately anyway.

**Persist suggestions on `ImportRecord`.** Would let a review be resumed without re-spending, and would give ADR-0002's audit trail a record of what was proposed. Rejected: it needs a migration and ADR-0008 export/restore handling for data with no financial meaning, it participates in the byte-identical round-trip assertion, and a stored suggestion outlives the category list it was computed against — a stale proposal presented with a confidence number is worse than no proposal.

**Auto-apply above 0.9, as the vendor recommends.** Rejected on all three grounds in the confidence section, but the decisive one is the model-version argument: bodger would be making an irreversible-by-default write against a threshold whose calibration is owned by someone else's release process.

## Consequences

**Good.** The feature is off until an operator opts in, so an instance that ignores M10 is unchanged in behaviour, dependencies, and privacy. Nothing it produces can reach the ledger without a person choosing it, structurally rather than by convention — the suggestion method writes nothing at all. The port speaks bodger's vocabulary, so the vendor is replaceable, including by a local model later. Suggestions cost one round trip per batch rather than one per row. And re-scoping the second use case surfaced a real, pre-existing bug: an imported transaction that duplicates a pending occurrence is invisible to duplicate detection today.

**Bad, and worth stating plainly:**

- **ADR-0012's "no key anywhere" property is gone for anyone who wants this feature.** It survives only as a default. A self-hoster who wants AI suggestions now needs an account, a key, and a billing relationship with a commercial vendor — precisely the setup friction ADR-0012 rejected exchangerate.host and Open Exchange Rates over. The difference is that FX was load-bearing for a core feature and this is not, but that is a difference of degree, and anyone reading ADR-0012 as "bodger does not do vendor keys" is now reading it too broadly.
- **Financial data leaves the machine, which for a self-hosted personal-finance app is a real change in kind.** Defaulting to off and sending a minimal projection reduces the blast radius; it does not eliminate it. Transaction descriptions are not anonymous — merchant names, people's names, and medical or legal providers all routinely appear in them. An operator enabling this is making a privacy decision on behalf of every user on the instance, and under ADR-0006's multi-user path they may not be the same person.
- **The spend is real and unmetered from bodger's side.** There is no in-app budget, quota display, or credit-remaining check, because the API exposes none the adapter could read cheaply. The 200-row cap bounds a single click, not a day; an operator can spend more than they expected by reviewing many large imports, and will find out from the vendor's dashboard rather than from bodger.
- **Rate limits are documented as "adjusting dynamically" and may change without notice.** The adapter cannot treat published limits as a contract, which is why `429` is a routine `Unavailable` with no retry loop rather than something clever.
- **The model is pinned, so it will go stale.** `jev-1.13.0` gets no improvements until someone bumps it deliberately, and the vendor publishes a known-jaggedness page per version. That is the correct trade against silently-moving calibration, but it is maintenance nobody is scheduled to do.
- **A build-tagged live test is a test that will rot.** Excluded from CI means nothing runs it, which means schema drift is caught whenever someone remembers, not when it happens. The alternative — running it in CI — trades that for a secret in the repository's CI configuration and a bill attached to every push, which is worse.
- **Two calls where one would do**, by choice, costing about $0.0008 per review. Defensible now and worth re-measuring if a third use case ever joins the same call site.
- **The 254-option ceiling is a real cliff, not a soft limit.** An actor with more than 254 categories gets no category suggestions at all, silently as far as the model is concerned. That is the right failure — truncation would be worse — but it needs a clear surface message rather than an empty result, or it will read as the feature being broken.
- **`SuggestionRow` is a projection someone will eventually be tempted to widen.** The privacy rule holds only as long as adding a field to it is treated as the boundary change it is. The adapter test that asserts `RawPayload` never reaches the wire is the only thing that will actually stop it.
