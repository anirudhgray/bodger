package recurring

import (
	"fmt"

	"github.com/anirudhgray/bodger/internal/domain"
)

// RecurringRule is data-model.md §11's template: the amount, account,
// category, description, and schedule a projected firing would be built
// from. Money has never moved. Its fields are unexported: the only way to
// produce one is NewRecurringRule.
//
// Unlike budgeting.Budget, a rule does not own its ScheduledOccurrences as
// an embedded aggregate. Occurrences are separately persisted, separately
// queried, and carry their own user-created state (skipped, materialised),
// so they are an independent entity generated *from* a rule rather than a
// child of it — the ERD in data-model.md §2 draws them as their own table
// for that reason.
//
// There is no currency field: amountMinor is denominated in the rule's
// account's currency, the same single-source-of-truth shape
// budgeting.BudgetLine uses against its budget. There is no kind field
// either: whether the money is an outflow or an inflow follows from the
// category's own kind, so a rule cannot declare a direction that
// contradicts its category (ADR-0014).
type RecurringRule struct {
	id          string
	userID      string
	accountID   string
	categoryID  string
	amountMinor int64
	description string
	schedule    Schedule
	startsOn    domain.Date
	endsOn      *domain.Date
	archivedAt  *domain.Date
}

// RecurringRuleOption sets one of a rule's optional fields at construction
// time. See WithEndsOn and WithArchivedAt.
type RecurringRuleOption func(*RecurringRule)

// WithEndsOn sets the last date a rule may fire on. This is where RFC
// 5545's UNTIL/COUNT live in this model — on the rule, not inside its
// Schedule, so exactly one field answers "when does this stop" (ADR-0014).
func WithEndsOn(endsOn domain.Date) RecurringRuleOption {
	return func(r *RecurringRule) {
		d := endsOn
		r.endsOn = &d
	}
}

// WithArchivedAt reconstructs a rule that was already archived. This
// exists for the sqlite adapter to restore a rule from its
// already-validated, already-persisted archived_at column — the same
// restore-not-re-derive shape as budgeting.WithArchivedAt — not a way for
// application code to archive a rule by construction; the archive use case
// is a separate, later issue.
func WithArchivedAt(archivedAt domain.Date) RecurringRuleOption {
	return func(r *RecurringRule) {
		d := archivedAt
		r.archivedAt = &d
	}
}

// NewRecurringRule constructs a RecurringRule. amountMinor must be
// strictly positive (the direction comes from the category, not the sign),
// the schedule must be one this package's constructors produced, and an
// ends_on, if set, must not fall before starts_on.
func NewRecurringRule(
	id, userID, accountID, categoryID string,
	amountMinor int64,
	description string,
	schedule Schedule,
	startsOn domain.Date,
	opts ...RecurringRuleOption,
) (RecurringRule, error) {
	if id == "" {
		return RecurringRule{}, ErrRuleEmptyID
	}
	if userID == "" {
		return RecurringRule{}, ErrRuleEmptyUserID
	}
	if accountID == "" {
		return RecurringRule{}, ErrRuleEmptyAccountID
	}
	if categoryID == "" {
		return RecurringRule{}, ErrRuleEmptyCategoryID
	}
	if amountMinor <= 0 {
		return RecurringRule{}, ErrRuleAmountNotPositive
	}
	if description == "" {
		return RecurringRule{}, ErrRuleEmptyDescription
	}
	if !schedule.Valid() {
		return RecurringRule{}, fmt.Errorf("%w: %q", ErrRuleInvalidSchedule, schedule.Frequency())
	}

	r := RecurringRule{
		id:          id,
		userID:      userID,
		accountID:   accountID,
		categoryID:  categoryID,
		amountMinor: amountMinor,
		description: description,
		schedule:    schedule,
		startsOn:    startsOn,
	}
	for _, opt := range opts {
		opt(&r)
	}
	if r.endsOn != nil && r.endsOn.Before(startsOn) {
		return RecurringRule{}, fmt.Errorf("%w: %s before %s", ErrRuleEndsBeforeStart, r.endsOn, startsOn)
	}
	return r, nil
}

// ID returns the rule's identifier.
func (r RecurringRule) ID() string { return r.id }

// UserID returns the ID of the user who owns the rule.
func (r RecurringRule) UserID() string { return r.userID }

// AccountID returns the account a materialised occurrence of this rule
// would post to.
func (r RecurringRule) AccountID() string { return r.accountID }

// CategoryID returns the category a materialised occurrence of this rule
// would be attributed to. It also decides the direction of the money —
// see the type's own doc comment.
func (r RecurringRule) CategoryID() string { return r.categoryID }

// AmountMinor returns the rule's planned amount, in its account's
// currency's minor units. Always positive.
func (r RecurringRule) AmountMinor() int64 { return r.amountMinor }

// Description returns the description a materialised occurrence would
// carry onto its transaction.
func (r RecurringRule) Description() string { return r.description }

// Schedule returns the rule's firing pattern.
func (r RecurringRule) Schedule() Schedule { return r.schedule }

// StartsOn returns the first date the rule may fire on — the anchor every
// firing is computed from (ADR-0014).
func (r RecurringRule) StartsOn() domain.Date { return r.startsOn }

// EndsOn returns the last date the rule may fire on, and false if it runs
// indefinitely.
func (r RecurringRule) EndsOn() (domain.Date, bool) {
	if r.endsOn == nil {
		return domain.Date{}, false
	}
	return *r.endsOn, true
}

// ArchivedAt returns the date the rule was archived, and false if it is
// active. Mirrors budgeting.Budget.ArchivedAt's shape.
func (r RecurringRule) ArchivedAt() (domain.Date, bool) {
	if r.archivedAt == nil {
		return domain.Date{}, false
	}
	return *r.archivedAt, true
}

// Archived reports whether the rule is currently archived — a convenience
// over ArchivedAt for callers that only care about the boolean state.
func (r RecurringRule) Archived() bool { return r.archivedAt != nil }

// OccurrencesBetween returns every date this rule fires on within
// [from, to] inclusive, already clipped to the rule's own starts_on and
// ends_on. An archived rule fires on nothing.
//
// It takes its window explicitly and never asks what day it is: the
// caller (issue #278's generation use case) resolves "today" once, in the
// application layer, from the injected clock and the actor's timezone.
func (r RecurringRule) OccurrencesBetween(from, to domain.Date) []domain.Date {
	if r.Archived() {
		return nil
	}
	if from.Before(r.startsOn) {
		from = r.startsOn
	}
	if endsOn, ok := r.EndsOn(); ok && endsOn.Before(to) {
		to = endsOn
	}
	return r.schedule.occurrencesBetween(r.startsOn, from, to)
}

// NextOccurrenceOnOrAfter returns the rule's first firing on or after
// from, and false if it has none (because the rule is archived, or ends
// before then). Same window discipline as OccurrencesBetween: the caller
// supplies the date.
func (r RecurringRule) NextOccurrenceOnOrAfter(from domain.Date) (domain.Date, bool) {
	if r.Archived() {
		return domain.Date{}, false
	}
	if from.Before(r.startsOn) {
		from = r.startsOn
	}
	endsOn, bounded := r.EndsOn()
	for n := 0; ; n++ {
		d := r.schedule.occurrenceAt(r.startsOn, n)
		if bounded && d.After(endsOn) {
			return domain.Date{}, false
		}
		if !d.Before(from) {
			return d, true
		}
	}
}
