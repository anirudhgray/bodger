-- +goose Up

-- Issue #301: duplicate detection's own gap, not the AI-suggestions work
-- migration 00016 exists for. findExactDuplicate/findSuspectedDuplicate
-- only ever searched committed transactions, so a pending
-- scheduled_occurrences row (rent, a subscription, generated ahead of
-- time by GenerateOccurrences but never yet paid) was invisible to
-- duplicate detection. This is advisory metadata pointing at the
-- occurrence believed to settle the same money, independent of the
-- duplicate_tier/duplicate_resolution and transfer_candidate_record_id
-- columns above: a record can carry any combination of the three, and
-- this column never changes a record's status by itself (an occurrence
-- match never blocks commit — see ImportRecord.MatchedOccurrenceID's doc
-- comment).
--
-- Nullable, same shape as transfer_candidate_record_id (migration 00013):
-- no ON DELETE behaviour beyond the default RESTRICT, since occurrences
-- are archived, never hard-deleted, by any current code path.
ALTER TABLE import_record ADD COLUMN matched_occurrence_id TEXT REFERENCES scheduled_occurrences (id);

-- +goose Down
ALTER TABLE import_record DROP COLUMN matched_occurrence_id;
