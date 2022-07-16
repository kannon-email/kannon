-- migrate:up
CREATE TABLE delivered (
  id serial PRIMARY KEY,
  email varchar(320) NOT NULL,
  message_id VARCHAR NOT NULL,
  timestamp timestamp NOT NULL DEFAULT NOW()
);

CREATE INDEX delivered_message_id_idx ON delivered (message_id);

CREATE TABLE accepted (
  id serial PRIMARY KEY,
  email varchar(320) NOT NULL,
  message_id VARCHAR NOT NULL,
  timestamp timestamp NOT NULL DEFAULT NOW()
);

CREATE INDEX accepted_message_id_idx ON accepted (message_id);


CREATE TABLE bounced (
  id serial PRIMARY KEY,
  email varchar(320) NOT NULL,
  message_id VARCHAR NOT NULL,
  err_code INT NOT NULL,
  err_msg VARCHAR NOT NULL,
  timestamp timestamp NOT NULL DEFAULT NOW()
);

CREATE INDEX bounced_message_id_idx ON bounced (message_id);

-- migrate:down

DROP TABLE accepted;
DROP TABLE bounced;
DROP TABLE delivered;