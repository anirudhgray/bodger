package http

import (
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
)

// createTransferRequest is POST /api/v1/transfers' request body. There is
// no currency field: a transfer's amount is parsed once, in from_account's
// own currency, and to_account's leg is built in to_account's own currency
// (app.RecordTransferCommand's doc comment) — the two accounts may have
// different currencies. When they do, to_amount is optional: omit it and
// the to-leg reuses amount's raw digits (today's default), or send it to
// state the to-leg's own amount independently. Either way, the application
// layer derives and persists the implied exchange rate between the two
// legs rather than rejecting the transfer (ledger.NewTransfer).
type createTransferRequest struct {
	FromAccount string   `json:"from_account" doc:"An account's ID or unique name: the account the money leaves."`
	ToAccount   string   `json:"to_account" doc:"An account's ID or unique name: the account the money arrives in."`
	Amount      string   `json:"amount" doc:"Always positive, in the from-account's own currency." format:"money"`
	ToAmount    string   `json:"to_amount,omitempty" doc:"The to-leg's own amount, in the to-account's own currency. Omit to reuse amount's raw digits, reinterpreted in the to-currency, as before. Set this to record a real cross-currency exchange rate rather than an accidental 1:1." format:"money"`
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
		ToAmount:       body.ToAmount,
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
