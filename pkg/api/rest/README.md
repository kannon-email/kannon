# REST adapter conventions

The approved `openapi.yaml` is the complete contract for all 21 operations. Do not
trim it to the endpoints implemented by one ticket. `go generate ./pkg/api/rest`
regenerates wire types and the standard HTTP server interface using pinned
oapi-codegen v2.6.0. The generator creates a temporary 3.0.3 view: nullable type
unions become nullable single types, inline discriminators are omitted (union
payloads remain), and parameter type names receive a prefix. The original 3.1.1
contract is never rewritten. Runtime validation must enforce the original contract.
Run `scripts/check-rest-contract.sh` to detect generation drift and application
imports of transports. Generated models and interfaces belong only in this adapter.

`NewHandler` mounts the versioned routes on the existing API listener. Extend its
protected mux for dependent resource slices. Authenticate Bearer credentials to
an `authz.Principal`, preserve its ID, apply optional Attribution, and pass the
context to existing application operations. Keep authorization in those operations.
The credential slice should extend authentication here for Domain API Keys;
legacy Connect authentication stays independent.

Use direct resource responses, 201 plus Location for creation, snake_case JSON,
10 MiB bounded strict JSON decoding, and the private return-error handler adapter for sanitized RFC 9457
errors. Every response receives a server-generated X-Request-ID. Never render
infrastructure errors or secrets. Domain listing uses an exclusive ascending
name boundary and one lookahead row, with a version/collection/order-bound opaque
cursor; changing page size is allowed. Other collections should define their own
query-bound cursor payloads, without a generic pagination framework.

HTTP acceptance tests use the complete adapter. Run `go test -race -short
./pkg/api/rest` without Docker; omit `-short` to run the PostgreSQL Domain journey.
Application authorization matrices remain in `internal/domains`.

Credential routes share the API Keys application service and the existing
`api_keys` table. Bearer lookup resolves the credential before authorization on
the target Domain; it does not infer identity from the requested path. Missing
legacy role metadata resolves to sender. Role metadata is fixed at issuance;
PATCH only accepts permanent revocation. Connect authentication remains separate.
API Key cursors bind the Domain, collection and descending created_at/ID order,
with an exclusive boundary and one lookahead row.

`TestPersistentCredentialJourney` exercises real PostgreSQL through HTTP and the
existing API Keys service. It defaults to the Docker harness. For a disposable,
empty local database, set `KANNON_TEST_DATABASE_URL` and run
`go test -race ./pkg/api/rest -run TestPersistentCredentialJourney`.
Each fixture creates and drops an isolated database; the test role needs CREATEDB.

Batch intake accepts exactly one HTML body or Domain-owned Template reference.
Global fields supply defaults; recipient fields override them in subject and HTML.
Values are literal, and email is reserved for the actual recipient address.
Missing placeholders and recipient email overrides refuse only the affected row.
Accepted HTML is copied into a private snapshot; the source Template ID is retained
as provenance without a live dependency. Replay never resolves the source again.
Scheduling and attachments are supported. Legacy gRPC personalization remains separate.

Successful Domain-scoped keys currently remain bound indefinitely (exceeding the
24-hour minimum). Only a fingerprint, accepted Batch identity, creation time and
intake counts are retained for replay; no request/response cache or inline refusal
records exist. Accepted HTML and stable Delivery identities are durable intake
facts for subsequent history slices. No history-query endpoints are added here.

For PostgreSQL integration tests without Docker, set `KANNON_TEST_DATABASE_URL`
to a disposable local PostgreSQL database. The test role must have CREATEDB;
each fixture creates and drops its own isolated database.

Aggregate Stats use the existing independent hourly Domain counters through the
Stats application service. Queries require timezone-qualified `from`/`to` and a
whole-hour Go `group_by` duration, return at most 1,000 complete buckets anchored
at `from`, and zero-fill all public event counts. Zero does not prove collection
uptime. No Batch filter is supported. `TestPersistentAggregateSeries` covers the
HTTP and application seams with the same disposable database setting above.
