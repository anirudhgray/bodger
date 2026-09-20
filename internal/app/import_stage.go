package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/anirudhgray/bodger/internal/app/importparse"
	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// maxImportFilenameLen caps a staged batch's stored filename — generous
// enough for any real upload, just enough of a bound that a pathological
// value can't grow the import_batch row without limit.
const maxImportFilenameLen = 255

// StageImportCommand stages a new import batch from a raw source file's
// bytes: parsing, mapping (account/category/currency resolution), and
// duplicate/transfer detection — everything ADR-0008's staged pipeline
// does short of commit (issue #211's separate concern) or a surface
// decoding an upload into this command (issue #212's separate concern).
//
// ColumnMapping is typed directly rather than a raw string: unlike a
// command's ambiguous fields (ADR-0005's AccountRef, Amount, Date, ...), a
// column mapping needs no I/O or clock to resolve — it's already exactly
// the column names the file has, the same "no I/O dependency" exception
// ADR-0005 carves out for Tags.
//
// SourceFormat names which importparse.Parser to use; "csv" is the only
// supported value today (ADR-0008 defers other formats to later issues).
type StageImportCommand struct {
	ActorID       string
	AccountRef    string
	Filename      string
	SourceFormat  string
	FileContent   []byte
	ColumnMapping importparse.ColumnMapping
}

// StageImportResult is the batch StageImport created, together with every
// record it staged, in the source file's own row order.
type StageImportResult struct {
	Batch   importing.ImportBatch
	Records []importing.ImportRecord
}

// StageImport implements issue #210's use case: turning an uploaded
// file's bytes into a staged, reviewable ImportBatch and its
// ImportRecords. Nothing it does touches a balance, a report, or a budget
// (ADR-0008) — every record it writes lands in ImportRecordStatusPending
// or -Ready, or (a tier-1 exact duplicate) -Excluded, never -Committed;
// producing real transactions is issue #211's separate Commit method,
// which this one never calls.
func (s *Service) StageImport(ctx context.Context, cmd StageImportCommand) (StageImportResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return StageImportResult{}, err
	}

	account, err := s.resolveOwnedAccount(ctx, cmd.ActorID, cmd.AccountRef)
	if err != nil {
		return StageImportResult{}, attachField(err, "account_ref")
	}

	// An empty format defaults to "csv" here, in the application layer,
	// rather than a surface hardcoding that default itself (ADR-0005: no
	// surface may "resolve any default") — today it's also the only
	// legal value, so the two cases collapse, but the distinction still
	// matters: a surface passes through whatever it was given, including
	// nothing, and this method is the one place that decides what
	// nothing means.
	format := strings.ToLower(strings.TrimSpace(cmd.SourceFormat))
	if format == "" {
		format = "csv"
	}
	if format != "csv" {
		return StageImportResult{}, errs.New(errs.InvalidInput).
			Explain("%q isn't a supported import format.", cmd.SourceFormat).
			Field("source_format").
			With("valid_formats", []string{"csv"})
	}

	filename, err := normalize.Text(cmd.Filename, maxImportFilenameLen)
	if err != nil {
		return StageImportResult{}, attachField(err, "filename")
	}
	if filename == "" {
		return StageImportResult{}, errs.New(errs.InvalidInput).Explain("A filename is required.").Field("filename")
	}

	parser, err := importparse.NewCSVParser(cmd.ColumnMapping, account.Currency(), s.Clock, s.Config.UserTimezone)
	if err != nil {
		return StageImportResult{}, err
	}
	parsedRows, err := parser.Parse(cmd.FileContent)
	if err != nil {
		return StageImportResult{}, err
	}

	batchID := s.IDs.NewID()
	batch, err := importing.NewImportBatch(batchID, cmd.ActorID, format, filename, hashFileContent(cmd.FileContent), account.ID())
	if err != nil {
		return StageImportResult{}, errs.New(errs.Internal).Wrap(err)
	}

	records := make([]importing.ImportRecord, 0, len(parsedRows))
	for _, row := range parsedRows {
		record, err := s.buildImportRecord(ctx, cmd.ActorID, batch, account, row)
		if err != nil {
			return StageImportResult{}, err
		}
		records = append(records, record)
	}

	if err := s.ImportBatches.Create(ctx, cmd.ActorID, batch); err != nil {
		return StageImportResult{}, err
	}
	if err := s.ImportRecords.CreateBatch(ctx, cmd.ActorID, records); err != nil {
		return StageImportResult{}, err
	}

	return StageImportResult{Batch: batch, Records: records}, nil
}

// hashFileContent returns a hex-encoded SHA-256 digest of content —
// ImportBatch's FileHash, per ADR-0008 ("content hash of the source file,
// for an idempotency check a later issue may add").
func hashFileContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// buildImportRecord resolves mapping (account/category) and runs
// duplicate/transfer/occurrence detection for one parsed row, returning
// the ImportRecord ready to persist — already transitioned to
// ImportRecordStatusReady (nothing to review), ImportRecordStatusExcluded
// (a tier-1 exact duplicate, auto-excluded per ADR-0008), or left at
// ImportRecordStatusPending (a tier-2 suspected duplicate and/or a
// matched occurrence, issue #309, awaiting the user's review).
func (s *Service) buildImportRecord(ctx context.Context, actorID string, batch importing.ImportBatch, account ledger.Account, row importparse.Row) (importing.ImportRecord, error) {
	var opts []importing.ImportRecordOption
	if row.PostedDate != nil {
		opts = append(opts, importing.WithPostedDate(*row.PostedDate))
	}
	if row.ExternalID != "" {
		opts = append(opts, importing.WithExternalID(row.ExternalID))
	}
	opts = append(opts, importing.WithResolvedAccount(account.ID()))

	if row.CategoryHint != "" {
		// An unresolved hint (no match, or an ambiguous one) is left for
		// the user to assign during review — a category is optional on a
		// posting, so this is never a fatal error for the row.
		if category, err := s.resolveOwnedCategory(ctx, actorID, row.CategoryHint); err == nil {
			opts = append(opts, importing.WithResolvedCategory(category.ID()))
		}
	}

	exactMatchID, isExact, err := s.findExactDuplicate(ctx, actorID, account.ID(), row.ExternalID)
	if err != nil {
		return importing.ImportRecord{}, err
	}

	var suspectedMatchID string
	var isSuspected bool
	if !isExact {
		suspectedMatchID, isSuspected, err = s.findSuspectedDuplicate(ctx, actorID, account.ID(), row.Amount, row.BookedDate, row.Description)
		if err != nil {
			return importing.ImportRecord{}, err
		}
	}

	switch {
	case isExact:
		match, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierExact, exactMatchID)
		if err != nil {
			return importing.ImportRecord{}, errs.New(errs.Internal).Wrap(err)
		}
		opts = append(opts, importing.WithDuplicateMatch(match))
	case isSuspected:
		match, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, suspectedMatchID)
		if err != nil {
			return importing.ImportRecord{}, errs.New(errs.Internal).Wrap(err)
		}
		opts = append(opts, importing.WithDuplicateMatch(match))
	}

	// Transfer detection only matters for a row that might actually be
	// committed — skip it for an already-excluded exact duplicate.
	if !isExact {
		transferRecordID, found, err := s.findTransferCandidateRecord(ctx, actorID, batch.ID(), account.ID(), row.Amount, row.BookedDate)
		if err != nil {
			return importing.ImportRecord{}, err
		}
		if found {
			opts = append(opts, importing.WithTransferCandidate(transferRecordID))
		}
	}

	// Occurrence matching (issue #301) is independent of, and runs
	// alongside, the transaction-shaped duplicate/transfer checks above —
	// it looks at pending recurring.ScheduledOccurrences, not committed
	// transactions or other staged records. Skipped for an already-excluded
	// exact duplicate for the same reason transfer detection is: nothing
	// about an excluded row's fate is still open to change.
	var hasOccurrenceMatch bool
	if !isExact {
		occurrenceID, found, err := s.findOccurrenceMatch(ctx, actorID, account.ID(), row.Amount, row.BookedDate, row.Description)
		if err != nil {
			return importing.ImportRecord{}, err
		}
		if found {
			match, err := importing.NewOccurrenceMatch(occurrenceID)
			if err != nil {
				return importing.ImportRecord{}, errs.New(errs.Internal).Wrap(err)
			}
			opts = append(opts, importing.WithOccurrenceMatch(match))
			hasOccurrenceMatch = true
		}
	}

	record, err := importing.NewImportRecord(
		s.IDs.NewID(), actorID, batch.ID(), row.RawPayload, row.BookedDate, row.Description, row.Amount, row.SortOrder, opts...,
	)
	if err != nil {
		return importing.ImportRecord{}, errs.New(errs.Internal).Wrap(err)
	}

	// A record clears review immediately (ImportRecordStatusReady) only
	// when neither of the two things that could hold it for review found
	// anything: no suspected duplicate (ADR-0008), and no occurrence match
	// (issue #309 — leaving an occurrence-matched row un-gated, as #301
	// originally landed it, meant detection had zero observable effect on
	// commit; see findOccurrenceMatch's and WithOccurrenceMatch's own doc
	// comments). A record can be pending for either reason, both, or
	// neither; ImportRecord.SettleStatus is what later clears it once
	// every pending reason on it has been resolved.
	switch {
	case isExact:
		record, err = record.MarkExcluded()
	case !isSuspected && !hasOccurrenceMatch:
		record, err = record.MarkReady()
	}
	if err != nil {
		return importing.ImportRecord{}, errs.New(errs.Internal).Wrap(err)
	}

	return record, nil
}
