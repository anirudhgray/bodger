-- +goose Up

-- data-model.md §5. `import_record_id` has no foreign key yet: the
-- import_records table doesn't exist until M5 (issue #3's out-of-scope
-- list) - it's stored as a plain, unconstrained id for now.
CREATE TABLE transactions (
    id                      TEXT PRIMARY KEY,
    user_id                 TEXT NOT NULL REFERENCES users (id),
    kind                    TEXT NOT NULL CHECK (kind IN ('outflow', 'inflow', 'transfer')),
    booked_date             TEXT NOT NULL,
    posted_date             TEXT,
    description             TEXT NOT NULL,
    notes                   TEXT NOT NULL DEFAULT '',
    deleted_at              TEXT,
    import_record_id        TEXT,
    external_id             TEXT,
    related_transaction_id  TEXT REFERENCES transactions (id),
    created_at              TEXT NOT NULL,
    updated_at              TEXT NOT NULL
);

-- Covers both "list this user's transactions" and the deleted_at is null
-- filter every query in the repository applies (issue #3's "done when").
CREATE INDEX idx_transactions_user_deleted_booked
    ON transactions (user_id, deleted_at, booked_date);

CREATE INDEX idx_transactions_related ON transactions (related_transaction_id);

-- +goose Down
DROP TABLE transactions;
