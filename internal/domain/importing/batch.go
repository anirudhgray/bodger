package importing

import "fmt"

// ImportBatchStatus is the status ADR-0008's staged pipeline moves an
// ImportBatch through: staged -> reviewed -> committed -> rolled_back.
// Nothing before ImportBatchStatusCommitted affects a balance, a report, or
// a budget (ADR-0008) — every status short of that is pure staging.
type ImportBatchStatus string

const (
	// ImportBatchStatusStaged is a batch's starting status: its records
	// exist, but the user hasn't confirmed them for commit yet. A batch may
	// sit here indefinitely — ADR-0008: "the pipeline can stop here
	// indefinitely." A retention policy for abandoned staged imports is
	// explicitly deferred by that ADR and is not this package's concern.
	ImportBatchStatusStaged ImportBatchStatus = "staged"

	// ImportBatchStatusReviewed means the user has confirmed the batch's
	// records (resolving any suspected duplicates along the way) and it is
	// ready to commit.
	ImportBatchStatusReviewed ImportBatchStatus = "reviewed"

	// ImportBatchStatusCommitted means the batch's records were written to
	// the ledger as real transactions, in one database transaction
	// (ADR-0008). The commit logic itself is a separate issue; this status
	// only records that it happened.
	ImportBatchStatusCommitted ImportBatchStatus = "committed"

	// ImportBatchStatusRolledBack means a committed batch's transactions
	// were soft-deleted — ADR-0008: "a batch remains rollback-able after
	// the fact." Only a committed batch can be rolled back. The rollback
	// logic itself is a separate issue; this status only records that it
	// happened.
	ImportBatchStatusRolledBack ImportBatchStatus = "rolled_back"
)

var validImportBatchStatuses = map[ImportBatchStatus]bool{
	ImportBatchStatusStaged:     true,
	ImportBatchStatusReviewed:   true,
	ImportBatchStatusCommitted:  true,
	ImportBatchStatusRolledBack: true,
}

// importBatchTransitions is the state machine's edge list: for a given
// current status, the one status a transition may move it to. Every
// transition is forward-only and one step at a time — there is no way to
// skip review, un-review a batch, or commit directly from rolled_back —
// matching ADR-0008's linear staged -> reviewed -> committed -> rolled_back
// pipeline exactly.
var importBatchTransitions = map[ImportBatchStatus]ImportBatchStatus{
	ImportBatchStatusStaged:    ImportBatchStatusReviewed,
	ImportBatchStatusReviewed:  ImportBatchStatusCommitted,
	ImportBatchStatusCommitted: ImportBatchStatusRolledBack,
}

// ImportBatch is one imported file or session: ADR-0008's "one file/
// session: source format, filename, hash, target account, status,
// timestamps" (data-model.md §12). created_at/updated_at are audit columns
// the persistence layer owns, the same way they are for ledger.Transaction
// — not a domain field here.
//
// Its fields are unexported: the only way to produce one is
// NewImportBatch, and the only way to move it through the status state
// machine is MarkReviewed, MarkCommitted, and MarkRolledBack.
type ImportBatch struct {
	id              string
	userID          string
	sourceFormat    string
	filename        string
	fileHash        string
	targetAccountID string
	status          ImportBatchStatus
}

// ImportBatchOption sets one of an ImportBatch's fields at construction
// time to something other than its default. See WithStatus.
type ImportBatchOption func(*ImportBatch)

// WithStatus sets the batch's status directly to status, bypassing the
// transition state machine (MarkReviewed/MarkCommitted/MarkRolledBack).
// This exists for the sqlite adapter to reconstruct a batch from its
// already-validated, already-persisted status column — the same
// restore-not-re-derive shape as ledger.Transaction.WithFxRate, not a way
// for application code to skip review or commit by construction.
// NewImportBatch defaults to ImportBatchStatusStaged, the only status a
// genuinely new batch is ever created in.
func WithStatus(status ImportBatchStatus) ImportBatchOption {
	return func(b *ImportBatch) { b.status = status }
}

// NewImportBatch constructs an ImportBatch in ImportBatchStatusStaged
// unless overridden by WithStatus. sourceFormat is a free-text label (e.g.
// "csv", "ofx") this package doesn't validate against a known list —
// recognising a format is the parser registry's job (a separate,
// out-of-scope issue), not this one's.
func NewImportBatch(id, userID, sourceFormat, filename, fileHash, targetAccountID string, opts ...ImportBatchOption) (ImportBatch, error) {
	if id == "" {
		return ImportBatch{}, ErrImportBatchEmptyID
	}
	if userID == "" {
		return ImportBatch{}, ErrImportBatchEmptyUserID
	}
	if sourceFormat == "" {
		return ImportBatch{}, ErrImportBatchEmptySourceFormat
	}
	if filename == "" {
		return ImportBatch{}, ErrImportBatchEmptyFilename
	}
	if fileHash == "" {
		return ImportBatch{}, ErrImportBatchEmptyFileHash
	}
	if targetAccountID == "" {
		return ImportBatch{}, ErrImportBatchEmptyTargetAccountID
	}

	b := ImportBatch{
		id:              id,
		userID:          userID,
		sourceFormat:    sourceFormat,
		filename:        filename,
		fileHash:        fileHash,
		targetAccountID: targetAccountID,
		status:          ImportBatchStatusStaged,
	}
	for _, opt := range opts {
		opt(&b)
	}
	if !validImportBatchStatuses[b.status] {
		return ImportBatch{}, fmt.Errorf("%w: %q", ErrImportBatchInvalidStatus, b.status)
	}
	return b, nil
}

// ID returns the batch's identifier.
func (b ImportBatch) ID() string { return b.id }

// UserID returns the ID of the user who owns the batch.
func (b ImportBatch) UserID() string { return b.userID }

// SourceFormat returns the free-text label identifying which parser
// produced this batch's records (e.g. "csv", "ofx").
func (b ImportBatch) SourceFormat() string { return b.sourceFormat }

// Filename returns the original filename the batch was imported from.
func (b ImportBatch) Filename() string { return b.filename }

// FileHash returns the content hash of the source file — for an
// idempotency check a later issue may add (e.g. refusing to re-stage a
// file that was already imported).
func (b ImportBatch) FileHash() string { return b.fileHash }

// TargetAccountID returns the account new transactions from this batch
// will be posted against once committed.
func (b ImportBatch) TargetAccountID() string { return b.targetAccountID }

// Status returns the batch's current status in ADR-0008's state machine.
func (b ImportBatch) Status() ImportBatchStatus { return b.status }

// transition returns a copy of b moved to next, or
// ErrImportBatchInvalidTransition if importBatchTransitions doesn't permit
// moving from b's current status to next.
func (b ImportBatch) transition(next ImportBatchStatus) (ImportBatch, error) {
	if importBatchTransitions[b.status] != next {
		return ImportBatch{}, fmt.Errorf("%w: %q -> %q", ErrImportBatchInvalidTransition, b.status, next)
	}
	b.status = next
	return b, nil
}

// MarkReviewed returns a copy of b transitioned from staged to reviewed; b
// itself is unchanged. It returns ErrImportBatchInvalidTransition if b
// isn't currently staged.
func (b ImportBatch) MarkReviewed() (ImportBatch, error) {
	return b.transition(ImportBatchStatusReviewed)
}

// MarkCommitted returns a copy of b transitioned from reviewed to
// committed; b itself is unchanged. It returns
// ErrImportBatchInvalidTransition if b isn't currently reviewed — a batch
// cannot be committed without going through review first, and cannot be
// committed twice. Writing the batch's transactions is a separate issue's
// concern; this only records that it happened.
func (b ImportBatch) MarkCommitted() (ImportBatch, error) {
	return b.transition(ImportBatchStatusCommitted)
}

// MarkRolledBack returns a copy of b transitioned from committed to
// rolled_back; b itself is unchanged. It returns
// ErrImportBatchInvalidTransition if b isn't currently committed — only a
// committed batch has transactions to undo (ADR-0008). Soft-deleting those
// transactions is a separate issue's concern; this only records that it
// happened.
func (b ImportBatch) MarkRolledBack() (ImportBatch, error) {
	return b.transition(ImportBatchStatusRolledBack)
}
