-- migrate:up

CREATE TYPE SENDING_POOL_STATUS AS ENUM (
    'initializing',
    'sending',
    'sent',
    'scheduled',
    'error'
);

CREATE TABLE domains (
    id SERIAL PRIMARY KEY,
    domain varchar(254) UNIQUE NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT NOW(),
    key varchar(50) NOT NULL,
    dkim_private_key varchar NOT NULL,
    dkim_public_key varchar NOT NULL
);
CREATE INDEX ON domains (domain);

CREATE TABLE messages (
    id SERIAL PRIMARY KEY,
    message_id varchar(50) NOT NULL,
    subject varchar NOT NULL,
    sender_email varchar(320) NOT NULL,
    sender_alias varchar(100) NOT NULL,
    template_id varchar(50) NOT NULL,
    domain varchar(254) NOT NULL
);
CREATE INDEX ON messages (message_id);
CREATE INDEX ON messages (domain);

CREATE TABLE sending_pool_emails (
    id SERIAL PRIMARY KEY,
    status SENDING_POOL_STATUS NOT NULL DEFAULT 'initializing',
    scheduled_time timestamp with time zone DEFAULT now() NOT NULL,
    original_scheduled_time timestamp with time zone NOT NULL,
    trial smallint DEFAULT 0 NOT NULL,
    email varchar(320) NOT NULL,
    message_id SERIAL NOT NULL,
    error_msg varchar NOT NULL DEFAULT '',
    error_code int NOT NULL DEFAULT 0,
    FOREIGN KEY (message_id) REFERENCES messages(id)
);
CREATE INDEX ON sending_pool_emails (scheduled_time, status);

CREATE TABLE templates (
    id SERIAL PRIMARY KEY,
    template_id varchar(50) NOT NULL,
    html varchar NOT NULL,
    domain varchar(254) NOT NULL
);
CREATE INDEX ON templates (domain);
CREATE INDEX ON templates (template_id);
CREATE INDEX ON templates (domain, template_id);

-- migrate:down

DROP TABLE templates;
DROP TABLE sending_pool_emails;
DROP TABLE messages;
DROP TABLE domains;
DROP TYPE SENDING_POOL_STATUS;