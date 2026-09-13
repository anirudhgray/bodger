package app

import (
	"context"
	"errors"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain/budgeting"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

const maxBudgetNameLen = 200

// BudgetResult wraps the Budget a create/update/archive/line-management
// use case produced or affected.
type BudgetResult struct {
	Budget budgeting.Budget
}

// BudgetLineInput is one line of a budget as a use case command sees it —
// a category reference (resolved the same way every other use case
// resolves one, via resolveOwnedCategory) and a raw amount string
// (normalize.Amount, denominated in the budget's own currency — a line has
// no currency of its own, per budgeting.BudgetLine's doc comment).
type BudgetLineInput struct {
	CategoryRef string
	Amount      string
	Rollover    bool
}

// CreateBudgetCommand creates a new budget with its initial lines in one
// call — data-model.md §10's Budget/BudgetLine aggregate has no
// independent existence for a line apart from its budget, so there is no
// "create an empty budget, add lines later" split forced on a caller
// (though AddBudgetLine below supports doing exactly that if a caller
// wants to). PeriodType is always monthly — data-model.md §10: "period_type
// (monthly initially)" — so it isn't a command field yet; a later
// milestone that adds a second period type will add one then. Currency
// walks the same precedence ladder CreateAccount uses (entry -> the
// actor's reporting currency -> instance default): a budget has no
// "account" rung, since it isn't attached to one. StartsOn defaults to
// today when empty, per normalize.DateOf's own convention.
type CreateBudgetCommand struct {
	ActorID  string
	Name     string
	Currency string
	StartsOn string
	Lines    []BudgetLineInput
}

// CreateBudget implements the "create" use case in issue #242's budgets
// scope.
func (s *Service) CreateBudget(ctx context.Context, cmd CreateBudgetCommand) (BudgetResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return BudgetResult{}, err
	}

	name, err := normalize.Text(cmd.Name, maxBudgetNameLen)
	if err != nil {
		return BudgetResult{}, attachField(err, "name")
	}
	if name == "" {
		return BudgetResult{}, errs.New(errs.InvalidInput).Explain("A name is required.").Field("name")
	}

	reportingCurrency, err := s.resolveReportingCurrency(ctx, cmd.ActorID)
	if err != nil {
		return BudgetResult{}, err
	}
	currency, err := normalize.Currency(cmd.Currency, "", reportingCurrency, s.Config.DefaultCurrency)
	if err != nil {
		return BudgetResult{}, err
	}

	startsOn, err := normalize.DateOf(cmd.StartsOn, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return BudgetResult{}, attachField(err, "starts_on")
	}

	budgetID := s.IDs.NewID()
	lines, err := s.buildBudgetLines(ctx, cmd.ActorID, budgetID, currency, cmd.Lines)
	if err != nil {
		return BudgetResult{}, err
	}

	budget, err := budgeting.NewBudget(budgetID, cmd.ActorID, name, budgeting.PeriodTypeMonthly, currency, startsOn, lines)
	if err != nil {
		return BudgetResult{}, wrapBudgetingError(err)
	}

	if err := s.Budgets.Create(ctx, cmd.ActorID, budget); err != nil {
		return BudgetResult{}, err
	}
	return BudgetResult{Budget: budget}, nil
}

// buildBudgetLines resolves and validates a batch of BudgetLineInputs into
// domain BudgetLines, denominated in currency (the owning budget's
// currency — a line has no currency of its own).
func (s *Service) buildBudgetLines(ctx context.Context, actorID, budgetID, currency string, inputs []BudgetLineInput) ([]budgeting.BudgetLine, error) {
	lines := make([]budgeting.BudgetLine, 0, len(inputs))
	for _, in := range inputs {
		line, err := s.buildBudgetLine(ctx, actorID, budgetID, currency, in)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// buildBudgetLine resolves a single BudgetLineInput's category reference
// and amount, then constructs the domain BudgetLine. The duplicate-
// category check across a budget's lines is left to budgeting.NewBudget
// itself (ErrBudgetDuplicateCategoryLine, translated by wrapBudgetingError)
// rather than duplicated here, since it needs the full set of lines to
// detect — this only builds one line in isolation.
func (s *Service) buildBudgetLine(ctx context.Context, actorID, budgetID, currency string, in BudgetLineInput) (budgeting.BudgetLine, error) {
	category, err := s.resolveOwnedCategory(ctx, actorID, in.CategoryRef)
	if err != nil {
		return budgeting.BudgetLine{}, attachField(err, "category_ref")
	}

	amountMinor, err := normalize.Amount(in.Amount, currency)
	if err != nil {
		return budgeting.BudgetLine{}, attachField(err, "amount")
	}

	line, err := budgeting.NewBudgetLine(s.IDs.NewID(), budgetID, category.ID(), amountMinor, in.Rollover)
	if err != nil {
		return budgeting.BudgetLine{}, wrapBudgetingError(err)
	}
	return line, nil
}

// UpdateBudgetCommand updates a budget's name and starts_on — its
// currency and period type are fixed at creation (data-model.md §10 gives
// no workflow for changing either, the same reasoning
// SetOpeningBalance's doc comment gives for an account's fixed currency),
// and its lines are managed separately via AddBudgetLine/UpdateBudgetLine/
// RemoveBudgetLine. Unlike an account or category, a budget is looked up
// by ID directly rather than resolveOwnedAccount/resolveOwnedCategory's
// ref (UUID-or-unique-name) resolution: data-model.md §10 establishes no
// name-uniqueness constraint for a budget's Name, so there is no
// candidate set a name could unambiguously resolve against.
type UpdateBudgetCommand struct {
	ActorID  string
	BudgetID string
	Name     string
	StartsOn string
}

// UpdateBudget implements the "update" use case in issue #242's budgets
// scope.
func (s *Service) UpdateBudget(ctx context.Context, cmd UpdateBudgetCommand) (BudgetResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return BudgetResult{}, err
	}
	existing, err := s.Budgets.Get(ctx, cmd.ActorID, cmd.BudgetID)
	if err != nil {
		return BudgetResult{}, attachField(err, "budget_id")
	}

	name, err := normalize.Text(cmd.Name, maxBudgetNameLen)
	if err != nil {
		return BudgetResult{}, attachField(err, "name")
	}
	if name == "" {
		return BudgetResult{}, errs.New(errs.InvalidInput).Explain("A name is required.").Field("name")
	}

	startsOn, err := normalize.DateOf(cmd.StartsOn, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return BudgetResult{}, attachField(err, "starts_on")
	}

	updated, err := budgeting.NewBudget(
		existing.ID(), existing.UserID(), name, existing.PeriodType(), existing.Currency(), startsOn, existing.Lines(),
		budgetArchivedAtOptions(existing)...,
	)
	if err != nil {
		return BudgetResult{}, wrapBudgetingError(err)
	}

	if err := s.Budgets.Update(ctx, cmd.ActorID, updated); err != nil {
		return BudgetResult{}, err
	}
	return BudgetResult{Budget: updated}, nil
}

// ArchiveBudgetCommand stops a budget from appearing in current listings
// and creation flows going forward, while leaving it and its lines fully
// queryable — a budget stopped in July shouldn't make June's already-
// computed actuals disappear (the same "hides from pickers, keeps
// history" contract ArchiveAccountCommand's doc comment describes for
// accounts). Issue #243 (actuals & reporting) reads Get/List exactly the
// same way for an archived budget as an active one; nothing here deletes
// or hides a BudgetLine.
type ArchiveBudgetCommand struct {
	ActorID  string
	BudgetID string
}

// ArchiveBudget implements the "archive" use case in issue #242's budgets
// scope. Archiving an already-archived budget is a no-op that returns its
// current state unchanged, the same idempotency ArchiveAccount gives —
// a retried archive call shouldn't fail, and re-stamping today's date
// over the budget's real archive date would lose information nobody
// asked to change.
func (s *Service) ArchiveBudget(ctx context.Context, cmd ArchiveBudgetCommand) (BudgetResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return BudgetResult{}, err
	}
	existing, err := s.Budgets.Get(ctx, cmd.ActorID, cmd.BudgetID)
	if err != nil {
		return BudgetResult{}, attachField(err, "budget_id")
	}
	if existing.Archived() {
		return BudgetResult{Budget: existing}, nil
	}

	today, err := normalize.DateOf("", s.Clock, s.Config.UserTimezone)
	if err != nil {
		return BudgetResult{}, err
	}

	updated, err := budgeting.NewBudget(
		existing.ID(), existing.UserID(), existing.Name(), existing.PeriodType(), existing.Currency(), existing.StartsOn(), existing.Lines(),
		budgeting.WithArchivedAt(today),
	)
	if err != nil {
		return BudgetResult{}, wrapBudgetingError(err)
	}

	if err := s.Budgets.Update(ctx, cmd.ActorID, updated); err != nil {
		return BudgetResult{}, err
	}
	return BudgetResult{Budget: updated}, nil
}

// GetBudgetQuery fetches a single budget (with its lines) by ID, scoped to
// the actor the same way every other budget use case is (ADR-0006). Not
// explicitly named in issue #242's own scope bullet list, but CRUD's "R"
// is needed by any surface that will ever show a budget back to a user
// (#244/#245) — the same gap GetAccount/ListAccounts fill for accounts.
type GetBudgetQuery struct {
	ActorID  string
	BudgetID string
}

// GetBudget implements the "fetch one" read this issue's CRUD needs.
func (s *Service) GetBudget(ctx context.Context, q GetBudgetQuery) (BudgetResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return BudgetResult{}, err
	}
	budget, err := s.Budgets.Get(ctx, q.ActorID, q.BudgetID)
	if err != nil {
		return BudgetResult{}, attachField(err, "budget_id")
	}
	return BudgetResult{Budget: budget}, nil
}

// ListBudgetsQuery lists every budget actorID owns, archived and active
// alike — filtering to "current" is a surface/UI concern (the same as
// ListAccounts returning archived accounts too), not a repository-level
// one.
type ListBudgetsQuery struct {
	ActorID string
}

// ListBudgetsResult is ListBudgets' result, in the repository's
// most-recently-created-first order.
type ListBudgetsResult struct {
	Budgets []budgeting.Budget
}

// ListBudgets implements the "list" read this issue's CRUD needs.
func (s *Service) ListBudgets(ctx context.Context, q ListBudgetsQuery) (ListBudgetsResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ListBudgetsResult{}, err
	}
	budgets, err := s.Budgets.List(ctx, q.ActorID)
	if err != nil {
		return ListBudgetsResult{}, err
	}
	return ListBudgetsResult{Budgets: budgets}, nil
}

// AddBudgetLineCommand adds one new line to an existing budget.
type AddBudgetLineCommand struct {
	ActorID     string
	BudgetID    string
	CategoryRef string
	Amount      string
	Rollover    bool
}

// AddBudgetLine implements the "add a line" use case in issue #242's
// budgets scope.
func (s *Service) AddBudgetLine(ctx context.Context, cmd AddBudgetLineCommand) (BudgetResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return BudgetResult{}, err
	}
	existing, err := s.Budgets.Get(ctx, cmd.ActorID, cmd.BudgetID)
	if err != nil {
		return BudgetResult{}, attachField(err, "budget_id")
	}

	line, err := s.buildBudgetLine(ctx, cmd.ActorID, existing.ID(), existing.Currency(), BudgetLineInput{
		CategoryRef: cmd.CategoryRef,
		Amount:      cmd.Amount,
		Rollover:    cmd.Rollover,
	})
	if err != nil {
		return BudgetResult{}, err
	}

	updated, err := budgeting.NewBudget(
		existing.ID(), existing.UserID(), existing.Name(), existing.PeriodType(), existing.Currency(), existing.StartsOn(),
		append(existing.Lines(), line), budgetArchivedAtOptions(existing)...,
	)
	if err != nil {
		return BudgetResult{}, wrapBudgetingError(err)
	}

	if err := s.Budgets.Update(ctx, cmd.ActorID, updated); err != nil {
		return BudgetResult{}, err
	}
	return BudgetResult{Budget: updated}, nil
}

// UpdateBudgetLineCommand updates an existing line's amount and rollover
// flag. Its category is fixed once created — changing which category a
// line targets is indistinguishable from removing one line and adding
// another, so RemoveBudgetLine + AddBudgetLine is that operation rather
// than a third way to express it here.
type UpdateBudgetLineCommand struct {
	ActorID  string
	BudgetID string
	LineID   string
	Amount   string
	Rollover bool
}

// UpdateBudgetLine implements the "update a line" use case in issue
// #242's budgets scope.
func (s *Service) UpdateBudgetLine(ctx context.Context, cmd UpdateBudgetLineCommand) (BudgetResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return BudgetResult{}, err
	}
	existing, err := s.Budgets.Get(ctx, cmd.ActorID, cmd.BudgetID)
	if err != nil {
		return BudgetResult{}, attachField(err, "budget_id")
	}

	lines := existing.Lines()
	idx, err := findBudgetLineIndex(lines, cmd.LineID)
	if err != nil {
		return BudgetResult{}, err
	}

	amountMinor, err := normalize.Amount(cmd.Amount, existing.Currency())
	if err != nil {
		return BudgetResult{}, attachField(err, "amount")
	}

	updatedLine, err := budgeting.NewBudgetLine(lines[idx].ID(), existing.ID(), lines[idx].CategoryID(), amountMinor, cmd.Rollover)
	if err != nil {
		return BudgetResult{}, wrapBudgetingError(err)
	}
	lines[idx] = updatedLine

	updated, err := budgeting.NewBudget(
		existing.ID(), existing.UserID(), existing.Name(), existing.PeriodType(), existing.Currency(), existing.StartsOn(),
		lines, budgetArchivedAtOptions(existing)...,
	)
	if err != nil {
		return BudgetResult{}, wrapBudgetingError(err)
	}

	if err := s.Budgets.Update(ctx, cmd.ActorID, updated); err != nil {
		return BudgetResult{}, err
	}
	return BudgetResult{Budget: updated}, nil
}

// RemoveBudgetLineCommand removes one line from an existing budget.
// Removing a budget's last remaining line is allowed — budgeting.NewBudget
// places no minimum on how many lines a Budget has.
type RemoveBudgetLineCommand struct {
	ActorID  string
	BudgetID string
	LineID   string
}

// RemoveBudgetLine implements the "remove a line" use case in issue
// #242's budgets scope.
func (s *Service) RemoveBudgetLine(ctx context.Context, cmd RemoveBudgetLineCommand) (BudgetResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return BudgetResult{}, err
	}
	existing, err := s.Budgets.Get(ctx, cmd.ActorID, cmd.BudgetID)
	if err != nil {
		return BudgetResult{}, attachField(err, "budget_id")
	}

	lines := existing.Lines()
	idx, err := findBudgetLineIndex(lines, cmd.LineID)
	if err != nil {
		return BudgetResult{}, err
	}
	remaining := append(lines[:idx], lines[idx+1:]...)

	updated, err := budgeting.NewBudget(
		existing.ID(), existing.UserID(), existing.Name(), existing.PeriodType(), existing.Currency(), existing.StartsOn(),
		remaining, budgetArchivedAtOptions(existing)...,
	)
	if err != nil {
		return BudgetResult{}, wrapBudgetingError(err)
	}

	if err := s.Budgets.Update(ctx, cmd.ActorID, updated); err != nil {
		return BudgetResult{}, err
	}
	return BudgetResult{Budget: updated}, nil
}

// BudgetLineAmount pairs line's planned amount with currency — always the
// owning Budget's own Currency() — as a money.Money a surface can render,
// since a BudgetLine stores no currency of its own (see BudgetLine's doc
// comment) and internal/surface/cli is barred from importing
// internal/domain/money to build one itself (docs/architecture.md §3).
// Returns a *errs.Error with code Internal if currency is invalid —
// unreachable in practice, since every Budget already validates its own
// currency at construction (budgeting.NewBudget).
func BudgetLineAmount(currency string, line budgeting.BudgetLine) (money.Money, error) {
	m, err := money.NewMoney(line.AmountMinor(), currency)
	if err != nil {
		return money.Money{}, errs.New(errs.Internal).Wrap(err)
	}
	return m, nil
}

// findBudgetLineIndex returns the index of the line identified by lineID
// within lines, or a *errs.Error with code NotFound if no line matches —
// a line is looked up by ID directly (never by category or position),
// since that's the one identifier a surface holds after listing a
// budget's lines back to the user.
func findBudgetLineIndex(lines []budgeting.BudgetLine, lineID string) (int, error) {
	for i, l := range lines {
		if l.ID() == lineID {
			return i, nil
		}
	}
	return 0, errs.New(errs.NotFound).Explain("No budget line matches %q.", lineID).Field("line_id")
}

// budgetArchivedAtOptions carries b's archived state forward into a
// reconstructed Budget — the same reconstruction-not-re-derivation shape
// accountArchivedAtPtr gives ledger.NewAccount, adapted to
// budgeting.NewBudget's functional-option shape (BudgetOption) rather
// than a plain pointer parameter.
func budgetArchivedAtOptions(b budgeting.Budget) []budgeting.BudgetOption {
	if d, ok := b.ArchivedAt(); ok {
		return []budgeting.BudgetOption{budgeting.WithArchivedAt(d)}
	}
	return nil
}

// wrapBudgetingError translates internal/domain/budgeting's sentinel
// errors into user-safe *errs.Error values, the same role wrapLedgerError
// and wrapTransferError play for internal/domain/ledger. By the time this
// package calls budgeting.NewBudget/NewBudgetLine, every use case above
// has already validated name, currency, and amount itself — in practice
// only ErrBudgetDuplicateCategoryLine is reachable from genuine user
// input (AddBudgetLine targeting a category the budget already has a
// line for), but every sentinel is handled so this stays correct if that
// ever changes.
func wrapBudgetingError(err error) error {
	if err == nil {
		return nil
	}
	var e *errs.Error
	if errors.As(err, &e) {
		return e
	}
	switch {
	case errors.Is(err, budgeting.ErrBudgetDuplicateCategoryLine):
		return errs.New(errs.InvalidInput).
			Explain("This budget already has a line for that category.").
			Field("category_ref")
	case errors.Is(err, budgeting.ErrBudgetLineAmountNotPositive):
		return errs.New(errs.InvalidInput).
			Explain("A budget line's amount must be greater than zero.").
			Field("amount")
	default:
		// ErrBudgetEmptyID/EmptyUserID/EmptyName/InvalidPeriodType/
		// InvalidCurrency and the BudgetLine empty-field sentinels are
		// all defensive here: every use case above already validated
		// the corresponding input via normalize.Text/normalize.Currency/
		// resolveOwnedCategory/s.IDs.NewID() before reaching this call,
		// so reaching this branch would mean this package's own
		// invariant broke, not that the user did anything wrong —
		// Internal, the same reasoning wrapTransferError's default case
		// gives.
		return errs.New(errs.Internal).Explain("That budget isn't valid.").Wrap(err)
	}
}
