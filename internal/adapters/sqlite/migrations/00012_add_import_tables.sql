-- +goose Up

-- ADR-0008 / data-model.md §12 / issue #208: the staged import pipeline's
-- only state. "Nothing before commit affects a balance, a report, or a
-- budget" (ADR-0008) — these two tables are exactly that staging area, and
-- every other M6 issue (parsing/mapping/duplicate-detection, commit,
-- rollback) writes to them rather than directly to transactions/postings.
--
-- Table names are singular (import_batch, import_record) rather than
-- plural like every other table in this schema, matching ADR-0008's and
-- data-model.md's own wording ("staged in import_batch and import_record
-- rows") verbatim.
CREATE TABLE import_batch (
    id                TEXT NOT NULL PRIMARY KEY,
    user_id           TEXT NOT NULL REFERENCES users (id),
    -- Free-text label identifying which parser produced this batch's
    -- records (e.g. "csv", "ofx") — not CHECK-constrained to a known list,
    -- because recognising formats is the parser registry's job (a later,
    -- out-of-scope issue), not this migration's.
    source_format     TEXT NOT NULL,
    filename          TEXT NOT NULL,
    -- Content hash of the source file, for an idempotency check a later
    -- issue may add (e.g. refusing to re-stage an already-imported file).
    file_hash         TEXT NOT NULL,
    target_account_id TEXT NOT NULL REFERENCES accounts (id),
    -- importing.ImportBatchStatus's four values, in ADR-0008's linear
    -- staged -> reviewed -> committed -> rolled_back order. The state
    -- machine itself (which transitions are legal) lives in the domain
    -- package, not here — this CHECK only guards against a value outside
    -- the known set ever reaching the column.
    status            TEXT NOT NULL CHECK (status IN ('staged', 'reviewed', 'committed', 'rolled_back')),
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);

CREATE INDEX idx_import_batch_user_id ON import_batch (user_id);

-- One row per source row (ADR-0008: "raw payload + normalised fields, one
-- row per source row"). ON DELETE CASCADE on import_batch_id mirrors
-- postings' own cascade on transaction_id (migration
-- 00006_create_postings.sql): records have no independent existence apart
-- from their batch, so this is schema hygiene for a hard-delete path
-- nothing calls yet, not a path the application layer relies on today —
-- a batch is abandoned by simply staying "staged" forever (ADR-0008
-- explicitly defers a retention policy), never hard-deleted by current
-- code.
CREATE TABLE import_record (
    id                                TEXT NOT NULL PRIMARY KEY,
    import_batch_id                   TEXT NOT NULL REFERENCES import_batch (id) ON DELETE CASCADE,
    user_id                           TEXT NOT NULL REFERENCES users (id),
    -- The untouched source row, exactly as the parser received it (CSV
    -- text, a JSON object, ...). This package has no opinion on its shape.
    raw_payload                       TEXT NOT NULL,
    -- The parser's already-normalised output (ADR-0008: "a parser's only
    -- job is to turn bytes into normalised ImportRecord fields") —
    -- data-model.md §9's booked_date/posted_date split applies here the
    -- same way it does on transactions.
    booked_date                       TEXT NOT NULL,
    posted_date                       TEXT,
    description                       TEXT NOT NULL,
    amount_minor                      INTEGER NOT NULL,
    currency                          TEXT NOT NULL REFERENCES currencies (code),
    -- The source system's own transaction ID, if it has one — ADR-0008's
    -- tier-1 exact-duplicate key is (account_id, external_id), checked
    -- against resolved_account_id once mapping (a later issue) has run.
    external_id                       TEXT,
    -- Written by the mapping stage (a later, out-of-scope issue); NULL
    -- until then.
    resolved_account_id               TEXT REFERENCES accounts (id),
    resolved_category_id              TEXT REFERENCES categories (id),
    -- The duplicate-detection candidate (a later, out-of-scope issue) this
    -- row matched against, if any. Both columns are NULL together or set
    -- together — there is no match without a matched transaction, and vice
    -- versa.
    duplicate_tier                    TEXT CHECK (duplicate_tier IN ('exact', 'suspected_duplicate')),
    duplicate_matched_transaction_id  TEXT REFERENCES transactions (id),
    -- The user's decision on that match (ADR-0008: "the decision is
    -- recorded on the ImportRecord for auditability"). Defaults to
    -- 'pending' rather than NULL because a row with no match at all is
    -- still, trivially, "no decision made" — the same value a matched-but-
    -- not-yet-reviewed row holds.
    duplicate_resolution              TEXT NOT NULL DEFAULT 'pending'
                                          CHECK (duplicate_resolution IN ('pending', 'confirmed_duplicate', 'not_duplicate')),
    -- importing.ImportRecordStatus's four values: pending is the only
    -- branching point (pending -> ready or pending -> excluded), then
    -- ready -> committed. See import_batch.status above for why the state
    -- machine itself isn't expressed as a CHECK.
    status                            TEXT NOT NULL CHECK (status IN ('pending', 'ready', 'excluded', 'committed')),
    -- The transaction this row produced, once its batch commits (a later,
    -- out-of-scope issue). Required exactly when status = 'committed'.
    transaction_id                    TEXT REFERENCES transactions (id),
    -- Preserves the source file's row order — display and processing
    -- order only, the same role posting.sort_order plays for a
    -- transaction's postings.
    sort_order                        INTEGER NOT NULL DEFAULT 0,
    created_at                        TEXT NOT NULL,
    updated_at                        TEXT NOT NULL,
    -- Table-level constraints must come after every column definition
    -- (SQLite syntax) — both cross-column invariants live here rather
    -- than inline next to the columns they mention.
    CHECK ((duplicate_tier IS NULL) = (duplicate_matched_transaction_id IS NULL)),
    CHECK (status != 'committed' OR transaction_id IS NOT NULL)
);

CREATE INDEX idx_import_record_batch_id ON import_record (import_batch_id);
CREATE INDEX idx_import_record_user_id ON import_record (user_id);

-- +goose Down
DROP TABLE import_record;
DROP TABLE import_batch;
