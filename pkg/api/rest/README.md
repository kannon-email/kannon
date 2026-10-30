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
10 MiB bounded strict JSON decoding, and `WriteProblem` for sanitized RFC 9457
errors. Every response receives a server-generated X-Request-ID. Never render
infrastructure errors or secrets. Domain listing uses an exclusive ascending
name boundary and one lookahead row, with a version/collection/order-bound opaque
cursor; changing page size is allowed. Other collections should define their own
query-bound cursor payloads, without a generic pagination framework.

HTTP acceptance tests use the complete adapter. Run `go test -race -short
./pkg/api/rest` without Docker; omit `-short` to run the PostgreSQL Domain journey.
Application authorization matrices remain in `internal/domains`.
