package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
)

// ScheduledOccurrenceFilter narrows a ScheduledOccurrenceRepository.List
// read. Every field is optional; a zero filter lists every occurrence the
// actor owns. Mirrors TransactionFilter's optional-pointer shape.
//
// The dates are plain calendar dates the application layer has already
// resolved (ADR-0005, ADR-0014) — this layer never asks what "today" is,
// and no method here takes a clock.
type ScheduledOccurrenceFilter struct {
	// RuleID limits the read to one rule's own occurrences.
	RuleID string

	// Status limits the read to one status — pending, for the "what is
	// due" query issues #278-#280 run.
	Status recurring.OccurrenceStatus

	// FromDate and ToDate bound occurrence_date inclusively.
	FromDate *domain.Date
	ToDate   *domain.Date
}

// ScheduledOccurrenceRepository persists ScheduledOccurrences —
// data-model.md §11's projections, which are never money that moved.
// Every method takes actorID and filters on it (ADR-0006), reaching the
// owner through the occurrence's rule rather than a duplicated user_id
// column, the same way a posting is scoped through its transaction
// (ADR-0014).
//
// Nothing here returns a posting, a transaction, or a monetary value, and
// no balance or analytics query reads this table. An occurrence carries no
// account and no currency, so there is no join key from one into a
// balance: materialisation (issue #279) creates a real transaction, and
// from then on it is the transaction a balance reads.
//
// Defined here, in the layer that consumes it (ADR-0007) —
// internal/adapters/sqlite implements it, and the application layer
// depends only on this interface, never on the adapter.
type ScheduledOccurrenceRepository interface {
	// CreateBatch persists every occurrence in occurrences in one
	// database transaction. Generation (issue #278) projects a whole
	// horizon of firings for a rule at once, so this is the write path
	// for that rather than N round trips through a single-row Create —
	// the same reasoning ImportRecordRepository.CreateBatch follows.
	//
	// It returns a *errs.Error with code NotAllowed if any occurrence's
	// rule isn't owned by actorID, Conflict if any occurrence duplicates
	// an existing (rule_id, occurrence_date) pair — which is what makes
	// generation safe to re-run — and nothing is written if any single
	// row fails.
	CreateBatch(ctx context.Context, actorID string, occurrences []recurring.ScheduledOccurrence) error

	// Get returns the occurrence identified by id, owned by actorID. It
	// returns a *errs.Error with code NotFound if no such occurrence
	// exists for that actor — including when it exists but belongs to a
	// different user's rule.
	Get(ctx context.Context, actorID, id string) (recurring.ScheduledOccurrence, error)

	// List returns every occurrence actorID owns that matches filter, in
	// ascending occurrence_date order (then by ID, so a rule with two
	// occurrences on one date — which the schema forbids — could still
	// never return a nondeterministic order).
	List(ctx context.Context, actorID string, filter ScheduledOccurrenceFilter) ([]recurring.ScheduledOccurrence, error)

	// Update replaces the stored state of the occurrence identified by
	// occurrence.ID(), owned by actorID — this is how a materialisation
	// or a skip is persisted. It returns a *errs.Error with code NotFound
	// if no such occurrence exists for that actor.
	Update(ctx context.Context, actorID string, occurrence recurring.ScheduledOccurrence) error

	// DeletePending removes ruleID's pending occurrences falling on or
	// after onOrAfter, for actorID. This is what a rule's schedule
	// changing needs (issue #278): the stale projection is discarded and
	// regenerated. Issue #278 also uses it, with onOrAfter set to the
	// rule's own StartsOn, to cancel every remaining pending occurrence
	// when a rule is archived.
	//
	// It is deliberately narrower than a general Delete. A materialised
	// occurrence is the provenance of a real transaction and a skipped one
	// is a decision the user made, so neither may be swept away by a
	// regeneration — and that restriction lives here, in the one place
	// that writes the DELETE, rather than in every future caller's own
	// WHERE clause.
	DeletePending(ctx context.Context, actorID, ruleID string, onOrAfter domain.Date) error
}
