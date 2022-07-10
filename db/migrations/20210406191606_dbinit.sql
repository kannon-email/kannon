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
    domain VARCHAR(254) UNIQUE NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    key VARCHAR(50) NOT NULL,
    dkim_private_key VARCHAR NOT NULL,
    dkim_public_key VARCHAR NOT NULL
);
CREATE INDEX ON domains (domain);

CREATE TABLE messages (
    message_id VARCHAR(50) PRIMARY KEY,
    subject VARCHAR NOT NULL,
    sender_email VARCHAR(320) NOT NULL,
    sender_alias VARCHAR(100) NOT NULL,
    template_id VARCHAR(50) NOT NULL,
    domain VARCHAR(254) NOT NULL
);
CREATE INDEX ON messages (message_id);
CREATE INDEX ON messages (domain);

CREATE TABLE sending_pool_emails (
    id SERIAL PRIMARY KEY,
    status SENDING_POOL_STATUS NOT NULL DEFAULT 'initializing',
    scheduled_time TIMESTAMP DEFAULT now() NOT NULL,
    original_scheduled_time TIMESTAMP NOT NULL,
    send_attempts_cnt INT DEFAULT 0 NOT NULL,
    email VARCHAR(320) NOT NULL,
    message_id VARCHAR(50) NOT NULL,
    error_msg VARCHAR NOT NULL DEFAULT '',
    error_code int NOT NULL DEFAULT 0,
    FOREIGN KEY (message_id) REFERENCES messages(message_id)
);
CREATE INDEX scheduled_time_status_idx ON sending_pool_emails (scheduled_time, status);
CREATE UNIQUE INDEX unique_emails_message_id_idx ON sending_pool_emails (email, message_id);

CREATE TABLE templates (
    id SERIAL PRIMARY KEY,
    template_id VARCHAR(50) NOT NULL,
    html VARCHAR NOT NULL,
    domain VARCHAR(254) NOT NULL
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