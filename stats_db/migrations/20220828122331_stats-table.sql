-- migrate:up
CREATE TABLE stats (
  id serial PRIMARY KEY,
  type VARCHAR NOT NULL,
  email varchar(320) NOT NULL,
  message_id VARCHAR NOT NULL,
  domain VARCHAR NOT NULL,
  timestamp TIMESTAMP NOT NULL DEFAULT NOW(),
  data JSONB NOT NULL
);

CREATE INDEX stats_type_message_id_type_timestamp_idx ON stats (message_id, domain, type, timestamp);
CREATE UNIQUE INDEX stats_email_message_id_type_timestamp_idx ON stats (email, message_id, domain, type, timestamp);

-- migrate:down

DROP TABLE stats;