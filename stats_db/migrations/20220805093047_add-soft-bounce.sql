-- migrate:up
CREATE TABLE soft_bounce (
  id serial PRIMARY KEY,
  email varchar(320) NOT NULL,
  message_id VARCHAR NOT NULL,
  domain VARCHAR NOT NULL,
  code INT NOT NULL,
  msg VARCHAR NOT NULL,
  timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX soft_bounce_message_id_idx ON soft_bounce (message_id, domain);


-- migrate:down

DROP TABLE soft_bounce;