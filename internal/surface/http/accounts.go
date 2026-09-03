package http

import (
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// createAccountRequest is POST /api/v1/accounts' request body.
// Every field passes through to app.CreateAccountCommand as the raw
// string the client sent (ADR-0005) — Type is validated against the
// domain's closed set of account kinds by the application layer, not
// here; Currency, when empty, resolves through the currency precedence
// ladder there too.
type createAccountRequest struct {
	Name               string `json:"name"`
	Type               string `json:"type" enum:"account_kind"`
	Currency           string `json:"currency,omitempty"`
	OpeningBalance     string `json:"opening_balance,omitempty" doc:"Defaults to zero." format:"money"`
	OpeningBalanceDate string `json:"opening_balance_date,omitempty" doc:"The date the opening balance is stated as of. Omit to state it as of today." format:"date"`
	Institution        string `json:"institution,omitempty"`
	SortOrder          int    `json:"sort_order,omitempty" doc:"Where the account sorts in a list; lower comes first."`
}

func (h *handlers) createAccount(w http.ResponseWriter, r *http.Request) {
	var body createAccountRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, err)
		return
	}

	result, err := h.svc.CreateAccount(r.Context(), app.CreateAccountCommand{
		ActorID:            actorID(r),
		Name:               body.Name,
		Kind:               body.Type,
		Currency:           body.Currency,
		OpeningBalance:     body.OpeningBalance,
		OpeningBalanceDate: body.OpeningBalanceDate,
		Institution:        body.Institution,
		SortOrder:          body.SortOrder,
	})
	if err != nil {
		h.respondError(w, err)
		return
	}
	respond(w, http.StatusCreated, accountViewFrom(result))
}

func (h *handlers) listAccounts(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ListAccounts(r.Context(), app.ListAccountsQuery{ActorID: actorID(r)})
	if err != nil {
		h.respondError(w, err)
		return
	}
	views := make([]accountView, 0, len(result.Accounts))
	for _, a := range result.Accounts {
		views = append(views, accountViewFrom(app.AccountResult{Account: a}))
	}
	respond(w, http.StatusOK, views)
}

func (h *handlers) getAccount(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.GetAccount(r.Context(), app.GetAccountQuery{
		ActorID:    actorID(r),
		AccountRef: r.PathValue("id"),
	})
	if err != nil {
		h.respondError(w, err)
		return
	}
	respond(w, http.StatusOK, accountViewFrom(result))
}

// patchAccountRequest is PATCH /api/v1/accounts/{id}'s request body. It
// expresses exactly one of two sibling update intents the application
// layer exposes as separate use cases — renaming, or re-declaring the
// opening balance — never both in the same request (see this package's
// doc comment for why choosing between them here is presentation
// routing, not a business decision).
type patchAccountRequest struct {
	Name               *string `json:"name,omitempty" doc:"The account's new name."`
	OpeningBalance     *string `json:"opening_balance,omitempty" doc:"The opening balance to re-declare." format:"money"`
	OpeningBalanceDate *string `json:"opening_balance_date,omitempty" doc:"The date the new opening balance is stated as of. Only read alongside opening_balance." format:"date"`
}

func (h *handlers) patchAccount(w http.ResponseWriter, r *http.Request) {
	var body patchAccountRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, err)
		return
	}

	id := r.PathValue("id")
	switch {
	case body.Name != nil && body.OpeningBalance == nil:
		result, err := h.svc.RenameAccount(r.Context(), app.RenameAccountCommand{
			ActorID: actorID(r), AccountRef: id, Name: *body.Name,
		})
		if err != nil {
			h.respondError(w, err)
			return
		}
		respond(w, http.StatusOK, accountViewFrom(result))

	case body.OpeningBalance != nil && body.Name == nil:
		var obDate string
		if body.OpeningBalanceDate != nil {
			obDate = *body.OpeningBalanceDate
		}
		result, err := h.svc.SetOpeningBalance(r.Context(), app.SetOpeningBalanceCommand{
			ActorID: actorID(r), AccountRef: id, OpeningBalance: *body.OpeningBalance, OpeningBalanceDate: obDate,
		})
		if err != nil {
			h.respondError(w, err)
			return
		}
		respond(w, http.StatusOK, accountViewFrom(result))

	default:
		h.respondError(w, errs.New(errs.InvalidInput).
			Explain("A request must set exactly one of \"name\" or \"opening_balance\"."))
	}
}

func (h *handlers) archiveAccount(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ArchiveAccount(r.Context(), app.ArchiveAccountCommand{
		ActorID: actorID(r), AccountRef: r.PathValue("id"),
	})
	if err != nil {
		h.respondError(w, err)
		return
	}
	respond(w, http.StatusOK, accountViewFrom(result))
}
