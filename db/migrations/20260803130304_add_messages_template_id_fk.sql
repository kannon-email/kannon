-- migrate:up
-- A Batch's Template has to outlive the Batch: GetSendingData joins messages to
-- templates, so a Template deleted while a Delivery of one of its Batches is
-- still pending leaves that Delivery with no Envelope that can ever be built
-- (ADR 0008).
--
-- The join is on template_id, and the code has always treated that column as
-- unique — every :one query keyed on it assumes so. The constraint that makes it
-- referenceable only writes down an invariant already relied upon.
ALTER TABLE templates ADD CONSTRAINT templates_template_id_key UNIQUE (template_id);

-- Redundant from here: the constraint's own index answers every lookup this one
-- did.
DROP INDEX templates_template_id_idx;

-- NOT VALID deliberately. Batches orphaned before this migration are the damage
-- the key exists to prevent, and refusing to install over them would leave every
-- future Batch unprotected too. The constraint is enforced in full on every
-- insert into messages and on every delete from templates regardless; only rows
-- that already exist go unchecked.
ALTER TABLE messages ADD CONSTRAINT messages_template_id_fkey
    FOREIGN KEY (template_id) REFERENCES templates (template_id) ON DELETE RESTRICT NOT VALID;

-- ON DELETE RESTRICT asks messages "does any Batch still reference this
-- Template?" on every Template delete, and Postgres does not index a
-- referencing column of its own accord.
CREATE INDEX messages_template_id_idx ON messages (template_id);

-- migrate:down
DROP INDEX messages_template_id_idx;

ALTER TABLE messages DROP CONSTRAINT messages_template_id_fkey;

ALTER TABLE templates DROP CONSTRAINT templates_template_id_key;

CREATE INDEX templates_template_id_idx ON templates (template_id);
