package http

import "github.com/anirudhgray/bodger/internal/app"

// accountView is the JSON shape of an account. currency and every money
// field are strings (ADR-0004: never a JSON number). opening_balance
// carries no separate currency field of its own — it's always in
// currency, the same way domain.Account's own OpeningBalance() is always
// in Currency() — repeating it would only be a second place for the two
// to disagree.
type accountView struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	Currency           string `json:"currency"`
	OpeningBalance     string `json:"opening_balance"`
	OpeningBalanceDate string `json:"opening_balance_date,omitempty"`
	Institution        string `json:"institution,omitempty"`
	SortOrder          int    `json:"sort_order"`
	Archived           bool   `json:"archived"`
	ArchivedAt         string `json:"archived_at,omitempty"`
}

func accountViewFrom(r app.AccountResult) accountView {
	a := r.Account
	v := accountView{
		ID:             a.ID(),
		Name:           a.Name(),
		Type:           string(a.Kind()),
		Currency:       a.Currency(),
		OpeningBalance: a.OpeningBalance().AmountString(),
		SortOrder:      a.SortOrder(),
		Archived:       a.Archived(),
	}
	if d, ok := a.OpeningBalanceDate(); ok {
		v.OpeningBalanceDate = d.String()
	}
	if inst, ok := a.Institution(); ok {
		v.Institution = inst
	}
	if d, ok := a.ArchivedAt(); ok {
		v.ArchivedAt = d.String()
	}
	return v
}

// categoryView is the JSON shape of a category.
type categoryView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ParentID   string `json:"parent_id,omitempty"`
	SortOrder  int    `json:"sort_order"`
	Archived   bool   `json:"archived"`
	ArchivedAt string `json:"archived_at,omitempty"`
}

func categoryViewFrom(r app.CategoryResult) categoryView {
	c := r.Category
	v := categoryView{
		ID:        c.ID(),
		Name:      c.Name(),
		Type:      string(c.Kind()),
		SortOrder: c.SortOrder(),
		Archived:  c.Archived(),
	}
	if pid, ok := c.ParentID(); ok {
		v.ParentID = pid
	}
	if d, ok := c.ArchivedAt(); ok {
		v.ArchivedAt = d.String()
	}
	return v
}

// transactionView is the JSON shape of a transaction. There is no
// "postings" concept exposed here (docs/ux-principles.md §2 bans that
// word, and the concept, from every user-facing string) — an outflow or
// inflow always has exactly one account and (optionally) one category in
// M1 (split entry is out of scope, per the app layer's own commands), and
// a transfer always has exactly two accounts and no category, so this
// exposes those directly, the same way internal/surface/cli's entryView
// does. account_id/category_id (outflow, inflow) and
// from_account_id/to_account_id (transfer) are populated depending on
// type; the fields that don't apply to a given transaction's type are
// simply omitted.
type transactionView struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Date          string   `json:"date"`
	Description   string   `json:"description"`
	Notes         string   `json:"notes,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	AccountID     string   `json:"account_id,omitempty"`
	CategoryID    string   `json:"category_id,omitempty"`
	FromAccountID string   `json:"from_account_id,omitempty"`
	ToAccountID   string   `json:"to_account_id,omitempty"`
	Amount        string   `json:"amount"`
	Currency      string   `json:"currency"`
}

func transactionViewFrom(r app.TransactionResult) transactionView {
	t := r.Transaction
	v := transactionView{
		ID:          t.ID(),
		Type:        string(t.Kind()),
		Date:        t.BookedDate().String(),
		Description: t.Description(),
		Notes:       t.Notes(),
	}
	for _, tag := range r.Tags {
		v.Tags = append(v.Tags, tag.String())
	}

	postings := t.Postings()
	switch len(postings) {
	case 1:
		p := postings[0]
		v.AccountID = p.AccountID()
		if cid, ok := p.CategoryID(); ok {
			v.CategoryID = cid
		}
		v.Amount = p.Amount().Abs().AmountString()
		v.Currency = p.Currency()
	case 2:
		// A transfer: data-model.md §14 guarantees postings[0] is the
		// negative (from) leg and postings[1] the positive (to) leg —
		// that order is set once, in internal/app.buildTransferPostings,
		// and never reshuffled afterwards.
		from, to := postings[0], postings[1]
		v.FromAccountID = from.AccountID()
		v.ToAccountID = to.AccountID()
		v.Amount = to.Amount().Abs().AmountString()
		v.Currency = to.Currency()
	}
	return v
}

// balanceView pairs one account with its computed balance as of the
// query's as-of date (app.AccountBalancesResult already carries both, in
// one app call — no second lookup needed to render the account's name
// here).
type balanceView struct {
	AccountID string `json:"account_id"`
	Account   string `json:"account"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
}

func balanceViewFrom(b app.AccountBalance) balanceView {
	return balanceView{
		AccountID: b.Account.ID(),
		Account:   b.Account.Name(),
		Amount:    b.Balance.AmountString(),
		Currency:  b.Balance.Currency(),
	}
}
