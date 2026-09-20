package http

import "github.com/anirudhgray/bodger/internal/app"

// importBatchView is the JSON shape of an import — ADR-0008's staged
// pipeline's own unit of work: one uploaded file, staged as an
// import_batch row and reviewed, committed, or rolled back as a whole.
// "batch" stays in the Go field/type names here (matching
// internal/domain/importing.ImportBatch and internal/app's own command
// names) but never appears in a doc string a person reads — see this
// package's vocabulary check (docs/ux-principles.md §2) — since "import"
// alone is what a user calls the same thing.
type importBatchView struct {
	ID              string `json:"id"`
	SourceFormat    string `json:"source_format" doc:"The uploaded file's format. Only \"csv\" is supported today."`
	Filename        string `json:"filename" doc:"The uploaded file's own name."`
	FileHash        string `json:"file_hash" doc:"A content hash of the uploaded file, for spotting an accidental re-upload."`
	TargetAccountID string `json:"target_account_id" doc:"The account new transactions from this import post against once committed."`
	Status          string `json:"status" enum:"import_batch_status"`
}

func importBatchViewFrom(r app.GetImportBatchResult) importBatchView {
	b := r.Batch
	return importBatchView{
		ID:              b.ID(),
		SourceFormat:    b.SourceFormat(),
		Filename:        b.Filename(),
		FileHash:        b.FileHash(),
		TargetAccountID: b.TargetAccountID(),
		Status:          string(b.Status()),
	}
}

// duplicateMatchView is the JSON shape of a candidate duplicate ADR-0008's
// two-tier detection found for a staged record.
type duplicateMatchView struct {
	Tier                 string `json:"tier" enum:"import_duplicate_tier" doc:"\"exact\" is a definite duplicate, already excluded automatically. \"suspected_duplicate\" needs your decision — see POST /api/v1/import-records/{id}/resolve."`
	MatchedTransactionID string `json:"matched_transaction_id" doc:"The existing transaction this record was matched against."`
	Resolution           string `json:"resolution" enum:"import_duplicate_resolution"`
}

// occurrenceMatchView is the JSON shape of a candidate occurrence match
// issue #301's detection found for a staged record — mirroring
// duplicateMatchView's own field-for-field shape, but pointing at a
// pending recurring occurrence rather than a committed transaction (see
// that detection's own doc comment, internal/app/import_duplicate.go, for
// why the two aren't the same type).
type occurrenceMatchView struct {
	OccurrenceID string `json:"occurrence_id" doc:"The pending recurring occurrence this record was matched against."`
	Resolution   string `json:"resolution" enum:"import_occurrence_match_resolution" doc:"\"pending\" needs your decision — see POST /api/v1/import-records/{id}/resolve-occurrence-match."`
}

// importRecordView is the JSON shape of one staged row.
//
// AccountID/CategoryID aren't named here — an import record's own
// resolved account and category, when set, aren't a fixed part of a
// posting the way transactionView's are (docs/ux-principles.md §2 bans
// the word "posting" from user-facing strings, and the concept from
// user-facing shape) — this exposes what mapping (a later, separate
// concern) already resolved directly as resolved_account_id/
// resolved_category_id, and leaves them empty when unresolved rather than
// inventing a placeholder.
type importRecordView struct {
	ID                        string               `json:"id"`
	ImportBatchID             string               `json:"import_id" doc:"The import this record was staged into."`
	BookedDate                string               `json:"booked_date" format:"date"`
	PostedDate                string               `json:"posted_date,omitempty" doc:"Set only when the source distinguished it from booked_date." format:"date"`
	Description               string               `json:"description"`
	Amount                    string               `json:"amount" doc:"Signed: negative for money out, positive for money in." format:"money"`
	Currency                  string               `json:"currency"`
	ExternalID                string               `json:"external_id,omitempty" doc:"The source's own ID for this row, when it had one."`
	ResolvedAccountID         string               `json:"resolved_account_id,omitempty"`
	ResolvedCategoryID        string               `json:"resolved_category_id,omitempty"`
	DuplicateMatch            *duplicateMatchView  `json:"duplicate_match,omitempty"`
	TransferCandidateRecordID string               `json:"transfer_candidate_record_id,omitempty" doc:"Another staged record this one might be the other leg of a transfer with. Informational only — resolving duplicate_match is what actually affects commit."`
	OccurrenceMatch           *occurrenceMatchView `json:"occurrence_match,omitempty" doc:"A pending recurring occurrence this record's money might already be accounted for by. Unlike transfer_candidate_record_id, an unresolved occurrence_match does block commit — see POST /api/v1/import-records/{id}/resolve-occurrence-match."`
	Status                    string               `json:"status" enum:"import_record_status"`
	TransactionID             string               `json:"transaction_id,omitempty" doc:"Set once this record has been committed."`
	SortOrder                 int                  `json:"sort_order" doc:"This record's position in the uploaded file."`
}

func importRecordViewFrom(r app.ResolveImportRecordResult) importRecordView {
	rec := r.Record
	v := importRecordView{
		ID:            rec.ID(),
		ImportBatchID: rec.ImportBatchID(),
		BookedDate:    rec.BookedDate().String(),
		Description:   rec.Description(),
		Amount:        rec.Amount().AmountString(),
		Currency:      rec.Amount().Currency(),
		Status:        string(rec.Status()),
		SortOrder:     rec.SortOrder(),
	}
	if d, ok := rec.PostedDate(); ok {
		v.PostedDate = d.String()
	}
	if id, ok := rec.ExternalID(); ok {
		v.ExternalID = id
	}
	if id, ok := rec.ResolvedAccountID(); ok {
		v.ResolvedAccountID = id
	}
	if id, ok := rec.ResolvedCategoryID(); ok {
		v.ResolvedCategoryID = id
	}
	if dm, ok := rec.DuplicateMatch(); ok {
		v.DuplicateMatch = &duplicateMatchView{
			Tier:                 string(dm.Tier()),
			MatchedTransactionID: dm.MatchedTransactionID(),
			Resolution:           string(dm.Resolution()),
		}
	}
	if id, ok := rec.TransferCandidateRecordID(); ok {
		v.TransferCandidateRecordID = id
	}
	if om, ok := rec.OccurrenceMatch(); ok {
		v.OccurrenceMatch = &occurrenceMatchView{OccurrenceID: om.OccurrenceID(), Resolution: string(om.Resolution())}
	}
	if id, ok := rec.TransactionID(); ok {
		v.TransactionID = id
	}
	return v
}

// importRecordViewFromOccurrenceResolution builds an importRecordView from
// ResolveImportRecordOccurrenceMatch's own result shape, reusing
// importRecordViewFrom by re-wrapping its Record — the same pattern
// createImportBatch/listImportRecords already use to share this view
// builder across every handler that produces an ImportRecord.
func importRecordViewFromOccurrenceResolution(r app.ResolveImportRecordOccurrenceMatchResult) importRecordView {
	return importRecordViewFrom(app.ResolveImportRecordResult{Record: r.Record})
}

// importBatchWithRecordsView is POST /api/v1/imports' response shape: the
// import StageImport just created, together with every record it staged.
type importBatchWithRecordsView struct {
	Batch   importBatchView    `json:"batch"`
	Records []importRecordView `json:"records"`
}

func importBatchWithRecordsViewFrom(r app.StageImportResult) importBatchWithRecordsView {
	v := importBatchWithRecordsView{Batch: importBatchViewFrom(app.GetImportBatchResult{Batch: r.Batch})}
	v.Records = make([]importRecordView, 0, len(r.Records))
	for _, rec := range r.Records {
		v.Records = append(v.Records, importRecordViewFrom(app.ResolveImportRecordResult{Record: rec}))
	}
	return v
}

// importCommitView is POST /api/v1/imports/{id}/commit's response shape:
// the committed import and the transactions it just created.
type importCommitView struct {
	Batch        importBatchView   `json:"batch"`
	Transactions []transactionView `json:"transactions"`
}

func importCommitViewFrom(r app.CommitImportBatchResult) importCommitView {
	v := importCommitView{Batch: importBatchViewFrom(app.GetImportBatchResult{Batch: r.Batch})}
	v.Transactions = make([]transactionView, 0, len(r.Transactions))
	for _, txn := range r.Transactions {
		v.Transactions = append(v.Transactions, transactionViewFrom(app.TransactionResult{Transaction: txn}))
	}
	return v
}

// importRollbackView is POST /api/v1/imports/{id}/rollback's response
// shape: the rolled-back import and the IDs of the transactions it
// deleted.
type importRollbackView struct {
	Batch          importBatchView `json:"batch"`
	TransactionIDs []string        `json:"transaction_ids" doc:"The transactions this rollback deleted."`
}

func importRollbackViewFrom(r app.RollbackImportBatchResult) importRollbackView {
	return importRollbackView{
		Batch:          importBatchViewFrom(app.GetImportBatchResult{Batch: r.Batch}),
		TransactionIDs: r.TransactionIDs,
	}
}

// suggestionSource is what every typesafe.ai suggestion this surface
// renders is attributed to (issue #306, ADR-0015, docs/ux-principles.md
// §6: "attributed to typesafe.ai by name... never as an unattributed
// hint"). It's a literal, not something read off app.ImportRowSuggestion:
// ports.SuggestionProvider deliberately carries no Name() (see that
// interface's own doc comment — nothing it returns is persisted, so
// there's no provenance column to fill), and an instance has only ever
// one configured provider for a suggestion to have come from.
const suggestionSource = "typesafe.ai"

// suggestedCategoryView is one staged row's surviving category
// suggestion (app.SuggestedCategory) — a proposal, never a value.
// Accepting it means committing the import and setting the resulting
// transaction's category yourself (PATCH /api/v1/transactions/{id}),
// exactly as you would without this endpoint — ADR-0015: "nothing here
// ever writes to the ledger."
type suggestedCategoryView struct {
	CategoryID string  `json:"category_id" doc:"The suggested category's ID."`
	Confidence float64 `json:"confidence" doc:"typesafe.ai's own confidence for this answer (0-1), already past bodger's 0.5 display threshold. Ordering only — not a value to act on by itself."`
}

// suggestedOccurrenceView is one staged row's surviving occurrence-match
// suggestion (app.SuggestedOccurrence). Accepting it means resolving the
// match yourself via POST /api/v1/import-records/{id}/resolve-occurrence-match,
// exactly as you would without a suggestion.
type suggestedOccurrenceView struct {
	OccurrenceID string  `json:"occurrence_id" doc:"The suggested pending occurrence's ID."`
	Confidence   float64 `json:"confidence" doc:"typesafe.ai's own confidence for this answer (0-1), already past bodger's 0.5 display threshold."`
}

// importRowSuggestionView is one staged row's suggestion(s)
// (app.ImportRowSuggestion) — Category and/or Occurrence are omitted
// when there was nothing worth showing for that half of the row.
type importRowSuggestionView struct {
	RecordID   string                   `json:"record_id"`
	Source     string                   `json:"source" doc:"Always \"typesafe.ai\" — which third party proposed this suggestion."`
	Category   *suggestedCategoryView   `json:"category,omitempty"`
	Occurrence *suggestedOccurrenceView `json:"occurrence,omitempty"`
}

func importRowSuggestionViewFrom(s app.ImportRowSuggestion) importRowSuggestionView {
	v := importRowSuggestionView{RecordID: s.RecordID, Source: suggestionSource}
	if s.Category != nil {
		v.Category = &suggestedCategoryView{CategoryID: s.Category.CategoryID, Confidence: s.Category.Confidence}
	}
	if s.Occurrence != nil {
		v.Occurrence = &suggestedOccurrenceView{OccurrenceID: s.Occurrence.OccurrenceID, Confidence: s.Occurrence.Confidence}
	}
	return v
}

// importSuggestionsView is GET /api/v1/imports/{id}/suggestions' response
// shape (app.SuggestForImportBatchResult) — issue #306.
type importSuggestionsView struct {
	Configured bool `json:"configured" doc:"False when this instance has no typesafe.ai key configured — not an error; every other field is then left zero. See docs/user-guide.md for how an operator enables this."`
	// Suggestions is ranked by confidence, best first (app.SuggestForImportBatchResult's own contract) — never re-sorted here.
	Suggestions []importRowSuggestionView `json:"suggestions"`
	// TooManyCategoryOptions lists staged record IDs that got no category
	// suggestion at all because this actor has more eligible categories of
	// that row's kind than typesafe.ai can be asked about in one question —
	// distinct from a record simply absent from Suggestions (which can
	// instead mean "asked, but nothing confident came back" or "the
	// request for this row failed" — see RowsFailed).
	TooManyCategoryOptions []string `json:"too_many_category_options,omitempty" doc:"Staged record IDs that got no category suggestion because this actor has too many categories of the row's kind for typesafe.ai to be asked about."`
	RowsSuggested          int      `json:"rows_suggested" doc:"How many staged rows actually had a suggestion request sent for them."`
	RowsFailed             int      `json:"rows_failed" doc:"How many of those rows typesafe.ai never answered. One row failing never discards another row's suggestion — every staged row is still fully listable and reviewable regardless."`
	// FailureReason is why RowsFailed rows got no answer — one dominant
	// reason for this whole request, not a per-row account (see
	// app.SuggestForImportBatchResult.FailureReason's own doc comment for
	// why). Empty when RowsFailed is 0. Its possible values are typesafe.ai's
	// own stable reason strings ("credential_rejected", "throttled",
	// "provider_unreachable" — internal/adapters/typesafe/errors.go), not
	// translated here: this is the machine-readable REST response, the same
	// way too_many_category_options is a list of IDs rather than prose.
	FailureReason string `json:"failure_reason,omitempty" doc:"Why rows_failed rows got no answer -- \"credential_rejected\", \"throttled\", or \"provider_unreachable\" -- empty when rows_failed is 0. One dominant reason for the whole request, not tracked per row."`
}

func importSuggestionsViewFrom(r app.SuggestForImportBatchResult) importSuggestionsView {
	v := importSuggestionsView{
		Configured:             r.Configured,
		TooManyCategoryOptions: r.TooManyCategoryOptions,
		RowsSuggested:          r.RowsSuggested,
		RowsFailed:             r.RowsFailed,
		FailureReason:          r.FailureReason,
	}
	v.Suggestions = make([]importRowSuggestionView, 0, len(r.Suggestions))
	for _, s := range r.Suggestions {
		v.Suggestions = append(v.Suggestions, importRowSuggestionViewFrom(s))
	}
	return v
}
