package http

import (
	"encoding/json"
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// restoreSnapshotRequest is POST /api/v1/restore's request body: the
// canonical JSON document to restore, under "document" — exactly the
// bodger.export/v1 shape GET /api/v1/export/json produces — alongside a
// "confirm" flag this handler enforces before calling app.RestoreSnapshot
// at all. Restoring wipes and replaces every account, category, and
// transaction the actor has, in one shot, with no preview-then-commit
// stage to catch a mistake the way the staged CSV import pipeline has —
// so a request that doesn't explicitly set "confirm": true never reaches
// the application layer.
//
// Document is left as json.RawMessage rather than decoded into a typed
// struct here: app.RestoreSnapshotQuery already takes the document as raw
// bytes and does its own full parsing and validation (internal/app/restore.go),
// so re-decoding its shape in this package would only be a second,
// redundant definition of the same wire format to keep in sync.
type restoreSnapshotRequest struct {
	Confirm  bool            `json:"confirm" doc:"Must be true. Restoring replaces every account, category, and transaction you currently have."`
	Document json.RawMessage `json:"document" doc:"The canonical JSON document to restore, exactly as produced by GET /api/v1/export/json."`
}

// restoreSnapshotView is the response body for POST /api/v1/restore: how
// many rows of each kind the restore installed, mirroring
// app.RestoreSnapshotResult field-for-field.
type restoreSnapshotView struct {
	Accounts     int `json:"accounts"`
	Categories   int `json:"categories"`
	Transactions int `json:"transactions"`
}

func restoreSnapshotViewFrom(r app.RestoreSnapshotResult) restoreSnapshotView {
	return restoreSnapshotView{Accounts: r.Accounts, Categories: r.Categories, Transactions: r.Transactions}
}

// restoreSnapshot handles POST /api/v1/restore: issue #227's REST wiring
// for #226's RestoreSnapshot use case. See restoreSnapshotRequest's doc
// comment for why "confirm" is checked here, before anything else runs.
func (h *handlers) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	var body restoreSnapshotRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}
	if !body.Confirm {
		h.respondError(w, r, errs.New(errs.InvalidInput).
			Explain(`Set "confirm": true to run this - it replaces every account, category, and transaction you have with the document's contents.`).
			Field("confirm"))
		return
	}

	result, err := h.svc.RestoreSnapshot(r.Context(), app.RestoreSnapshotQuery{
		ActorID:  actorID(r),
		Document: body.Document,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, restoreSnapshotViewFrom(result))
}
