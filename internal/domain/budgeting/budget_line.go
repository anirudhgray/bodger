package budgeting

// BudgetLine is one category's planned amount within a Budget, for one
// period-type cycle — data-model.md §10: `Budget` -> `BudgetLine` is a
// one-to-many aggregate, and a line has no independent existence apart
// from its budget (it is never fetched or persisted on its own). Its
// fields are unexported: the only way to produce one is NewBudgetLine.
//
// There is no currency field here: a line's amount is always in its
// budget's currency, the same reasoning ledger.Account applies to keep a
// single source of truth for currency rather than a second field that
// could disagree with the first.
type BudgetLine struct {
	id          string
	budgetID    string
	categoryID  string
	amountMinor int64
	rollover    bool
}

// NewBudgetLine constructs a BudgetLine. amountMinor must be strictly
// positive — a budget line plans to spend or receive something, and a zero
// or negative plan has no meaning here (data-model.md §10 doesn't model a
// negative or zero budget line).
//
// rollover defaults to false; data-model.md §10 and §13 explain the column
// exists so a later milestone can implement rollover behaviour without a
// migration, but nothing reads or interprets it yet.
func NewBudgetLine(id, budgetID, categoryID string, amountMinor int64, rollover bool) (BudgetLine, error) {
	if id == "" {
		return BudgetLine{}, ErrBudgetLineEmptyID
	}
	if budgetID == "" {
		return BudgetLine{}, ErrBudgetLineEmptyBudgetID
	}
	if categoryID == "" {
		return BudgetLine{}, ErrBudgetLineEmptyCategoryID
	}
	if amountMinor <= 0 {
		return BudgetLine{}, ErrBudgetLineAmountNotPositive
	}

	return BudgetLine{
		id:          id,
		budgetID:    budgetID,
		categoryID:  categoryID,
		amountMinor: amountMinor,
		rollover:    rollover,
	}, nil
}

// ID returns the line's identifier.
func (l BudgetLine) ID() string { return l.id }

// BudgetID returns the ID of the budget this line belongs to.
func (l BudgetLine) BudgetID() string { return l.budgetID }

// CategoryID returns the category this line plans an amount for.
func (l BudgetLine) CategoryID() string { return l.categoryID }

// AmountMinor returns the line's planned amount, in the owning budget's
// currency's minor units.
func (l BudgetLine) AmountMinor() int64 { return l.amountMinor }

// Rollover reports whether an unspent (or overspent) amount is meant to
// carry into the next period. Reserved for a later milestone — see
// data-model.md §13; nothing computes actuals against this yet.
func (l BudgetLine) Rollover() bool { return l.rollover }
