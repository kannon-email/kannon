-- migrate:up
DROP TABLE prepared;
DROP TABLE hard_bounced;
DROP TABLE accepted;
DROP TABLE open;
DROP TABLE click;
DROP TABLE soft_bounce;

-- migrate:down

CREATE TABLE prepared (
  id serial PRIMARY KEY,
  email varchar(320) NOT NULL,
  message_id VARCHAR NOT NULL,
  domain VARCHAR NOT NULL,
  timestamp TIMESTAMP NOT NULL DEFAULT NOW(),
  first_timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX prepared_message_id_idx ON prepared (message_id, domain);
CREATE UNIQUE INDEX prepared_email_message_id_idx ON prepared (email, message_id, domain);

CREATE TABLE accepted (
  id serial PRIMARY KEY,
  email varchar(320) NOT NULL,
  message_id VARCHAR NOT NULL,
  domain VARCHAR NOT NULL,
  timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX accepted_message_id_idx ON accepted (message_id, domain);
CREATE UNIQUE INDEX accepted_email_message_id_idx ON accepted (email, message_id, domain);

CREATE TABLE hard_bounced (
  id serial PRIMARY KEY,
  email varchar(320) NOT NULL,
  message_id VARCHAR NOT NULL,
  domain VARCHAR NOT NULL,
  err_code INT NOT NULL,
  err_msg VARCHAR NOT NULL,
  timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX hard_bounced_message_id_idx ON hard_bounced (message_id, domain);
CREATE UNIQUE INDEX hard_bounced_email_message_id_idx ON hard_bounced (email, message_id, domain);

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
