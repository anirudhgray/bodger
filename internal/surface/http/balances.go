package http

import (
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
)

// balancesView is GET /api/v1/balances' response shape: the date every
// balance below is computed as of (echoing back what "" or "today"
// resolved to — the application layer decided that, not this handler),
// and every account's balance as of that date.
type balancesView struct {
	AsOf     string        `json:"as_of" doc:"The date every balance below is computed as of." format:"date"`
	Balances []balanceView `json:"balances"`
}

func (h *handlers) getBalances(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.AccountBalances(r.Context(), app.AccountBalancesQuery{
		ActorID: actorID(),
		AsOf:    r.URL.Query().Get("as_of"),
	})
	if err != nil {
		respondError(w, err)
		return
	}

	views := make([]balanceView, 0, len(result.Balances))
	for _, b := range result.Balances {
		views = append(views, balanceViewFrom(b))
	}
	respond(w, http.StatusOK, balancesView{AsOf: result.AsOf.String(), Balances: views})
}
