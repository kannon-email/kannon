-- migrate:up
CREATE TABLE stats_keys (
    id VARCHAR PRIMARY KEY,
    private_key VARCHAR NOT NULL,
    public_key VARCHAR NOT NULL,
    creation_time TIMESTAMP NOT NULL DEFAULT NOW(),
    expiration_time TIMESTAMP NOT NULL
);

-- migrate:down

DROP TABLE stats_keys;