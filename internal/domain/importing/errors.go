package importing

import "errors"

// Named errors returned by this package. Callers should match on these
// with errors.Is; the wrapped text (added by the constructor that returns
// them) carries the offending value for humans, the sentinel carries the
// identity for code.
var (
	// ErrImportBatchEmptyID is returned when an import batch is constructed
	// with an empty ID.
	ErrImportBatchEmptyID = errors.New("importing: import batch id must not be empty")

	// ErrImportBatchEmptyUserID is returned when an import batch is
	// constructed with an empty user ID.
	ErrImportBatchEmptyUserID = errors.New("importing: import batch user id must not be empty")

	// ErrImportBatchEmptySourceFormat is returned when an import batch is
	// constructed with an empty source format.
	ErrImportBatchEmptySourceFormat = errors.New("importing: import batch source format must not be empty")

	// ErrImportBatchEmptyFilename is returned when an import batch is
	// constructed with an empty filename.
	ErrImportBatchEmptyFilename = errors.New("importing: import batch filename must not be empty")

	// ErrImportBatchEmptyFileHash is returned when an import batch is
	// constructed with an empty file hash.
	ErrImportBatchEmptyFileHash = errors.New("importing: import batch file hash must not be empty")

	// ErrImportBatchEmptyTargetAccountID is returned when an import batch
	// is constructed with an empty target account ID.
	ErrImportBatchEmptyTargetAccountID = errors.New("importing: import batch target account id must not be empty")

	// ErrImportBatchInvalidStatus is returned when an import batch is
	// constructed (via WithStatus) with a status outside the known set.
	ErrImportBatchInvalidStatus = errors.New("importing: invalid import batch status")

	// ErrImportBatchInvalidTransition is returned by MarkReviewed,
	// MarkCommitted, and MarkRolledBack when the batch's current status
	// doesn't permit the requested transition.
	ErrImportBatchInvalidTransition = errors.New("importing: invalid import batch status transition")

	// ErrImportRecordEmptyID is returned when an import record is
	// constructed with an empty ID.
	ErrImportRecordEmptyID = errors.New("importing: import record id must not be empty")

	// ErrImportRecordEmptyUserID is returned when an import record is
	// constructed with an empty user ID.
	ErrImportRecordEmptyUserID = errors.New("importing: import record user id must not be empty")

	// ErrImportRecordEmptyImportBatchID is returned when an import record
	// is constructed with an empty import batch ID.
	ErrImportRecordEmptyImportBatchID = errors.New("importing: import record import batch id must not be empty")

	// ErrImportRecordEmptyRawPayload is returned when an import record is
	// constructed with an empty raw payload.
	ErrImportRecordEmptyRawPayload = errors.New("importing: import record raw payload must not be empty")

	// ErrImportRecordEmptyDescription is returned when an import record is
	// constructed with an empty description.
	ErrImportRecordEmptyDescription = errors.New("importing: import record description must not be empty")

	// ErrImportRecordInvalidStatus is returned when an import record is
	// constructed (via WithRecordStatus) with a status outside the known
	// set.
	ErrImportRecordInvalidStatus = errors.New("importing: invalid import record status")

	// ErrImportRecordInvalidTransition is returned by MarkReady,
	// MarkExcluded, and MarkCommitted when the record's current status
	// doesn't permit the requested transition.
	ErrImportRecordInvalidTransition = errors.New("importing: invalid import record status transition")

	// ErrImportRecordNoDuplicateMatch is returned by Resolve when the
	// record has no DuplicateMatch to resolve.
	ErrImportRecordNoDuplicateMatch = errors.New("importing: cannot resolve a duplicate match that was never recorded")

	// ErrImportRecordEmptyTransactionID is returned by MarkCommitted when
	// given an empty transaction ID, and by the constructor when a record
	// is reconstructed with ImportRecordStatusCommitted but no transaction
	// ID.
	ErrImportRecordEmptyTransactionID = errors.New("importing: import record transaction id must not be empty")

	// ErrDuplicateMatchInvalidTier is returned when a DuplicateMatch is
	// constructed with a tier outside the two ADR-0008 defines.
	ErrDuplicateMatchInvalidTier = errors.New("importing: invalid duplicate match tier")

	// ErrDuplicateMatchEmptyTransactionID is returned when a DuplicateMatch
	// is constructed with an empty matched transaction ID.
	ErrDuplicateMatchEmptyTransactionID = errors.New("importing: duplicate match matched transaction id must not be empty")

	// ErrDuplicateMatchInvalidResolution is returned by Resolve when given
	// a resolution outside the known set, or DuplicateResolutionPending
	// (not a real decision).
	ErrDuplicateMatchInvalidResolution = errors.New("importing: invalid duplicate match resolution")

	// ErrDuplicateMatchExactNotResolvable is returned by Resolve when
	// called on a tier-1 exact match — ADR-0008: an exact match "is a
	// definite duplicate and is skipped" automatically, with no user
	// decision to record.
	ErrDuplicateMatchExactNotResolvable = errors.New("importing: an exact duplicate match has no resolution to record — it is skipped automatically")
)
