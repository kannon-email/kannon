-- migrate:up
-- The stats stream has no MaxAge: markers must survive stats retention and
-- worker restarts for as long as an old message can be replayed. No Recipient,
-- payload, or event timestamp is stored. Incarnation preserves sequence reuse
-- after stream recreation; text preserves the server's nanosecond precision.
CREATE TABLE counted_stat_events (
    stream text NOT NULL,
    stored_at text NOT NULL,
    sequence numeric(20, 0) NOT NULL CHECK (sequence > 0),
    PRIMARY KEY (stream, stored_at, sequence)
);

-- migrate:down
DROP TABLE counted_stat_events;
