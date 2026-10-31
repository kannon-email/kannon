-- migrate:up
ALTER TABLE delivery_intake_facts
 ADD COLUMN status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','scheduled','sending','delivered','rejected','bounced','failed')),
 ADD COLUMN scheduled_at timestamptz NOT NULL DEFAULT now(),
 ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now(),
 ADD COLUMN attempt_count integer NOT NULL DEFAULT 0,
 ADD COLUMN outcome_at timestamptz,
 ADD COLUMN error_code text NOT NULL DEFAULT '',
 ADD COLUMN error_detail text NOT NULL DEFAULT '';

-- Initialize from the accepted worker record, including future schedules.
CREATE FUNCTION initialize_delivery_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 SELECT original_scheduled_time INTO NEW.scheduled_at FROM sending_pool_emails WHERE message_id=NEW.batch_id AND email=NEW.email;
 NEW.scheduled_at := COALESCE(NEW.scheduled_at,now());
 IF NEW.scheduled_at > now() THEN NEW.status := 'scheduled'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER initialize_delivery_history BEFORE INSERT ON delivery_intake_facts FOR EACH ROW EXECUTE FUNCTION initialize_delivery_history();

-- Worker claims and retries update the same durable record in their transaction.
CREATE FUNCTION update_delivery_history_pool() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE delivery_intake_facts SET
 status=CASE NEW.status::text WHEN 'sending' THEN 'sending' WHEN 'scheduled' THEN 'scheduled' ELSE 'pending' END,
 attempt_count=NEW.send_attempts_cnt + CASE WHEN NEW.status::text='sending' THEN 1 ELSE 0 END, updated_at=now()
 WHERE batch_id=NEW.message_id AND email=NEW.email AND outcome_at IS NULL;
 RETURN NEW;
END $$;
CREATE TRIGGER update_delivery_history_pool AFTER UPDATE ON sending_pool_emails FOR EACH ROW EXECUTE FUNCTION update_delivery_history_pool();

-- Outcomes arrive through the existing Stats writer, even after pool removal.
-- Never associate engagement identities with accepted recipient identities.
CREATE FUNCTION update_delivery_history_outcome() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.type::text NOT IN ('delivered','rejected','bounced','failed') THEN RETURN NEW; END IF;
 UPDATE delivery_intake_facts f SET status=NEW.type::text,
 outcome_at=NEW.timestamp, updated_at=GREATEST(f.updated_at,NEW.timestamp),
 error_code=CASE WHEN NEW.type::text='delivered' THEN '' ELSE NEW.type::text END,
 error_detail=CASE WHEN NEW.type::text='delivered' THEN '' ELSE COALESCE(NEW.data->'rejected'->>'reason',NEW.data->'failed'->>'reason',NEW.data->'bounced'->>'msg','') END
 FROM batch_intake_receipts r
 WHERE f.batch_id=r.batch_id AND r.domain=NEW.domain AND f.batch_id=NEW.message_id AND f.email=NEW.email
 AND (f.outcome_at IS NULL OR NEW.timestamp>f.outcome_at)
 AND NOT (f.status='bounced' AND NEW.type::text='delivered');
 RETURN NEW;
END $$;
CREATE TRIGGER update_delivery_history_outcome AFTER INSERT ON stats FOR EACH ROW EXECUTE FUNCTION update_delivery_history_outcome();
-- migrate:down
DROP TRIGGER update_delivery_history_outcome ON stats;
DROP FUNCTION update_delivery_history_outcome();
DROP TRIGGER update_delivery_history_pool ON sending_pool_emails;
DROP FUNCTION update_delivery_history_pool();
DROP TRIGGER initialize_delivery_history ON delivery_intake_facts;
DROP FUNCTION initialize_delivery_history();
ALTER TABLE delivery_intake_facts DROP COLUMN status, DROP COLUMN scheduled_at, DROP COLUMN updated_at, DROP COLUMN attempt_count, DROP COLUMN outcome_at, DROP COLUMN error_code, DROP COLUMN error_detail;
