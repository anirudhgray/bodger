package http

import (
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
)

// accountKindTotalView is one ledger.AccountKind's balances, summed and
// converted into the reporting currency (app.AccountKindTotal).
type accountKindTotalView struct {
	Kind   string `json:"kind" enum:"account_kind"`
	Amount string `json:"amount" format:"money"`
}

// currencyTotalView is every account in one currency, summed in that
// currency — raw, unconverted (app.CurrencyTotal).
type currencyTotalView struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount" format:"money"`
}

// balanceTotalsView is GET /api/v1/balances/totals' response shape: the
// date every total below is computed as of, the reporting currency Overall
// and ByCategory are expressed in, the overall net balance, the
// per-category and per-currency breakdowns, and (only for accounts the
// reporting-currency conversion couldn't cover) the shortfall list — the
// same "report it, don't drop it" contract balancesView.Unconverted
// already follows.
type balanceTotalsView struct {
	AsOf        string                   `json:"as_of" doc:"The date every total below is computed as of." format:"date"`
	Currency    string                   `json:"currency" doc:"The reporting currency \"overall\" and \"by_category\" are expressed in."`
	Overall     string                   `json:"overall" format:"money"`
	ByCategory  []accountKindTotalView   `json:"by_category"`
	ByCurrency  []currencyTotalView      `json:"by_currency"`
	Unconverted []unconvertedBalanceView `json:"unconverted,omitempty"`
}

func balanceTotalsViewFrom(r app.BalanceTotalsResult) balanceTotalsView {
	v := balanceTotalsView{
		AsOf:     r.AsOf.String(),
		Currency: r.Options.ReportingCurrency,
		Overall:  r.Overall.AmountString(),
	}
	for _, c := range r.ByCategory {
		v.ByCategory = append(v.ByCategory, accountKindTotalView{
			Kind:   string(c.Kind),
			Amount: c.Total.AmountString(),
		})
	}
	for _, c := range r.ByCurrency {
		v.ByCurrency = append(v.ByCurrency, currencyTotalView{
			Currency: c.Currency,
			Amount:   c.Total.AmountString(),
		})
	}
	for _, u := range r.Unconverted {
		v.Unconverted = append(v.Unconverted, unconvertedBalanceView{
			Account: u.Account.Name(),
			Reason:  u.Reason,
		})
	}
	return v
}

// getBalanceTotals handles GET /api/v1/balances/totals: issue #195's
// totals-overview use case. "currency" is optional here (unlike
// report's own conversion options, which require it) — left unset, it
// resolves through ADR-0004's ladder server-side
// (Service.resolveBalanceTotalsCurrency), the same "no client-supplied
// currency required" contract issue #199 established for the analytics
// endpoints. "policy" is required: Overall and ByCategory are always
// converted aggregates, never left unconverted the way plain
// GET /api/v1/balances' per-account figures can be.
func (h *handlers) getBalanceTotals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.svc.BalanceTotals(r.Context(), app.BalanceTotalsQuery{
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
	respond(w, http.StatusOK, balanceTotalsViewFrom(result))
}
