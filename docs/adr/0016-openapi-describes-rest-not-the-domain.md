# OpenAPI describes REST, not the domain

Accepted for the new REST implementation; implementation is pending.

OpenAPI is the versioned source contract for REST and drives wire types, HTTP
handler interfaces and documentation. Generated types belong to the REST adapter,
not to domain models, application operation inputs/results or storage schemas.
This extends ADR 0012's decision about protobuf to the second transport and accepts
explicit mapping work so changing a wire contract does not redefine the domain.

REST and legacy gRPC adapters translate into application operations using types
owned by those operations. Authorization, intake rules, Batch content snapshots,
atomic persistence and idempotency live behind those interfaces; HTTP decoding,
query/header syntax, status codes and Problem Details stay in the REST adapter.
In particular, idempotency retains an application creation receipt, not a serialized
HTTP response or inline rejection details, and an HTTP handler does not become the only implementation of a
business rule. Legacy semantic differences must be handled explicitly.

Domain/application modules must not import REST-generated packages or HTTP/Connect
request and response types. Adapter and persistence dependencies point toward
application interfaces. Import checks and application-level tests should enforce
this separation in the implementation.

Reuse the existing application modules and add the missing Batch creation
operation. Separation does not require per-endpoint factories, command buses,
pass-through layers or mechanically duplicated models. Batch idempotency is a
specific transactional operation with a minimal receipt; cursor pagination uses
fixed ordering and row positions without persisted sessions or snapshots.
