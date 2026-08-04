-- migrate:up

-- One row per authorization decision Guard reached (ADR 0010). The register is
-- written and never read by Kannon, so the schema is shaped by what it must be
-- able to say later rather than by any query that exists today.
CREATE TABLE audit_records (
    -- The identifier comes from the producer, not from a sequence: it is what
    -- makes a redelivered message insert nothing the second time, while leaving
    -- two genuinely simultaneous identical operations as two rows — which any
    -- natural key over the columns below would have collapsed into one.
    id text PRIMARY KEY,

    -- Stamped by the producer too, and timestamptz where the older columns are
    -- not: this instant crosses a process boundary as JSON over NATS, so the
    -- offset has to travel with it. A consumer catching up after an outage
    -- would otherwise date a week of decisions to the moment it recovered.
    occurred_at timestamptz NOT NULL,

    -- The credential that acted, and only sometimes a person: a shared Admin
    -- Token makes this the constant 'admin-token' until per-operator
    -- credentials land (ADR 0009). NOT NULL, and holding the empty string
    -- exactly when outcome is 'no-principal' — a request nothing authenticated
    -- has no Principal, and the outcome column is what says so, rather than a
    -- NULL every future query would have to carry a case for.
    principal text NOT NULL,

    -- The Resource as its segments, which is the representation the model
    -- compares on. Not the joined path: a Resource's own type calls that form
    -- display only, and a prefix query over it would match a differently-named
    -- Domain.
    resource text[] NOT NULL,

    action text NOT NULL,

    -- allowed | denied | no-principal. Text and not an enum, because the closed
    -- vocabulary lives in authz and a second definition here would have to be
    -- migrated in step with it.
    outcome text NOT NULL,

    -- The Attribution, the Principal's Grants, and which check refused. jsonb
    -- so that growing what a decision records costs no migration, and so that
    -- the one piece of personal data an Audit Record holds stays in a single
    -- place an erasure request can scan.
    data jsonb NOT NULL
);

-- The only index, and it exists for the retention sweep — the same reason
-- stats_timestamp_idx does. Indexes on principal or on a resource prefix wait
-- for the read that will state the shape it queries by; CREATE INDEX is
-- reversible and a speculative one is a guess kept forever.
CREATE INDEX audit_records_occurred_at_idx ON audit_records (occurred_at);

-- migrate:down

DROP INDEX audit_records_occurred_at_idx;

DROP TABLE audit_records;
