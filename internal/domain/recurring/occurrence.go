package recurring

import (
	"fmt"

	"github.com/anirudhgray/bodger/internal/domain"
)

// OccurrenceStatus is the closed set of states a ScheduledOccurrence can
// be in — data-model.md §11's pending / materialised / skipped, and
// nothing else. Mirrors importing.ImportRecordStatus's enum shape.
type OccurrenceStatus string

const (
	// OccurrenceStatusPending is a projected firing nobody has acted on
	// yet. Money has not moved.
	OccurrenceStatusPending OccurrenceStatus = "pending"

	// OccurrenceStatusMaterialised is a firing that became a real
	// transaction. The occurrence still isn't the money — the transaction
	// it points at is (data-model.md §11).
	OccurrenceStatusMaterialised OccurrenceStatus = "materialised"

	// OccurrenceStatusSkipped is a firing the user decided not to record.
	// Money never moved and never will for this date.
	OccurrenceStatusSkipped OccurrenceStatus = "skipped"
)

var validOccurrenceStatuses = map[OccurrenceStatus]bool{
	OccurrenceStatusPending:      true,
	OccurrenceStatusMaterialised: true,
	OccurrenceStatusSkipped:      true,
}

// occurrenceTransitions is the state machine MarkMaterialised and
// MarkSkipped enforce: a pending occurrence can go either way, and a
// terminal one can go nowhere. Same table-driven shape
// importing.ImportRecord uses.
var occurrenceTransitions = map[OccurrenceStatus]map[OccurrenceStatus]bool{
	OccurrenceStatusPending: {
		OccurrenceStatusMaterialised: true,
		OccurrenceStatusSkipped:      true,
	},
	OccurrenceStatusMaterialised: {},
	OccurrenceStatusSkipped:      {},
}

// ScheduledOccurrence is data-model.md §11's projection: one firing of a
// RecurringRule on one date. Money has still never moved, and this type
// is deliberately incapable of saying otherwise — it carries no account,
// no currency, and no Money, so there is no arithmetic anyone could
// perform on it that would produce a balance-shaped number, and no join
// key from it to an account (ADR-0014).
//
// Its fields are unexported: the only way to produce one is
// NewScheduledOccurrence.
type ScheduledOccurrence struct {
	id             string
	ruleID         string
	occurrenceDate domain.Date
	status         OccurrenceStatus
	transactionID  *string
}

// ScheduledOccurrenceOption sets one of an occurrence's optional fields at
// construction time. See WithOccurrenceStatus and WithTransactionID.
type ScheduledOccurrenceOption func(*ScheduledOccurrence)

// WithOccurrenceStatus sets the occurrence's status directly, bypassing
// the transition state machine (MarkMaterialised/MarkSkipped). This exists
// for the sqlite adapter to reconstruct an occurrence from its
// already-validated, already-persisted status column — see
// importing.WithRecordStatus for the same shape and rationale.
// NewScheduledOccurrence defaults to OccurrenceStatusPending, the only
// status a genuinely new occurrence is ever created in.
func WithOccurrenceStatus(status OccurrenceStatus) ScheduledOccurrenceOption {
	return func(o *ScheduledOccurrence) { o.status = status }
}

// WithTransactionID sets the transaction this occurrence materialised
// into, for reconstructing an already-materialised occurrence from
// storage — the counterpart to
// WithOccurrenceStatus(OccurrenceStatusMaterialised). Application code
// recording a fresh materialisation should use MarkMaterialised instead,
// which enforces the pending -> materialised transition.
func WithTransactionID(transactionID string) ScheduledOccurrenceOption {
	return func(o *ScheduledOccurrence) {
		if transactionID != "" {
			v := transactionID
			o.transactionID = &v
		}
	}
}

// NewScheduledOccurrence constructs a ScheduledOccurrence in
// OccurrenceStatusPending unless overridden by WithOccurrenceStatus.
//
// The status and the transaction ID must agree: a materialised occurrence
// must carry the transaction it produced, and a pending or skipped one
// must not carry any — a projection pointing at real money is exactly the
// conflation data-model.md §11 forbids, so it is rejected here and again
// by a CHECK constraint in the schema.
func NewScheduledOccurrence(id, ruleID string, occurrenceDate domain.Date, opts ...ScheduledOccurrenceOption) (ScheduledOccurrence, error) {
	if id == "" {
		return ScheduledOccurrence{}, ErrOccurrenceEmptyID
	}
	if ruleID == "" {
		return ScheduledOccurrence{}, ErrOccurrenceEmptyRuleID
	}

	o := ScheduledOccurrence{
		id:             id,
		ruleID:         ruleID,
		occurrenceDate: occurrenceDate,
		status:         OccurrenceStatusPending,
	}
	for _, opt := range opts {
		opt(&o)
	}
	if !validOccurrenceStatuses[o.status] {
		return ScheduledOccurrence{}, fmt.Errorf("%w: %q", ErrOccurrenceInvalidStatus, o.status)
	}
	if o.status == OccurrenceStatusMaterialised && o.transactionID == nil {
		return ScheduledOccurrence{}, ErrOccurrenceEmptyTransactionID
	}
	if o.status != OccurrenceStatusMaterialised && o.transactionID != nil {
		return ScheduledOccurrence{}, fmt.Errorf("%w: status %q", ErrOccurrenceUnexpectedTransactionID, o.status)
	}
	return o, nil
}

// ID returns the occurrence's identifier.
func (o ScheduledOccurrence) ID() string { return o.id }

// RuleID returns the rule this occurrence is a firing of. It is also the
// only route to an owner: an occurrence carries no user_id of its own, the
// same way a posting is scoped through its transaction (ADR-0014).
func (o ScheduledOccurrence) RuleID() string { return o.ruleID }

// OccurrenceDate returns the calendar date the rule fires on — no time, no
// timezone, the same type and the same reasoning as a transaction's
// booked date (data-model.md §9).
func (o ScheduledOccurrence) OccurrenceDate() domain.Date { return o.occurrenceDate }

// Status returns the occurrence's position in the pending ->
// materialised/skipped state machine.
func (o ScheduledOccurrence) Status() OccurrenceStatus { return o.status }

// TransactionID returns the transaction this occurrence materialised into,
// and false if it hasn't been materialised. This is a one-way provenance
// pointer: nothing traverses it in the other direction, and a balance
// reads the transaction, never the occurrence.
func (o ScheduledOccurrence) TransactionID() (string, bool) {
	if o.transactionID == nil {
		return "", false
	}
	return *o.transactionID, true
}

// MarkMaterialised returns a copy of o recorded as having produced
// transactionID. o itself is unchanged. It returns
// ErrOccurrenceInvalidTransition unless o is pending, and
// ErrOccurrenceEmptyTransactionID if transactionID is empty.
func (o ScheduledOccurrence) MarkMaterialised(transactionID string) (ScheduledOccurrence, error) {
	if transactionID == "" {
		return ScheduledOccurrence{}, ErrOccurrenceEmptyTransactionID
	}
	next, err := o.transition(OccurrenceStatusMaterialised)
	if err != nil {
		return ScheduledOccurrence{}, err
	}
	id := transactionID
	next.transactionID = &id
	return next, nil
}

// MarkSkipped returns a copy of o recorded as skipped. o itself is
// unchanged. It returns ErrOccurrenceInvalidTransition unless o is
// pending.
func (o ScheduledOccurrence) MarkSkipped() (ScheduledOccurrence, error) {
	return o.transition(OccurrenceStatusSkipped)
}

func (o ScheduledOccurrence) transition(next OccurrenceStatus) (ScheduledOccurrence, error) {
	if !occurrenceTransitions[o.status][next] {
		return ScheduledOccurrence{}, fmt.Errorf("%w: %q -> %q", ErrOccurrenceInvalidTransition, o.status, next)
	}
	o.status = next
	return o, nil
}
