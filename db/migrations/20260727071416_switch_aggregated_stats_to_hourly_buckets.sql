-- migrate:up

-- aggregated_stats.timestamp used to be a UTC day (midnight); from now on it is a
-- UTC hour, matching the granularity the raw timeline already reports, so a client
-- can roll the buckets up into days of its own timezone. The schema is unchanged:
-- only the meaning of `timestamp` moves.
--
-- Rebuilding the whole table from `stats` would destroy history. `stats` is pruned
-- by retention (one year by default) while `aggregated_stats` never is, so the
-- oldest aggregated rows count events that exist nowhere else. The rewrite is
-- therefore bounded to the window `stats` fully covers, and starts at the first
-- *whole* day it holds (the oldest day is normally half eaten by retention, so
-- rebuilding it would under-report).
--
-- Rows older than that cutoff are left untouched as day buckets at 00:00. They keep
-- summing correctly on a daily or weekly axis; on an hourly axis they all pile onto
-- hour 0 of their day, which is the honest rendering of "we know the day, not the
-- hour".
--
-- Inside the rewritten window aggregated_stats can legitimately exceed what `stats`
-- holds: events tracked in anonymous mode are counted in aggregate only and never
-- produce a per-recipient row. That surplus is preserved per (domain, day, type)
-- and re-attached to hour 0 of its day — it has no hour of its own — summed into
-- whatever hourly bucket already lands there, so the primary key still holds.
-- Net effect per (domain, day, type): GREATEST(previous aggregate, rows in `stats`).

SET statement_timeout = '300s';

-- One row, ts NULL when `stats` is empty: every statement below then matches
-- nothing and the table is left exactly as it was.
CREATE TEMP TABLE aggregated_stats_cutoff ON COMMIT DROP AS
SELECT (date_trunc('day', MIN(timestamp)) + INTERVAL '1 day')::TIMESTAMP AS ts
FROM stats;

CREATE TEMP TABLE aggregated_stats_hourly ON COMMIT DROP AS
WITH raw_hourly AS (
    SELECT domain, date_trunc('hour', timestamp) AS ts, type, COUNT(*) AS count
    FROM stats
    WHERE timestamp >= (SELECT ts FROM aggregated_stats_cutoff)
    GROUP BY domain, date_trunc('hour', timestamp), type
),
raw_daily AS (
    SELECT domain, date_trunc('day', ts) AS day, type, SUM(count) AS count
    FROM raw_hourly
    GROUP BY domain, date_trunc('day', ts), type
),
aggregated_daily AS (
    SELECT domain, date_trunc('day', timestamp) AS day, type, SUM(count) AS count
    FROM aggregated_stats
    WHERE timestamp >= (SELECT ts FROM aggregated_stats_cutoff)
    GROUP BY domain, date_trunc('day', timestamp), type
),
-- Events counted in aggregate but absent from `stats` (anonymous mode).
surplus AS (
    SELECT a.domain, a.day AS ts, a.type, a.count - COALESCE(r.count, 0) AS count
    FROM aggregated_daily a
    LEFT JOIN raw_daily r
        ON r.domain = a.domain AND r.day = a.day AND r.type = a.type
    WHERE a.count > COALESCE(r.count, 0)
)
SELECT domain, ts::TIMESTAMP AS timestamp, type, SUM(count)::BIGINT AS count
FROM (
    SELECT * FROM raw_hourly
    UNION ALL
    SELECT * FROM surplus
) buckets
GROUP BY domain, ts, type;

DELETE FROM aggregated_stats
WHERE timestamp >= (SELECT ts FROM aggregated_stats_cutoff);

INSERT INTO aggregated_stats (domain, timestamp, type, count)
SELECT domain, timestamp, type, count FROM aggregated_stats_hourly;

RESET statement_timeout;


-- migrate:down

-- Roll the hourly buckets back up into day buckets. Rows that were never rewritten
-- (older than the cutoff above) are already day buckets and fold into themselves.

SET statement_timeout = '300s';

CREATE TEMP TABLE aggregated_stats_daily ON COMMIT DROP AS
SELECT domain, date_trunc('day', timestamp)::TIMESTAMP AS timestamp, type, SUM(count)::BIGINT AS count
FROM aggregated_stats
GROUP BY domain, date_trunc('day', timestamp), type;

DELETE FROM aggregated_stats;

INSERT INTO aggregated_stats (domain, timestamp, type, count)
SELECT domain, timestamp, type, count FROM aggregated_stats_daily;

RESET statement_timeout;
