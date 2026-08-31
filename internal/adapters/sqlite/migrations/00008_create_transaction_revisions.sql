-- +goose Up

-- data-model.md §7: editing a transaction writes a row here capturing the
-- previous state as JSON, plus actor and timestamp - an append-only audit
-- trail, never read by analytics. Deliberately loose (JSON blob, no typed
-- columns): its only reader is a human asking "what did I change", never a
-- report.
CREATE TABLE transaction_revisions (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    transaction_id TEXT NOT NULL REFERENCES transactions (id),
    user_id        TEXT NOT NULL REFERENCES users (id),
    previous_state TEXT NOT NULL,
    created_at     TEXT NOT NULL
);

CREATE INDEX idx_transaction_revisions_transaction_id ON transaction_revisions (transaction_id);

-- +goose Down
DROP TABLE transaction_revisions;
