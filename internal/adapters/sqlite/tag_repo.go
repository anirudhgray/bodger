package sqlite

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// TagRepository implements ports.TagRepository over a *DB.
type TagRepository struct {
	db *DB
}

// NewTagRepository constructs a TagRepository backed by db.
func NewTagRepository(db *DB) *TagRepository {
	return &TagRepository{db: db}
}

var _ ports.TagRepository = (*TagRepository)(nil)

// List implements ports.TagRepository.
func (r *TagRepository) List(ctx context.Context, actorID string) ([]ledger.Tag, error) {
	if err := requireActorID(actorID); err != nil {
		return nil, err
	}

	rows, err := r.db.read.QueryContext(ctx, `
		SELECT value FROM tags WHERE user_id = ? ORDER BY value
	`, actorID)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var tags []ledger.Tag
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		t, err := ledger.NewTag(value)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		tags = append(tags, t)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	return tags, nil
}
