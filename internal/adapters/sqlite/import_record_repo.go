package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// ImportRecordRepository implements ports.ImportRecordRepository over a
// *DB.
type ImportRecordRepository struct {
	db *DB
}

// NewImportRecordRepository constructs an ImportRecordRepository backed by
// db.
func NewImportRecordRepository(db *DB) *ImportRecordRepository {
	return &ImportRecordRepository{db: db}
}

var _ ports.ImportRecordRepository = (*ImportRecordRepository)(nil)

// importRecordSelect is the column list every read path in this file
// selects, ahead of whatever WHERE/ORDER BY clause the caller appends.
const importRecordSelect = `
	SELECT id, import_batch_id, user_id, raw_payload, booked_date, posted_date, description,
	       amount_minor, currency, external_id, resolved_account_id, resolved_category_id,
	       duplicate_tier, duplicate_matched_transaction_id, duplicate_resolution,
	       transfer_candidate_record_id,
	       status, transaction_id, sort_order
	FROM import_record
`

// CreateBatch implements ports.ImportRecordRepository.
func (r *ImportRecordRepository) CreateBatch(ctx context.Context, actorID string, records []importing.ImportRecord) error {
	if len(records) == 0 {
		return nil
	}
	for _, rec := range records {
		if err := requireActor(actorID, rec.UserID()); err != nil {
			return err
		}
	}

	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	now := formatTime(r.db.clock.Now())
	for _, rec := range records {
		if err := insertImportRecord(ctx, tx, actorID, rec, now); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}

// Get implements ports.ImportRecordRepository.
func (r *ImportRecordRepository) Get(ctx context.Context, actorID, id string) (importing.ImportRecord, error) {
	if err := requireActorID(actorID); err != nil {
		return importing.ImportRecord{}, err
	}

	row := r.db.read.QueryRowContext(ctx, importRecordSelect+`
		WHERE id = ? AND user_id = ?
	`, id, actorID)

	rec, err := scanImportRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return importing.ImportRecord{}, errs.New(errs.NotFound).Explain("No import record with ID %q.", id).Field("id")
	}
	if err != nil {
		return importing.ImportRecord{}, errs.New(errs.Internal).Wrap(err)
	}
	return rec, nil
}

// ListByImportBatch implements ports.ImportRecordRepository.
func (r *ImportRecordRepository) ListByImportBatch(ctx context.Context, actorID, importBatchID string) ([]importing.ImportRecord, error) {
	if err := requireActorID(actorID); err != nil {
		return nil, err
	}

	rows, err := r.db.read.QueryContext(ctx, importRecordSelect+`
		WHERE import_batch_id = ? AND user_id = ?
		ORDER BY sort_order, id
	`, importBatchID, actorID)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var records []importing.ImportRecord
	for rows.Next() {
		rec, err := scanImportRecord(rows)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	return records, nil
}

// Update implements ports.ImportRecordRepository.
func (r *ImportRecordRepository) Update(ctx context.Context, actorID string, record importing.ImportRecord) error {
	if err := requireActor(actorID, record.UserID()); err != nil {
		return err
	}

	now := formatTime(r.db.clock.Now())
	postedDate, hasPostedDate := record.PostedDate()
	externalID, hasExternalID := record.ExternalID()
	resolvedAccountID, hasResolvedAccountID := record.ResolvedAccountID()
	resolvedCategoryID, hasResolvedCategoryID := record.ResolvedCategoryID()
	transactionID, hasTransactionID := record.TransactionID()
	duplicateTier, matchedTransactionID, duplicateResolution := nullableDuplicateMatch(record)
	transferCandidateID, hasTransferCandidate := record.TransferCandidateRecordID()

	result, err := r.db.write.ExecContext(ctx, `
		UPDATE import_record
		SET raw_payload = ?, booked_date = ?, posted_date = ?, description = ?, amount_minor = ?, currency = ?,
		    external_id = ?, resolved_account_id = ?, resolved_category_id = ?,
		    duplicate_tier = ?, duplicate_matched_transaction_id = ?, duplicate_resolution = ?,
		    transfer_candidate_record_id = ?,
		    status = ?, transaction_id = ?, sort_order = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`,
		record.RawPayload(), formatDate(record.BookedDate()), nullableDate(postedDate, hasPostedDate), record.Description(),
		record.Amount().AmountMinor(), record.Amount().Currency(),
		nullableString(externalID, hasExternalID), nullableString(resolvedAccountID, hasResolvedAccountID), nullableString(resolvedCategoryID, hasResolvedCategoryID),
		duplicateTier, matchedTransactionID, duplicateResolution,
		nullableString(transferCandidateID, hasTransferCandidate),
		string(record.Status()), nullableString(transactionID, hasTransactionID), record.SortOrder(), now,
		record.ID(), actorID,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't update import record %q.", record.ID())
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No import record with ID %q.", record.ID()).Field("id")
	}
	return nil
}

// insertImportRecord writes one import_record row through tx — a
// transaction shared across every row CreateBatch inserts, so the whole
// batch's records land atomically.
func insertImportRecord(ctx context.Context, tx execer, actorID string, record importing.ImportRecord, now string) *errs.Error {
	postedDate, hasPostedDate := record.PostedDate()
	externalID, hasExternalID := record.ExternalID()
	resolvedAccountID, hasResolvedAccountID := record.ResolvedAccountID()
	resolvedCategoryID, hasResolvedCategoryID := record.ResolvedCategoryID()
	transactionID, hasTransactionID := record.TransactionID()
	duplicateTier, matchedTransactionID, duplicateResolution := nullableDuplicateMatch(record)
	transferCandidateID, hasTransferCandidate := record.TransferCandidateRecordID()

	_, err := tx.ExecContext(ctx, `
		INSERT INTO import_record (
			id, import_batch_id, user_id, raw_payload, booked_date, posted_date, description,
			amount_minor, currency, external_id, resolved_account_id, resolved_category_id,
			duplicate_tier, duplicate_matched_transaction_id, duplicate_resolution,
			transfer_candidate_record_id,
			status, transaction_id, sort_order, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		record.ID(), record.ImportBatchID(), actorID, record.RawPayload(), formatDate(record.BookedDate()),
		nullableDate(postedDate, hasPostedDate), record.Description(),
		record.Amount().AmountMinor(), record.Amount().Currency(),
		nullableString(externalID, hasExternalID), nullableString(resolvedAccountID, hasResolvedAccountID),
		nullableString(resolvedCategoryID, hasResolvedCategoryID),
		duplicateTier, matchedTransactionID, duplicateResolution,
		nullableString(transferCandidateID, hasTransferCandidate),
		string(record.Status()), nullableString(transactionID, hasTransactionID), record.SortOrder(), now, now,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't create import record %q.", record.ID())
	}
	return nil
}

// nullableDuplicateMatch returns the three column values to write for
// record's DuplicateMatch: both tier/matched-transaction-id columns NULL
// and resolution 'pending' unless record actually carries one.
func nullableDuplicateMatch(record importing.ImportRecord) (tier, matchedTransactionID sql.NullString, resolution string) {
	match, ok := record.DuplicateMatch()
	if !ok {
		return sql.NullString{}, sql.NullString{}, string(importing.DuplicateResolutionPending)
	}
	return sql.NullString{String: string(match.Tier()), Valid: true},
		sql.NullString{String: match.MatchedTransactionID(), Valid: true},
		string(match.Resolution())
}

func scanImportRecord(row rowScanner) (importing.ImportRecord, error) {
	var (
		id, importBatchID, userID, rawPayload, bookedDateCol, description string
		postedDateCol                                                     sql.NullString
		amountMinor                                                       int64
		currency                                                          string
		externalIDCol                                                     sql.NullString
		resolvedAccountIDCol                                              sql.NullString
		resolvedCategoryIDCol                                             sql.NullString
		duplicateTierCol                                                  sql.NullString
		duplicateMatchedTxnIDCol                                          sql.NullString
		duplicateResolutionCol                                            string
		transferCandidateIDCol                                            sql.NullString
		status                                                            string
		transactionIDCol                                                  sql.NullString
		sortOrder                                                         int
	)
	if err := row.Scan(
		&id, &importBatchID, &userID, &rawPayload, &bookedDateCol, &postedDateCol, &description,
		&amountMinor, &currency, &externalIDCol, &resolvedAccountIDCol, &resolvedCategoryIDCol,
		&duplicateTierCol, &duplicateMatchedTxnIDCol, &duplicateResolutionCol,
		&transferCandidateIDCol,
		&status, &transactionIDCol, &sortOrder,
	); err != nil {
		return importing.ImportRecord{}, err
	}

	bookedDate, err := parseDate(bookedDateCol)
	if err != nil {
		return importing.ImportRecord{}, err
	}
	amount, err := money.NewMoney(amountMinor, currency)
	if err != nil {
		return importing.ImportRecord{}, err
	}

	var opts []importing.ImportRecordOption
	if postedDateCol.Valid {
		d, err := parseDate(postedDateCol.String)
		if err != nil {
			return importing.ImportRecord{}, err
		}
		opts = append(opts, importing.WithPostedDate(d))
	}
	if externalIDCol.Valid {
		opts = append(opts, importing.WithExternalID(externalIDCol.String))
	}
	if resolvedAccountIDCol.Valid {
		opts = append(opts, importing.WithResolvedAccount(resolvedAccountIDCol.String))
	}
	if resolvedCategoryIDCol.Valid {
		opts = append(opts, importing.WithResolvedCategory(resolvedCategoryIDCol.String))
	}
	if duplicateTierCol.Valid {
		match, err := importing.NewDuplicateMatch(importing.DuplicateMatchTier(duplicateTierCol.String), duplicateMatchedTxnIDCol.String)
		if err != nil {
			return importing.ImportRecord{}, err
		}
		if importing.DuplicateResolution(duplicateResolutionCol) != importing.DuplicateResolutionPending {
			match, err = match.Resolve(importing.DuplicateResolution(duplicateResolutionCol))
			if err != nil {
				return importing.ImportRecord{}, err
			}
		}
		opts = append(opts, importing.WithDuplicateMatch(match))
	}
	if transferCandidateIDCol.Valid {
		opts = append(opts, importing.WithTransferCandidate(transferCandidateIDCol.String))
	}
	opts = append(opts, importing.WithRecordStatus(importing.ImportRecordStatus(status)))
	if transactionIDCol.Valid {
		opts = append(opts, importing.WithTransactionID(transactionIDCol.String))
	}

	return importing.NewImportRecord(id, userID, importBatchID, rawPayload, bookedDate, description, amount, sortOrder, opts...)
}
