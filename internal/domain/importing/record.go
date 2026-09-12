package importing

import (
	"fmt"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

// ImportRecordStatus is the per-row status ADR-0008's pipeline moves an
// ImportRecord through, independently of its parent ImportBatch's own
// status: pending -> ready -> committed, or pending -> excluded.
type ImportRecordStatus string

const (
	// ImportRecordStatusPending is a record's starting status: parsed and
	// normalised, but not yet cleared for commit — awaiting mapping,
	// duplicate-detection, and (for a suspected duplicate) the user's
	// review.
	ImportRecordStatusPending ImportRecordStatus = "pending"

	// ImportRecordStatusReady means the record has no unresolved duplicate
	// concern (no match at all, or a suspected match the user dismissed)
	// and will be written to the ledger when its batch commits.
	ImportRecordStatusReady ImportRecordStatus = "ready"

	// ImportRecordStatusExcluded means the record will not be written to
	// the ledger — either a tier-1 exact duplicate (skipped automatically,
	// ADR-0008) or a tier-2 suspected duplicate the user confirmed.
	ImportRecordStatusExcluded ImportRecordStatus = "excluded"

	// ImportRecordStatusCommitted means the record's batch was committed
	// and this row produced a real transaction, named by TransactionID.
	ImportRecordStatusCommitted ImportRecordStatus = "committed"
)

var validImportRecordStatuses = map[ImportRecordStatus]bool{
	ImportRecordStatusPending:   true,
	ImportRecordStatusReady:     true,
	ImportRecordStatusExcluded:  true,
	ImportRecordStatusCommitted: true,
}

// importRecordTransitions is the state machine's edge list: for a given
// current status, the set of statuses a transition may move it to.
// Pending is the only branching point (a record either clears review or
// doesn't); every other step is linear, and there is no way back from
// ImportRecordStatusExcluded or ImportRecordStatusCommitted.
var importRecordTransitions = map[ImportRecordStatus]map[ImportRecordStatus]bool{
	ImportRecordStatusPending: {
		ImportRecordStatusReady:    true,
		ImportRecordStatusExcluded: true,
	},
	ImportRecordStatusReady: {
		ImportRecordStatusCommitted: true,
	},
}

// ImportRecord is one source row: ADR-0008's "raw payload + normalised
// fields, one row per source row" (data-model.md §12: "raw payload,
// normalised fields, resolved account/category, dedup verdict, status,
// resulting transaction_id").
//
// The normalised fields (bookedDate, postedDate, description, amount,
// externalID) are already-validated domain values by the time an
// ImportRecord is constructed — producing them from a source file's raw
// bytes is the parser's job (a separate, out-of-scope issue; ADR-0008: "a
// parser's only job is to turn bytes into normalised ImportRecord fields").
// resolvedAccountID/resolvedCategoryID and the DuplicateMatch are likewise
// written by later stages of the pipeline (mapping and duplicate
// detection); this type only has to hold them and gate the status
// transitions between pending, ready/excluded, and committed.
//
// Its fields are unexported: the only way to produce one is
// NewImportRecord, and the only way to move it through the status state
// machine is MarkReady, MarkExcluded, and MarkCommitted.
type ImportRecord struct {
	id                 string
	userID             string
	importBatchID      string
	rawPayload         string
	bookedDate         domain.Date
	postedDate         *domain.Date
	description        string
	amount             money.Money
	externalID         *string
	resolvedAccountID  *string
	resolvedCategoryID *string
	duplicateMatch     *DuplicateMatch
	status             ImportRecordStatus
	transactionID      *string
	sortOrder          int
}

// ImportRecordOption sets one of an ImportRecord's optional fields at
// construction time. See WithPostedDate, WithExternalID,
// WithResolvedAccount, WithResolvedCategory, WithDuplicateMatch,
// WithRecordStatus, and WithTransactionID.
type ImportRecordOption func(*ImportRecord)

// WithPostedDate sets the date the source reported, distinct from
// bookedDate — data-model.md §9's posted_date, "populated only by imports
// where the source distinguishes it."
func WithPostedDate(d domain.Date) ImportRecordOption {
	return func(r *ImportRecord) {
		posted := d
		r.postedDate = &posted
	}
}

// WithExternalID sets the source system's own ID for this row, used for
// ADR-0008's tier-1 exact-duplicate check ("(account_id, external_id) is
// unique").
func WithExternalID(id string) ImportRecordOption {
	return func(r *ImportRecord) {
		if id != "" {
			v := id
			r.externalID = &v
		}
	}
}

// WithResolvedAccount sets the account the mapping stage (a separate,
// out-of-scope issue) resolved this row to.
func WithResolvedAccount(accountID string) ImportRecordOption {
	return func(r *ImportRecord) {
		if accountID != "" {
			v := accountID
			r.resolvedAccountID = &v
		}
	}
}

// WithResolvedCategory sets the category the mapping stage (a separate,
// out-of-scope issue) resolved this row to.
func WithResolvedCategory(categoryID string) ImportRecordOption {
	return func(r *ImportRecord) {
		if categoryID != "" {
			v := categoryID
			r.resolvedCategoryID = &v
		}
	}
}

// WithDuplicateMatch attaches a DuplicateMatch — the candidate ADR-0008's
// two-tier detection (a separate, out-of-scope issue) found for this row,
// if any.
func WithDuplicateMatch(match DuplicateMatch) ImportRecordOption {
	return func(r *ImportRecord) {
		m := match
		r.duplicateMatch = &m
	}
}

// WithRecordStatus sets the record's status directly, bypassing the
// transition state machine (MarkReady/MarkExcluded/MarkCommitted). This
// exists for the sqlite adapter to reconstruct a record from its
// already-validated, already-persisted status column — see ImportBatch's
// WithStatus for the same shape and rationale. NewImportRecord defaults to
// ImportRecordStatusPending, the only status a genuinely new record is
// ever created in.
func WithRecordStatus(status ImportRecordStatus) ImportRecordOption {
	return func(r *ImportRecord) { r.status = status }
}

// WithTransactionID sets the transaction this record produced, for
// reconstructing an already-committed record from storage — the
// counterpart to WithRecordStatus(ImportRecordStatusCommitted). Application
// code recording a fresh commit should use MarkCommitted instead, which
// enforces the ready -> committed transition.
func WithTransactionID(transactionID string) ImportRecordOption {
	return func(r *ImportRecord) {
		if transactionID != "" {
			v := transactionID
			r.transactionID = &v
		}
	}
}

// NewImportRecord constructs an ImportRecord in ImportRecordStatusPending
// unless overridden by WithRecordStatus. rawPayload is the untouched
// source row (this package has no opinion on its shape — CSV text, a JSON
// object, whatever the parser received); bookedDate, description, and
// amount are the parser's already-normalised output. sortOrder preserves
// the source file's row order, the same role posting.sort_order plays for
// a transaction's postings.
func NewImportRecord(id, userID, importBatchID, rawPayload string, bookedDate domain.Date, description string, amount money.Money, sortOrder int, opts ...ImportRecordOption) (ImportRecord, error) {
	if id == "" {
		return ImportRecord{}, ErrImportRecordEmptyID
	}
	if userID == "" {
		return ImportRecord{}, ErrImportRecordEmptyUserID
	}
	if importBatchID == "" {
		return ImportRecord{}, ErrImportRecordEmptyImportBatchID
	}
	if rawPayload == "" {
		return ImportRecord{}, ErrImportRecordEmptyRawPayload
	}
	if description == "" {
		return ImportRecord{}, ErrImportRecordEmptyDescription
	}

	r := ImportRecord{
		id:            id,
		userID:        userID,
		importBatchID: importBatchID,
		rawPayload:    rawPayload,
		bookedDate:    bookedDate,
		description:   description,
		amount:        amount,
		sortOrder:     sortOrder,
		status:        ImportRecordStatusPending,
	}
	for _, opt := range opts {
		opt(&r)
	}
	if !validImportRecordStatuses[r.status] {
		return ImportRecord{}, fmt.Errorf("%w: %q", ErrImportRecordInvalidStatus, r.status)
	}
	if r.status == ImportRecordStatusCommitted && r.transactionID == nil {
		return ImportRecord{}, ErrImportRecordEmptyTransactionID
	}
	return r, nil
}

// ID returns the record's identifier.
func (r ImportRecord) ID() string { return r.id }

// UserID returns the ID of the user who owns the record.
func (r ImportRecord) UserID() string { return r.userID }

// ImportBatchID returns the ID of the batch this record belongs to.
func (r ImportRecord) ImportBatchID() string { return r.importBatchID }

// RawPayload returns the untouched source row this record was parsed from.
func (r ImportRecord) RawPayload() string { return r.rawPayload }

// BookedDate returns the row's normalised booked date.
func (r ImportRecord) BookedDate() domain.Date { return r.bookedDate }

// PostedDate returns the date the source reported, and false if none was
// set.
func (r ImportRecord) PostedDate() (domain.Date, bool) {
	if r.postedDate == nil {
		return domain.Date{}, false
	}
	return *r.postedDate, true
}

// Description returns the row's normalised description.
func (r ImportRecord) Description() string { return r.description }

// Amount returns the row's normalised, signed amount.
func (r ImportRecord) Amount() money.Money { return r.amount }

// ExternalID returns the source system's own ID for this row, and false if
// it has none.
func (r ImportRecord) ExternalID() (string, bool) {
	if r.externalID == nil {
		return "", false
	}
	return *r.externalID, true
}

// ResolvedAccountID returns the account the mapping stage resolved this
// row to, and false if it hasn't been resolved yet.
func (r ImportRecord) ResolvedAccountID() (string, bool) {
	if r.resolvedAccountID == nil {
		return "", false
	}
	return *r.resolvedAccountID, true
}

// ResolvedCategoryID returns the category the mapping stage resolved this
// row to, and false if it hasn't been resolved yet (or the row has none,
// e.g. a transfer).
func (r ImportRecord) ResolvedCategoryID() (string, bool) {
	if r.resolvedCategoryID == nil {
		return "", false
	}
	return *r.resolvedCategoryID, true
}

// DuplicateMatch returns the duplicate-detection candidate found for this
// row, and false if none was found.
func (r ImportRecord) DuplicateMatch() (DuplicateMatch, bool) {
	if r.duplicateMatch == nil {
		return DuplicateMatch{}, false
	}
	return *r.duplicateMatch, true
}

// Status returns the record's current status in ADR-0008's state machine.
func (r ImportRecord) Status() ImportRecordStatus { return r.status }

// TransactionID returns the transaction this record produced, and false if
// it hasn't been committed.
func (r ImportRecord) TransactionID() (string, bool) {
	if r.transactionID == nil {
		return "", false
	}
	return *r.transactionID, true
}

// SortOrder returns the record's position within its batch's source file —
// display and processing order, never a business invariant.
func (r ImportRecord) SortOrder() int { return r.sortOrder }

// Resolve returns a copy of r with the user's decision recorded on its
// DuplicateMatch — ADR-0008: "the decision is recorded on the ImportRecord
// for auditability." r itself is unchanged. It returns
// ErrImportRecordNoDuplicateMatch if r has no DuplicateMatch to resolve,
// and otherwise whatever DuplicateMatch.Resolve itself returns.
func (r ImportRecord) Resolve(resolution DuplicateResolution) (ImportRecord, error) {
	if r.duplicateMatch == nil {
		return ImportRecord{}, ErrImportRecordNoDuplicateMatch
	}
	resolved, err := r.duplicateMatch.Resolve(resolution)
	if err != nil {
		return ImportRecord{}, err
	}
	r.duplicateMatch = &resolved
	return r, nil
}

// transition returns a copy of r moved to next, or
// ErrImportRecordInvalidTransition if importRecordTransitions doesn't
// permit moving from r's current status to next.
func (r ImportRecord) transition(next ImportRecordStatus) (ImportRecord, error) {
	if !importRecordTransitions[r.status][next] {
		return ImportRecord{}, fmt.Errorf("%w: %q -> %q", ErrImportRecordInvalidTransition, r.status, next)
	}
	r.status = next
	return r, nil
}

// MarkReady returns a copy of r transitioned from pending to ready; r
// itself is unchanged. It returns ErrImportRecordInvalidTransition if r
// isn't currently pending.
func (r ImportRecord) MarkReady() (ImportRecord, error) {
	return r.transition(ImportRecordStatusReady)
}

// MarkExcluded returns a copy of r transitioned from pending to excluded;
// r itself is unchanged. It returns ErrImportRecordInvalidTransition if r
// isn't currently pending.
func (r ImportRecord) MarkExcluded() (ImportRecord, error) {
	return r.transition(ImportRecordStatusExcluded)
}

// MarkCommitted returns a copy of r transitioned from ready to committed,
// recording transactionID as the transaction this row produced; r itself
// is unchanged. It returns ErrImportRecordEmptyTransactionID if
// transactionID is empty, and ErrImportRecordInvalidTransition if r isn't
// currently ready. Writing the transaction itself is a separate issue's
// concern; this only records that it happened.
func (r ImportRecord) MarkCommitted(transactionID string) (ImportRecord, error) {
	if transactionID == "" {
		return ImportRecord{}, ErrImportRecordEmptyTransactionID
	}
	out, err := r.transition(ImportRecordStatusCommitted)
	if err != nil {
		return ImportRecord{}, err
	}
	tid := transactionID
	out.transactionID = &tid
	return out, nil
}
