-- migrate:up
CREATE TYPE template_type AS ENUM (
    'transient',
    'template'
);

ALTER TABLE templates ADD COLUMN type template_type NOT NULL DEFAULT 'transient';
ALTER TABLE templates ADD COLUMN title VARCHAR(200) NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN created_at TIMESTAMP NOT NULL DEFAULT NOW();
ALTER TABLE templates ADD COLUMN updated_at TIMESTAMP NOT NULL DEFAULT NOW();

CREATE INDEX template_type_domain_idx ON templates (type, domain);

-- migrate:down

DROP INDEX template_type_domain_idx;
ALTER TABLE templates DROP COLUMN type;
ALTER TABLE templates DROP COLUMN title;
ALTER TABLE templates DROP COLUMN created_at;
ALTER TABLE templates DROP COLUMN updated_at;
DROP TYPE TEMPLATE_TYPE;