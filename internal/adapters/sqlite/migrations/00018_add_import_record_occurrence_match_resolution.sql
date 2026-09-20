-- +goose Up

-- Issue #309: closes the gap left by #301/migration 00017 — an occurrence
-- match (matched_occurrence_id) needs its own persisted resolution, the
-- same way duplicate_resolution tracks the user's decision on
-- duplicate_tier/duplicate_matched_transaction_id, alongside it.
--
-- This can't reuse duplicate_resolution's own column or be inferred from
-- matched_occurrence_id's mere presence/absence: issue #309 lets a record
-- carry an unresolved DuplicateMatch and an unresolved occurrence match at
-- the same time, and resolving one must never resolve or bypass the
-- other (importing.ImportRecord.SettleStatus). If a record's occurrence
-- match resolves (materialize or dismiss) while its DuplicateMatch is
-- still unresolved, the record stays 'pending' until the DuplicateMatch
-- resolves too — at which point SettleStatus needs to know what the
-- occurrence match's own resolution *was* to pick the correct final
-- status (exclude wins over ready). A bare presence/absence check on
-- matched_occurrence_id can't answer that once the intervening 'pending'
-- period has passed, so its own resolution needs a durable column, same
-- shape as duplicate_resolution.
--
-- Defaults to 'pending' rather than NULL for the same reason
-- duplicate_resolution does (migration 00012): a row with no occurrence
-- match at all is still, trivially, "no decision made."
ALTER TABLE import_record ADD COLUMN matched_occurrence_resolution TEXT NOT NULL DEFAULT 'pending'
    CHECK (matched_occurrence_resolution IN ('pending', 'materialized', 'dismissed'));

-- +goose Down
ALTER TABLE import_record DROP COLUMN matched_occurrence_resolution;
