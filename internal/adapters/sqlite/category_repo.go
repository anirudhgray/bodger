package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// CategoryRepository implements ports.CategoryRepository over a *DB.
type CategoryRepository struct {
	db *DB
}

// NewCategoryRepository constructs a CategoryRepository backed by db.
func NewCategoryRepository(db *DB) *CategoryRepository {
	return &CategoryRepository{db: db}
}

var _ ports.CategoryRepository = (*CategoryRepository)(nil)

// Create implements ports.CategoryRepository.
func (r *CategoryRepository) Create(ctx context.Context, actorID string, category ledger.Category) error {
	if err := requireActor(actorID, category.UserID()); err != nil {
		return err
	}
	if err := r.checkNoCycle(ctx, actorID, category); err != nil {
		return err
	}

	now := formatTime(r.db.clock.Now())
	parentID, hasParent := category.ParentID()

	_, err := r.db.write.ExecContext(ctx, `
		INSERT INTO categories (id, user_id, parent_id, name, kind, archived, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		category.ID(), actorID, nullableString(parentID, hasParent), category.Name(), string(category.Kind()),
		boolToInt(category.Archived()), now, now,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't create category %q.", category.Name())
	}
	return nil
}

// Get implements ports.CategoryRepository.
func (r *CategoryRepository) Get(ctx context.Context, actorID, id string) (ledger.Category, error) {
	if err := requireActorID(actorID); err != nil {
		return ledger.Category{}, err
	}

	row := r.db.read.QueryRowContext(ctx, `
		SELECT id, user_id, parent_id, name, kind, archived
		FROM categories
		WHERE id = ? AND user_id = ?
	`, id, actorID)

	category, err := scanCategory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ledger.Category{}, errs.New(errs.NotFound).Explain("No category with ID %q.", id).Field("id")
	}
	if err != nil {
		return ledger.Category{}, errs.New(errs.Internal).Wrap(err)
	}
	return category, nil
}

// List implements ports.CategoryRepository.
func (r *CategoryRepository) List(ctx context.Context, actorID string) ([]ledger.Category, error) {
	if err := requireActorID(actorID); err != nil {
		return nil, err
	}

	rows, err := r.db.read.QueryContext(ctx, `
		SELECT id, user_id, parent_id, name, kind, archived
		FROM categories
		WHERE user_id = ?
		ORDER BY name
	`, actorID)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var categories []ledger.Category
	for rows.Next() {
		category, err := scanCategory(rows)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	return categories, nil
}

// Update implements ports.CategoryRepository.
func (r *CategoryRepository) Update(ctx context.Context, actorID string, category ledger.Category) error {
	if err := requireActor(actorID, category.UserID()); err != nil {
		return err
	}
	if err := r.checkNoCycle(ctx, actorID, category); err != nil {
		return err
	}

	now := formatTime(r.db.clock.Now())
	parentID, hasParent := category.ParentID()

	result, err := r.db.write.ExecContext(ctx, `
		UPDATE categories
		SET parent_id = ?, name = ?, kind = ?, archived = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`,
		nullableString(parentID, hasParent), category.Name(), string(category.Kind()),
		boolToInt(category.Archived()), now,
		category.ID(), actorID,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't update category %q.", category.Name())
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No category with ID %q.", category.ID()).Field("id")
	}
	return nil
}

// checkNoCycle walks the chain of parents starting at category's declared
// parent and fails if it ever reaches category's own ID — a cycle deeper
// than the immediate self-parent check NewCategory already makes
// (data-model.md's ledger.Category doc comment: "deeper cycles... are a
// repository/application-layer concern, not this constructor's").
func (r *CategoryRepository) checkNoCycle(ctx context.Context, actorID string, category ledger.Category) *errs.Error {
	parentID, hasParent := category.ParentID()
	if !hasParent {
		return nil
	}

	var found int
	err := r.db.write.QueryRowContext(ctx, `
		WITH RECURSIVE chain(id, parent_id) AS (
			SELECT id, parent_id FROM categories WHERE id = ? AND user_id = ?
			UNION ALL
			SELECT c.id, c.parent_id FROM categories c
			JOIN chain ON c.id = chain.parent_id
		)
		SELECT 1 FROM chain WHERE id = ? LIMIT 1
	`, parentID, actorID, category.ID()).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return errs.New(errs.InvalidInput).
		Explain("Category %q can't be its own ancestor.", category.Name()).
		Field("parent_id")
}

func scanCategory(row rowScanner) (ledger.Category, error) {
	var (
		id, userID, name, kind string
		parentIDCol            sql.NullString
		archived               int
	)
	if err := row.Scan(&id, &userID, &parentIDCol, &name, &kind, &archived); err != nil {
		return ledger.Category{}, err
	}

	var parentIDPtr *string
	if parentIDCol.Valid {
		v := parentIDCol.String
		parentIDPtr = &v
	}

	return ledger.NewCategory(id, userID, parentIDPtr, name, ledger.CategoryKind(kind), archived != 0)
}
