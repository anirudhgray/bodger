-- +goose Up

-- ADR-0007 / data-model.md §10 / issue #241: a Budget is a plan, kept
-- strictly separate from what actually happened. A budget's periods are
-- computed, not stored (data-model.md §10), so there is no period table
-- here -- only the plan itself.
CREATE TABLE budgets (
    id          TEXT NOT NULL PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users (id),
    name        TEXT NOT NULL,
    -- budgeting.PeriodType's only value today (data-model.md §10:
    -- "monthly initially"). The state machine itself, if this ever grows
    -- one, would live in the domain package, not here -- this CHECK only
    -- guards against a value outside the known set ever reaching the
    -- column, the same role the import_batch.status CHECK plays.
    period_type TEXT NOT NULL DEFAULT 'monthly' CHECK (period_type IN ('monthly')),
    currency    TEXT NOT NULL REFERENCES currencies (code),
    starts_on   TEXT NOT NULL,
    archived_at TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX idx_budgets_user_id ON budgets (user_id);

-- One row per category a budget plans for (data-model.md §10). ON DELETE
-- CASCADE on budget_id mirrors postings' own cascade on transaction_id
-- (migration 00006_create_postings.sql): a line has no independent
-- existence apart from its budget. category_id has no cascade -- like
-- postings.category_id, a category referenced by a budget line is never
-- deleted out from under it (categories are archived, not hard-deleted).
--
-- There is no per-line currency column: a line's amount is always in its
-- budget's own currency (budgeting.BudgetLine's doc comment) -- the same
-- single-source-of-truth reasoning accounts.currency already applies.
CREATE TABLE budget_lines (
    id           TEXT NOT NULL PRIMARY KEY,
    budget_id    TEXT NOT NULL REFERENCES budgets (id) ON DELETE CASCADE,
    category_id  TEXT NOT NULL REFERENCES categories (id),
    amount_minor INTEGER NOT NULL,
    -- Reserved for a later milestone (data-model.md §13): leaving the
    -- column out now would cost a migration later, and adding it now
    -- costs nothing. Nothing reads or interprets it yet.
    rollover     INTEGER NOT NULL DEFAULT 0 CHECK (rollover IN (0, 1)),
    -- One line per category per budget -- data-model.md §10 has no notion
    -- of "the" actual/remaining for a category split across two lines of
    -- the same budget.
    UNIQUE (budget_id, category_id)
);

CREATE INDEX idx_budget_lines_budget_id ON budget_lines (budget_id);
CREATE INDEX idx_budget_lines_category_id ON budget_lines (category_id);

-- +goose Down
DROP TABLE budget_lines;
DROP TABLE budgets;
