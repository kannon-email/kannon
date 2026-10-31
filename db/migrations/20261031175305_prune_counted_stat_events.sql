-- migrate:up
-- Markers are now pruned once the kannon-stats stream can no longer redeliver
-- the event they name. stored_at is RFC3339Nano text, which does not sort as
-- time (trailing fractional zeros are trimmed), but its first 19 characters,
-- the UTC second, are fixed-width and do under the C collation.
CREATE INDEX counted_stat_events_stored_second_idx ON counted_stat_events ((left(stored_at, 19) COLLATE "C"));

-- migrate:down
DROP INDEX counted_stat_events_stored_second_idx;
