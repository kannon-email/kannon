-- migrate:up

CREATE TABLE stats (
    id integer NOT NULL,
    type VARCHAR NOT NULL,
    email VARCHAR NOT NULL,
    message_id VARCHAR NOT NULL,
    domain VARCHAR NOT NULL,
    timestamp TIMESTAMP DEFAULT now() NOT NULL,
    data JSONB NOT NULL
);

CREATE INDEX stats_type_message_id_type_timestamp_idx ON stats (message_id, domain, type, timestamp);
CREATE UNIQUE INDEX stats_email_message_id_type_timestamp_idx ON stats (email, message_id, domain, type, timestamp);

-- migrate:down

DROP TABLE stats;
