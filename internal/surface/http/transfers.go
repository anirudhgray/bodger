package http

import (
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
)

// createTransferRequest is POST /api/v1/transfers' request body. There is
// no currency field: a transfer's amount is parsed once, in from_account's
// own currency, and to_account's leg is built in to_account's own currency
// (app.RecordTransferCommand's doc comment) — the two accounts may have
// different currencies. When they do, the application layer derives and
// persists the implied exchange rate between the two legs rather than
// rejecting the transfer (ledger.NewTransfer). Sending the to-leg's amount
// independently, rather than reusing the from-leg's raw number, is a
// separate future change to this request shape.
type createTransferRequest struct {
	FromAccount string   `json:"from_account" doc:"An account's ID or unique name: the account the money leaves."`
	ToAccount   string   `json:"to_account" doc:"An account's ID or unique name: the account the money arrives in."`
	Amount      string   `json:"amount" doc:"Always positive, in the from-account's own currency." format:"money"`
	Date        string   `json:"date,omitempty" doc:"Omit to book the transfer to today in the account owner's own timezone." format:"date"`
	Description string   `json:"description"`
	Notes       string   `json:"notes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func (h *handlers) createTransfer(w http.ResponseWriter, r *http.Request) {
	var body createTransferRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	tags, terr := decodeTags(body.Tags)
	if terr != nil {
		h.respondError(w, r, terr)
		return
	}

	result, err := h.svc.RecordTransfer(r.Context(), app.RecordTransferCommand{
		ActorID:        actorID(r),
		FromAccountRef: body.FromAccount,
		ToAccountRef:   body.ToAccount,
		Amount:         body.Amount,
		Date:           body.Date,
		Description:    body.Description,
		Notes:          body.Notes,
		Tags:           tags,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusCreated, transactionViewFrom(result))
}
