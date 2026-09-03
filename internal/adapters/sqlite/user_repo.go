package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// UserRepository implements ports.UserRepository over a *DB.
type UserRepository struct {
	db *DB
}

// NewUserRepository constructs a UserRepository backed by db.
func NewUserRepository(db *DB) *UserRepository {
	return &UserRepository{db: db}
}

var _ ports.UserRepository = (*UserRepository)(nil)

// GetByID implements ports.UserRepository.
func (r *UserRepository) GetByID(ctx context.Context, id string) (ports.User, error) {
	row := r.db.read.QueryRowContext(ctx, `
		SELECT id, password_hash, created_at
		FROM users
		WHERE id = ?
	`, id)

	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.User{}, errs.New(errs.NotFound).Explain("No user with ID %q.", id).Field("id")
	}
	if err != nil {
		return ports.User{}, errs.New(errs.Internal).Wrap(err)
	}
	return user, nil
}

// SetPasswordHash implements ports.UserRepository.
func (r *UserRepository) SetPasswordHash(ctx context.Context, actorID, passwordHash string) error {
	if err := requireActorID(actorID); err != nil {
		return err
	}

	result, err := r.db.write.ExecContext(ctx, `
		UPDATE users SET password_hash = ? WHERE id = ?
	`, passwordHash, actorID)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No user with ID %q.", actorID).Field("id")
	}
	return nil
}

func scanUser(row rowScanner) (ports.User, error) {
	var (
		id              string
		passwordHashCol sql.NullString
		createdAtCol    string
	)
	if err := row.Scan(&id, &passwordHashCol, &createdAtCol); err != nil {
		return ports.User{}, err
	}

	createdAt, err := parseTime(createdAtCol)
	if err != nil {
		return ports.User{}, err
	}

	var passwordHashPtr *string
	if passwordHashCol.Valid {
		v := passwordHashCol.String
		passwordHashPtr = &v
	}

	return ports.User{ID: id, PasswordHash: passwordHashPtr, CreatedAt: createdAt}, nil
}
