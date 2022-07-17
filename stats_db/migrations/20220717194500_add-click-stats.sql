-- migrate:up
CREATE TABLE click (
  id serial PRIMARY KEY,
  email varchar(320) NOT NULL,
  message_id VARCHAR NOT NULL,
  domain VARCHAR NOT NULL,
  ip VARCHAR NOT NULL,
  url VARCHAR NOT NULL,
  user_agent VARCHAR NOT NULL,
  timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX click_message_id_idx ON click (message_id, domain);


-- migrate:down

DROP TABLE click;