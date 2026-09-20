package app

import (
	"context"
	"sort"
	"strings"

	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// maxSuggestionRows caps a single SuggestForImportBatch run, per ADR-0015
// "Bounding the spend": a package constant, not configuration, so a
// 5,000-row CSV can't silently become a 5,000-request spend from one
// click. Rows beyond this are simply not sent — see SuggestForImportBatch.
const maxSuggestionRows = 200

// maxSuggestionOptions is the number of options a single typesafe.ai
// Choice question may actually offer: the vendor's own ceiling of 255,
// minus the one slot ADR-0015 reserves on every Choice for an explicit
// "none of these" answer. An actor with more eligible categories than
// this (of one CategoryKind) gets no category suggestion for the affected
// rows at all — ADR-0015 is explicit that truncating the option set would
// be worse than not asking, since the correct answer might be the one
// left out and nothing would signal that.
const maxSuggestionOptions = 254

// suggestionConfidenceThreshold is ADR-0015's app-layer display gate: the
// vendor's own band boundary, applied only to decide what's worth
// surfacing — never to decide what's written (nothing this method returns
// is ever applied to anything).
const suggestionConfidenceThreshold = 0.5

// SuggestForImportBatchQuery identifies the staged import batch to
// generate advisory category/occurrence suggestions for.
type SuggestForImportBatchQuery struct {
	ActorID        string
	ImportBatchRef string
}

// SuggestedCategory is one row's surviving category suggestion — already
// past the confidence threshold. CategoryConfidence is the provider's own,
// raw value (never re-derived or rounded here), carried through so a
// surface can disclose it later (issue #306, not this one).
type SuggestedCategory struct {
	CategoryID string
	Confidence float64
}

// SuggestedOccurrence is one row's surviving occurrence-match suggestion —
// already past the confidence threshold. Confidence is the provider's own,
// raw value, for the same reason as SuggestedCategory's.
type SuggestedOccurrence struct {
	OccurrenceID string
	Confidence   float64
}

// ImportRowSuggestion carries whatever survived ADR-0015's confidence
// threshold for one staged row. Category and/or Occurrence are nil when
// there's nothing worth showing for that half of the row — discarded by
// the threshold, no eligible candidate existed, or (Category only) the
// actor had too many categories of that kind to ask about at all (see
// SuggestForImportBatchResult.TooManyCategoryOptions). Neither field is
// ever pre-selected or applied anywhere by this method or any caller of
// it — see SuggestForImportBatch's own doc comment.
type ImportRowSuggestion struct {
	RecordID   string
	Category   *SuggestedCategory
	Occurrence *SuggestedOccurrence
}

// SuggestForImportBatchResult is SuggestForImportBatch's read-only output.
// Nothing on it is ever persisted, by this method or anything that calls
// it (ADR-0015: "Nothing here ever writes to the ledger").
type SuggestForImportBatchResult struct {
	// Configured is false when this bodger instance has no typesafe.ai
	// key set (s.Suggestions == nil) — a distinct, non-error result, per
	// ADR-0015 "When there is no key, say so". No HTTP call is made and
	// every other field below is left zero.
	Configured bool

	// Suggestions is every row's surviving suggestion (RowsFailed and
	// rows with nothing worth showing are simply absent), ranked by
	// confidence — the best suggestion in the batch first. Each entry
	// carries its own RecordID; callers index by that, never by
	// position, mirroring ports.SuggestionProvider's own "sparse and
	// unordered" contract for the raw provider results this is built
	// from.
	Suggestions []ImportRowSuggestion

	// TooManyCategoryOptions lists the RecordIDs of rows for which no
	// category question was asked at all because the actor has more than
	// maxSuggestionOptions eligible categories of that row's kind —
	// ADR-0015's "return no category suggestion rather than truncating".
	// This is distinct from a RecordID simply missing from Suggestions,
	// which could instead mean "asked, but nothing confident came back"
	// or "the provider failed for this row" — a surface needs to tell
	// those apart to say something specific about the too-many-options
	// case (issue #306, not this one).
	TooManyCategoryOptions []string

	// RowsSuggested is how many staged rows actually had a suggestion
	// request sent for them — after the excluded/already-categorised
	// filter, the maxSuggestionRows cap, and dropping any row that ended
	// up with nothing at all worth asking (no category question because
	// of too-many-options or an empty category list, and no eligible
	// occurrence candidate either).
	RowsSuggested int

	// RowsFailed is the subset of RowsSuggested the provider never
	// answered — one failed HTTP request never discards another row's
	// good answer (ADR-0015 "Partial success is a success"), but a
	// surface still needs to know some rows came back empty because of a
	// failure rather than because nothing matched.
	RowsFailed int
}

// SuggestForImportBatch implements issue #305: ADR-0015's one new
// application-layer method, and the only caller of ports.SuggestionProvider
// anywhere in bodger. It proposes a category and/or a pending-occurrence
// match for a staged import batch's still-unresolved rows — advisory only.
// This method writes nothing, ever: not to an ImportRecord, not to a
// category assignment, not to a ScheduledOccurrence. There is no
// confidence value at which it pre-selects or assigns anything; the only
// way any of this reaches the ledger is a human separately calling
// ResolveImportRecord/ResolveImportRecordOccurrenceMatch/
// CreateCategory-style assignment exactly as they would have without this
// method existing.
//
// Code answers the arithmetic; the model answers only the semantics
// (ADR-0015). Every row's category options are filtered by CategoryKind
// from the row's own amount sign before the provider ever sees them, and
// every row's occurrence candidates are narrowed to same-currency,
// same-amount, in-window pending occurrences by eligibleOccurrences
// (import_duplicate.go) before the provider ever sees them — the model is
// only ever asked which already-eligible option a row's description
// refers to, never asked to compare a date or an amount itself, because
// jev-1.13 is documented to be unreliable at both.
//
// Row selection: a staged record is considered only if it is not
// ImportRecordStatusExcluded (a tier-1 exact duplicate will never become a
// transaction) and has no ResolvedCategoryID (buildImportRecord already
// resolved a CategoryHint deterministically; a probabilistic suggestion
// must never override a deterministic match). Eligible rows beyond
// maxSuggestionRows are simply not sent.
//
// Unconfigured: if s.Suggestions is nil (no BODGER_TYPESAFE_API_KEY on
// this instance — see Service.Suggestions' own doc comment, service.go),
// this returns {Configured: false} immediately, before touching any
// repository beyond validating the command itself. Not an error, and the
// provider is never constructed let alone called.
//
// Provider failure: this method is read-only and off the path of every
// write, so a provider error never makes the review unusable — every
// staged row is still exactly as listable via ListImportRecords as it was
// before this call. Per ports.SuggestionProvider's own contract, Suggest
// returns an error only when every attempted row failed, and even then
// this method turns that into RowsFailed rather than propagating it: a
// failed suggestion run is worth reporting, not worth blocking review
// over.
func (s *Service) SuggestForImportBatch(ctx context.Context, q SuggestForImportBatchQuery) (SuggestForImportBatchResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return SuggestForImportBatchResult{}, err
	}
	batchID := strings.TrimSpace(q.ImportBatchRef)
	if batchID == "" {
		return SuggestForImportBatchResult{}, errs.New(errs.InvalidInput).Explain("An import batch ID is required.").Field("import_batch_ref")
	}

	// Unconfigured is checked before any repository read beyond the
	// command validation above: an instance with no key makes no outbound
	// call and does no extra work finding out it has nothing to say.
	if s.Suggestions == nil {
		return SuggestForImportBatchResult{}, nil
	}

	if _, err := s.ImportBatches.Get(ctx, q.ActorID, batchID); err != nil {
		return SuggestForImportBatchResult{}, err
	}
	records, err := s.ImportRecords.ListByImportBatch(ctx, q.ActorID, batchID)
	if err != nil {
		return SuggestForImportBatchResult{}, err
	}

	eligible := make([]importing.ImportRecord, 0, len(records))
	for _, r := range records {
		if r.Status() == importing.ImportRecordStatusExcluded {
			continue
		}
		if _, hasCategory := r.ResolvedCategoryID(); hasCategory {
			continue
		}
		eligible = append(eligible, r)
	}
	if len(eligible) > maxSuggestionRows {
		eligible = eligible[:maxSuggestionRows]
	}
	if len(eligible) == 0 {
		return SuggestForImportBatchResult{Configured: true}, nil
	}

	categoryOptionsByKind, tooManyByKind, err := s.suggestionCategoryOptions(ctx, q.ActorID)
	if err != nil {
		return SuggestForImportBatchResult{}, err
	}

	rows := make([]ports.SuggestionRow, 0, len(eligible))
	var tooManyCategoryOptions []string
	for _, r := range eligible {
		kind := ledger.CategoryKindExpense
		if !r.Amount().IsNegative() {
			kind = ledger.CategoryKindIncome
		}

		var catOptions []ports.CategoryOption
		if tooManyByKind[kind] {
			tooManyCategoryOptions = append(tooManyCategoryOptions, r.ID())
		} else {
			catOptions = categoryOptionsByKind[kind]
		}

		var occOptions []ports.OccurrenceOption
		if accountID, ok := r.ResolvedAccountID(); ok {
			candidates, err := s.eligibleOccurrences(ctx, q.ActorID, accountID, r.Amount(), r.BookedDate())
			if err != nil {
				return SuggestForImportBatchResult{}, err
			}
			// A Choice's option ceiling applies here exactly as it does to
			// categories (ADR-0015's 255-option limit isn't specific to
			// categorisation); in practice, same-currency/exact-amount/
			// 3-day-window narrowing makes this vanishingly unlikely to
			// bite, so — unlike categories — there's no dedicated
			// "too many" state for it: it's simply treated the same as no
			// eligible candidates.
			if len(candidates) <= maxSuggestionOptions {
				occOptions = make([]ports.OccurrenceOption, 0, len(candidates))
				for _, c := range candidates {
					occOptions = append(occOptions, ports.OccurrenceOption{
						OccurrenceID: c.Occurrence.ID(),
						Description:  c.Rule.Description(),
					})
				}
			}
		}

		if len(catOptions) == 0 && len(occOptions) == 0 {
			// Nothing worth asking about this row at all — not a
			// failure, just nothing to send. Its RecordID may still be in
			// tooManyCategoryOptions above, which is tracked independently
			// of whether a request went out.
			continue
		}

		rows = append(rows, ports.SuggestionRow{
			RecordID:             r.ID(),
			Description:          r.Description(),
			Amount:               r.Amount(),
			Date:                 r.BookedDate(),
			Categories:           catOptions,
			OccurrenceCandidates: occOptions,
		})
	}

	result := SuggestForImportBatchResult{
		Configured:             true,
		TooManyCategoryOptions: tooManyCategoryOptions,
		RowsSuggested:          len(rows),
	}
	if len(rows) == 0 {
		return result, nil
	}

	// ports.SuggestionProvider's own contract: partial success is a
	// success (an error here means every attempted row failed), and the
	// returned slice is sparse/unordered — indexed by RecordID below,
	// never assumed positionally aligned with rows. A non-nil err is
	// deliberately not propagated: it becomes RowsFailed instead, so a
	// provider outage degrades the result rather than failing the call.
	provided, _ := s.Suggestions.Suggest(ctx, rows)

	bySuggestionRecordID := make(map[string]ports.RowSuggestion, len(provided))
	for _, rs := range provided {
		bySuggestionRecordID[rs.RecordID] = rs
	}

	suggestions := make([]ImportRowSuggestion, 0, len(provided))
	for _, r := range rows {
		rs, ok := bySuggestionRecordID[r.RecordID]
		if !ok {
			continue
		}

		var out ImportRowSuggestion
		out.RecordID = r.RecordID
		hasSomething := false
		if rs.CategoryID != "" && rs.CategoryConfidence >= suggestionConfidenceThreshold {
			out.Category = &SuggestedCategory{CategoryID: rs.CategoryID, Confidence: rs.CategoryConfidence}
			hasSomething = true
		}
		if rs.OccurrenceID != "" && rs.OccurrenceConfidence >= suggestionConfidenceThreshold {
			out.Occurrence = &SuggestedOccurrence{OccurrenceID: rs.OccurrenceID, Confidence: rs.OccurrenceConfidence}
			hasSomething = true
		}
		if hasSomething {
			suggestions = append(suggestions, out)
		}
	}
	sort.Slice(suggestions, func(i, j int) bool {
		ci, cj := bestConfidence(suggestions[i]), bestConfidence(suggestions[j])
		if ci != cj {
			return ci > cj
		}
		return suggestions[i].RecordID < suggestions[j].RecordID
	})

	result.Suggestions = suggestions
	result.RowsFailed = len(rows) - len(provided)
	return result, nil
}

// bestConfidence returns the higher of s's surviving Category/Occurrence
// confidences, for ranking SuggestForImportBatch's result — a row with
// only one of the two simply ranks by that one.
func bestConfidence(s ImportRowSuggestion) float64 {
	best := 0.0
	if s.Category != nil && s.Category.Confidence > best {
		best = s.Category.Confidence
	}
	if s.Occurrence != nil && s.Occurrence.Confidence > best {
		best = s.Occurrence.Confidence
	}
	return best
}

// suggestionCategoryOptions builds actorID's category option set for each
// CategoryKind — ADR-0015's "filter the actor's category list by
// CategoryKind... in code, never offered to the model as a mixed set" —
// resolved once per SuggestForImportBatch call rather than once per row,
// since it doesn't depend on any one row. Archived categories are left out
// the same way they're left out of any picker (ledger.Category.Archived's
// own doc comment: "hidden from pickers") — a suggestion is a proposal to
// pick from exactly the set a human reviewer would see.
//
// tooMany reports, per kind, whether that kind's option count exceeds
// maxSuggestionOptions — ADR-0015's option-ceiling refusal is decided once
// here rather than recomputed per row.
func (s *Service) suggestionCategoryOptions(ctx context.Context, actorID string) (options map[ledger.CategoryKind][]ports.CategoryOption, tooMany map[ledger.CategoryKind]bool, err error) {
	categories, err := s.Categories.List(ctx, actorID)
	if err != nil {
		return nil, nil, err
	}

	options = map[ledger.CategoryKind][]ports.CategoryOption{
		ledger.CategoryKindExpense: nil,
		ledger.CategoryKindIncome:  nil,
	}
	for _, c := range categories {
		if c.Archived() {
			continue
		}
		options[c.Kind()] = append(options[c.Kind()], ports.CategoryOption{ID: c.ID(), Name: c.Name()})
	}

	tooMany = map[ledger.CategoryKind]bool{
		ledger.CategoryKindExpense: len(options[ledger.CategoryKindExpense]) > maxSuggestionOptions,
		ledger.CategoryKindIncome:  len(options[ledger.CategoryKindIncome]) > maxSuggestionOptions,
	}
	for kind, over := range tooMany {
		if over {
			options[kind] = nil
		}
	}
	return options, tooMany, nil
}
