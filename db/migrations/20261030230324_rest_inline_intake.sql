-- migrate:up
CREATE TABLE batch_intake_receipts (
 domain text NOT NULL,
 idempotency_key text NOT NULL,
 fingerprint text NOT NULL,
 batch_id text NOT NULL UNIQUE REFERENCES messages(message_id),
 created_at timestamptz NOT NULL,
 accepted_count integer NOT NULL CHECK (accepted_count > 0),
 rejected_count integer NOT NULL CHECK (rejected_count >= 0),
 PRIMARY KEY(domain, idempotency_key)
);
CREATE TABLE batch_content_snapshots (
 batch_id text PRIMARY KEY REFERENCES messages(message_id),
 html text NOT NULL
);
-- Durable accepted identity survives removal from the worker Pool. History
-- slices extend these facts with lifecycle events; refused input never enters it.
CREATE TABLE delivery_intake_facts (
 id text PRIMARY KEY,
 batch_id text NOT NULL REFERENCES batch_intake_receipts(batch_id),
 email text NOT NULL,
 tracking jsonb NOT NULL,
 UNIQUE(batch_id,email)
);
-- migrate:down
DROP TABLE delivery_intake_facts;
DROP TABLE batch_intake_receipts;
DROP TABLE batch_content_snapshots;
