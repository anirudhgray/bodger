package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// ImportBatchRepository implements ports.ImportBatchRepository over a *DB.
type ImportBatchRepository struct {
	db *DB
}

// NewImportBatchRepository constructs an ImportBatchRepository backed by
// db.
func NewImportBatchRepository(db *DB) *ImportBatchRepository {
	return &ImportBatchRepository{db: db}
}

var _ ports.ImportBatchRepository = (*ImportBatchRepository)(nil)

// Create implements ports.ImportBatchRepository.
func (r *ImportBatchRepository) Create(ctx context.Context, actorID string, batch importing.ImportBatch) error {
	if err := requireActor(actorID, batch.UserID()); err != nil {
		return err
	}

	now := formatTime(r.db.clock.Now())
	_, err := r.db.write.ExecContext(ctx, `
		INSERT INTO import_batch (id, user_id, source_format, filename, file_hash, target_account_id, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		batch.ID(), actorID, batch.SourceFormat(), batch.Filename(), batch.FileHash(), batch.TargetAccountID(),
		string(batch.Status()), now, now,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't create import batch %q.", batch.Filename())
	}
	return nil
}

// Get implements ports.ImportBatchRepository.
func (r *ImportBatchRepository) Get(ctx context.Context, actorID, id string) (importing.ImportBatch, error) {
	if err := requireActorID(actorID); err != nil {
		return importing.ImportBatch{}, err
	}

	row := r.db.read.QueryRowContext(ctx, `
		SELECT id, user_id, source_format, filename, file_hash, target_account_id, status
		FROM import_batch
		WHERE id = ? AND user_id = ?
	`, id, actorID)

	batch, err := scanImportBatch(row)
	if errors.Is(err, sql.ErrNoRows) {
		return importing.ImportBatch{}, errs.New(errs.NotFound).Explain("No import batch with ID %q.", id).Field("id")
	}
	if err != nil {
		return importing.ImportBatch{}, errs.New(errs.Internal).Wrap(err)
	}
	return batch, nil
}

// List implements ports.ImportBatchRepository.
func (r *ImportBatchRepository) List(ctx context.Context, actorID string) ([]importing.ImportBatch, error) {
	if err := requireActorID(actorID); err != nil {
		return nil, err
	}

	rows, err := r.db.read.QueryContext(ctx, `
		SELECT id, user_id, source_format, filename, file_hash, target_account_id, status
		FROM import_batch
		WHERE user_id = ?
		ORDER BY created_at DESC, id DESC
	`, actorID)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var batches []importing.ImportBatch
	for rows.Next() {
		batch, err := scanImportBatch(rows)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		batches = append(batches, batch)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	return batches, nil
}

// Update implements ports.ImportBatchRepository.
func (r *ImportBatchRepository) Update(ctx context.Context, actorID string, batch importing.ImportBatch) error {
	if err := requireActor(actorID, batch.UserID()); err != nil {
		return err
	}

	now := formatTime(r.db.clock.Now())
	result, err := r.db.write.ExecContext(ctx, `
		UPDATE import_batch
		SET source_format = ?, filename = ?, file_hash = ?, target_account_id = ?, status = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`,
		batch.SourceFormat(), batch.Filename(), batch.FileHash(), batch.TargetAccountID(),
		string(batch.Status()), now,
		batch.ID(), actorID,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't update import batch %q.", batch.Filename())
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No import batch with ID %q.", batch.ID()).Field("id")
	}
	return nil
}

func scanImportBatch(row rowScanner) (importing.ImportBatch, error) {
	var id, userID, sourceFormat, filename, fileHash, targetAccountID, status string
	if err := row.Scan(&id, &userID, &sourceFormat, &filename, &fileHash, &targetAccountID, &status); err != nil {
		return importing.ImportBatch{}, err
	}
	return importing.NewImportBatch(id, userID, sourceFormat, filename, fileHash, targetAccountID,
		importing.WithStatus(importing.ImportBatchStatus(status)))
}
