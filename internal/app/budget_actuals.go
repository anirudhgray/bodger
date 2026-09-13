package app

import (
	"context"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/budgeting"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// defaultBudgetHistoryMonths is BudgetHistoryQuery.Months' default when
// left at zero -- enough for a compact trend view without a caller having
// to know a "sensible" value.
const defaultBudgetHistoryMonths = 6

// BudgetActualsQuery asks for one budget's actual-vs-plan for a single
// period. Period is any date that falls within the target month (the same
// "any date, we resolve the month" convention normalize.DateOf's callers
// use elsewhere) -- "" resolves to the current month, per data-model.md
// §10: periods are computed, not stored, so any month (including one
// nobody has looked at yet) always has a period.
type BudgetActualsQuery struct {
	ActorID  string
	BudgetID string
	Period   string
}

// BudgetLineActuals is one line's plan-vs-actual for the period a
// BudgetActualsResult covers.
type BudgetLineActuals struct {
	Line budgeting.BudgetLine
	// Budgeted is Line.AmountMinor(), wrapped as Money in the budget's own
	// currency -- carried here so a caller never has to reach back into
	// Line and re-wrap it itself.
	Budgeted money.Money
	// Actual is net spend against Line's category subtree for the period:
	// spending minus any inflow (e.g. a refund) in the same subtree, so a
	// partially refunded purchase doesn't overstate what was actually
	// spent. Postings outside the budget's own currency are converted via
	// ConvertAmount under PolicyTransactionDate (data-model.md §9: a
	// historical figure is looked up at its own transaction's date, not
	// "today") -- see BudgetActualsResult.Unconverted for what couldn't be.
	Actual money.Money
	// Remaining is Budgeted minus Actual -- negative when the line is over
	// budget, per the issue's own "remaining = budgeted - actual".
	Remaining money.Money
	// Utilisation is Actual / Budgeted. Zero when Budgeted is zero -- not
	// reachable through normal use (budgeting.NewBudgetLine already
	// requires a strictly positive amount), but guarded defensively rather
	// than dividing by zero if that invariant is ever loosened.
	Utilisation float64
}

// BudgetActualsResult is BudgetActuals' result: one BudgetLineActuals per
// line of Budget, for the calendar month [From, To].
type BudgetActualsResult struct {
	Budget   budgeting.Budget
	From, To domain.Date
	Lines    []BudgetLineActuals
	// Unconverted names every posting a line's conversion couldn't cover,
	// pooled across all of Budget's lines -- the same "report it, don't
	// drop it" contract AccountBalancesResult.Unconverted and every M5
	// analytics result already follow (ADR-0004).
	//
	// A known imprecision, currently unreachable but worth flagging for
	// whoever adds split-transaction entry (multiple postings across
	// categories on one transaction; RecordOutflowCommand doesn't expose
	// this yet, per its own doc comment): ports.TransactionFilter.CategoryID
	// matches at the *transaction* level (any posting in the subtree), so
	// once a split transaction exists it could carry an unrelated posting
	// outside the queried line's subtree in the same result set.
	// UnconvertedPosting carries no CategoryID to filter that sibling out
	// by (unlike a converted contribution, which this package does verify
	// against the category tree before summing into Actual -- see
	// categoryInSubtree). Narrow enough not to justify widening the shared
	// UnconvertedPosting type today, but worth revisiting once splits land.
	Unconverted []UnconvertedPosting
}

// BudgetActuals implements issue #243's single-period use case: for each
// of Budget's lines, sum actual spend against its category subtree over
// the resolved period, and report remaining/utilisation alongside it.
func (s *Service) BudgetActuals(ctx context.Context, q BudgetActualsQuery) (BudgetActualsResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return BudgetActualsResult{}, err
	}
	budget, err := s.Budgets.Get(ctx, q.ActorID, q.BudgetID)
	if err != nil {
		return BudgetActualsResult{}, attachField(err, "budget_id")
	}

	from, to, err := s.resolveBudgetPeriod(q.Period, budget.StartsOn())
	if err != nil {
		return BudgetActualsResult{}, err
	}

	return s.budgetActualsForPeriod(ctx, q.ActorID, budget, from, to)
}

// BudgetHistoryQuery asks for BudgetActuals repeated over Months
// consecutive calendar months ending at Period's month (Period following
// the same "any date in the target month" convention as
// BudgetActualsQuery.Period; "" resolves to the current month). Months <=
// 0 defaults to defaultBudgetHistoryMonths.
type BudgetHistoryQuery struct {
	ActorID  string
	BudgetID string
	Period   string
	Months   int
}

// BudgetHistoryResult is BudgetHistory's result: one BudgetActualsResult
// per month, ascending (oldest first) -- the order a trend chart wants to
// plot left-to-right.
type BudgetHistoryResult struct {
	Budget  budgeting.Budget
	Periods []BudgetActualsResult
}

// BudgetHistory implements issue #243's history use case: a thin repeat
// of the single-period computation over a range of months, not a new
// storage concept, per the issue's own scope note. A budget's own
// StartsOn clamps how far back this goes -- there is no period before it
// (data-model.md §10) -- so Periods may hold fewer than Months entries
// for a budget that hasn't existed that long, rather than erroring.
func (s *Service) BudgetHistory(ctx context.Context, q BudgetHistoryQuery) (BudgetHistoryResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return BudgetHistoryResult{}, err
	}
	budget, err := s.Budgets.Get(ctx, q.ActorID, q.BudgetID)
	if err != nil {
		return BudgetHistoryResult{}, attachField(err, "budget_id")
	}

	months := q.Months
	if months <= 0 {
		months = defaultBudgetHistoryMonths
	}

	ref, err := normalize.DateOf(q.Period, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return BudgetHistoryResult{}, attachField(err, "period")
	}
	startsOnFrom, _, err := monthRange(budget.StartsOn().Year(), budget.StartsOn().Month())
	if err != nil {
		return BudgetHistoryResult{}, errs.New(errs.Internal).Wrap(err)
	}

	// Walk backward from ref's month so the StartsOn clamp is a simple
	// "stop early" check, then reverse once at the end -- simpler than
	// computing how many months exist ahead of time and walking forward.
	var periods []BudgetActualsResult
	y, m := ref.Year(), ref.Month()
	for range months {
		from, to, err := monthRange(y, m)
		if err != nil {
			return BudgetHistoryResult{}, errs.New(errs.Internal).Wrap(err)
		}
		if from.Before(startsOnFrom) {
			break
		}
		result, err := s.budgetActualsForPeriod(ctx, q.ActorID, budget, from, to)
		if err != nil {
			return BudgetHistoryResult{}, err
		}
		periods = append(periods, result)
		y, m = addMonths(y, m, -1)
	}

	for i, j := 0, len(periods)-1; i < j; i, j = i+1, j-1 {
		periods[i], periods[j] = periods[j], periods[i]
	}

	return BudgetHistoryResult{Budget: budget, Periods: periods}, nil
}

// resolveBudgetPeriod resolves period (any date in the target month, ""
// meaning the current month) to that calendar month's [from, to] bounds,
// in the actor's configured timezone (via normalize.DateOf, the same
// single timezone-resolution path every other date boundary in this
// package goes through -- data-model.md §9, ADR-0005). It rejects a
// period whose month starts before startsOn's own month: data-model.md
// §10 computes a period for any month relative to a budget's StartsOn,
// never before it.
func (s *Service) resolveBudgetPeriod(period string, startsOn domain.Date) (domain.Date, domain.Date, error) {
	ref, err := normalize.DateOf(period, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return domain.Date{}, domain.Date{}, attachField(err, "period")
	}

	from, to, err := monthRange(ref.Year(), ref.Month())
	if err != nil {
		return domain.Date{}, domain.Date{}, errs.New(errs.Internal).Wrap(err)
	}
	startFrom, _, err := monthRange(startsOn.Year(), startsOn.Month())
	if err != nil {
		return domain.Date{}, domain.Date{}, errs.New(errs.Internal).Wrap(err)
	}
	if from.Before(startFrom) {
		return domain.Date{}, domain.Date{}, errs.New(errs.InvalidInput).
			Explain("This budget has no period before %s.", startFrom.String()).
			Field("period")
	}
	return from, to, nil
}

// budgetActualsForPeriod computes BudgetActualsResult for budget over the
// already-resolved, already-validated [from, to] calendar month. Shared
// by BudgetActuals (one call) and BudgetHistory (one call per month) so
// the two use cases can never compute a period's actuals two different
// ways.
func (s *Service) budgetActualsForPeriod(ctx context.Context, actorID string, budget budgeting.Budget, from, to domain.Date) (BudgetActualsResult, error) {
	categories, err := s.Categories.List(ctx, actorID)
	if err != nil {
		return BudgetActualsResult{}, err
	}
	byID := make(map[string]ledger.Category, len(categories))
	for _, c := range categories {
		byID[c.ID()] = c
	}

	opts := AnalyticsOptions{ReportingCurrency: budget.Currency(), Policy: PolicyTransactionDate}

	lines := make([]BudgetLineActuals, 0, len(budget.Lines()))
	var unconverted []UnconvertedPosting
	for _, line := range budget.Lines() {
		lineActuals, lineUnconverted, err := s.budgetLineActuals(ctx, actorID, budget.Currency(), line, from, to, opts, byID)
		if err != nil {
			return BudgetActualsResult{}, err
		}
		lines = append(lines, lineActuals)
		unconverted = append(unconverted, lineUnconverted...)
	}

	return BudgetActualsResult{Budget: budget, From: from, To: to, Lines: lines, Unconverted: unconverted}, nil
}

// budgetLineActuals computes one line's actual/remaining/utilisation.
// ports.TransactionFilter.CategoryID already restricts to transactions
// with at least one posting in line's category subtree (ADR-0009), but
// convertPostings converts *every* posting of a matching transaction --
// which would include a split transaction's postings in an unrelated
// category, if split entry existed yet (see BudgetActualsResult.Unconverted's
// doc comment). This re-checks each contribution against the category
// tree via categoryInSubtree before summing, defensively, so that once
// splits do land, a sibling posting on a different category still can't
// inflate this line's actual.
func (s *Service) budgetLineActuals(ctx context.Context, actorID, budgetCurrency string, line budgeting.BudgetLine, from, to domain.Date, opts AnalyticsOptions, byID map[string]ledger.Category) (BudgetLineActuals, []UnconvertedPosting, error) {
	filter := ports.TransactionFilter{CategoryID: line.CategoryID(), FromDate: &from, ToDate: &to}
	contributions, unconverted, err := s.convertPostings(ctx, actorID, filter, opts)
	if err != nil {
		return BudgetLineActuals{}, nil, err
	}

	var spendMinor, incomeMinor int64
	for _, c := range contributions {
		if c.CategoryID == "" || !categoryInSubtree(byID, line.CategoryID(), c.CategoryID) {
			continue
		}
		if c.Amount.IsNegative() {
			spendMinor += c.Amount.Abs().AmountMinor()
		} else {
			incomeMinor += c.Amount.AmountMinor()
		}
	}
	actualMinor := spendMinor - incomeMinor

	budgeted, err := money.NewMoney(line.AmountMinor(), budgetCurrency)
	if err != nil {
		return BudgetLineActuals{}, nil, errs.New(errs.Internal).Wrap(err)
	}
	actual, err := money.NewMoney(actualMinor, budgetCurrency)
	if err != nil {
		return BudgetLineActuals{}, nil, errs.New(errs.Internal).Wrap(err)
	}
	remaining, err := budgeted.Subtract(actual)
	if err != nil {
		return BudgetLineActuals{}, nil, errs.New(errs.Internal).Wrap(err)
	}

	var utilisation float64
	if line.AmountMinor() != 0 {
		utilisation = float64(actualMinor) / float64(line.AmountMinor())
	}

	return BudgetLineActuals{
		Line:        line,
		Budgeted:    budgeted,
		Actual:      actual,
		Remaining:   remaining,
		Utilisation: utilisation,
	}, unconverted, nil
}

// categoryInSubtree reports whether candidateID is rootID itself or a
// descendant of it in the actor's category tree -- the same
// walk-to-the-root check topLevelCategoryIndex performs, generalized to
// an arbitrary root instead of always the top-level ancestor.
func categoryInSubtree(byID map[string]ledger.Category, rootID, candidateID string) bool {
	cur, ok := byID[candidateID]
	if !ok {
		return false
	}
	for {
		if cur.ID() == rootID {
			return true
		}
		parentID, ok := cur.ParentID()
		if !ok {
			return false
		}
		cur, ok = byID[parentID]
		if !ok {
			return false
		}
	}
}
