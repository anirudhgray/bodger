-- +goose Up

-- data-model.md §6. Same simplification as accounts (00003): no
-- `sort_order` and `archived` is a bool, because
-- internal/domain/ledger.Category doesn't model those yet.
CREATE TABLE categories (
    id        TEXT PRIMARY KEY,
    user_id   TEXT NOT NULL REFERENCES users (id),
    parent_id TEXT REFERENCES categories (id),
    name      TEXT NOT NULL,
    kind      TEXT NOT NULL CHECK (kind IN ('expense', 'income')),
    archived  INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_categories_user_id ON categories (user_id);
CREATE INDEX idx_categories_parent_id ON categories (parent_id);

-- "Unique among siblings" (data-model.md §6). A plain
-- UNIQUE(user_id, parent_id, name) wouldn't catch duplicate top-level
-- names: SQL treats every NULL parent_id as distinct from every other
-- NULL, so two partial indexes are needed - one for the top-level group,
-- one for every other (non-null) parent group.
CREATE UNIQUE INDEX idx_categories_unique_top_level_name
    ON categories (user_id, name)
    WHERE parent_id IS NULL;

CREATE UNIQUE INDEX idx_categories_unique_sibling_name
    ON categories (user_id, parent_id, name)
    WHERE parent_id IS NOT NULL;

-- +goose Down
DROP TABLE categories;
