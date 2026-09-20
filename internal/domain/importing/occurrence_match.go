package importing

import "fmt"

// OccurrenceMatchResolution is the user's decision about an OccurrenceMatch
// (issue #309) — DuplicateResolution's counterpart, but with different
// verbs: an occurrence match is resolved by materialising the matched
// occurrence or dismissing the match, never by "confirming" or "not"-ing a
// duplicate (findOccurrenceMatch's doc comment, internal/app/import_duplicate.go,
// explains why an occurrence match was never shaped like a DuplicateMatch
// in the first place).
type OccurrenceMatchResolution string

const (
	// OccurrenceMatchResolutionPending means no decision has been recorded
	// yet.
	OccurrenceMatchResolutionPending OccurrenceMatchResolution = "pending"

	// OccurrenceMatchResolutionMaterialized means the user chose to turn
	// the matched occurrence into its own real transaction
	// (app.MaterialiseOccurrence, using the rule's own projected amount and
	// date) — the occurrence's transaction now authoritatively covers this
	// row's money, so the ImportRecord itself is excluded from commit
	// rather than double-counting it.
	OccurrenceMatchResolutionMaterialized OccurrenceMatchResolution = "materialized"

	// OccurrenceMatchResolutionDismissed means the user decided the match
	// was a false positive, or prefers the imported row as the real
	// transaction: the matched occurrence is left completely untouched
	// (still pending, for a human to separately materialise or skip later
	// through the existing recurring-occurrences surface), and the
	// ImportRecord proceeds toward commit like any other.
	OccurrenceMatchResolutionDismissed OccurrenceMatchResolution = "dismissed"
)

var validOccurrenceMatchResolutions = map[OccurrenceMatchResolution]bool{
	OccurrenceMatchResolutionPending:      true,
	OccurrenceMatchResolutionMaterialized: true,
	OccurrenceMatchResolutionDismissed:    true,
}

// OccurrenceMatch is the pending recurring.ScheduledOccurrence issue #301's
// findOccurrenceMatch believes an ImportRecord's money settles, plus the
// user's resolution of it (issue #309). Its fields are unexported: the
// only way to produce one is NewOccurrenceMatch, and the only way to
// record a resolution is Resolve.
//
// Unlike DuplicateMatch, this carries no "tier" and no matched-transaction
// ID — an occurrence isn't a transaction (ADR-0014: it carries no account,
// no currency, no Money), so there's nothing to compare tiers against and
// nothing shaped like DuplicateMatch's MatchedTransactionID to expose;
// OccurrenceID is the only thing this points at.
type OccurrenceMatch struct {
	occurrenceID string
	resolution   OccurrenceMatchResolution
}

// NewOccurrenceMatch constructs an OccurrenceMatch against occurrenceID —
// the pending recurring.ScheduledOccurrence findOccurrenceMatch believes
// settles the same money — with no resolution recorded yet
// (OccurrenceMatchResolutionPending).
func NewOccurrenceMatch(occurrenceID string) (OccurrenceMatch, error) {
	if occurrenceID == "" {
		return OccurrenceMatch{}, ErrOccurrenceMatchEmptyOccurrenceID
	}
	return OccurrenceMatch{occurrenceID: occurrenceID, resolution: OccurrenceMatchResolutionPending}, nil
}

// OccurrenceID returns the ID of the pending recurring.ScheduledOccurrence
// this match points at.
func (m OccurrenceMatch) OccurrenceID() string { return m.occurrenceID }

// Resolution returns the user's decision so far —
// OccurrenceMatchResolutionPending until Resolve succeeds.
func (m OccurrenceMatch) Resolution() OccurrenceMatchResolution { return m.resolution }

// Resolved reports whether the user has recorded a decision.
func (m OccurrenceMatch) Resolved() bool { return m.resolution != OccurrenceMatchResolutionPending }

// Resolve returns a copy of m with the user's decision recorded; m itself
// is unchanged. It returns ErrOccurrenceMatchInvalidResolution if
// resolution isn't OccurrenceMatchResolutionMaterialized or
// -Dismissed.
func (m OccurrenceMatch) Resolve(resolution OccurrenceMatchResolution) (OccurrenceMatch, error) {
	if !validOccurrenceMatchResolutions[resolution] || resolution == OccurrenceMatchResolutionPending {
		return OccurrenceMatch{}, fmt.Errorf("%w: %q", ErrOccurrenceMatchInvalidResolution, resolution)
	}
	m.resolution = resolution
	return m, nil
}
