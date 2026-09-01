package http

import (
	"net/http"
	"strconv"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

const (
	transactionTypeOutflow = "outflow"
	transactionTypeInflow  = "inflow"
)

// createTransactionRequest is POST /api/v1/transactions' request body.
// Type selects which of RecordOutflow/RecordInflow this handler calls —
// picking between them is presentation-layer routing to the right
// application method, not a business decision about the request's
// content (see this package's doc comment). date, when omitted, resolves
// to the correct booked date in the actor's timezone entirely inside the
// application layer (internal/app/normalize.DateOf) — this handler never
// reads the wall clock or otherwise decides what "today" means.
type createTransactionRequest struct {
	Type        string   `json:"type" enum:"recordable_transaction_kind"`
	Account     string   `json:"account" doc:"An account's ID or unique name."`
	Category    string   `json:"category,omitempty" doc:"A category's ID or unique name."`
	Amount      string   `json:"amount" doc:"Always positive; type says which way the money moves." format:"money"`
	Currency    string   `json:"currency,omitempty"`
	Date        string   `json:"date,omitempty" doc:"Omit to book the transaction to today in the account owner's own timezone." format:"date"`
	Description string   `json:"description"`
	Notes       string   `json:"notes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func (h *handlers) createTransaction(w http.ResponseWriter, r *http.Request) {
	var body createTransactionRequest
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, err)
		return
	}

	tags, terr := decodeTags(body.Tags)
	if terr != nil {
		respondError(w, terr)
		return
	}

	var (
		result app.TransactionResult
		err    error
	)
	switch body.Type {
	case transactionTypeOutflow:
		result, err = h.svc.RecordOutflow(r.Context(), app.RecordOutflowCommand{
			ActorID: actorID(), AccountRef: body.Account, Amount: body.Amount, Currency: body.Currency,
			CategoryRef: body.Category, Date: body.Date, Description: body.Description, Notes: body.Notes, Tags: tags,
		})
	case transactionTypeInflow:
		result, err = h.svc.RecordInflow(r.Context(), app.RecordInflowCommand{
			ActorID: actorID(), AccountRef: body.Account, Amount: body.Amount, Currency: body.Currency,
			CategoryRef: body.Category, Date: body.Date, Description: body.Description, Notes: body.Notes, Tags: tags,
		})
	default:
		respondError(w, errs.New(errs.InvalidInput).
			Explain("%q isn't a valid transaction type. Use \"outflow\" or \"inflow\" here, or POST /api/v1/transfers for a transfer.", body.Type).
			Field("type"))
		return
	}
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusCreated, transactionViewFrom(result))
}

func (h *handlers) getTransaction(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.GetTransaction(r.Context(), app.GetTransactionQuery{
		ActorID:        actorID(),
		TransactionRef: r.PathValue("id"),
	})
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, transactionViewFrom(result))
}

// editTransactionRequest is PATCH /api/v1/transactions/{id}'s request
// body — a full replacement of every field here, matching
// app.EditTransactionCommand's own full-replacement semantics (its doc
// comment explains why: ADR-0005's raw-string command fields already use
// "" to mean specific things, which would conflict with also meaning
// "leave this field alone"). Account/Category apply when editing an
// outflow or inflow; FromAccount/ToAccount apply when editing a transfer.
// Whichever pair doesn't match the transaction's own kind is ignored by
// the application layer, not by this handler.
type editTransactionRequest struct {
	Account     string   `json:"account,omitempty" doc:"An account's ID or unique name. Read when editing an outflow or an inflow."`
	Category    string   `json:"category,omitempty" doc:"A category's ID or unique name. Read when editing an outflow or an inflow."`
	FromAccount string   `json:"from_account,omitempty" doc:"An account's ID or unique name. Read when editing a transfer."`
	ToAccount   string   `json:"to_account,omitempty" doc:"An account's ID or unique name. Read when editing a transfer."`
	Currency    string   `json:"currency,omitempty"`
	Amount      string   `json:"amount" doc:"Always positive; the transaction's own type says which way the money moves." format:"money"`
	Date        string   `json:"date,omitempty" doc:"Omit to book the transaction to today in the account owner's own timezone." format:"date"`
	Description string   `json:"description"`
	Notes       string   `json:"notes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func (h *handlers) editTransaction(w http.ResponseWriter, r *http.Request) {
	var body editTransactionRequest
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, err)
		return
	}

	tags, terr := decodeTags(body.Tags)
	if terr != nil {
		respondError(w, terr)
		return
	}

	result, err := h.svc.EditTransaction(r.Context(), app.EditTransactionCommand{
		ActorID:        actorID(),
		TransactionRef: r.PathValue("id"),
		AccountRef:     body.Account,
		Currency:       body.Currency,
		CategoryRef:    body.Category,
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
	respond(w, http.StatusOK, transactionViewFrom(result))
}

func (h *handlers) deleteTransaction(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.DeleteTransaction(r.Context(), app.DeleteTransactionCommand{
		ActorID: actorID(), TransactionRef: r.PathValue("id"),
	})
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, transactionViewFrom(result))
}

// transactionListView is GET /api/v1/transactions' response shape: the
// page of matching transactions, plus an opaque cursor for the next page
// — empty once there isn't one.
type transactionListView struct {
	Data       []transactionView `json:"data"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"An opaque token for the next page. Present only when there is a next page; its encoding isn't part of the API's contract and may change."`
}

func (h *handlers) listTransactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	offset, cerr := decodeCursor(q.Get("cursor"))
	if cerr != nil {
		respondError(w, cerr)
		return
	}

	limit := 0
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			respondError(w, errs.New(errs.InvalidInput).Explain("%q isn't a valid page size.", raw).Field("limit"))
			return
		}
		limit = n
	}

	result, err := h.svc.ListTransactions(r.Context(), app.ListTransactionsQuery{
		ActorID:     actorID(),
		AccountRef:  q.Get("account"),
		CategoryRef: q.Get("category"),
		Kind:        q.Get("type"),
		DateFrom:    q.Get("from"),
		DateTo:      q.Get("to"),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		respondError(w, err)
		return
	}

	views := make([]transactionView, 0, len(result.Transactions))
	for _, t := range result.Transactions {
		views = append(views, transactionViewFrom(app.TransactionResult{Transaction: t}))
	}

	respond(w, http.StatusOK, transactionListView{
		Data:       views,
		NextCursor: nextCursorFor(len(result.Transactions), result.Limit, result.Offset),
	})
}

// nextCursorFor decides whether GET /transactions' response should carry
// a next_cursor: it should when this page came back exactly as full as
// the application layer's own effective page size (ListTransactionsResult's
// Limit — already defaulted and clamped there, never re-derived here),
// since a short page is the only signal this offset-paginated query gives
// that there's nothing left. This is a heuristic, not an exact count: the
// query doesn't report a total, and asking it to would mean a second
// query this handler has no business making.
func nextCursorFor(returned, effectiveLimit, offset int) string {
	if returned < effectiveLimit {
		return ""
	}
	return encodeCursor(offset + returned)
}
