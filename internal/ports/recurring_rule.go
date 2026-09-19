package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/recurring"
)

// RecurringRuleRepository persists RecurringRules — data-model.md §11's
// templates, which are never money that moved. Every method takes actorID
// and filters on it (ADR-0006): a rule belonging to a different user is
// invisible to Get and List, and Create and Update reject a rule whose own
// UserID() doesn't match actorID.
//
// Unlike BudgetRepository, this is not an aggregate root over its
// children: a rule's ScheduledOccurrences are separately persisted and
// separately queried through ScheduledOccurrenceRepository, because they
// carry their own user-created state (skipped, materialised) and are read
// in date-ranged slices across rules rather than only ever as one rule's
// list (ADR-0014).
//
// Nothing here returns a posting, a transaction, or a monetary value, and
// no balance or analytics query reads these tables — the structural half
// of data-model.md §11's "an occurrence never contributes to a balance".
//
// Defined here, in the layer that consumes it (ADR-0007's "the repository
// boundary is what keeps this reversible") — internal/adapters/sqlite
// implements it, and the application layer depends only on this
// interface, never on the adapter.
type RecurringRuleRepository interface {
	// Create persists a new recurring rule. It returns a *errs.Error with
	// code NotAllowed if rule.UserID() != actorID.
	Create(ctx context.Context, actorID string, rule recurring.RecurringRule) error

	// Get returns the rule identified by id, owned by actorID. It returns
	// a *errs.Error with code NotFound if no such rule exists for that
	// actor — including when the rule exists but belongs to a different
	// user.
	Get(ctx context.Context, actorID, id string) (recurring.RecurringRule, error)

	// List returns every rule actorID owns, most recently created first,
	// archived ones included — the same all-inclusive shape
	// BudgetRepository.List has, so a caller that wants only active rules
	// filters on RecurringRule.Archived itself rather than this method
	// deciding for it.
	List(ctx context.Context, actorID string) ([]recurring.RecurringRule, error)

	// Update replaces the stored state of the rule identified by
	// rule.ID(), owned by actorID. It returns a *errs.Error with code
	// NotFound if no such rule exists for that actor, and NotAllowed if
	// rule.UserID() != actorID.
	Update(ctx context.Context, actorID string, rule recurring.RecurringRule) error
}
