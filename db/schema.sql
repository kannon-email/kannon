\restrict dbmate

-- Dumped from database version 18.6 (Homebrew)
-- Dumped by pg_dump version 18.6 (Homebrew)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;


--
-- Name: EXTENSION pgcrypto; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION pgcrypto IS 'cryptographic functions';


--
-- Name: template_type; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.template_type AS ENUM (
    'transient',
    'template'
);


--
-- Name: complete_batch_history(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.complete_batch_history() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 -- Serialize terminal transitions for one Batch, including concurrent final outcomes.
 PERFORM 1 FROM batch_intake_receipts WHERE batch_id=NEW.batch_id FOR UPDATE;
 UPDATE batch_intake_receipts r SET completed_at=clock_timestamp()
 WHERE r.batch_id=NEW.batch_id AND r.completed_at IS NULL
 AND NOT EXISTS (SELECT 1 FROM delivery_intake_facts f WHERE f.batch_id=r.batch_id AND f.outcome_at IS NULL);
 RETURN NEW;
END $$;


--
-- Name: initialize_delivery_history(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.initialize_delivery_history() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 SELECT scheduled_at, created_at INTO STRICT NEW.scheduled_at, NEW.updated_at
 FROM batch_intake_receipts WHERE batch_id=NEW.batch_id;
 NEW.status := CASE WHEN NEW.scheduled_at > NEW.updated_at THEN 'scheduled' ELSE 'pending' END;
 RETURN NEW;
END $$;


--
-- Name: update_delivery_history_outcome(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.update_delivery_history_outcome() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
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


--
-- Name: update_delivery_history_pool(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.update_delivery_history_pool() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 UPDATE delivery_intake_facts SET
 status=CASE NEW.status::text WHEN 'sending' THEN 'sending' WHEN 'scheduled' THEN 'scheduled' ELSE 'pending' END,
 attempt_count=NEW.send_attempts_cnt + CASE WHEN NEW.status::text='sending' THEN 1 ELSE 0 END, updated_at=now()
 WHERE batch_id=NEW.message_id AND email=NEW.email AND outcome_at IS NULL;
 RETURN NEW;
END $$;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: Account; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public."Account" (
    id text NOT NULL,
    "userId" text NOT NULL,
    type text NOT NULL,
    provider text NOT NULL,
    "providerAccountId" text NOT NULL,
    refresh_token text,
    access_token text,
    expires_at integer,
    token_type text,
    scope text,
    id_token text,
    session_state text
);


--
-- Name: Example; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public."Example" (
    id text NOT NULL,
    "createdAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "updatedAt" timestamp(3) without time zone NOT NULL
);


--
-- Name: Session; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public."Session" (
    id text NOT NULL,
    "sessionToken" text NOT NULL,
    "userId" text NOT NULL,
    expires timestamp(3) without time zone NOT NULL
);


--
-- Name: User; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public."User" (
    id text NOT NULL,
    name text NOT NULL,
    email text NOT NULL,
    "emailVerified" timestamp(3) without time zone,
    image text NOT NULL,
    "isAdmin" boolean DEFAULT false NOT NULL
);


--
-- Name: VerificationToken; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public."VerificationToken" (
    identifier text NOT NULL,
    token text NOT NULL,
    expires timestamp(3) without time zone NOT NULL
);


--
-- Name: aggregated_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.aggregated_stats (
    domain character varying NOT NULL,
    "timestamp" timestamp without time zone NOT NULL,
    type character varying NOT NULL,
    count bigint DEFAULT 0 NOT NULL
);


--
-- Name: api_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_keys (
    id character varying(512) NOT NULL,
    name character varying(100) NOT NULL,
    domain character varying(512) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    expires_at timestamp without time zone,
    is_active boolean DEFAULT true NOT NULL,
    deactivated_at timestamp without time zone,
    key_hash character varying(64) NOT NULL,
    key_prefix character varying(10) NOT NULL,
    roles text[]
);


--
-- Name: audit_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audit_records (
    id text NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    principal text NOT NULL,
    resource text[] NOT NULL,
    action text NOT NULL,
    outcome text NOT NULL,
    data jsonb NOT NULL
);


--
-- Name: batch_content_snapshots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.batch_content_snapshots (
    batch_id text NOT NULL,
    html text NOT NULL,
    source_template_id text DEFAULT ''::text NOT NULL
);


--
-- Name: batch_intake_receipts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.batch_intake_receipts (
    scheduled_at timestamp with time zone NOT NULL,
    attachments jsonb DEFAULT '[]'::jsonb NOT NULL,
    domain text NOT NULL,
    idempotency_key text NOT NULL,
    fingerprint text NOT NULL,
    batch_id text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    accepted_count integer NOT NULL,
    rejected_count integer NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT batch_intake_receipts_accepted_count_check CHECK ((accepted_count > 0)),
    CONSTRAINT batch_intake_receipts_rejected_count_check CHECK ((rejected_count >= 0))
);


--
-- Name: counted_stat_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.counted_stat_events (
    stream text NOT NULL,
    stored_at text NOT NULL,
    sequence numeric(20,0) NOT NULL,
    CONSTRAINT counted_stat_events_sequence_check CHECK ((sequence > (0)::numeric))
);


--
-- Name: delivery_intake_facts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.delivery_intake_facts (
    id text NOT NULL,
    batch_id text NOT NULL,
    email text NOT NULL,
    tracking jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    scheduled_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    outcome_at timestamp with time zone,
    error_code text DEFAULT ''::text NOT NULL,
    error_detail text DEFAULT ''::text NOT NULL,
    CONSTRAINT delivery_intake_facts_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'scheduled'::text, 'sending'::text, 'delivered'::text, 'rejected'::text, 'bounced'::text, 'failed'::text])))
);


--
-- Name: domain_template; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.domain_template (
    name text NOT NULL,
    domain text NOT NULL,
    "templateId" text NOT NULL,
    source jsonb NOT NULL
);


--
-- Name: domain_user; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.domain_user (
    domain text NOT NULL,
    "userId" text NOT NULL
);


--
-- Name: domains; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.domains (
    id integer NOT NULL,
    domain character varying(254) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    dkim_private_key character varying NOT NULL,
    dkim_public_key character varying NOT NULL,
    tracking jsonb DEFAULT '{"links": "identified", "opens": "identified"}'::jsonb NOT NULL,
    CONSTRAINT domains_domain_check CHECK (((domain)::text ~ '^[a-z0-9_-]+(\.[a-z0-9_-]+)+$'::text))
);


--
-- Name: domains_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.domains_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: domains_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.domains_id_seq OWNED BY public.domains.id;


--
-- Name: messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.messages (
    message_id character varying NOT NULL,
    subject character varying NOT NULL,
    sender_email character varying(320) NOT NULL,
    sender_alias character varying(100) NOT NULL,
    template_id character varying NOT NULL,
    domain character varying(254) NOT NULL,
    attachments jsonb,
    headers jsonb DEFAULT '{}'::jsonb NOT NULL,
    tracking jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.schema_migrations (
    version character varying NOT NULL
);


--
-- Name: sending_pool_emails; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sending_pool_emails (
    id integer NOT NULL,
    scheduled_time timestamp without time zone DEFAULT now() NOT NULL,
    original_scheduled_time timestamp without time zone NOT NULL,
    send_attempts_cnt integer DEFAULT 0 NOT NULL,
    email character varying(320) NOT NULL,
    message_id character varying NOT NULL,
    fields jsonb DEFAULT '{}'::jsonb NOT NULL,
    status character varying(100) DEFAULT 'initializing'::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    domain character varying NOT NULL,
    tracking jsonb DEFAULT '{"links": "identified", "opens": "identified"}'::jsonb NOT NULL,
    claimed_at timestamp without time zone
);


--
-- Name: sending_pool_emails_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sending_pool_emails_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sending_pool_emails_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sending_pool_emails_id_seq OWNED BY public.sending_pool_emails.id;


--
-- Name: stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.stats (
    id integer NOT NULL,
    type character varying NOT NULL,
    email character varying NOT NULL,
    message_id character varying NOT NULL,
    domain character varying NOT NULL,
    "timestamp" timestamp without time zone DEFAULT now() NOT NULL,
    data jsonb NOT NULL
);


--
-- Name: stats_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.stats_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: stats_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.stats_id_seq OWNED BY public.stats.id;


--
-- Name: stats_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.stats_keys (
    id character varying NOT NULL,
    private_key character varying NOT NULL,
    public_key character varying NOT NULL,
    creation_time timestamp without time zone DEFAULT now() NOT NULL,
    expiration_time timestamp without time zone NOT NULL
);


--
-- Name: templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.templates (
    id integer NOT NULL,
    template_id character varying NOT NULL,
    html character varying NOT NULL,
    domain character varying(254) NOT NULL,
    type public.template_type DEFAULT 'transient'::public.template_type NOT NULL,
    title character varying(200) DEFAULT ''::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: templates_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.templates_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: templates_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.templates_id_seq OWNED BY public.templates.id;


--
-- Name: domains id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.domains ALTER COLUMN id SET DEFAULT nextval('public.domains_id_seq'::regclass);


--
-- Name: sending_pool_emails id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sending_pool_emails ALTER COLUMN id SET DEFAULT nextval('public.sending_pool_emails_id_seq'::regclass);


--
-- Name: stats id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.stats ALTER COLUMN id SET DEFAULT nextval('public.stats_id_seq'::regclass);


--
-- Name: templates id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.templates ALTER COLUMN id SET DEFAULT nextval('public.templates_id_seq'::regclass);


--
-- Name: Account Account_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public."Account"
    ADD CONSTRAINT "Account_pkey" PRIMARY KEY (id);


--
-- Name: Example Example_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public."Example"
    ADD CONSTRAINT "Example_pkey" PRIMARY KEY (id);


--
-- Name: Session Session_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public."Session"
    ADD CONSTRAINT "Session_pkey" PRIMARY KEY (id);


--
-- Name: User User_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public."User"
    ADD CONSTRAINT "User_pkey" PRIMARY KEY (id);


--
-- Name: aggregated_stats aggregated_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.aggregated_stats
    ADD CONSTRAINT aggregated_stats_pkey PRIMARY KEY (domain, "timestamp", type);


--
-- Name: api_keys api_keys_key_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_key_hash_key UNIQUE (key_hash);


--
-- Name: api_keys api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);


--
-- Name: audit_records audit_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_records
    ADD CONSTRAINT audit_records_pkey PRIMARY KEY (id);


--
-- Name: batch_content_snapshots batch_content_snapshots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.batch_content_snapshots
    ADD CONSTRAINT batch_content_snapshots_pkey PRIMARY KEY (batch_id);


--
-- Name: batch_intake_receipts batch_intake_receipts_batch_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.batch_intake_receipts
    ADD CONSTRAINT batch_intake_receipts_batch_id_key UNIQUE (batch_id);


--
-- Name: batch_intake_receipts batch_intake_receipts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.batch_intake_receipts
    ADD CONSTRAINT batch_intake_receipts_pkey PRIMARY KEY (domain, idempotency_key);


--
-- Name: counted_stat_events counted_stat_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.counted_stat_events
    ADD CONSTRAINT counted_stat_events_pkey PRIMARY KEY (stream, stored_at, sequence);


--
-- Name: delivery_intake_facts delivery_intake_facts_batch_id_email_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.delivery_intake_facts
    ADD CONSTRAINT delivery_intake_facts_batch_id_email_key UNIQUE (batch_id, email);


--
-- Name: delivery_intake_facts delivery_intake_facts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.delivery_intake_facts
    ADD CONSTRAINT delivery_intake_facts_pkey PRIMARY KEY (id);


--
-- Name: domain_template domain_template_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.domain_template
    ADD CONSTRAINT domain_template_pkey PRIMARY KEY ("templateId");


--
-- Name: domain_user domain_user_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.domain_user
    ADD CONSTRAINT domain_user_pkey PRIMARY KEY (domain, "userId");


--
-- Name: domains domains_domain_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.domains
    ADD CONSTRAINT domains_domain_key UNIQUE (domain);


--
-- Name: domains domains_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.domains
    ADD CONSTRAINT domains_pkey PRIMARY KEY (id);


--
-- Name: messages messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_pkey PRIMARY KEY (message_id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: sending_pool_emails sending_pool_emails_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sending_pool_emails
    ADD CONSTRAINT sending_pool_emails_pkey PRIMARY KEY (id);


--
-- Name: stats_keys stats_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.stats_keys
    ADD CONSTRAINT stats_keys_pkey PRIMARY KEY (id);


--
-- Name: stats stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.stats
    ADD CONSTRAINT stats_pkey PRIMARY KEY (id);


--
-- Name: templates templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.templates
    ADD CONSTRAINT templates_pkey PRIMARY KEY (id);


--
-- Name: Account_provider_providerAccountId_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX "Account_provider_providerAccountId_key" ON public."Account" USING btree (provider, "providerAccountId");


--
-- Name: Session_sessionToken_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX "Session_sessionToken_key" ON public."Session" USING btree ("sessionToken");


--
-- Name: User_email_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX "User_email_key" ON public."User" USING btree (email);


--
-- Name: VerificationToken_identifier_token_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX "VerificationToken_identifier_token_key" ON public."VerificationToken" USING btree (identifier, token);


--
-- Name: VerificationToken_token_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX "VerificationToken_token_key" ON public."VerificationToken" USING btree (token);


--
-- Name: aggregated_stats_timestamp_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX aggregated_stats_timestamp_idx ON public.aggregated_stats USING btree ("timestamp");


--
-- Name: api_keys_domain_created_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX api_keys_domain_created_id_idx ON public.api_keys USING btree (domain, created_at DESC, id DESC);


--
-- Name: api_keys_domain_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX api_keys_domain_idx ON public.api_keys USING btree (domain);


--
-- Name: api_keys_expires_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX api_keys_expires_at_idx ON public.api_keys USING btree (expires_at) WHERE (expires_at IS NOT NULL);


--
-- Name: api_keys_key_hash_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX api_keys_key_hash_active_idx ON public.api_keys USING btree (key_hash) WHERE (is_active = true);


--
-- Name: audit_records_occurred_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_records_occurred_at_idx ON public.audit_records USING btree (occurred_at);


--
-- Name: batch_history_expiration; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX batch_history_expiration ON public.batch_intake_receipts USING btree (completed_at) WHERE (completed_at IS NOT NULL);


--
-- Name: batch_history_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX batch_history_order ON public.batch_intake_receipts USING btree (domain, created_at DESC, batch_id DESC);


--
-- Name: counted_stat_events_stored_second_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX counted_stat_events_stored_second_idx ON public.counted_stat_events USING btree ("left"(stored_at, 19) COLLATE "C");


--
-- Name: delivery_history_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX delivery_history_order ON public.delivery_intake_facts USING btree (batch_id, id);


--
-- Name: domain_template_domain_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX domain_template_domain_idx ON public.domain_template USING btree (domain);


--
-- Name: domain_template_templateId_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX "domain_template_templateId_key" ON public.domain_template USING btree ("templateId");


--
-- Name: domain_user_domain_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX domain_user_domain_idx ON public.domain_user USING btree (domain);


--
-- Name: domains_domain_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX domains_domain_idx ON public.domains USING btree (domain);


--
-- Name: messages_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX messages_created_at_idx ON public.messages USING btree (created_at);


--
-- Name: messages_domain_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX messages_domain_idx ON public.messages USING btree (domain);


--
-- Name: messages_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX messages_message_id_idx ON public.messages USING btree (message_id);


--
-- Name: sending_pool_emails_status_claimed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sending_pool_emails_status_claimed_at_idx ON public.sending_pool_emails USING btree (status, claimed_at) WHERE ((status)::text = ANY (ARRAY[('sending'::character varying)::text, ('validating'::character varying)::text]));


--
-- Name: sending_pool_emails_status_scheduled_time_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sending_pool_emails_status_scheduled_time_idx ON public.sending_pool_emails USING btree (status, scheduled_time) WHERE ((status)::text = ANY (ARRAY[('to_validate'::character varying)::text, ('scheduled'::character varying)::text]));


--
-- Name: stats_email_message_id_type_timestamp_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX stats_email_message_id_type_timestamp_idx ON public.stats USING btree (email, message_id, domain, type, "timestamp");


--
-- Name: stats_timestamp_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX stats_timestamp_idx ON public.stats USING btree ("timestamp");


--
-- Name: stats_type_message_id_type_timestamp_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX stats_type_message_id_type_timestamp_idx ON public.stats USING btree (message_id, domain, type, "timestamp");


--
-- Name: template_type_domain_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX template_type_domain_idx ON public.templates USING btree (type, domain);


--
-- Name: templates_domain_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX templates_domain_idx ON public.templates USING btree (domain);


--
-- Name: templates_domain_template_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX templates_domain_template_id_idx ON public.templates USING btree (domain, template_id);


--
-- Name: templates_template_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX templates_template_id_idx ON public.templates USING btree (template_id);


--
-- Name: unique_emails_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX unique_emails_message_id_idx ON public.sending_pool_emails USING btree (email, message_id);


--
-- Name: delivery_intake_facts complete_batch_history; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER complete_batch_history AFTER UPDATE ON public.delivery_intake_facts FOR EACH ROW EXECUTE FUNCTION public.complete_batch_history();


--
-- Name: delivery_intake_facts initialize_delivery_history; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER initialize_delivery_history BEFORE INSERT ON public.delivery_intake_facts FOR EACH ROW EXECUTE FUNCTION public.initialize_delivery_history();


--
-- Name: stats update_delivery_history_outcome; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER update_delivery_history_outcome AFTER INSERT ON public.stats FOR EACH ROW EXECUTE FUNCTION public.update_delivery_history_outcome();


--
-- Name: sending_pool_emails update_delivery_history_pool; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER update_delivery_history_pool AFTER UPDATE ON public.sending_pool_emails FOR EACH ROW EXECUTE FUNCTION public.update_delivery_history_pool();


--
-- Name: Account Account_userId_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public."Account"
    ADD CONSTRAINT "Account_userId_fkey" FOREIGN KEY ("userId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: Session Session_userId_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public."Session"
    ADD CONSTRAINT "Session_userId_fkey" FOREIGN KEY ("userId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: api_keys api_keys_domain_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_domain_fkey FOREIGN KEY (domain) REFERENCES public.domains(domain) ON DELETE CASCADE;


--
-- Name: batch_content_snapshots batch_content_snapshots_batch_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.batch_content_snapshots
    ADD CONSTRAINT batch_content_snapshots_batch_id_fkey FOREIGN KEY (batch_id) REFERENCES public.messages(message_id);


--
-- Name: delivery_intake_facts delivery_intake_facts_batch_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.delivery_intake_facts
    ADD CONSTRAINT delivery_intake_facts_batch_id_fkey FOREIGN KEY (batch_id) REFERENCES public.batch_intake_receipts(batch_id);


--
-- Name: domain_user domain_user_userId_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.domain_user
    ADD CONSTRAINT "domain_user_userId_fkey" FOREIGN KEY ("userId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE RESTRICT;


--
-- Name: sending_pool_emails sending_pool_emails_message_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sending_pool_emails
    ADD CONSTRAINT sending_pool_emails_message_id_fkey FOREIGN KEY (message_id) REFERENCES public.messages(message_id);


--
-- PostgreSQL database dump complete
--

\unrestrict dbmate


--
-- Dbmate schema migrations
--

INSERT INTO public.schema_migrations (version) VALUES
    ('20210406191606'),
    ('20220717130048'),
    ('20220806075424'),
    ('20220809092503'),
    ('20220830073617'),
    ('20220904111715'),
    ('20240420090612'),
    ('20240421080953'),
    ('20260104120000'),
    ('20260106120000'),
    ('20260124120000'),
    ('20260214120000'),
    ('20260214120001'),
    ('20260214120002'),
    ('20260509102644'),
    ('20260726084414'),
    ('20260727071416'),
    ('20260803094036'),
    ('20260804082406'),
    ('20260804135145'),
    ('20261029221540'),
    ('20261030225738'),
    ('20261030230324'),
    ('20261030231401'),
    ('20261030231407'),
    ('20261030231457'),
    ('20261030231941'),
    ('20261030232432'),
    ('20261031075047'),
    ('20261031174350'),
    ('20261031174606'),
    ('20261031175305');
