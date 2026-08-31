-- +goose Up

CREATE TABLE users (
    id         TEXT PRIMARY KEY,
    created_at TEXT NOT NULL
);

-- ADR-0006: every user-owned table carries user_id from the first
-- migration even though M1 has exactly one user. This is that user, with a
-- fixed and well-known ID so every surface has an ActorID before auth
-- exists (M2). Keep this literal in sync with ports.SeededUserID —
-- internal/adapters/sqlite tests assert the two match.
INSERT INTO users (id, created_at)
VALUES ('00000000-0000-0000-0000-000000000001', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

-- +goose Down
DROP TABLE users;
