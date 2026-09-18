-- +goose Up

-- ADR-0007 / ADR-0014 / data-model.md §11 / issue #276: a RecurringRule
-- is a template -- amount, account, category, description, and a
-- schedule. Money has never moved, and nothing in this table can make it
-- look as though it has: there are no postings here, no currency column
-- (amount_minor is denominated in the account's own currency, the same
-- single-source-of-truth shape budget_lines uses against its budget), and
-- no kind column (outflow vs. inflow follows from the category's own
-- kind, so a rule cannot declare a direction contradicting its category).
--
-- The schedule is stored as typed columns rather than an RFC 5545 RRULE
-- string. ADR-0014 records why: a string column can hold grammar no
-- evaluator in this system can compute, and bodger's month-end clamping
-- deliberately differs from RFC 5545's own BYMONTHDAY semantics, so
-- storing RRULE text would claim a conformance the implementation
-- doesn't have. The final CHECK is what makes the subset unwritable
-- from outside the domain layer as well as unrepresentable inside it.
--
-- day_of_month accepts 29, 30, and 31: a month too short for the
-- declared day clamps to its own last day when the rule is evaluated
-- (ADR-0014) -- the stored day is the declared one, never a clamped one.
--
-- ends_on is where RFC 5545's UNTIL/COUNT live, on the rule rather than
-- inside its schedule, so exactly one column answers "when does this
-- stop". archived_at mirrors budgets.archived_at.
CREATE TABLE recurring_rules (
    id             TEXT NOT NULL PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users (id),
    account_id     TEXT NOT NULL REFERENCES accounts (id),
    category_id    TEXT NOT NULL REFERENCES categories (id),
    amount_minor   INTEGER NOT NULL CHECK (amount_minor > 0),
    description    TEXT NOT NULL,
    frequency      TEXT NOT NULL CHECK (frequency IN ('weekly', 'monthly', 'yearly')),
    interval_count INTEGER NOT NULL DEFAULT 1 CHECK (interval_count >= 1),
    weekday        INTEGER CHECK (weekday BETWEEN 0 AND 6),
    day_of_month   INTEGER CHECK (day_of_month BETWEEN 1 AND 31),
    month_of_year  INTEGER CHECK (month_of_year BETWEEN 1 AND 12),
    starts_on      TEXT NOT NULL,
    ends_on        TEXT,
    archived_at    TEXT,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    -- Exactly the positional fields the declared frequency needs, and no
    -- others -- the schema half of recurring.Schedule's own "only one way
    -- to construct this" rule.
    CHECK (
        (frequency = 'weekly'  AND weekday IS NOT NULL AND day_of_month IS NULL     AND month_of_year IS NULL)
        OR (frequency = 'monthly' AND weekday IS NULL  AND day_of_month IS NOT NULL AND month_of_year IS NULL)
        OR (frequency = 'yearly'  AND weekday IS NULL  AND day_of_month IS NOT NULL AND month_of_year IS NOT NULL)
    )
);

CREATE INDEX idx_recurring_rules_user_id ON recurring_rules (user_id);
CREATE INDEX idx_recurring_rules_account_id ON recurring_rules (account_id);
CREATE INDEX idx_recurring_rules_category_id ON recurring_rules (category_id);

-- One projected firing of a rule on one date (data-model.md §11). Money
-- has still never moved. There is deliberately no account_id and no
-- currency here: an occurrence with an account would have a join key into
-- a balance, which is the first half of every way forecast money ends up
-- in one (ADR-0014). Ownership is reached through rule_id, exactly the
-- way postings are scoped through their transaction rather than carrying
-- a user_id of their own.
--
-- ON DELETE CASCADE on rule_id mirrors budget_lines' cascade on
-- budget_id. transaction_id has no cascade and is a one-way provenance
-- pointer into transactions: nothing traverses it in the other
-- direction, and a balance reads the transaction, never the occurrence.
--
-- UNIQUE (rule_id, occurrence_date) is what makes ADR-0014's generation
-- idempotent: re-running generation over an overlapping window can
-- neither duplicate a row nor resurrect one the user skipped. It also
-- serves every rule-scoped, date-ordered read, so no separate rule_id
-- index is needed.
CREATE TABLE scheduled_occurrences (
    id              TEXT NOT NULL PRIMARY KEY,
    rule_id         TEXT NOT NULL REFERENCES recurring_rules (id) ON DELETE CASCADE,
    occurrence_date TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'materialised', 'skipped')),
    transaction_id  TEXT REFERENCES transactions (id),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    -- Status and provenance can't drift apart, even through a direct
    -- write: a materialised occurrence has a transaction, and a pending
    -- or skipped one does not. recurring.NewScheduledOccurrence enforces
    -- the same rule in the domain layer.
    CHECK ((status = 'materialised') = (transaction_id IS NOT NULL)),
    UNIQUE (rule_id, occurrence_date)
);

-- The query issues #278-#280 actually run: pending occurrences in a date
-- range, narrowed to one actor by joining recurring_rules (which has its
-- own user_id index, above).
CREATE INDEX idx_scheduled_occurrences_status_date ON scheduled_occurrences (status, occurrence_date);
CREATE INDEX idx_scheduled_occurrences_transaction_id ON scheduled_occurrences (transaction_id);

-- +goose Down
DROP TABLE scheduled_occurrences;
DROP TABLE recurring_rules;
