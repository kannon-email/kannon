-- migrate:up

CREATE TABLE aggregated_stats (
    domain VARCHAR NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    type VARCHAR NOT NULL,
    count BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (domain, timestamp, type)
);

SET statement_timeout = '300s';

INSERT INTO aggregated_stats (domain, timestamp, type, count)
SELECT
    domain,
    date_trunc('day', timestamp)::TIMESTAMP AS timestamp,
    type,
    COUNT(*) AS count
FROM stats
GROUP BY domain, date_trunc('day', timestamp), type;

RESET statement_timeout;


-- migrate:down
DROP TABLE aggregated_stats;