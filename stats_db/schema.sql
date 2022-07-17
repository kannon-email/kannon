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

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: accepted; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.accepted (
    id integer NOT NULL,
    email character varying(320) NOT NULL,
    message_id character varying NOT NULL,
    domain character varying NOT NULL,
    "timestamp" timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: accepted_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.accepted_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: accepted_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.accepted_id_seq OWNED BY public.accepted.id;


--
-- Name: hard_bounced; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.hard_bounced (
    id integer NOT NULL,
    email character varying(320) NOT NULL,
    message_id character varying NOT NULL,
    domain character varying NOT NULL,
    err_code integer NOT NULL,
    err_msg character varying NOT NULL,
    "timestamp" timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: hard_bounced_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.hard_bounced_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: hard_bounced_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.hard_bounced_id_seq OWNED BY public.hard_bounced.id;


--
-- Name: open; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.open (
    id integer NOT NULL,
    email character varying(320) NOT NULL,
    message_id character varying NOT NULL,
    domain character varying NOT NULL,
    ip character varying NOT NULL,
    user_agent character varying NOT NULL,
    "timestamp" timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: open_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.open_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: open_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.open_id_seq OWNED BY public.open.id;


--
-- Name: prepared; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.prepared (
    id integer NOT NULL,
    email character varying(320) NOT NULL,
    message_id character varying NOT NULL,
    domain character varying NOT NULL,
    "timestamp" timestamp without time zone DEFAULT now() NOT NULL,
    first_timestamp timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: prepared_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.prepared_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: prepared_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.prepared_id_seq OWNED BY public.prepared.id;


--
-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.schema_migrations (
    version character varying(255) NOT NULL
);


--
-- Name: accepted id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.accepted ALTER COLUMN id SET DEFAULT nextval('public.accepted_id_seq'::regclass);


--
-- Name: hard_bounced id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hard_bounced ALTER COLUMN id SET DEFAULT nextval('public.hard_bounced_id_seq'::regclass);


--
-- Name: open id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.open ALTER COLUMN id SET DEFAULT nextval('public.open_id_seq'::regclass);


--
-- Name: prepared id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prepared ALTER COLUMN id SET DEFAULT nextval('public.prepared_id_seq'::regclass);


--
-- Name: accepted accepted_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.accepted
    ADD CONSTRAINT accepted_pkey PRIMARY KEY (id);


--
-- Name: hard_bounced hard_bounced_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hard_bounced
    ADD CONSTRAINT hard_bounced_pkey PRIMARY KEY (id);


--
-- Name: open open_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.open
    ADD CONSTRAINT open_pkey PRIMARY KEY (id);


--
-- Name: prepared prepared_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prepared
    ADD CONSTRAINT prepared_pkey PRIMARY KEY (id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: accepted_email_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX accepted_email_message_id_idx ON public.accepted USING btree (email, message_id, domain);


--
-- Name: accepted_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX accepted_message_id_idx ON public.accepted USING btree (message_id, domain);


--
-- Name: hard_bounced_email_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX hard_bounced_email_message_id_idx ON public.hard_bounced USING btree (email, message_id, domain);


--
-- Name: hard_bounced_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX hard_bounced_message_id_idx ON public.hard_bounced USING btree (message_id, domain);


--
-- Name: open_email_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX open_email_message_id_idx ON public.open USING btree (email, message_id, domain);


--
-- Name: open_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX open_message_id_idx ON public.open USING btree (message_id, domain);


--
-- Name: prepared_email_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX prepared_email_message_id_idx ON public.prepared USING btree (email, message_id, domain);


--
-- Name: prepared_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX prepared_message_id_idx ON public.prepared USING btree (message_id, domain);


--
-- PostgreSQL database dump complete
--


--
-- Dbmate schema migrations
--

INSERT INTO public.schema_migrations (version) VALUES
    ('20220715160003'),
    ('20220717173338');
