-- +goose Up

-- data-model.md §5. ON DELETE CASCADE on transaction_id: postings have no
-- independent existence, and the application layer never hard-deletes a
-- transaction (deletion is always the deleted_at soft delete, ADR-0002) -
-- the cascade exists for schema hygiene, not as a path anything calls.
CREATE TABLE postings (
    id           TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transactions (id) ON DELETE CASCADE,
    account_id   TEXT NOT NULL REFERENCES accounts (id),
    amount_minor INTEGER NOT NULL,
    currency     TEXT NOT NULL REFERENCES currencies (code),
    category_id  TEXT REFERENCES categories (id),
    sort_order   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_postings_transaction_id ON postings (transaction_id);

-- The balance formula (data-model.md §4) joins postings to accounts by
-- account_id, then filters transactions by booked_date and deleted_at.
CREATE INDEX idx_postings_account_id ON postings (account_id);
CREATE INDEX idx_postings_category_id ON postings (category_id);

-- +goose Down
DROP TABLE postings;
