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
-- Name: bounced; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bounced (
    id integer NOT NULL,
    email character varying(320) NOT NULL,
    message_id character varying NOT NULL,
    err_code integer NOT NULL,
    err_msg character varying NOT NULL,
    "timestamp" timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: bounced_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bounced_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: bounced_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bounced_id_seq OWNED BY public.bounced.id;


--
-- Name: delivered; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.delivered (
    id integer NOT NULL,
    email character varying(320) NOT NULL,
    message_id character varying NOT NULL,
    "timestamp" timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: delivered_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.delivered_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: delivered_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.delivered_id_seq OWNED BY public.delivered.id;


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
-- Name: bounced id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bounced ALTER COLUMN id SET DEFAULT nextval('public.bounced_id_seq'::regclass);


--
-- Name: delivered id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.delivered ALTER COLUMN id SET DEFAULT nextval('public.delivered_id_seq'::regclass);


--
-- Name: accepted accepted_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.accepted
    ADD CONSTRAINT accepted_pkey PRIMARY KEY (id);


--
-- Name: bounced bounced_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bounced
    ADD CONSTRAINT bounced_pkey PRIMARY KEY (id);


--
-- Name: delivered delivered_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.delivered
    ADD CONSTRAINT delivered_pkey PRIMARY KEY (id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: accepted_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX accepted_message_id_idx ON public.accepted USING btree (message_id);


--
-- Name: bounced_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX bounced_message_id_idx ON public.bounced USING btree (message_id);


--
-- Name: delivered_message_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX delivered_message_id_idx ON public.delivered USING btree (message_id);


--
-- PostgreSQL database dump complete
--


--
-- Dbmate schema migrations
--

INSERT INTO public.schema_migrations (version) VALUES
    ('20220715160003');
