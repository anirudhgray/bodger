package http

import (
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
)

// exportJSONFilename and exportCSVFilename are the two export routes' own
// Content-Disposition filenames. Both are static: a caller-visible name
// derived from "today" would need clock access this package structurally
// cannot have (ADR-0005), and the canonical JSON document's own contract
// already forbids embedding a generation timestamp in the payload itself
// (ADR-0008) — there's no reason for the filename to carry one either.
const (
	exportJSONFilename = "bodger-export.json"
	exportCSVFilename  = "bodger-export.csv"
)

// exportJSON handles GET /api/v1/export/json: a full, unfiltered backup as
// ADR-0008's canonical bodger.export/v1 document. The response body is
// the document's raw bytes, not the usual {"data": ...} envelope — ADR-0008
// requires this export to be byte-identical for the same underlying data,
// and wrapping it would mean asserting byte-identity on a JSON structure
// this package invents rather than on the document app.ExportJSON actually
// produced.
func (h *handlers) exportJSON(w http.ResponseWriter, r *http.Request) {
	data, err := h.svc.ExportJSON(r.Context(), app.ExportJSONQuery{ActorID: actorID(r)})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	writeDownload(w, "application/json; charset=utf-8", exportJSONFilename, data)
}

// exportCSV handles GET /api/v1/export/csv: transactions as ADR-0008's
// one-row-per-posting CSV, optionally narrowed by the same filter
// dimensions /api/v1/analytics/* accepts (parseReportFilterQuery,
// analytics.go) — every filter parameter omitted exports every
// transaction. Like exportJSON, the response body is the CSV's raw bytes,
// not a JSON envelope.
func (h *handlers) exportCSV(w http.ResponseWriter, r *http.Request) {
	data, err := h.svc.ExportCSV(r.Context(), app.ExportCSVQuery{
		ActorID: actorID(r),
		Filter:  parseReportFilterQuery(r.URL.Query()),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	writeDownload(w, "text/csv; charset=utf-8", exportCSVFilename, data)
}
