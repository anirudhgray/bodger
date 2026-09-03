-- +goose Up

-- ADR-0006/ADR-0007, issue #54: the persistence half of M2 auth. Stores the
-- exact shapes internal/platform/auth (issue #53) already produces —
-- password_hash is a self-describing argon2id PHC string, and both
-- token_hash columns are hex-encoded SHA-256 of the plaintext token the
-- user actually holds. Nothing here computes or verifies a credential;
-- that's the application layer's job (issue #55).

-- Plain additive column: no existing row has a password yet (the M1 seeded
-- user predates auth entirely), so it's nullable rather than NOT NULL with
-- a synthetic default that would silently "verify" against nothing.
ALTER TABLE users ADD COLUMN password_hash TEXT;

-- Session cookie (ADR-0006): opaque random token, stored only as its hash.
-- created_at/last_used_at/expires_at are the sliding-expiry bookkeeping the
-- issue calls for — the application layer's clock decides what "now" is
-- and how far to slide expires_at forward on each use; this table just
-- persists whatever it's given. ADR-0006 also settles session revocation
-- as "a DELETE", not a soft-revoke flag, unlike api_tokens below.
CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id),
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   TEXT NOT NULL,
    last_used_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL
);

CREATE INDEX idx_sessions_user_id ON sessions (user_id);

-- API token (ADR-0006): `bdg_`-prefixed opaque random token, stored only as
-- its hash. name is the user-supplied label shown back to them ("laptop",
-- "backup script") since the plaintext itself is shown exactly once, at
-- creation. last_used_at and expires_at are both optional: a token may
-- never have been used yet, and an expiry is opt-in per ADR-0006's
-- credential table ("optional expiry"). revoked_at is a soft revoke rather
-- than a DELETE, so a revoked token stays listable — unlike a session,
-- which ADR-0006 revokes by deleting outright.
CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id),
    token_hash   TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    last_used_at TEXT,
    expires_at   TEXT,
    revoked_at   TEXT
);

CREATE INDEX idx_api_tokens_user_id ON api_tokens (user_id);

-- +goose Down

DROP TABLE api_tokens;
DROP TABLE sessions;
ALTER TABLE users DROP COLUMN password_hash;
