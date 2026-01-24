-- Dumped from database version 17.6 (Homebrew)
-- Dumped by pg_dump version 17.6 (Homebrew)

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
-- Name: template_type; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.template_type AS ENUM (
    'transient',
    'template'
);


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
-- Name: api_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_keys (
    id character varying(512) NOT NULL,
    key character varying(512) NOT NULL,
    name character varying(100) NOT NULL,
    domain character varying(512) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    expires_at timestamp without time zone,
    is_active boolean DEFAULT true NOT NULL,
    deactivated_at timestamp without time zone
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
    dkim_public_key character varying NOT NULL
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
    headers jsonb
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
    domain character varying NOT NULL
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
-- Name: api_keys api_keys_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_key_key UNIQUE (key);


--
-- Name: api_keys api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);


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
-- Name: api_keys_domain_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX api_keys_domain_idx ON public.api_keys USING btree (domain);


--
-- Name: api_keys_expires_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX api_keys_expires_at_idx ON public.api_keys USING btree (expires_at) WHERE (expires_at IS NOT NULL);


--
-- Name: api_keys_key_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX api_keys_key_active_idx ON public.api_keys USING btree (key) WHERE (is_active = true);


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
-- Name: messages_domain_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX messages_domain_idx ON public.messages USING btree (domain);


--
-- Name: messages_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX messages_message_id_idx ON public.messages USING btree (message_id);


--
-- Name: stats_email_message_id_type_timestamp_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX stats_email_message_id_type_timestamp_idx ON public.stats USING btree (email, message_id, domain, type, "timestamp");


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
    ('20260124120000');
