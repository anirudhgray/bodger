-- +goose Up

-- data-model.md §6. Tag is free-form and many-to-many with Transaction,
-- never Posting. internal/domain/ledger.Tag carries only its normalised
-- value (no id) - NewTag always normalises the same raw input to the same
-- value, so (user_id, value) is the natural key rather than a synthetic id.
CREATE TABLE tags (
    user_id    TEXT NOT NULL REFERENCES users (id),
    value      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (user_id, value)
);

CREATE TABLE transaction_tags (
    transaction_id TEXT NOT NULL REFERENCES transactions (id) ON DELETE CASCADE,
    user_id        TEXT NOT NULL,
    tag_value      TEXT NOT NULL,
    PRIMARY KEY (transaction_id, tag_value),
    FOREIGN KEY (user_id, tag_value) REFERENCES tags (user_id, value)
);

CREATE INDEX idx_transaction_tags_user_tag ON transaction_tags (user_id, tag_value);

-- +goose Down
DROP TABLE transaction_tags;
DROP TABLE tags;
