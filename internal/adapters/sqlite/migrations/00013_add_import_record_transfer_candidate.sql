-- +goose Up

-- ADR-0008 / issue #210: cross-account transfer detection is "proposed,
-- never applied automatically" — an outflow on one account and an inflow
-- on another that look like the same movement. This is advisory metadata
-- on the outflow (or inflow) side pointing at the ImportRecord believed to
-- be its other leg, independent of the duplicate_tier/duplicate_resolution
-- columns above: a record can carry both, neither, or just one, and this
-- column never changes a record's status by itself.
--
-- Nullable and self-referential (no ON DELETE behaviour beyond the
-- default RESTRICT), since a transfer candidate may point at a record in
-- a different, independently-lifecycled batch — cascading its deletion
-- from here would be a surprise. import_record rows are never hard-deleted
-- by any current code path (see migration 00012's own note on
-- import_batch's ON DELETE CASCADE).
ALTER TABLE import_record ADD COLUMN transfer_candidate_record_id TEXT REFERENCES import_record (id);

-- +goose Down
ALTER TABLE import_record DROP COLUMN transfer_candidate_record_id;
