package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

// CategoryOption is one category the provider may choose from, already
// filtered to the kind the row's amount sign implies (ADR-0015: an
// expense row is only ever offered expense categories, and vice versa).
// That filter happens in the application layer, in code, before the
// provider ever sees the row — it is a deterministic narrowing, not
// something asked of the model.
type CategoryOption struct {
	ID   string
	Name string
}

// OccurrenceOption is one pending ScheduledOccurrence the application
// layer has ALREADY established is date- and amount-eligible for its row
// — same currency, equal absolute amount, booked date within the existing
// duplicate-detection window (ADR-0015 "Code answers the arithmetic").
// The provider is never asked to compare a date or an amount here: jev
// (typesafe.ai's decision model, pinned at jev-1.13) is documented as
// unable to reliably do either, so both comparisons are resolved
// deterministically before a request is ever built. All that's left for
// the provider is the question it's actually good at — which of these
// already-eligible occurrences does this row's description refer to.
type OccurrenceOption struct {
	OccurrenceID string
	Description  string
}

// SuggestionRow is the minimal projection of one staged import row that
// leaves the application layer for a suggestion request (ADR-0015 "What
// leaves the machine"). It is deliberately never an
// importing.ImportRecord: that type's RawPayload carries the entire
// original source line, which for a real bank export routinely includes
// account numbers, running balances, and reference fields that were never
// needed to guess a category or an occurrence match. Passing the domain
// record here would leak all of that on every call, invisibly — this
// explicit, narrower type is what makes the privacy boundary a visible,
// reviewable change to a port rather than an aspiration.
type SuggestionRow struct {
	RecordID    string
	Description string
	Amount      money.Money
	Date        domain.Date

	// Categories and OccurrenceCandidates are this row's own bounded
	// option sets, narrowed deterministically by the caller. An empty
	// OccurrenceCandidates means the occurrence question is not asked
	// for this row at all — the caller found no date/amount-eligible
	// pending occurrence, and the provider is never invited to invent a
	// match out of an empty or irrelevant set.
	Categories           []CategoryOption
	OccurrenceCandidates []OccurrenceOption
}

// RowSuggestion carries both answers for one row. An empty CategoryID or
// OccurrenceID means "no answer" — not an error, and not a match.
//
// CategoryConfidence and OccurrenceConfidence are the provider's own,
// entirely unfiltered: this port never drops a low-confidence answer, and
// never decides what's worth showing. ADR-0015 puts that threshold
// (bodger currently discards anything below 0.5) in the application
// layer, above this port, as ordinary policy — the same reason the port
// carries no "display" concept at all. A confidence number's meaning is
// also calibrated per model version, which is one more reason it isn't
// baked into adapter or port behaviour.
type RowSuggestion struct {
	RecordID             string
	CategoryID           string
	CategoryConfidence   float64
	OccurrenceID         string
	OccurrenceConfidence float64
}

// SuggestOutcome carries a Suggest call's own failure accounting —
// alongside the returned suggestion slice and the existing error return,
// never instead of either. It exists because ADR-0015's "partial success
// is a success" contract reserves Suggest's error return for "every row
// failed": on a partial failure (some rows answered, some didn't) that
// error stays nil, so without a separate signal the reason a row got no
// answer would be observed by the adapter and then simply lost. Repurposing
// the error return to also mean "something failed" would conflate two
// different signals callers already rely on being distinct — see
// Suggest's own doc comment — so this is a third return value instead.
//
// FailedRows is how many rows in this call failed — 0 when nothing did,
// in which case FailureReason is always "".
//
// FailureReason is one dominant reason for the whole call, not a per-row
// account: every row in a single Suggest call shares the same instance
// credential and the same provider, so a real failure (a rejected
// credential, a rate limit, an outage) overwhelmingly produces the same
// reason on every failed row in that call — per-row tracking would be more
// precision than any surface actually needs. When failures genuinely
// differ within one call (rare — e.g. a mid-run rate limit after some rows
// already succeeded on a still-valid connection), an adapter reporting its
// last-observed reason here is an acceptable, honest simplification, not a
// hidden one. Its value is one of an adapter's own stable reason strings
// (e.g. internal/adapters/typesafe/errors.go's "credential_rejected",
// "throttled", "provider_unreachable") — this port has no closed set of
// its own, the same way it names no vendor vocabulary elsewhere.
type SuggestOutcome struct {
	FailedRows    int
	FailureReason string
}

// SuggestionProvider answers two advisory questions per staged import
// row — which category and which pending occurrence it most likely
// corresponds to — without ever writing anything (ADR-0015, M10 ·
// Valinor). It is the one port through which bodger's application layer
// talks to typesafe.ai, expressed entirely in bodger's own vocabulary:
// nothing here names a Choice, a Score, a Noul, a state, or a jev model
// version, which is what keeps the vendor swappable, including by a
// locally-hosted classifier that satisfies this same interface with no
// change above it.
//
// Unlike FxRateProvider, this interface has no Name() method. FxRateProvider
// carries one because a fetched rate's source is persisted verbatim as
// fx_rate's `source` column — a provenance fact something downstream has
// to store. Nothing a SuggestionProvider returns is ever persisted (a
// suggestion is discarded the moment the reviewer accepts or dismisses
// it, per ADR-0015's call-site design); with no row to write, there is no
// provenance column to fill, so there is nothing for a Name() to serve.
type SuggestionProvider interface {
	// Suggest answers every row's questions, fanning out concurrently
	// under the hood — the port is batch-shaped (a slice in, a slice
	// out), while how that becomes requests (one per row, concurrently,
	// bounded) is adapter mechanism the application layer has no opinion
	// about, mirroring how FxRateProvider.FetchRange leaves "how many HTTP
	// calls is this range" to the adapter.
	//
	// The returned slice is sparse and unordered: a row with nothing
	// worth answering (no category needed, no eligible occurrence, or a
	// per-row failure) simply has no entry. Callers must index the result
	// by RecordID and must never assume positional alignment with rows —
	// the slice is not "rows, in order, with an occasional gap".
	//
	// It returns the suggestions it did obtain, plus an error only when
	// every row failed — ADR-0015's "partial success is a success": one
	// row erroring must never discard every other row's good answer. The
	// returned SuggestOutcome carries that same failure alongside a
	// partial failure too, which the error return alone cannot (see
	// SuggestOutcome's own doc comment) — FailedRows is 0 and
	// FailureReason is "" when nothing failed, on both a total success and
	// an empty rows slice.
	Suggest(ctx context.Context, rows []SuggestionRow) ([]RowSuggestion, SuggestOutcome, error)
}
