-- migrate:up
CREATE TABLE open (
  id serial PRIMARY KEY,
  email varchar(320) NOT NULL,
  message_id VARCHAR NOT NULL,
  domain VARCHAR NOT NULL,
  ip VARCHAR NOT NULL,
  user_agent VARCHAR NOT NULL,
  timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX open_message_id_idx ON open (message_id, domain);
CREATE UNIQUE INDEX open_email_message_id_idx ON open (email, message_id, domain);


-- migrate:down

DROP TABLE open;