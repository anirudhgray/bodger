package http

import (
	"io"
	"net/http"
	"net/url"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/app/importparse"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// maxImportUploadBytes bounds how much of POST /api/v1/imports' request
// body this handler will read into memory. Generous for a real bank or
// card statement (even years of history is a few MB of CSV) - just
// bounded so a malformed or oversized upload can't grow this process's
// memory without limit. There's no existing precedent for a request-body
// size cap elsewhere in this surface (every other route's body is a
// small JSON object), since this is the first route whose body is an
// arbitrary uploaded file.
const maxImportUploadBytes = 20 << 20 // 20 MiB

// readImportUpload reads r's entire body as the uploaded file's raw
// bytes, capped at maxImportUploadBytes. This mirrors export.go's own
// raw-bytes convention in reverse: an export route's response body is the
// document itself, not a {"data": ...} envelope, because wrapping it
// would break ADR-0008's byte-identity guarantee; an import route's
// request body is the uploaded file itself for the same shaped reason —
// there is nothing about "which bytes did you upload" that a JSON
// envelope would add, and wrapping arbitrary file bytes in JSON would
// mean re-encoding them (e.g. base64) for no benefit. account, filename,
// and the column mapping travel as query parameters instead, since the
// body is already spoken for.
func readImportUpload(w http.ResponseWriter, r *http.Request) ([]byte, *errs.Error) {
	if r.Body == nil {
		return nil, errs.New(errs.InvalidInput).Explain("An uploaded file is required.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImportUploadBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, errs.New(errs.InvalidInput).
			Explain("Couldn't read the uploaded file — it may be larger than the %d MB limit.", maxImportUploadBytes>>20).
			Wrap(err)
	}
	return data, nil
}

// columnMappingFromQuery reads POST /api/v1/imports' column-mapping query
// parameters into importparse.ColumnMapping — every field here needs no
// I/O or clock to resolve (it's already exactly the column names the
// uploaded file has), the same "no dependency" exception ADR-0005 carves
// out for a raw column mapping (see internal/app/import_stage.go's own
// doc comment on StageImportCommand.ColumnMapping).
func columnMappingFromQuery(q url.Values) importparse.ColumnMapping {
	return importparse.ColumnMapping{
		DateColumn:        q.Get("date_column"),
		DescriptionColumn: q.Get("description_column"),
		AmountColumn:      q.Get("amount_column"),
		PostedDateColumn:  q.Get("posted_date_column"),
		CurrencyColumn:    q.Get("currency_column"),
		ExternalIDColumn:  q.Get("external_id_column"),
		CategoryColumn:    q.Get("category_column"),
	}
}

func (h *handlers) createImportBatch(w http.ResponseWriter, r *http.Request) {
	data, rerr := readImportUpload(w, r)
	if rerr != nil {
		h.respondError(w, r, rerr)
		return
	}

	q := r.URL.Query()
	result, err := h.svc.StageImport(r.Context(), app.StageImportCommand{
		ActorID:       actorID(r),
		AccountRef:    q.Get("account"),
		Filename:      q.Get("filename"),
		SourceFormat:  q.Get("format"),
		FileContent:   data,
		ColumnMapping: columnMappingFromQuery(q),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusCreated, importBatchWithRecordsViewFrom(result))
}

func (h *handlers) listImportBatches(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ListImportBatches(r.Context(), app.ListImportBatchesQuery{ActorID: actorID(r)})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	views := make([]importBatchView, 0, len(result.Batches))
	for _, b := range result.Batches {
		views = append(views, importBatchViewFrom(app.GetImportBatchResult{Batch: b}))
	}
	respond(w, http.StatusOK, views)
}

func (h *handlers) getImportBatch(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.GetImportBatch(r.Context(), app.GetImportBatchQuery{
		ActorID: actorID(r), ImportBatchRef: r.PathValue("id"),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, importBatchViewFrom(result))
}

func (h *handlers) listImportRecords(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ListImportRecords(r.Context(), app.ListImportRecordsQuery{
		ActorID: actorID(r), ImportBatchRef: r.PathValue("id"),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	views := make([]importRecordView, 0, len(result.Records))
	for _, rec := range result.Records {
		views = append(views, importRecordViewFrom(app.ResolveImportRecordResult{Record: rec}))
	}
	respond(w, http.StatusOK, views)
}

func (h *handlers) commitImportBatch(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.CommitImportBatch(r.Context(), app.CommitImportBatchCommand{
		ActorID: actorID(r), ImportBatchRef: r.PathValue("id"),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, importCommitViewFrom(result))
}

func (h *handlers) rollbackImportBatch(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.RollbackImportBatch(r.Context(), app.RollbackImportBatchCommand{
		ActorID: actorID(r), ImportBatchRef: r.PathValue("id"),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, importRollbackViewFrom(result))
}

// resolveImportRecordRequest is POST /api/v1/import-records/{id}/resolve's
// request body: the user's decision on a staged record's suspected
// duplicate. Resolution's two values are the same wire strings
// importing.DuplicateResolutionConfirmed/-Dismissed already use — this
// handler passes it straight through to app.ResolveImportRecordCommand,
// which validates it (see that command's own doc comment for why no
// validation belongs here).
type resolveImportRecordRequest struct {
	Resolution string `json:"resolution" enum:"import_duplicate_resolution" doc:"\"confirmed_duplicate\" excludes the record from commit; \"not_duplicate\" clears it for commit."`
}

func (h *handlers) resolveImportRecord(w http.ResponseWriter, r *http.Request) {
	var body resolveImportRecordRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	result, err := h.svc.ResolveImportRecord(r.Context(), app.ResolveImportRecordCommand{
		ActorID: actorID(r), ImportRecordRef: r.PathValue("id"), Resolution: body.Resolution,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, importRecordViewFrom(result))
}

// resolveImportRecordOccurrenceMatchRequest is
// POST /api/v1/import-records/{id}/resolve-occurrence-match's request
// body: the user's decision on a staged record's matched pending
// occurrence (issue #309). This handler passes Resolution straight
// through to app.ResolveImportRecordOccurrenceMatchCommand, which
// validates it — the same "no validation belongs here" reasoning
// resolveImportRecordRequest's own doc comment gives.
type resolveImportRecordOccurrenceMatchRequest struct {
	Resolution string `json:"resolution" enum:"import_occurrence_match_resolution" doc:"\"materialized\" turns the matched occurrence into its own transaction and excludes this record from commit; \"dismissed\" leaves the occurrence untouched and clears this record for commit."`
}

func (h *handlers) resolveImportRecordOccurrenceMatch(w http.ResponseWriter, r *http.Request) {
	var body resolveImportRecordOccurrenceMatchRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	result, err := h.svc.ResolveImportRecordOccurrenceMatch(r.Context(), app.ResolveImportRecordOccurrenceMatchCommand{
		ActorID: actorID(r), ImportRecordRef: r.PathValue("id"), Resolution: body.Resolution,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, importRecordViewFromOccurrenceResolution(result))
}
