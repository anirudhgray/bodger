package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/budgeting"
)

// BudgetRepository persists Budgets, including their owned BudgetLines as
// part of the same aggregate — there is no separate BudgetLineRepository,
// since a line has no independent existence apart from its budget
// (data-model.md §10). Every method takes actorID and filters on it
// (ADR-0006): a budget belonging to a different user is invisible to Get
// and List, and Create and Update reject a budget whose own UserID doesn't
// match actorID.
//
// Defined here, in the layer that consumes it (ADR-0007's "the repository
// boundary is what keeps this reversible") — internal/adapters/sqlite
// implements it, and the application layer depends only on this
// interface, never on the adapter.
type BudgetRepository interface {
	// Create persists a new budget and its lines in one atomic write. It
	// returns a *errs.Error with code NotAllowed if budget.UserID() !=
	// actorID.
	Create(ctx context.Context, actorID string, budget budgeting.Budget) error

	// Get returns the budget identified by id, owned by actorID, with its
	// lines. It returns a *errs.Error with code NotFound if no such budget
	// exists for that actor — including when the budget exists but
	// belongs to a different user.
	Get(ctx context.Context, actorID, id string) (budgeting.Budget, error)

	// List returns every budget actorID owns, most recently created
	// first, each with its lines.
	List(ctx context.Context, actorID string) ([]budgeting.Budget, error)

	// Update replaces the stored state of the budget identified by
	// budget.ID(), owned by actorID, including replacing its lines
	// wholesale with budget.Lines() — the same replace-the-children shape
	// TransactionRepository.Update uses for postings. It returns a
	// *errs.Error with code NotFound if no such budget exists for that
	// actor, and NotAllowed if budget.UserID() != actorID.
	Update(ctx context.Context, actorID string, budget budgeting.Budget) error
}
