package app

import (
	"context"
	"strings"

	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
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
//
// An occurrence match (importing.WithOccurrenceMatch, issue #301) is
// different from a transfer candidate: unlike a transfer, it does gate
// commit (buildImportRecord, issue #309), and has its own resolve verb,
// ResolveImportRecordOccurrenceMatch, rather than this method — an
// occurrence match's two outcomes (materialise or dismiss the occurrence)
// don't fit DuplicateResolution's vocabulary, and materialising has a
// side effect (a new transaction) this method has no business triggering.
// A record can be pending for an unresolved DuplicateMatch, an unresolved
// occurrence match, or both at once; resolving one here never resolves or
// bypasses the other, and this method's own record.SettleStatus call
// leaves the record pending until every reason on it has cleared — see
// that method's own doc comment (internal/domain/importing/record.go).
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

	// SettleStatus, not a direct MarkExcluded/MarkReady switch on
	// resolution: a record can also carry an unresolved occurrence match
	// (issue #309), and resolving this DuplicateMatch alone must never
	// bypass that — see SettleStatus's own doc comment for the full
	// "leaves pending until every reason clears" rule and its
	// exclusion-wins-over-ready tiebreak.
	settled, err := resolved.SettleStatus()
	if err != nil {
		return ResolveImportRecordResult{}, errs.New(errs.Internal).Wrap(err)
	}

	if err := s.ImportRecords.Update(ctx, cmd.ActorID, settled); err != nil {
		return ResolveImportRecordResult{}, err
	}
	return ResolveImportRecordResult{Record: settled}, nil
}

// ResolveImportRecordOccurrenceMatchCommand records the user's decision
// about one staged record's occurrence match (issue #309's counterpart to
// ResolveImportRecordCommand, for the match findOccurrenceMatch attaches
// via importing.WithOccurrenceMatch — see that function's own doc comment,
// import_duplicate.go, for why an occurrence match was never shaped like a
// DuplicateMatch, and ResolveImportRecordCommand's doc comment for why
// this needs its own command rather than reusing that one: different
// resolution vocabulary, and a materialize outcome that — unlike anything
// ResolveImportRecord does — has its own ledger side effect via
// MaterialiseOccurrence).
//
// Resolution is one of importing.OccurrenceMatchResolutionMaterialized or
// importing.OccurrenceMatchResolutionDismissed, passed straight through
// rather than pre-validated here — the same reasoning
// ResolveImportRecordCommand's own doc comment gives.
//
//   - Materialized calls MaterialiseOccurrence for the matched occurrence
//     (the rule's own projected amount and date — overriding either at
//     materialisation time is issue #289's separate, deferred concern),
//     then excludes this record: the occurrence's own new transaction now
//     authoritatively covers this row's money, so committing the row too
//     would double it.
//   - Dismissed leaves the matched occurrence completely untouched — still
//     pending, for a human to separately materialise or skip later
//     through the existing recurring-occurrences surface — and clears
//     this record for commit.
//
// Deliberately no third "skip the occurrence" outcome: skipping is an
// independent decision about the recurring rule's own schedule, already
// reachable through SkipOccurrence/`bodger recurring skip`/
// POST .../skip, and conflating it here would give this command two
// unrelated reasons to touch the occurrence.
type ResolveImportRecordOccurrenceMatchCommand struct {
	ActorID         string
	ImportRecordRef string
	Resolution      string

	// OccurrenceID identifies the pending recurring.ScheduledOccurrence to
	// resolve against when the record has no pre-existing OccurrenceMatch
	// of its own — issue #307's AI-suggested occurrence match, offered by
	// SuggestForImportBatch even for a row findOccurrenceMatch's own
	// staging-time gate let through as ready with nothing attached (that
	// gate additionally requires description similarity; the AI's own
	// candidate narrowing deliberately doesn't, per ADR-0015 "the model
	// answers the semantics"). Ignored when the record already carries a
	// match — that match's own OccurrenceID is authoritative, exactly as
	// before this field existed. Required, and re-validated against the
	// same eligibility narrowing SuggestForImportBatch itself uses, when
	// the record has none: a client-supplied ID is never trusted without
	// re-confirming it actually names a pending, date/amount/currency-
	// eligible occurrence for this exact row.
	OccurrenceID string
}

// ResolveImportRecordOccurrenceMatchResult is the record
// ResolveImportRecordOccurrenceMatch updated, together with the
// occurrence and transaction a materialize resolution produced. Occurrence
// and Transaction are both nil for a dismiss resolution, which leaves the
// occurrence (and the ledger) completely untouched.
type ResolveImportRecordOccurrenceMatchResult struct {
	Record      importing.ImportRecord
	Occurrence  *recurring.ScheduledOccurrence
	Transaction *ledger.Transaction
}

// ResolveImportRecordOccurrenceMatch implements issue #309's occurrence-
// match review use case — see ResolveImportRecordOccurrenceMatchCommand's
// own doc comment for the two outcomes, and for OccurrenceID's own
// separate, issue #307 case.
//
// A record with no existing OccurrenceMatch attaches one first, built
// from OccurrenceID and re-validated against eligibleOccurrences (never
// trusting a client-supplied ID); this branch requires the record be
// ImportRecordStatusPending or -Ready (PreconditionFailed otherwise — a
// committed or excluded record is done, the same as the existing-match
// branch's own "already resolved" refusal below). A record that already
// carries one refuses (PreconditionFailed) only when it's already
// resolved — a record is resolved exactly once, matching ADR-0008's
// "recorded... for auditability" reasoning DuplicateMatch already
// follows.
//
// What happens to the record's status afterward depends on whether it
// was Pending or Ready going in. A Pending record uses SettleStatus, not
// a direct MarkExcluded/MarkReady switch: it can also carry an unresolved
// DuplicateMatch (issue #309's gating change means both can coexist), and
// resolving the occurrence match alone must never bypass that — see
// SettleStatus's own doc comment (internal/domain/importing/record.go)
// for the full rule and its exclusion-wins-over-ready tiebreak. A Ready
// record (issue #307's case: findOccurrenceMatch's own staging-time gate
// missed this match, so nothing ever held the record pending on it) uses
// ExcludeAfterLateOccurrenceMatch instead when the resolution is
// materialized — SettleStatus is a no-op on anything but Pending, and
// without this second path the occurrence's own new transaction and this
// record's still-uncommitted one would double the same money. A Ready
// record resolved dismissed needs no status change at all; leaving it
// Ready (SettleStatus's own no-op) is already correct.
//
// For a materialize resolution, MaterialiseOccurrence's side effect (a
// new transaction, and the occurrence itself moving to
// recurring.OccurrenceStatusMaterialised) always happens regardless of
// which status path follows — only the record's own status transition
// branches on Pending vs. Ready, not the occurrence's.
func (s *Service) ResolveImportRecordOccurrenceMatch(ctx context.Context, cmd ResolveImportRecordOccurrenceMatchCommand) (ResolveImportRecordOccurrenceMatchResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return ResolveImportRecordOccurrenceMatchResult{}, err
	}
	recordID := strings.TrimSpace(cmd.ImportRecordRef)
	if recordID == "" {
		return ResolveImportRecordOccurrenceMatchResult{}, errs.New(errs.InvalidInput).Explain("An import record ID is required.").Field("import_record_ref")
	}

	resolution := importing.OccurrenceMatchResolution(strings.TrimSpace(cmd.Resolution))
	if resolution != importing.OccurrenceMatchResolutionMaterialized && resolution != importing.OccurrenceMatchResolutionDismissed {
		return ResolveImportRecordOccurrenceMatchResult{}, errs.New(errs.InvalidInput).
			Explain("%q isn't a valid resolution.", cmd.Resolution).
			Field("resolution").
			With("valid_resolutions", []string{string(importing.OccurrenceMatchResolutionMaterialized), string(importing.OccurrenceMatchResolutionDismissed)})
	}

	record, err := s.ImportRecords.Get(ctx, cmd.ActorID, recordID)
	if err != nil {
		return ResolveImportRecordOccurrenceMatchResult{}, err
	}

	originalStatus := record.Status()
	working := record
	if match, ok := record.OccurrenceMatch(); ok {
		if match.Resolved() {
			return ResolveImportRecordOccurrenceMatchResult{}, errs.New(errs.PreconditionFailed).
				Explain("Import record %q's occurrence match was already resolved as %s.", record.ID(), match.Resolution()).
				Field("import_record_ref")
		}
	} else {
		occurrenceID := strings.TrimSpace(cmd.OccurrenceID)
		if occurrenceID == "" {
			return ResolveImportRecordOccurrenceMatchResult{}, errs.New(errs.PreconditionFailed).
				Explain("Import record %q has no matched occurrence to resolve.", record.ID()).
				Field("import_record_ref")
		}
		if originalStatus != importing.ImportRecordStatusPending && originalStatus != importing.ImportRecordStatusReady {
			return ResolveImportRecordOccurrenceMatchResult{}, errs.New(errs.PreconditionFailed).
				Explain("Import record %q has already been committed or excluded.", record.ID()).
				Field("import_record_ref")
		}
		accountID, hasAccount := record.ResolvedAccountID()
		if !hasAccount {
			return ResolveImportRecordOccurrenceMatchResult{}, errs.New(errs.PreconditionFailed).
				Explain("Import record %q has no resolved account yet.", record.ID()).
				Field("import_record_ref")
		}
		eligible, err := s.eligibleOccurrences(ctx, cmd.ActorID, accountID, record.Amount(), record.BookedDate())
		if err != nil {
			return ResolveImportRecordOccurrenceMatchResult{}, err
		}
		var eligibleHere bool
		for _, c := range eligible {
			if c.Occurrence.ID() == occurrenceID {
				eligibleHere = true
				break
			}
		}
		if !eligibleHere {
			return ResolveImportRecordOccurrenceMatchResult{}, errs.New(errs.PreconditionFailed).
				Explain("%q isn't a pending occurrence eligible to match import record %q.", occurrenceID, record.ID()).
				Field("occurrence_id")
		}
		newMatch, err := importing.NewOccurrenceMatch(occurrenceID)
		if err != nil {
			return ResolveImportRecordOccurrenceMatchResult{}, errs.New(errs.Internal).Wrap(err)
		}
		working, err = record.AttachOccurrenceMatch(newMatch)
		if err != nil {
			return ResolveImportRecordOccurrenceMatchResult{}, errs.New(errs.Internal).Wrap(err)
		}
	}

	var occurrencePtr *recurring.ScheduledOccurrence
	var txnPtr *ledger.Transaction
	if resolution == importing.OccurrenceMatchResolutionMaterialized {
		match, _ := working.OccurrenceMatch()
		materialised, err := s.MaterialiseOccurrence(ctx, MaterialiseOccurrenceCommand{
			ActorID: cmd.ActorID, OccurrenceID: match.OccurrenceID(),
		})
		if err != nil {
			return ResolveImportRecordOccurrenceMatchResult{}, err
		}
		occ, txn := materialised.Occurrence, materialised.Transaction
		occurrencePtr, txnPtr = &occ, &txn
	}

	resolved, err := working.ResolveOccurrenceMatch(resolution)
	if err != nil {
		return ResolveImportRecordOccurrenceMatchResult{}, errs.New(errs.Internal).Wrap(err)
	}

	var settled importing.ImportRecord
	if originalStatus == importing.ImportRecordStatusReady && resolution == importing.OccurrenceMatchResolutionMaterialized {
		settled, err = resolved.ExcludeAfterLateOccurrenceMatch()
	} else {
		settled, err = resolved.SettleStatus()
	}
	if err != nil {
		return ResolveImportRecordOccurrenceMatchResult{}, errs.New(errs.Internal).Wrap(err)
	}

	if err := s.ImportRecords.Update(ctx, cmd.ActorID, settled); err != nil {
		return ResolveImportRecordOccurrenceMatchResult{}, err
	}
	return ResolveImportRecordOccurrenceMatchResult{Record: settled, Occurrence: occurrencePtr, Transaction: txnPtr}, nil
}
