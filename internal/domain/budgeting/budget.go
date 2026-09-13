// Package budgeting holds the pure financial model for data-model.md §10's
// budgets: a Budget is a plan, kept strictly separate from what actually
// happened. No I/O, no clock, no database — same ADR-0005 boundary
// internal/domain/ledger and internal/domain/importing hold to: anything
// that needs a repository lookup or "now" (period resolution, actuals)
// belongs to internal/app, in a later issue.
package budgeting

import (
	"fmt"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

// PeriodType is the cycle a Budget's lines repeat on. Only
// PeriodTypeMonthly exists today — data-model.md §10: "period_type
// (monthly initially)" and §13 defers custom periods to a later milestone.
type PeriodType string

// PeriodTypeMonthly is the only PeriodType a Budget can be constructed
// with today.
const PeriodTypeMonthly PeriodType = "monthly"

var validPeriodTypes = map[PeriodType]bool{
	PeriodTypeMonthly: true,
}

// Budget is a plan for a set of categories, kept strictly separate from
// what actually happened (data-model.md §10). Its fields are unexported:
// the only way to produce one is NewBudget. It owns its BudgetLines as a
// single aggregate — the same shape ledger.Transaction embeds []Posting —
// since a line has no independent existence apart from its budget.
type Budget struct {
	id         string
	userID     string
	name       string
	periodType PeriodType
	currency   string
	startsOn   domain.Date
	archivedAt *domain.Date
	lines      []BudgetLine
}

// BudgetOption sets one of a Budget's optional fields at construction
// time. See WithArchivedAt.
type BudgetOption func(*Budget)

// WithArchivedAt reconstructs a Budget that was already archived. This
// exists for the sqlite adapter to restore a budget from its
// already-validated, already-persisted archived_at column — the same
// restore-not-re-derive shape as importing.WithStatus — not a way for
// application code to archive a budget by construction; the actual archive
// use case is a separate, later issue (#242).
func WithArchivedAt(archivedAt domain.Date) BudgetOption {
	return func(b *Budget) {
		d := archivedAt
		b.archivedAt = &d
	}
}

// NewBudget constructs a Budget. lines must target distinct categories —
// two lines planning the same category within one budget would leave
// "the" actual/remaining for that category ambiguous, so it's rejected at
// construction rather than left for a caller to resolve later.
func NewBudget(id, userID, name string, periodType PeriodType, currency string, startsOn domain.Date, lines []BudgetLine, opts ...BudgetOption) (Budget, error) {
	if id == "" {
		return Budget{}, ErrBudgetEmptyID
	}
	if userID == "" {
		return Budget{}, ErrBudgetEmptyUserID
	}
	if name == "" {
		return Budget{}, ErrBudgetEmptyName
	}
	if !validPeriodTypes[periodType] {
		return Budget{}, fmt.Errorf("%w: %q", ErrBudgetInvalidPeriodType, periodType)
	}
	if _, ok := money.LookupCurrency(currency); !ok {
		return Budget{}, fmt.Errorf("%w: %q", ErrBudgetInvalidCurrency, currency)
	}

	cp := make([]BudgetLine, len(lines))
	copy(cp, lines)

	seenCategories := make(map[string]bool, len(cp))
	for _, l := range cp {
		if seenCategories[l.CategoryID()] {
			return Budget{}, fmt.Errorf("%w: %q", ErrBudgetDuplicateCategoryLine, l.CategoryID())
		}
		seenCategories[l.CategoryID()] = true
	}

	b := Budget{
		id:         id,
		userID:     userID,
		name:       name,
		periodType: periodType,
		currency:   currency,
		startsOn:   startsOn,
		lines:      cp,
	}
	for _, opt := range opts {
		opt(&b)
	}
	return b, nil
}

// ID returns the budget's identifier.
func (b Budget) ID() string { return b.id }

// UserID returns the ID of the user who owns the budget.
func (b Budget) UserID() string { return b.userID }

// Name returns the budget's user-facing name.
func (b Budget) Name() string { return b.name }

// PeriodType returns the cycle the budget's lines repeat on.
func (b Budget) PeriodType() PeriodType { return b.periodType }

// Currency returns the budget's currency. Every BudgetLine's AmountMinor
// is denominated in this currency.
func (b Budget) Currency() string { return b.currency }

// StartsOn returns the date the budget's periods are computed from —
// data-model.md §10: periods are computed, not stored, so a period for
// any month exists relative to this date whether or not anyone has looked
// at it yet.
func (b Budget) StartsOn() domain.Date { return b.startsOn }

// ArchivedAt returns the instant the budget was archived, and false if it
// is active. Mirrors ledger.Account.ArchivedAt's shape.
func (b Budget) ArchivedAt() (domain.Date, bool) {
	if b.archivedAt == nil {
		return domain.Date{}, false
	}
	return *b.archivedAt, true
}

// Archived reports whether the budget is currently archived — a
// convenience over ArchivedAt for callers that only care about the
// boolean state.
func (b Budget) Archived() bool { return b.archivedAt != nil }

// Lines returns a copy of the budget's lines — mutating the returned slice
// never affects b.
func (b Budget) Lines() []BudgetLine {
	cp := make([]BudgetLine, len(b.lines))
	copy(cp, b.lines)
	return cp
}
