package budgeting

import "errors"

// Named errors returned by this package. Callers should match on these with
// errors.Is; the wrapped text (added by the constructor that returns them)
// carries the offending value for humans, the sentinel carries the identity
// for code.
var (
	// ErrBudgetEmptyID is returned when a budget is constructed with an
	// empty ID.
	ErrBudgetEmptyID = errors.New("budgeting: budget id must not be empty")

	// ErrBudgetEmptyUserID is returned when a budget is constructed with an
	// empty user ID.
	ErrBudgetEmptyUserID = errors.New("budgeting: budget user id must not be empty")

	// ErrBudgetEmptyName is returned when a budget is constructed with an
	// empty name.
	ErrBudgetEmptyName = errors.New("budgeting: budget name must not be empty")

	// ErrBudgetInvalidPeriodType is returned when a budget is constructed
	// with a period type outside the known set (only PeriodTypeMonthly, for
	// now — data-model.md §10's "monthly initially").
	ErrBudgetInvalidPeriodType = errors.New("budgeting: invalid budget period type")

	// ErrBudgetInvalidCurrency is returned when a budget is constructed
	// with a currency code that isn't recognised reference data.
	ErrBudgetInvalidCurrency = errors.New("budgeting: invalid budget currency")

	// ErrBudgetDuplicateCategoryLine is returned when a budget is
	// constructed with more than one line targeting the same category.
	ErrBudgetDuplicateCategoryLine = errors.New("budgeting: budget has more than one line for the same category")

	// ErrBudgetLineEmptyID is returned when a budget line is constructed
	// with an empty ID.
	ErrBudgetLineEmptyID = errors.New("budgeting: budget line id must not be empty")

	// ErrBudgetLineEmptyBudgetID is returned when a budget line is
	// constructed with an empty budget ID.
	ErrBudgetLineEmptyBudgetID = errors.New("budgeting: budget line budget id must not be empty")

	// ErrBudgetLineEmptyCategoryID is returned when a budget line is
	// constructed with an empty category ID.
	ErrBudgetLineEmptyCategoryID = errors.New("budgeting: budget line category id must not be empty")

	// ErrBudgetLineAmountNotPositive is returned when a budget line is
	// constructed with an amount_minor that isn't strictly positive.
	ErrBudgetLineAmountNotPositive = errors.New("budgeting: budget line amount_minor must be positive")
)
