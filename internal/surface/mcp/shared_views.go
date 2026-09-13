package mcp

import "github.com/anirudhgray/bodger/internal/app"

// convertedAmountView renders an app.ConvertedAmount inline: the
// converted figure plus its full ADR-0004 provenance (rate, rate date,
// source, staleness, policy) -- mirrors internal/surface/cli/balance.go's
// convertedBalanceView field for field, reproduced here rather than
// imported, since internal/surface packages never import one another
// (the same reason this package doesn't import internal/domain directly:
// docs/architecture.md §2's layer table).
type convertedAmountView struct {
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	Rate       string `json:"rate"`
	RateDate   string `json:"rate_date"`
	RateSource string `json:"rate_source"`
	Stale      bool   `json:"stale"`
	Policy     string `json:"policy"`
}

func convertedAmountViewFrom(c app.ConvertedAmount) convertedAmountView {
	return convertedAmountView{
		Amount:     c.Amount.AmountString(),
		Currency:   c.Amount.Currency(),
		Rate:       c.Rate.Value().String(),
		RateDate:   c.RateDate.String(),
		RateSource: c.RateSource,
		Stale:      c.Stale,
		Policy:     string(c.Policy),
	}
}

// unconvertedBalanceView names one account a conversion couldn't cover
// (app.UnconvertedBalance) -- listed plainly with its reason, the same
// "not silently dropped" rule internal/surface/cli/balance.go's own
// unconvertedBalanceView follows.
type unconvertedBalanceView struct {
	Account string `json:"account"`
	Reason  string `json:"reason"`
}

func unconvertedBalanceViewsFrom(u []app.UnconvertedBalance) []unconvertedBalanceView {
	views := make([]unconvertedBalanceView, 0, len(u))
	for _, b := range u {
		views = append(views, unconvertedBalanceView{Account: b.Account.Name(), Reason: b.Reason})
	}
	return views
}

// unconvertedPostingView names one posting a conversion couldn't cover
// (app.UnconvertedPosting) -- the analytics/budget-actuals counterpart of
// unconvertedBalanceView, keyed by transaction rather than account.
type unconvertedPostingView struct {
	TransactionID string `json:"transaction_id"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	Reason        string `json:"reason"`
}

func unconvertedPostingViewsFrom(u []app.UnconvertedPosting) []unconvertedPostingView {
	views := make([]unconvertedPostingView, 0, len(u))
	for _, p := range u {
		views = append(views, unconvertedPostingView{
			TransactionID: p.TransactionID,
			Amount:        p.Amount.AmountString(),
			Currency:      p.Amount.Currency(),
			Reason:        p.Reason,
		})
	}
	return views
}
