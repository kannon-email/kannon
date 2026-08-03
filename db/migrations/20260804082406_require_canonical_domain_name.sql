-- migrate:up

-- Canonical form (ADR 0008): lower-case, dot-separated labels of [a-z0-9_-],
-- with no leading, trailing or empty label — which also rules out a
-- single-label name, since it requires at least two labels.
ALTER TABLE domains
  ADD CONSTRAINT domains_domain_check
  CHECK (domain ~ '^[a-z0-9_-]+(\.[a-z0-9_-]+)+$');

-- migrate:down

ALTER TABLE domains DROP CONSTRAINT domains_domain_check;
