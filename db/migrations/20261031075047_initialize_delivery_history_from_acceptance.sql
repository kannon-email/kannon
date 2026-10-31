-- migrate:up
-- The receipt records one application-clock acceptance decision. PostgreSQL now()
-- is transaction-start time, which precedes immediate-send acceptance and need
-- not agree with an injected application clock. Worker retries and outcomes
-- continue to update history through their existing triggers.
CREATE OR REPLACE FUNCTION initialize_delivery_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 SELECT scheduled_at, created_at INTO STRICT NEW.scheduled_at, NEW.updated_at
 FROM batch_intake_receipts WHERE batch_id=NEW.batch_id;
 NEW.status := CASE WHEN NEW.scheduled_at > NEW.updated_at THEN 'scheduled' ELSE 'pending' END;
 RETURN NEW;
END $$;

-- migrate:down
CREATE OR REPLACE FUNCTION initialize_delivery_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 SELECT original_scheduled_time INTO NEW.scheduled_at FROM sending_pool_emails WHERE message_id=NEW.batch_id AND email=NEW.email;
 NEW.scheduled_at := COALESCE(NEW.scheduled_at,now());
 IF NEW.scheduled_at > now() THEN NEW.status := 'scheduled'; END IF;
 RETURN NEW;
END $$;
