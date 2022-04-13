SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: sending_pool_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.sending_pool_status AS ENUM (
    'initializing',
    'sending',
    'sent',
    'scheduled',
    'error'
);


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: domains; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.domains (
    id integer NOT NULL,
    domain character varying(254) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    key character varying(50) NOT NULL,
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
    id integer NOT NULL,
    message_id character varying(50) NOT NULL,
    subject character varying NOT NULL,
    sender_email character varying(320) NOT NULL,
    sender_alias character varying(100) NOT NULL,
    template_id character varying(50) NOT NULL,
    domain character varying(254) NOT NULL
);


--
-- Name: messages_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.messages_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: messages_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.messages_id_seq OWNED BY public.messages.id;


--
-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.schema_migrations (
    version character varying(255) NOT NULL
);


--
-- Name: sending_pool_emails; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sending_pool_emails (
    id integer NOT NULL,
    status public.sending_pool_status DEFAULT 'initializing'::public.sending_pool_status NOT NULL,
    scheduled_time timestamp with time zone DEFAULT now() NOT NULL,
    original_scheduled_time timestamp with time zone NOT NULL,
    trial smallint DEFAULT 0 NOT NULL,
    email character varying(320) NOT NULL,
    message_id integer NOT NULL,
    error_msg character varying DEFAULT ''::character varying NOT NULL,
    error_code integer DEFAULT 0 NOT NULL
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
-- Name: sending_pool_emails_message_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sending_pool_emails_message_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sending_pool_emails_message_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sending_pool_emails_message_id_seq OWNED BY public.sending_pool_emails.message_id;


--
-- Name: templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.templates (
    id integer NOT NULL,
    template_id character varying(50) NOT NULL,
    html character varying NOT NULL,
    domain character varying(254) NOT NULL
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
-- Name: messages id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.messages ALTER COLUMN id SET DEFAULT nextval('public.messages_id_seq'::regclass);


--
-- Name: sending_pool_emails id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sending_pool_emails ALTER COLUMN id SET DEFAULT nextval('public.sending_pool_emails_id_seq'::regclass);


--
-- Name: sending_pool_emails message_id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sending_pool_emails ALTER COLUMN message_id SET DEFAULT nextval('public.sending_pool_emails_message_id_seq'::regclass);


--
-- Name: templates id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.templates ALTER COLUMN id SET DEFAULT nextval('public.templates_id_seq'::regclass);


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
    ADD CONSTRAINT messages_pkey PRIMARY KEY (id);


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
-- Name: templates templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.templates
    ADD CONSTRAINT templates_pkey PRIMARY KEY (id);


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
-- Name: sending_pool_emails_scheduled_time_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sending_pool_emails_scheduled_time_status_idx ON public.sending_pool_emails USING btree (scheduled_time, status);


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
-- Name: sending_pool_emails sending_pool_emails_message_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sending_pool_emails
    ADD CONSTRAINT sending_pool_emails_message_id_fkey FOREIGN KEY (message_id) REFERENCES public.messages(id);


--
-- PostgreSQL database dump complete
--


--
-- Dbmate schema migrations
--

INSERT INTO public.schema_migrations (version) VALUES
    ('20210406191606');
