-- migrate:up
ALTER TABLE batch_intake_receipts ADD COLUMN completed_at timestamptz;
CREATE INDEX batch_history_order ON batch_intake_receipts(domain,created_at DESC,batch_id DESC);
CREATE INDEX delivery_history_order ON delivery_intake_facts(batch_id,id);
CREATE FUNCTION complete_batch_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Serialize terminal transitions for one Batch, including concurrent final outcomes.
 PERFORM 1 FROM batch_intake_receipts WHERE batch_id=NEW.batch_id FOR UPDATE;
 UPDATE batch_intake_receipts r SET completed_at=clock_timestamp()
 WHERE r.batch_id=NEW.batch_id AND r.completed_at IS NULL
 AND NOT EXISTS (SELECT 1 FROM delivery_intake_facts f WHERE f.batch_id=r.batch_id AND f.outcome_at IS NULL);
 RETURN NEW;
END $$;
CREATE TRIGGER complete_batch_history AFTER UPDATE ON delivery_intake_facts FOR EACH ROW EXECUTE FUNCTION complete_batch_history();
UPDATE batch_intake_receipts r SET completed_at=(SELECT max(outcome_at) FROM delivery_intake_facts f WHERE f.batch_id=r.batch_id)
WHERE NOT EXISTS(SELECT 1 FROM delivery_intake_facts f WHERE f.batch_id=r.batch_id AND f.outcome_at IS NULL);
-- migrate:down
DROP TRIGGER complete_batch_history ON delivery_intake_facts;
DROP FUNCTION complete_batch_history();
DROP INDEX batch_history_order;
DROP INDEX delivery_history_order;
ALTER TABLE batch_intake_receipts DROP COLUMN completed_at;
