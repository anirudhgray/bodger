package app

import (
	"context"
	"strings"

	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// ListImportBatchesQuery lists every import batch the actor owns —
// issue #212's batch-history use case.
type ListImportBatchesQuery struct {
	ActorID string
}

// ListImportBatchesResult is every import batch the actor owns, most
// recently created first (ImportBatchRepository.List's own order).
type ListImportBatchesResult struct {
	Batches []importing.ImportBatch
}

// ListImportBatches is a thin read over ImportBatches.List: seeing every
// batch's current status (staged, reviewed, committed, or rolled back,
// ADR-0008) is what lets a user find the one they meant to resume
// reviewing, commit, or roll back — the read-only counterpart to
// StageImport/CommitImportBatch/RollbackImportBatch's writes.
func (s *Service) ListImportBatches(ctx context.Context, q ListImportBatchesQuery) (ListImportBatchesResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ListImportBatchesResult{}, err
	}
	batches, err := s.ImportBatches.List(ctx, q.ActorID)
	if err != nil {
		return ListImportBatchesResult{}, err
	}
	return ListImportBatchesResult{Batches: batches}, nil
}

// GetImportBatchQuery identifies the import batch to look up.
type GetImportBatchQuery struct {
	ActorID        string
	ImportBatchRef string
}

// GetImportBatchResult is the queried batch.
type GetImportBatchResult struct {
	Batch importing.ImportBatch
}

// GetImportBatch implements issue #212's single-batch status lookup: the
// one piece of information every step after staging needs first — is
// this batch still staged, does it need review, has it already been
// committed or rolled back.
func (s *Service) GetImportBatch(ctx context.Context, q GetImportBatchQuery) (GetImportBatchResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return GetImportBatchResult{}, err
	}
	batchID := strings.TrimSpace(q.ImportBatchRef)
	if batchID == "" {
		return GetImportBatchResult{}, errs.New(errs.InvalidInput).Explain("An import batch ID is required.").Field("import_batch_ref")
	}
	batch, err := s.ImportBatches.Get(ctx, q.ActorID, batchID)
	if err != nil {
		return GetImportBatchResult{}, err
	}
	return GetImportBatchResult{Batch: batch}, nil
}

// ListImportRecordsQuery identifies the import batch whose staged records
// to list.
type ListImportRecordsQuery struct {
	ActorID        string
	ImportBatchRef string
}

// ListImportRecordsResult is every record staged into the queried batch,
// in the source file's own row order — each one carrying whatever
// duplicate/transfer flags StageImport (#210) found for it.
type ListImportRecordsResult struct {
	Records []importing.ImportRecord
}

// ListImportRecords implements issue #212's review use case: seeing every
// record a batch staged, together with the flags ADR-0008's duplicate/
// transfer detection attached to each — what a user reviews before
// deciding whether anything needs ResolveImportRecord, or the batch is
// already clear to commit.
func (s *Service) ListImportRecords(ctx context.Context, q ListImportRecordsQuery) (ListImportRecordsResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ListImportRecordsResult{}, err
	}
	batchID := strings.TrimSpace(q.ImportBatchRef)
	if batchID == "" {
		return ListImportRecordsResult{}, errs.New(errs.InvalidInput).Explain("An import batch ID is required.").Field("import_batch_ref")
	}

	// Confirms the batch exists (and belongs to this actor) before
	// listing its records, so an unknown batch ID reports NotFound rather
	// than a silently empty list — the same reasoning
	// ImportBatchTransactions' doc comment (import_commit.go) gives for
	// the identical check.
	if _, err := s.ImportBatches.Get(ctx, q.ActorID, batchID); err != nil {
		return ListImportRecordsResult{}, err
	}

	records, err := s.ImportRecords.ListByImportBatch(ctx, q.ActorID, batchID)
	if err != nil {
		return ListImportRecordsResult{}, err
	}
	return ListImportRecordsResult{Records: records}, nil
}

// ResolveImportRecordCommand records the user's decision about one staged
// record's duplicate proposal — ADR-0008: "the user resolves it; the
// decision is recorded on the ImportRecord for auditability."
//
// Resolution is one of importing.DuplicateResolutionConfirmed
// ("confirmed_duplicate") or importing.DuplicateResolutionDismissed
// ("not_duplicate"), passed straight through rather than pre-validated
// here — the same "no I/O or clock dependency" exception ADR-0005 carves
// out for a column mapping or a tag. Resolving an already-fully-specified
// decision needs no repository lookup or wall-clock access, so there's
// nothing here a surface couldn't equally have done itself if it were
// allowed to; importing.ImportRecord.Resolve is what actually validates
// it, this method only decides what happens to the record's *status*
// once that validation succeeds (see below).
//
// There is deliberately no separate "resolve a transfer candidate" verb.
// ADR-0008 is explicit that a transfer candidate
// (importing.WithTransferCandidate) is "proposed, never applied
// automatically", and unlike a DuplicateMatch it carries no resolution
// field of its own — nothing about commit behaviour changes because of
// it, with or without a decision recorded. What actually blocks a
// commit — and so is what a user is really resolving when a record's
// transfer candidate happens to coincide with a suspected duplicate
// (CommitImportBatch's own doc comment, import_commit.go) — is the
// record's DuplicateMatch, exactly what this method already handles: a
// transfer candidate with no accompanying DuplicateMatch never reaches
// ImportRecordStatusPending in the first place (buildImportRecord, #210,
// only holds a record pending for a suspected duplicate, never for a
// transfer candidate alone), so it has nothing pending to resolve. A
// surface reviewing a record still sees its own
// TransferCandidateRecordID (ListImportRecords) for display — this
// method just isn't where acting on it happens, because ADR-0008 gives it
// nothing to act on.
type ResolveImportRecordCommand struct {
	ActorID         string
	ImportRecordRef string
	Resolution      string
}

// ResolveImportRecordResult is the record ResolveImportRecord updated.
type ResolveImportRecordResult struct {
	Record importing.ImportRecord
}

// ResolveImportRecord implements issue #212's review use case: recording
// the user's decision on a record's suspected duplicate and moving it out
// of ImportRecordStatusPending accordingly — confirming it
// (DuplicateResolutionConfirmed) excludes the record from commit
// (ImportRecordStatusExcluded); dismissing it (DuplicateResolutionDismissed)
// clears the record for commit (ImportRecordStatusReady). Both are
// exactly the transitions CommitImportBatch (#211) requires every record
// to have made before a batch may commit.
//
// It refuses (PreconditionFailed) a record with no DuplicateMatch at all,
// a tier-1 exact match (ADR-0008: skipped automatically, never surfaced
// for review — DuplicateMatch.Resolve's own ErrDuplicateMatchExactNotResolvable
// case, given a clear message here rather than surfacing as an opaque
// Internal error), or a match that was already resolved: a record is
// resolved exactly once, matching ADR-0008's "recorded... for
// auditability" — silently allowing a second, different decision would
// make that record worthless.
func (s *Service) ResolveImportRecord(ctx context.Context, cmd ResolveImportRecordCommand) (ResolveImportRecordResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return ResolveImportRecordResult{}, err
	}
	recordID := strings.TrimSpace(cmd.ImportRecordRef)
	if recordID == "" {
		return ResolveImportRecordResult{}, errs.New(errs.InvalidInput).Explain("An import record ID is required.").Field("import_record_ref")
	}

	resolution := importing.DuplicateResolution(strings.TrimSpace(cmd.Resolution))
	if resolution != importing.DuplicateResolutionConfirmed && resolution != importing.DuplicateResolutionDismissed {
		return ResolveImportRecordResult{}, errs.New(errs.InvalidInput).
			Explain("%q isn't a valid resolution.", cmd.Resolution).
			Field("resolution").
			With("valid_resolutions", []string{string(importing.DuplicateResolutionConfirmed), string(importing.DuplicateResolutionDismissed)})
	}

	record, err := s.ImportRecords.Get(ctx, cmd.ActorID, recordID)
	if err != nil {
		return ResolveImportRecordResult{}, err
	}

	match, ok := record.DuplicateMatch()
	if !ok {
		return ResolveImportRecordResult{}, errs.New(errs.PreconditionFailed).
			Explain("Import record %q has no duplicate match to resolve.", record.ID()).
			Field("import_record_ref")
	}
	if match.Tier() == importing.DuplicateMatchTierExact {
		return ResolveImportRecordResult{}, errs.New(errs.PreconditionFailed).
			Explain("Import record %q is an exact duplicate; it was excluded automatically and there's nothing to resolve.", record.ID()).
			Field("import_record_ref")
	}
	if match.Resolved() {
		return ResolveImportRecordResult{}, errs.New(errs.PreconditionFailed).
			Explain("Import record %q was already resolved as %s.", record.ID(), match.Resolution()).
			Field("import_record_ref")
	}

	resolved, err := record.Resolve(resolution)
	if err != nil {
		return ResolveImportRecordResult{}, errs.New(errs.Internal).Wrap(err)
	}

	switch resolution {
	case importing.DuplicateResolutionConfirmed:
		resolved, err = resolved.MarkExcluded()
	case importing.DuplicateResolutionDismissed:
		resolved, err = resolved.MarkReady()
	}
	if err != nil {
		return ResolveImportRecordResult{}, errs.New(errs.Internal).Wrap(err)
	}

	if err := s.ImportRecords.Update(ctx, cmd.ActorID, resolved); err != nil {
		return ResolveImportRecordResult{}, err
	}
	return ResolveImportRecordResult{Record: resolved}, nil
}
