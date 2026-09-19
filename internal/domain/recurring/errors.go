package recurring

import "errors"

// Named errors returned by this package. Callers should match on these with
// errors.Is; the wrapped text (added by the constructor that returns them)
// carries the offending value for humans, the sentinel carries the identity
// for code.
var (
	// ErrScheduleInvalidFrequency is returned when a schedule is
	// constructed with a frequency outside ADR-0014's supported subset.
	ErrScheduleInvalidFrequency = errors.New("recurring: invalid schedule frequency")

	// ErrScheduleIntervalNotPositive is returned when a schedule is
	// constructed with an interval below 1 — "every zero months" has no
	// meaning, and a negative interval would run a rule backwards.
	ErrScheduleIntervalNotPositive = errors.New("recurring: schedule interval must be at least 1")

	// ErrScheduleInvalidWeekday is returned when a weekly schedule is
	// constructed with a weekday outside time.Sunday..time.Saturday.
	ErrScheduleInvalidWeekday = errors.New("recurring: invalid schedule weekday")

	// ErrScheduleInvalidDayOfMonth is returned when a monthly or yearly
	// schedule is constructed with a day of month outside 1..31. 29, 30,
	// and 31 are all accepted: a month too short for the declared day
	// clamps to its own last day rather than being rejected or skipped
	// (ADR-0014).
	ErrScheduleInvalidDayOfMonth = errors.New("recurring: invalid schedule day of month")

	// ErrScheduleInvalidMonth is returned when a yearly schedule is
	// constructed with a month outside time.January..time.December.
	ErrScheduleInvalidMonth = errors.New("recurring: invalid schedule month")

	// ErrRuleEmptyID is returned when a recurring rule is constructed with
	// an empty ID.
	ErrRuleEmptyID = errors.New("recurring: rule id must not be empty")

	// ErrRuleEmptyUserID is returned when a recurring rule is constructed
	// with an empty user ID.
	ErrRuleEmptyUserID = errors.New("recurring: rule user id must not be empty")

	// ErrRuleEmptyAccountID is returned when a recurring rule is
	// constructed with an empty account ID.
	ErrRuleEmptyAccountID = errors.New("recurring: rule account id must not be empty")

	// ErrRuleEmptyCategoryID is returned when a recurring rule is
	// constructed with an empty category ID. A rule posts to exactly one
	// account/category pair (ADR-0014) — the category is what decides
	// whether the money is an outflow or an inflow, so it is required,
	// not optional as it is on a transfer posting.
	ErrRuleEmptyCategoryID = errors.New("recurring: rule category id must not be empty")

	// ErrRuleEmptyDescription is returned when a recurring rule is
	// constructed with an empty description — the same requirement the
	// application layer already places on a transaction's description,
	// since a materialised occurrence becomes one.
	ErrRuleEmptyDescription = errors.New("recurring: rule description must not be empty")

	// ErrRuleAmountNotPositive is returned when a recurring rule is
	// constructed with an amount_minor that isn't strictly positive. The
	// direction of the money comes from the category's kind, not from the
	// sign (ADR-0014), so a negative or zero planned amount has no
	// meaning here.
	ErrRuleAmountNotPositive = errors.New("recurring: rule amount_minor must be positive")

	// ErrRuleInvalidSchedule is returned when a recurring rule is
	// constructed with a zero or otherwise unconstructed Schedule.
	ErrRuleInvalidSchedule = errors.New("recurring: rule schedule is not a valid schedule")

	// ErrRuleEndsBeforeStart is returned when a recurring rule is
	// constructed with an ends_on strictly before its starts_on.
	ErrRuleEndsBeforeStart = errors.New("recurring: rule ends_on must not be before starts_on")

	// ErrOccurrenceEmptyID is returned when a scheduled occurrence is
	// constructed with an empty ID.
	ErrOccurrenceEmptyID = errors.New("recurring: occurrence id must not be empty")

	// ErrOccurrenceEmptyRuleID is returned when a scheduled occurrence is
	// constructed with an empty rule ID.
	ErrOccurrenceEmptyRuleID = errors.New("recurring: occurrence rule id must not be empty")

	// ErrOccurrenceInvalidStatus is returned when a scheduled occurrence
	// is constructed (via WithOccurrenceStatus) with a status outside the
	// known set.
	ErrOccurrenceInvalidStatus = errors.New("recurring: invalid occurrence status")

	// ErrOccurrenceEmptyTransactionID is returned when a scheduled
	// occurrence is constructed as materialised without the transaction
	// it materialised into.
	ErrOccurrenceEmptyTransactionID = errors.New("recurring: a materialised occurrence must carry its transaction id")

	// ErrOccurrenceUnexpectedTransactionID is returned when a scheduled
	// occurrence that isn't materialised is constructed carrying a
	// transaction ID. A pending or skipped occurrence pointing at a real
	// transaction is the exact conflation data-model.md §11 forbids.
	ErrOccurrenceUnexpectedTransactionID = errors.New("recurring: only a materialised occurrence may carry a transaction id")

	// ErrOccurrenceInvalidTransition is returned when a scheduled
	// occurrence is moved between statuses the state machine doesn't
	// permit — anything other than pending -> materialised and
	// pending -> skipped.
	ErrOccurrenceInvalidTransition = errors.New("recurring: invalid occurrence status transition")
)
