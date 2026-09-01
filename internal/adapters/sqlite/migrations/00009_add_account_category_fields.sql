-- +goose Up

-- Follow-on to 00003/00004 (issue #22): internal/domain/ledger.Account and
-- .Category now model institution, sort_order, and archived_at, so this
-- persists the columns their comments said were deliberately left out.
--
-- `institution` and `sort_order` are plain additive columns that
-- ALTER TABLE ... ADD COLUMN could handle directly, but converting
-- `archived INTEGER` into `archived_at TEXT` (nullable timestamp) isn't
-- something ADD COLUMN/RENAME COLUMN can do (there's no ALTER TABLE ...
-- ALTER COLUMN in SQLite) - so the whole table is rebuilt in one pass via
-- the create-copy-drop-rename dance ADR-0007 calls out: build the new
-- table shape, copy rows across translating the changed column, drop the
-- old table, and rename the new one into place.

CREATE TABLE accounts_new (
    id                     TEXT PRIMARY KEY,
    user_id                TEXT NOT NULL REFERENCES users (id),
    name                   TEXT NOT NULL,
    kind                   TEXT NOT NULL CHECK (kind IN ('bank', 'cash', 'credit_card', 'wallet', 'investment', 'loan', 'other')),
    currency               TEXT NOT NULL REFERENCES currencies (code),
    institution            TEXT,
    opening_balance_minor  INTEGER NOT NULL DEFAULT 0,
    opening_balance_date   TEXT,
    archived_at            TEXT,
    sort_order             INTEGER NOT NULL DEFAULT 0,
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL,
    UNIQUE (user_id, name)
);

-- A previously archived row (archived = 1) has no recorded archive
-- instant to carry forward, since the old column never stored one. This
-- migration backfills updated_at as the archive instant: it's the closest
-- fact already on the row to "when did this last change", and is strictly
-- better than losing the archived state outright.
INSERT INTO accounts_new (id, user_id, name, kind, currency, institution, opening_balance_minor, opening_balance_date, archived_at, sort_order, created_at, updated_at)
SELECT id, user_id, name, kind, currency, NULL, opening_balance_minor, opening_balance_date,
       CASE WHEN archived = 1 THEN updated_at ELSE NULL END,
       0, created_at, updated_at
FROM accounts;

DROP TABLE accounts;
ALTER TABLE accounts_new RENAME TO accounts;

CREATE INDEX idx_accounts_user_id ON accounts (user_id);

CREATE TABLE categories_new (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id),
    parent_id  TEXT REFERENCES categories (id),
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('expense', 'income')),
    archived_at TEXT,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO categories_new (id, user_id, parent_id, name, kind, archived_at, sort_order, created_at, updated_at)
SELECT id, user_id, parent_id, name, kind,
       CASE WHEN archived = 1 THEN updated_at ELSE NULL END,
       0, created_at, updated_at
FROM categories;

DROP TABLE categories;
ALTER TABLE categories_new RENAME TO categories;

CREATE INDEX idx_categories_user_id ON categories (user_id);
CREATE INDEX idx_categories_parent_id ON categories (parent_id);

CREATE UNIQUE INDEX idx_categories_unique_top_level_name
    ON categories (user_id, name)
    WHERE parent_id IS NULL;

CREATE UNIQUE INDEX idx_categories_unique_sibling_name
    ON categories (user_id, parent_id, name)
    WHERE parent_id IS NOT NULL;

-- +goose Down

-- Reverses the Up migration exactly: rebuild the pre-00009 shape (archived
-- bool, no institution/sort_order) and translate archived_at back into a
-- bool - any non-NULL archived_at, past or future, becomes archived = 1,
-- matching the domain's Archived() = (archivedAt != nil) definition this
-- migration exists to persist.

CREATE TABLE accounts_old (
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

INSERT INTO accounts_old (id, user_id, name, kind, currency, opening_balance_minor, opening_balance_date, archived, created_at, updated_at)
SELECT id, user_id, name, kind, currency, opening_balance_minor, opening_balance_date,
       CASE WHEN archived_at IS NOT NULL THEN 1 ELSE 0 END,
       created_at, updated_at
FROM accounts;

DROP TABLE accounts;
ALTER TABLE accounts_old RENAME TO accounts;

CREATE INDEX idx_accounts_user_id ON accounts (user_id);

CREATE TABLE categories_old (
    id        TEXT PRIMARY KEY,
    user_id   TEXT NOT NULL REFERENCES users (id),
    parent_id TEXT REFERENCES categories (id),
    name      TEXT NOT NULL,
    kind      TEXT NOT NULL CHECK (kind IN ('expense', 'income')),
    archived  INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO categories_old (id, user_id, parent_id, name, kind, archived, created_at, updated_at)
SELECT id, user_id, parent_id, name, kind,
       CASE WHEN archived_at IS NOT NULL THEN 1 ELSE 0 END,
       created_at, updated_at
FROM categories;

DROP TABLE categories;
ALTER TABLE categories_old RENAME TO categories;

CREATE INDEX idx_categories_user_id ON categories (user_id);
CREATE INDEX idx_categories_parent_id ON categories (parent_id);

CREATE UNIQUE INDEX idx_categories_unique_top_level_name
    ON categories (user_id, name)
    WHERE parent_id IS NULL;

CREATE UNIQUE INDEX idx_categories_unique_sibling_name
    ON categories (user_id, parent_id, name)
    WHERE parent_id IS NOT NULL;
