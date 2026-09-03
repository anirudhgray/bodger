package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// SessionRepository implements ports.SessionRepository over a *DB.
type SessionRepository struct {
	db *DB
}

// NewSessionRepository constructs a SessionRepository backed by db.
func NewSessionRepository(db *DB) *SessionRepository {
	return &SessionRepository{db: db}
}

var _ ports.SessionRepository = (*SessionRepository)(nil)

// Create implements ports.SessionRepository.
func (r *SessionRepository) Create(ctx context.Context, actorID string, session ports.Session) error {
	if err := requireActor(actorID, session.UserID); err != nil {
		return err
	}

	_, err := r.db.write.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, created_at, last_used_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		session.ID, actorID, session.TokenHash,
		formatTime(session.CreatedAt), formatTime(session.LastUsedAt), formatTime(session.ExpiresAt),
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't create session.")
	}
	return nil
}

// GetByTokenHash implements ports.SessionRepository.
func (r *SessionRepository) GetByTokenHash(ctx context.Context, tokenHash string) (ports.Session, error) {
	row := r.db.read.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, created_at, last_used_at, expires_at
		FROM sessions
		WHERE token_hash = ?
	`, tokenHash)

	session, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.Session{}, errs.New(errs.NotFound).Explain("No session found.")
	}
	if err != nil {
		return ports.Session{}, errs.New(errs.Internal).Wrap(err)
	}
	return session, nil
}

// Touch implements ports.SessionRepository.
func (r *SessionRepository) Touch(ctx context.Context, actorID, id string, lastUsedAt, expiresAt time.Time) error {
	if err := requireActorID(actorID); err != nil {
		return err
	}

	result, err := r.db.write.ExecContext(ctx, `
		UPDATE sessions
		SET last_used_at = ?, expires_at = ?
		WHERE id = ? AND user_id = ?
	`, formatTime(lastUsedAt), formatTime(expiresAt), id, actorID)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't renew session.")
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No session with ID %q.", id).Field("id")
	}
	return nil
}

// Delete implements ports.SessionRepository.
func (r *SessionRepository) Delete(ctx context.Context, actorID, id string) error {
	if err := requireActorID(actorID); err != nil {
		return err
	}

	result, err := r.db.write.ExecContext(ctx, `
		DELETE FROM sessions WHERE id = ? AND user_id = ?
	`, id, actorID)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No session with ID %q.", id).Field("id")
	}
	return nil
}

func scanSession(row rowScanner) (ports.Session, error) {
	var (
		id, userID, tokenHash                     string
		createdAtCol, lastUsedAtCol, expiresAtCol string
	)
	if err := row.Scan(&id, &userID, &tokenHash, &createdAtCol, &lastUsedAtCol, &expiresAtCol); err != nil {
		return ports.Session{}, err
	}

	createdAt, err := parseTime(createdAtCol)
	if err != nil {
		return ports.Session{}, err
	}
	lastUsedAt, err := parseTime(lastUsedAtCol)
	if err != nil {
		return ports.Session{}, err
	}
	expiresAt, err := parseTime(expiresAtCol)
	if err != nil {
		return ports.Session{}, err
	}

	return ports.Session{
		ID:         id,
		UserID:     userID,
		TokenHash:  tokenHash,
		CreatedAt:  createdAt,
		LastUsedAt: lastUsedAt,
		ExpiresAt:  expiresAt,
	}, nil
}
