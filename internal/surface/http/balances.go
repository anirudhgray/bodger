package http

import (
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
)

// unconvertedBalanceView names an account the request's currency/policy
// conversion couldn't cover (AccountBalancesResult.Unconverted) — listed
// plainly with its reason rather than silently dropped from the response.
// Mirrors internal/surface/cli/balance.go's unconvertedBalanceView
// field-for-field.
type unconvertedBalanceView struct {
	Account string `json:"account"`
	Reason  string `json:"reason"`
}

// balancesView is GET /api/v1/balances' response shape: the date every
// balance below is computed as of (echoing back what "" or "today"
// resolved to — the application layer decided that, not this handler),
// every account's balance as of that date, and (only when the request set
// a target currency) any accounts the conversion couldn't cover.
type balancesView struct {
	AsOf        string                   `json:"as_of" doc:"The date every balance below is computed as of." format:"date"`
	Balances    []balanceView            `json:"balances"`
	Unconverted []unconvertedBalanceView `json:"unconverted,omitempty"`
}

func (h *handlers) getBalances(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.svc.AccountBalances(r.Context(), app.AccountBalancesQuery{
		ActorID:        actorID(r),
		AsOf:           q.Get("as_of"),
		TargetCurrency: q.Get("currency"),
		Policy:         app.ConversionPolicy(q.Get("policy")),
		PinnedDate:     q.Get("pinned_date"),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	views := make([]balanceView, 0, len(result.Balances))
	for _, b := range result.Balances {
		views = append(views, balanceViewFrom(b))
	}
	view := balancesView{AsOf: result.AsOf.String(), Balances: views}
	for _, u := range result.Unconverted {
		view.Unconverted = append(view.Unconverted, unconvertedBalanceView{
			Account: u.Account.Name(),
			Reason:  u.Reason,
		})
	}
	respond(w, http.StatusOK, view)
}
