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
infrastructure errors or secrets.

Every list goes through `page.go`: `readPage` parses `limit` (default 100, 1..1000)
and `cursor`, `writePage` trims the one lookahead row and encodes the next cursor.
A cursor is base64url JSON of a scope digest and the last row's typed position.
The scope names the collection and order plus every value that narrows it
(Domain, parent ID, filters), so a cursor replayed elsewhere is `invalid_cursor`;
changing page size is allowed. Application operations and repositories receive a
`keyset.Page` of that typed position and never see cursor syntax.

HTTP acceptance tests use the complete adapter. Run `go test -race
./pkg/api/rest ./internal/intake ./internal/stats` with Docker available. The REST
short suite is not Docker-free (the Delivery lifecycle test still uses PostgreSQL).
Application authorization matrices remain in `internal/domains`.

Credential routes share the API Keys application service and the existing
`api_keys` table. Bearer lookup resolves the credential before authorization on
the target Domain; it does not infer identity from the requested path. Missing
legacy role metadata resolves to sender. Role metadata is fixed at issuance;
PATCH only accepts permanent revocation. Connect authentication remains separate.

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

Request parsing uses private value/error functions. `decodeBody` reads at most
10 MiB plus one sentinel byte, checks explicit nulls, and strictly decodes one
JSON value without a ResponseWriter. The size includes trailing whitespace.
Unlike MaxBytesReader, this reader does not request that net/http close an
oversized request's connection; normal server body draining/connection policy
applies. It never reads an unbounded body into memory.

Parse RawQuery once. Shared name/scalar validation rejects malformed encoding,
unknown names and repeated scalars; only event `type` may repeat. Name checks
are deterministic and precede multiplicity checks. Page size and cursor are
adapter syntax, checked before authorization for every list, events included;
event filter validation remains behind the application's authorization Guard.

Two deliberate history-query corrections return 400: malformed percent encoding
or semicolon queries are now rejected (rather than silently discarded by
URL.Query), and unknown names use `unknown_query_parameter`, including detail
routes, consistently with the other operations.

Batch input reuses generated DTOs, with a strict content-union adapter and an
attachment presence check. `content_base64` now accepts only JSON strings as
specified by OpenAPI; numeric arrays previously accepted by Go's byte decoder
return 400 `malformed_request`. Missing content returns 422; an empty string is
valid, and invalid base64 remains 400. Template creation intentionally uses
TemplatePatch as a presence adapter so omitted title differs from empty title.
Missing recipient email still reaches intake as an inline refusal. Explicit null
anywhere remains a whole-request 422 with an escaped JSON pointer. Maps, tracking
and attachments retain their prior application shapes for idempotency replay.
