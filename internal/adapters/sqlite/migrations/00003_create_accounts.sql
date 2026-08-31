-- +goose Up

-- data-model.md §4. This deliberately persists less than that section's
-- full column list: internal/domain/ledger.Account (issue #2, already
-- merged) doesn't model `institution` or `sort_order` yet, and represents
-- "hidden from pickers" as a plain bool rather than an `archived_at`
-- timestamp. Adding those columns is a follow-up once the domain type
-- grows the fields to put in them, rather than storing data the domain
-- layer can't round-trip.
CREATE TABLE accounts (
    id                     TEXT PRIMARY KEY,
    user_id                TEXT NOT NULL REFERENCES users (id),
    name                   TEXT NOT NULL,
    kind                   TEXT NOT NULL CHECK (kind IN ('bank', 'cash', 'credit_card', 'wallet', 'investment', 'loan', 'other')),
    currency               TEXT NOT NULL REFERENCES currencies (code),
    opening_balance_minor  INTEGER NOT NULL DEFAULT 0,
    opening_balance_date   TEXT,
    archived               INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL,
    UNIQUE (user_id, name)
);

CREATE INDEX idx_accounts_user_id ON accounts (user_id);

-- +goose Down
DROP TABLE accounts;
