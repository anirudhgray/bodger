package http

import (
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
)

// createTransferRequest is POST /api/v1/transfers' request body. There is
// no currency field: a transfer's amount is always in from_account's own
// currency (app.RecordTransferCommand's doc comment) — the application
// layer resolves the to-account leg's currency itself and rejects a
// cross-currency transfer, per M1's single-currency-per-account scope.
type createTransferRequest struct {
	FromAccount string   `json:"from_account"`
	ToAccount   string   `json:"to_account"`
	Amount      string   `json:"amount"`
	Date        string   `json:"date,omitempty"`
	Description string   `json:"description"`
	Notes       string   `json:"notes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func (h *handlers) createTransfer(w http.ResponseWriter, r *http.Request) {
	var body createTransferRequest
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, err)
		return
	}

	tags, terr := decodeTags(body.Tags)
	if terr != nil {
		respondError(w, terr)
		return
	}

	result, err := h.svc.RecordTransfer(r.Context(), app.RecordTransferCommand{
		ActorID:        actorID(),
		FromAccountRef: body.FromAccount,
		ToAccountRef:   body.ToAccount,
		Amount:         body.Amount,
		Date:           body.Date,
		Description:    body.Description,
		Notes:          body.Notes,
		Tags:           tags,
	})
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusCreated, transactionViewFrom(result))
}
