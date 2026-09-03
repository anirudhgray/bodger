package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// APITokenRepository implements ports.APITokenRepository over a *DB.
type APITokenRepository struct {
	db *DB
}

// NewAPITokenRepository constructs an APITokenRepository backed by db.
func NewAPITokenRepository(db *DB) *APITokenRepository {
	return &APITokenRepository{db: db}
}

var _ ports.APITokenRepository = (*APITokenRepository)(nil)

// Create implements ports.APITokenRepository.
func (r *APITokenRepository) Create(ctx context.Context, actorID string, token ports.APIToken) error {
	if err := requireActor(actorID, token.UserID); err != nil {
		return err
	}

	_, err := r.db.write.ExecContext(ctx, `
		INSERT INTO api_tokens (id, user_id, token_hash, name, created_at, last_used_at, expires_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		token.ID, actorID, token.TokenHash, token.Name, formatTime(token.CreatedAt),
		nullableTimeValue(token.LastUsedAt), nullableTimeValue(token.ExpiresAt), nullableTimeValue(token.RevokedAt),
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't create API token %q.", token.Name)
	}
	return nil
}

// GetByTokenHash implements ports.APITokenRepository.
func (r *APITokenRepository) GetByTokenHash(ctx context.Context, tokenHash string) (ports.APIToken, error) {
	row := r.db.read.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, name, created_at, last_used_at, expires_at, revoked_at
		FROM api_tokens
		WHERE token_hash = ?
	`, tokenHash)

	token, err := scanAPIToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.APIToken{}, errs.New(errs.NotFound).Explain("No API token found.")
	}
	if err != nil {
		return ports.APIToken{}, errs.New(errs.Internal).Wrap(err)
	}
	return token, nil
}

// List implements ports.APITokenRepository.
func (r *APITokenRepository) List(ctx context.Context, actorID string) ([]ports.APIToken, error) {
	if err := requireActorID(actorID); err != nil {
		return nil, err
	}

	rows, err := r.db.read.QueryContext(ctx, `
		SELECT id, user_id, token_hash, name, created_at, last_used_at, expires_at, revoked_at
		FROM api_tokens
		WHERE user_id = ?
		ORDER BY created_at DESC, id DESC
	`, actorID)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var tokens []ports.APIToken
	for rows.Next() {
		token, err := scanAPIToken(rows)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	return tokens, nil
}

// Touch implements ports.APITokenRepository.
func (r *APITokenRepository) Touch(ctx context.Context, actorID, id string, lastUsedAt time.Time) error {
	if err := requireActorID(actorID); err != nil {
		return err
	}

	result, err := r.db.write.ExecContext(ctx, `
		UPDATE api_tokens
		SET last_used_at = ?
		WHERE id = ? AND user_id = ?
	`, formatTime(lastUsedAt), id, actorID)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't record API token use.")
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No API token with ID %q.", id).Field("id")
	}
	return nil
}

// Revoke implements ports.APITokenRepository.
func (r *APITokenRepository) Revoke(ctx context.Context, actorID, id string, revokedAt time.Time) error {
	if err := requireActorID(actorID); err != nil {
		return err
	}

	result, err := r.db.write.ExecContext(ctx, `
		UPDATE api_tokens
		SET revoked_at = ?
		WHERE id = ? AND user_id = ?
	`, formatTime(revokedAt), id, actorID)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't revoke API token.")
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No API token with ID %q.", id).Field("id")
	}
	return nil
}

func scanAPIToken(row rowScanner) (ports.APIToken, error) {
	var (
		id, userID, tokenHash, name string
		createdAtCol                string
		lastUsedAtCol               sql.NullString
		expiresAtCol                sql.NullString
		revokedAtCol                sql.NullString
	)
	if err := row.Scan(&id, &userID, &tokenHash, &name, &createdAtCol, &lastUsedAtCol, &expiresAtCol, &revokedAtCol); err != nil {
		return ports.APIToken{}, err
	}

	createdAt, err := parseTime(createdAtCol)
	if err != nil {
		return ports.APIToken{}, err
	}
	lastUsedAt, err := parseNullableTime(lastUsedAtCol)
	if err != nil {
		return ports.APIToken{}, err
	}
	expiresAt, err := parseNullableTime(expiresAtCol)
	if err != nil {
		return ports.APIToken{}, err
	}
	revokedAt, err := parseNullableTime(revokedAtCol)
	if err != nil {
		return ports.APIToken{}, err
	}

	return ports.APIToken{
		ID:         id,
		UserID:     userID,
		TokenHash:  tokenHash,
		Name:       name,
		CreatedAt:  createdAt,
		LastUsedAt: lastUsedAt,
		ExpiresAt:  expiresAt,
		RevokedAt:  revokedAt,
	}, nil
}
