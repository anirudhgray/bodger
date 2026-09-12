// Package importing holds the pure domain model for ADR-0008's staged
// import pipeline: ImportBatch, ImportRecord, and DuplicateMatch, plus the
// status state machines each of the first two moves through. No I/O, no
// clock, no database — the same discipline internal/domain/ledger holds,
// for the same reason (ADR-0005): anything that needs a repository lookup
// or "now" belongs to internal/app, never here.
//
// This package models the pipeline's *state*, not its behaviour. Parsing a
// source file into ImportRecords, resolving accounts/categories, running
// the two-tier duplicate-detection heuristic, and committing or rolling
// back a batch are all separate, later issues (ADR-0008's "everything
// before commit is staged ... rows"); this package only has to represent
// what a staged row and a staged batch look like, and which status
// transitions are legal.
//
// Named "importing" rather than "import": the latter is a Go keyword and
// can't be a package name.
package importing

import "fmt"

// DuplicateMatchTier is which of ADR-0008's two duplicate-detection tiers
// produced a DuplicateMatch.
type DuplicateMatchTier string

const (
	// DuplicateMatchTierExact is tier 1: the source's own external ID
	// matches an existing transaction on the same account exactly.
	// ADR-0008 treats this as a definite duplicate, skipped automatically —
	// it is never surfaced for the user to resolve (see DuplicateMatch's
	// Resolve).
	DuplicateMatchTierExact DuplicateMatchTier = "exact"

	// DuplicateMatchTierSuspected is tier 2: a heuristic match (same
	// account, exact amount and currency, booked_date within +/-3 days,
	// and a similar description) that is surfaced for the user to
	// resolve — never auto-merged (ADR-0008).
	DuplicateMatchTierSuspected DuplicateMatchTier = "suspected_duplicate"
)

var validDuplicateMatchTiers = map[DuplicateMatchTier]bool{
	DuplicateMatchTierExact:     true,
	DuplicateMatchTierSuspected: true,
}

// DuplicateResolution is the user's decision about a DuplicateMatch —
// ADR-0008: "the user resolves it; the decision is recorded on the
// ImportRecord for auditability."
type DuplicateResolution string

const (
	// DuplicateResolutionPending means no decision has been recorded yet.
	// A tier-1 DuplicateMatch (DuplicateMatchTierExact) stays at this value
	// forever — Resolve refuses to change it, since an exact match is
	// skipped automatically and has no decision to record.
	DuplicateResolutionPending DuplicateResolution = "pending"

	// DuplicateResolutionConfirmed means the user agreed the match is a
	// genuine duplicate: the record should be excluded from commit.
	DuplicateResolutionConfirmed DuplicateResolution = "confirmed_duplicate"

	// DuplicateResolutionDismissed means the user decided the match is not
	// actually a duplicate: the record proceeds toward commit like any
	// other.
	DuplicateResolutionDismissed DuplicateResolution = "not_duplicate"
)

var validDuplicateResolutions = map[DuplicateResolution]bool{
	DuplicateResolutionPending:   true,
	DuplicateResolutionConfirmed: true,
	DuplicateResolutionDismissed: true,
}

// DuplicateMatch is a candidate duplicate ADR-0008's two-tier detection
// found for an ImportRecord, plus the user's resolution of it. Its fields
// are unexported: the only way to produce one is NewDuplicateMatch, and the
// only way to record a resolution is Resolve.
//
// This type models the shape a match takes, not the detection logic that
// produces one — comparing records, walking the +/-3-day window, and
// scoring description similarity all belong to the parsing/mapping issue
// this one explicitly defers to.
type DuplicateMatch struct {
	tier                 DuplicateMatchTier
	matchedTransactionID string
	resolution           DuplicateResolution
}

// NewDuplicateMatch constructs a DuplicateMatch for tier against
// matchedTransactionID — the existing transaction the candidate collided
// with — with no resolution recorded yet (DuplicateResolutionPending).
func NewDuplicateMatch(tier DuplicateMatchTier, matchedTransactionID string) (DuplicateMatch, error) {
	if !validDuplicateMatchTiers[tier] {
		return DuplicateMatch{}, fmt.Errorf("%w: %q", ErrDuplicateMatchInvalidTier, tier)
	}
	if matchedTransactionID == "" {
		return DuplicateMatch{}, ErrDuplicateMatchEmptyTransactionID
	}
	return DuplicateMatch{
		tier:                 tier,
		matchedTransactionID: matchedTransactionID,
		resolution:           DuplicateResolutionPending,
	}, nil
}

// Tier returns which detection tier produced this match.
func (m DuplicateMatch) Tier() DuplicateMatchTier { return m.tier }

// MatchedTransactionID returns the ID of the existing transaction this
// candidate was matched against.
func (m DuplicateMatch) MatchedTransactionID() string { return m.matchedTransactionID }

// Resolution returns the user's decision so far — DuplicateResolutionPending
// until Resolve succeeds.
func (m DuplicateMatch) Resolution() DuplicateResolution { return m.resolution }

// Resolved reports whether the user has recorded a decision.
func (m DuplicateMatch) Resolved() bool { return m.resolution != DuplicateResolutionPending }

// Resolve returns a copy of m with the user's decision recorded; m itself
// is unchanged. It returns ErrDuplicateMatchExactNotResolvable if m is a
// tier-1 exact match (ADR-0008: skipped automatically, never reviewed),
// and ErrDuplicateMatchInvalidResolution if resolution isn't
// DuplicateResolutionConfirmed or DuplicateResolutionDismissed.
func (m DuplicateMatch) Resolve(resolution DuplicateResolution) (DuplicateMatch, error) {
	if m.tier == DuplicateMatchTierExact {
		return DuplicateMatch{}, ErrDuplicateMatchExactNotResolvable
	}
	if !validDuplicateResolutions[resolution] || resolution == DuplicateResolutionPending {
		return DuplicateMatch{}, fmt.Errorf("%w: %q", ErrDuplicateMatchInvalidResolution, resolution)
	}
	m.resolution = resolution
	return m, nil
}
