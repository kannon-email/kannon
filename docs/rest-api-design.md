# REST API design

Status: accepted. The user approved the consolidated contract in Q46 and the
subsequent simplification review (five roles and lean implementation constraints).
No REST implementation or database migration has been performed.

Implementation specification: [GitHub issue #476](https://github.com/kannon-email/kannon/issues/476),
published in English with the prd and ready-for-agent labels after confirmation
of the testing interfaces. A local copy is available in `docs/specs/rest-api-v1beta1.md`.

The machine-readable contract is `api/openapi/v1beta1.yaml` (OpenAPI 3.1.1).
It contains 21 operations across 13 paths and 48 schemas, and has passed structural
OpenAPI validation. It is approved but not implemented: `x-design-review` records
acceptance and the separate implementation follow-up.
The specification covers application resources and the public health endpoint.

## Existing interfaces

The codebase observations below reflect the design interview baseline at commit
c28d620. Recheck implementation progress against current main before starting a
ticket; later refactoring does not change the approved target contract.

The application API is currently exposed through Connect handlers, with protobuf
contracts. `pkg/api/api.go` mounts Admin, Mailer, Stats v1, Stats v2, and health.
There is no REST/OpenAPI contract in the current repository.

| Surface | Existing operations | Contract |
| --- | --- | --- |
| Domains | List, get, create, set Tracking Policy | `.proto/kannon/admin/apiv1/adminapiv1.proto` |
| Templates | Create, update, delete, get, list | Same Admin contract |
| API Keys | Create, list, get, deactivate | Same Admin contract |
| Mailer | SendHTML, SendTemplate | `.proto/kannon/mailer/apiv1/mailerapiv1.proto` |
| Stats v1 | Individual stat records and aggregates over a Domain/date range | `.proto/kannon/stats/apiv1/statsapiv1.proto` |
| Stats v2 | Aggregates over a Domain/date range | `.proto/kannon/stats/apiv2/statsapiv2.proto` |
| Health | Health RPC | `.proto/kannon/admin/apiv1/hz.proto` |
| Tracker | Open pixel `/o/`, click redirect `/c/` | `pkg/tracker/srv.go` |
| Inbound SMTP | Bounce/DSN intake | `pkg/smtp/` |

Admin and Stats use the configured Admin Token; Mailer authenticates a
Domain-bound API Key. Health is unauthenticated. Tracking uses signed tokens.

## Domain constraints already established

`CONTEXT.md` is the repository's canonical domain vocabulary. It remains the
source for definitions; this document records API design work, not a second glossary.

- A send request creates a Batch; accepted Recipients become Deliveries.
- The existing `message_id` identifies a Batch, not an individual Delivery.
- A Domain owns Templates, API Keys, Batches, and Deliveries.
- Inline HTML creates a Transient Template; curated Templates are Persistent.
- Intake may reject individual Recipients while accepting the others. The send
  response reports accepted/rejected counts and individual rejection reasons.
- Accepted for sending does not mean Delivered. Delivered means acceptance by
  the remote SMTP server, not inbox placement.
- Tracking Policy and One-Click Unsubscribe have different semantics and must
  retain them across transports.
- ADR 0012 (`0012-protobuf-is-a-wire-format-not-a-domain-model.md`) separates
  the domain from protobuf; a new transport need not copy protobuf field shapes.

## Gaps and design pressure

- No public Batch get/list or Delivery lifecycle API exists in the current RPC
  contracts. Exposing these is additional application behavior, not route mapping.
- Stats v1 detail and Stats v2 aggregates need a coherent REST vocabulary.
- Current pagination differs: Templates/Stats use skip/take, API Keys use
  limit/offset, and Domains have no pagination parameters.
- The legacy Sender shape uses email/alias; CONTEXT.md already notes that its
  meaning is closer to the email From field.
- Creating a retry-safe Batch needs an explicit client-facing idempotency
  contract; it must not be inferred from worker retry safeguards.
- Delivery rows are currently deleted when processing terminates (`internal/pool/pool.go`,
  `internal/db/pool.sql`). Durable post-delivery reads need new persisted history;
  remaining stat events cannot reproduce the full record.
- Batch rows do not currently retain creation time, intake counts or rejection
  details. Stable creation replay and useful Batch reads require additional data.
- Dispatch resolves the current Template body (`internal/db/pool.sql:GetSendingData`).
  Updating or deleting a referenced Template can affect queued Batches. The agreed
  REST contract requires an immutable content snapshot instead (ADR 0015).
- The current Delivery identity is constrained by unique `(email, message_id)`.
  Duplicate recipient addresses cannot become independent Deliveries without a
  data-model change. REST will reject subsequent duplicate occurrences explicitly.
- Persisted aggregates are Domain/hour/type counters, not per-Batch counters
  (`internal/db/aggregated_stats.sql`). The per-Batch wording in CONTEXT.md needs
  reconciliation; no per-Batch aggregate capability has been promised yet.
- API Keys already store hashes and reveal the secret only on creation; they have
  no persisted role selection today. All resolve to `sender`. The agreed selective
  credentials require storage and issuance changes.

## Decision tree

Confirmed scope: REST covers the whole application API, including administration,
sending, and statistics (user clarification). It is not a sending-only API.

Confirmed decisions:

1. REST becomes the primary application contract. Existing gRPC APIs will be
   deprecated, but will not be removed immediately. gRPC receives necessary fixes;
   new functionality goes to REST. No removal date is fixed.
2. The first REST design includes Batch reads and inspection of associated
   Deliveries, beyond the operations currently exposed. Cancellation and
   rescheduling are deferred from the beta.
3. A global administrator must be able to perform all application operations
   across Domains, including sending. Domain credentials must support sending,
   administration, and statistics within their Domain. Credentials can carry
   selected capabilities, including sending-only and statistics-only access, or
   full administration of their Domain. Existing keys remain sending-only.
   Domain administrators can issue credentials within their own authority;
   issuance cannot confer powers beyond the issuer's or access to other Domains.
4. The administrator can act for any Domain, including sending, while retaining
   the authenticated administrator identity in audit records. No Domain key is
   impersonated or needed for the operation. This agrees with ADR 0008.
5. Global and Domain credentials use the same endpoints. Domain ownership is
   explicit in `/v1beta1/domains/{domain}/...`; there is no parallel `/admin` API.
6. `POST /v1beta1/domains/{domain}/batches` replaces the two send operations in REST.
   The payload uses `from: {email, name}` and a discriminated `content` object:
   either `{type: "html", html: ...}` or `{type: "template", template_id: ...}`.
   Recipients carry `email` and optional personalization `fields`. Scheduling,
   tracking, attachments and one-click unsubscribe remain options of this request.
7. Intake accepts valid Recipients while reporting individual refusals with their
   original request index. If every Recipient is refused, the request returns an
   error and creates no Batch. A request-wide error prevents the entire send.
   This deliberately differs from the legacy all-rejected success response.
8. Batch creation requires `Idempotency-Key`, scoped to the Domain. Repeating the
   same key and content returns the same Batch and original intake counts without
   creating another Batch; inline rejection details are not replayed. Reusing the
   key for different content is a conflict. The creation receipt is
   recoverable for at least 24 hours from the first successful creation, even for
   future-scheduled delivery. Replay does not retry rejected Recipients. After
   expiry, key reuse can create a new Batch. Every attempt is authorized anew.
   Failed-request and concurrent-request behavior is specified in decision 28.
9. Statistical events and aggregate counters are distinct API concepts. Aggregates
   follow Stats v2 semantics, including anonymous tracking and counters surviving
   detail retention. Counting available detail events need not reproduce aggregates.

10. All REST errors use RFC 9457 Problem Details (`application/problem+json`),
    extended with stable application `code`, `request_id`, and validation `errors`
    carrying `code`, `pointer` and `detail`. Human-readable text is not a client
    branching contract. Problem types use urn:kannon:problem:<code> identifiers.
    Partial Recipient rejections use the same codes and field references in the
    success representation. HTTP categories: 400 malformed request; 401 invalid
    or absent credential; 403 insufficient authority; 404 not found; 409 conflict;
    422 invalid content; 429 rate limit; 5xx service failure without internal detail.
11. Batch creation returns 201 Created and a Location pointing to the Batch only
    after the Batch and accepted Deliveries have been persisted atomically.
    Delivery remains asynchronous. A timeout after commit is recovered by an
    idempotent replay; partial persistence is not an acceptable success or failure.
12. All resource lists use `limit` and opaque `cursor`, returning
    `{items: [...], next_cursor: ...}` with null at the end and deterministic
    ordering per collection. Page numbers and mandatory total counts are excluded;
    dashboard counts, where needed, are explicit capabilities.
13. A Batch freezes the Template content at creation. Later Template updates or
    deletion cannot alter queued sends. The Template reference remains provenance.
14. Deliveries have stable public identifiers and remain queryable after worker
    processing terminates, until the agreed history retention deadline.
    The history includes recipient, times and outcome. Opens/clicks are separate
    events; a later asynchronous bounce can update a previously Delivered outcome.
15. Within a Batch, the first occurrence of a recipient address is retained for
    intake processing; subsequent duplicates are rejected as `duplicate_recipient`
    with their request index, even when personalization differs. Two intended sends
    to the same address require separate Batches. Address comparison rules remain
    defined in decision 40; local parts are not case-folded.
16. REST uses `Authorization: Bearer <credential>` for both Admin Token and Domain
    API Keys, with Domain supplied by the URL. Legacy gRPC authentication remains
    unchanged during transition.
17. The initial REST role catalogue is accepted: `admin` (all operations under its
    Anchor), `sender` (create Batches), `batch-reader` (read/list Batches and
    Deliveries including recipients), `analyst` (individual events and aggregates),
    and `metrics-reader` (aggregates only). These five roles compose on one
    credential. Template and credential management are admin-only in the beta;
    template-editor and key-manager are deferred. Existing
    `sender` keys do not gain read access. `admin` at a Domain is the full local
    administrator; the same role at the root is global.
18. Batch/Delivery history retention is fixed at 30 days from Batch processing
    completion in the first version, not configurable. A Batch with pending or
    scheduled Deliveries is not expired. Opens/clicks do not restart the clock.
    The history expiration is exposed to clients. Stats event retention remains
    separate; the user did not approve changing that setting.
19. API Key roles are fixed at creation. The plaintext secret is shown only once;
    reads never return it. Changing authority or rotating the secret requires a
    new key and revocation of the old one. Revocation uses
    `PATCH /v1beta1/domains/{domain}/api-keys/{key_id}` with `{status: "revoked"}`;
    repeated revocation is idempotent, reactivation is forbidden, and the key's
    record remains readable.
20. The initial REST prefix is `/v1beta1/`, replacing the initially proposed `/v1/`.
    Domain operations are create/list/get and Tracking Policy update. Domain names
    are immutable. Domain deletion, DNS verification and DKIM rotation are outside
    the first release. Template operations are create/list/get/update/delete;
    listings expose Persistent Templates, and updates support title and/or HTML.
21. Batch operations are create/list/get, with list/get for nested Deliveries.
    Cancellation and rescheduling are deferred; clients cannot mutate Batch intent.
22. Statistics use `/stats/events` and `/stats/aggregated` under the Domain.
    Filters are query parameters. Events support time range, type and Batch filters
    and cursor pagination. Aggregates support time range and duration-based UTC
    grouping by event type, at Domain level only. No aggregate Batch filter is
    promised because current counters do not retain that dimension.
23. OpenAPI is the primary versioned REST contract, authored independently from
    protobuf. Types, HTTP handler interfaces and documentation are derived from it.
    The accepted specification family is OpenAPI 3.1; Go generator selection is
    pending and must not dictate the domain model.
24. Single-resource responses return the resource directly. Lists use items and
    next_cursor. JSON names use snake_case; a resource's own identifier is `id`,
    references use names such as `batch_id` and `template_id`. IDs are opaque
    strings and clients must not interpret their shape.
25. Stats queries require `from` (inclusive) and `to` (exclusive). Input timestamps
    include a timezone; responses use UTC. Multiple event types use repeated query
    parameters (`type=delivered&type=bounced`). List limit defaults to 100 with a
    maximum of 1000. Unknown parameters or invalid values are rejected explicitly.
    Aggregate grouping uses Go duration syntax, e.g. `group_by=1h` or `24h`, with
    a minimum of one hour, replacing the proposed hour/day enum. Exact durations
    and bucket alignment are specified in decision 27.
26. API implementation and domain logic remain separate. Generated OpenAPI DTOs,
    HTTP requests/responses, status codes, Problem Details and query syntax belong
    to the REST adapter. Domain/application modules own typed inputs, results,
    business validation, authorization, atomic creation, content snapshots and
    idempotency behavior. They do not import the REST adapter or generated types.
    This extends ADR 0012's transport independence to REST (ADR 0016).
27. Aggregate group_by durations must be whole-hour multiples of at least one
    hour. Buckets start at `from`, which must align to a UTC hour; `to - from`
    must be a multiple of the selected duration. For example 1h, 2h, 6h, 24h
    and 168h are valid; 30m and 1h30m are refused. No partial buckets are returned.
28. A request rejected before Batch creation leaves its idempotency key available
    for a corrected request. Successful creation, including partial acceptance,
    binds the key to the input and application result for the guaranteed window.
    Concurrent requests cannot both create a Batch; while one is in progress,
    another receives 409 with `idempotency_in_progress` and retry guidance.
    A response lost after commit is recovered by replay without duplicate sending.
29. Public Batch states are pending, processing and completed. Delivery states are
    pending, scheduled, sending, delivered, rejected, bounced and failed. Completed
    means no further attempts remain, not universal delivery. Batch reads expose
    Delivery state counts. Later bounces can update outcomes without restarting
    Batch processing or its retention clock; engagement does not change state.
30. Lists return summaries. Batch summaries contain identity, sender, subject,
    times, state and counts; details additionally expose frozen content, Tracking
    Policy, unsubscribe configuration and attachment metadata. Recipients are read
    through the paginated Delivery collection. Template lists omit HTML; detail
    reads include it. Attachment bytes are excluded from initial read endpoints.
    Batch completed_at and expires_at are null until processing completes.
31. Batch creation accepts at most 2,000 Recipients and a total HTTP body of
    10 MiB, including base64 attachment expansion. Excess recipients fail the
    request with 422, excess body size with 413. Neither is silently truncated.
32. `GET /v1beta1/health` is public: 200 when checks pass, 503 when not ready.
    Responses are minimal and do not disclose infrastructure/database errors.
33. Evolution within v1beta1 remains compatible. Breaking changes require a new
    API version. gRPC stays in maintenance, with necessary fixes and no removal
    date fixed; new features target REST.
34. Intake rejections are response-only: rejected recipient records/details must
    not be persisted, including indirectly in an idempotency response cache. No
    `/batches/{batch_id}/rejections` endpoint is introduced. Rejections after a
    Delivery exists are recorded in Delivery history and observable as status
    rejected, including through a status filter on the Delivery list. SMTP
    rejections remain Bounced according to the established outcome vocabulary.
    This clarification takes precedence over the earlier exact-result replay
    proposal wherever it would require storing inline rejections.
35. Idempotent replay returns the same Batch and original intake counts, but no
    inline rejection details. The caller must retain those from the initial
    response if needed; if that response is lost, the details cannot be recovered.
    No inline rejection record or serialized request/response containing rejected
    recipient data is persisted to support replay.
36. Stats events and counters retain their own retention independently of the
    30-day Batch/Delivery history. Events may retain original identifiers after
    the corresponding history expires; GET on expired Batch/Delivery returns 404.
37. Omitted scheduled_at means immediate sending. If provided, it must be in the
    future at first creation, otherwise 422. A valid idempotent replay does not
    reapply the future-date check after the scheduled time has passed.
38. Aggregate responses include empty buckets with zero counts, with at most 1,000
    buckets per request. Clients use a larger group_by or split longer ranges.
    Zero means no counted events, not proof that collection was operational.
39. REST personalization merges global_fields defaults with recipient fields taking
    precedence, and applies the result to both HTML and subject. The reserved email
    variable is the actual recipient address. Unresolved placeholders reject that
    Recipient inline. Legacy gRPC keeps its existing personalization behavior.
40. Recipient address comparison removes outer whitespace, lowercases only the
    domain part and preserves local-part case. No dot or plus-tag normalization is
    performed. Later duplicate occurrences are rejected after this normalization.
41. Duplicate attachment filenames fail the whole request with 422 and stable code
    duplicate_attachment_filename, rather than silently replacing an attachment.
42. API Key expires_at is optional; absent means no automatic expiration. An explicit
    expiry must be in the future. Existing api_keys records and secrets are retained
    in the same table: no replacement table, data migration, reissuance or backfill
    of existing keys. Additional role metadata may be introduced additively in that
    table; missing legacy role metadata resolves to sender on the existing Domain.
    This is a storage design constraint, not authorization to execute a migration.
43. REST carries X-Kannon-Attribution with the existing semantics and validation:
    a claim recorded for audit, never authority, requiring attribute on the target.
    Admin holds it; the other accepted roles do not. Authenticated credential
    identity is always retained. Existing UTF-8, control-character and 256-byte
    validation rules remain applicable.
44. Beta list ordering is fixed: Domains by name ascending; Templates and Deliveries
    by ID ascending; Batches/API Keys by creation time descending with ID as the
    tie-breaker; events by timestamp descending with ID as the tie-breaker;
    aggregate buckets chronologically ascending. Cursors bind Domain, collection
    and filters. Reuse with a different query fails with 400. Pagination is not a
    snapshot of an actively changing system.

### Contract acceptance

The consolidated OpenAPI contract, including concrete schema names and
representations, was approved in Q46. It uses 201 for initial Batch creation and
200 for replay; public error type identifiers are urn:kannon:problem:<code>, avoiding a
dependency on any particular hosted documentation URL. The exact machine-readable
code catalogue is embedded in x-error-codes; clients must tolerate future codes.
Timestamps use RFC 3339 with explicit timezone on input and UTC on output; clients
must not depend on a fixed fractional-second precision.

The subsequent simplification review narrowed the beta to five roles: admin,
sender, batch-reader, analyst and metrics-reader. Only admin can manage Templates
and credentials within its Anchor. There is no limited-role credential issuer
in this version; a Domain admin may issue supported roles only for its own Domain.
The existing authority model remains; no generic grant-delegation engine is needed.

Implementation follow-up is deliberately separate: choose a compatible Go
generator, implement application interfaces and persistence, enforce import rules,
test wire/domain behavior, and decide where to serve documentation. None of this
changes the approved requirement to preserve existing API Keys in api_keys.

### Intake rejection versus Delivery rejection

An intake-rejected Recipient never becomes a Delivery and its address, fields and
rejection details are not stored. The initial creation response explains the
refusal. A Validator rejection occurs after a Delivery was created: that Delivery
remains in the 30-day history with its rejected outcome. A remote SMTP refusal is
a bounce, not another meaning of rejected.

Exact replay of the original partial-acceptance response is deliberately excluded:
revalidating input against current state cannot guarantee the same reasons, and
storing those reasons would violate response-only intake rejection. The accepted
guarantee is the same Batch and original intake counts without inline details.
The contract maps initial creation to 201 BatchCreated and replay to 200 BatchReplay,
which omits rejected_recipients entirely rather than returning an empty array that
could be mistaken for no rejections. Mutable current state is read through GET.

### Implementation separation

- REST decodes and checks transport shape, authenticates a Bearer credential into
  a Principal, maps requests into application values and maps results/errors back
  to HTTP. Parsing `group_by` into a duration happens here; whether that duration
  can be answered exactly is an application rule shared by callers.
- Application operations own use-case orchestration and authorization. Batch
  creation is one operation owning acceptance, immutable content, persistence and
  idempotency. HTTP's Idempotency-Key is translated into a typed application input;
  the application stores a transport-independent creation receipt, not a cached
  HTTP body or inline rejection details.
- Domain models own invariants without OpenAPI, Connect or HTTP dependencies.
  Database adapters implement persistence interfaces; HTTP handlers do not execute
  SQL or implement worker lifecycle rules.
- Existing gRPC adapters can invoke the same application modules, mapping to their
  own wire contracts. Any legacy semantic differences need explicit compatibility
  handling rather than silently changing existing clients' behavior.
- Verification must exercise business rules through application interfaces and
  wire behavior through adapter/contract checks. An import restriction should
  enforce the dependency direction so this does not depend only on code review.

### Accepted implementation simplicity constraints

- Reuse existing domains, templates, apikeys and stats application modules. Add the
  missing Batch creation operation behind an application interface. The path is
  REST adapter to application operation to persistence; transport separation does
  not justify command buses, per-endpoint factories or extra pass-through layers.
- Keep generated HTTP types out of domain/application interfaces. Introduce other
  representations only where they own distinct semantics; do not mechanically
  duplicate every object at every layer.
- Delivery history is a simple persistent record updated by lifecycle transitions.
  Start with queries for Batch state counts rather than synchronized duplicate
  counters. Event sourcing and a generic projection framework are not required.
  Optimize only when observed workload justifies it. This does not change the
  existing Stats event pipeline or Domain aggregate counters.
- Implement idempotency specifically for Batch creation, with a unique constraint,
  transaction and minimal creation receipt. Do not introduce a generic middleware
  cache or reusable idempotency framework for hypothetical operations.
- Implement cursors from fixed ordering and the last row's position, bound to query
  context as agreed. No persistent pagination sessions or distributed snapshots.
- Preserve content snapshots, atomic Batch creation, idempotency, uniform errors
  and the distinction between inline and later rejections. These are required
  behavior, not candidates for simplification.

### RBAC implementation status

The authorization engine already exists in `internal/authz`: a Principal holds
Grants, each binding a Role to an Anchor; rules map Actions to resource kinds.
`Guard` enforces decisions in application services and records them for audit.
Attenuation narrows reach without changing identity. The engine is independent of
Connect and can be reused by REST.

Only `admin` and `sender` are currently registered in the executable catalogue.
The configured Admin Token resolves to root admin. Every stored API Key resolves
unconditionally to sender on its Domain (`internal/apikeys/principal.go`); role
selection is not persisted or accepted by the current key creation service.
Admin/Stats transport authentication currently accepts only the Admin Token;
Mailer uses Domain API Keys.

The newly agreed five-role catalogue, persisted credential role metadata, admin-only
issuance with Domain restrictions,
REST authentication adapter and Batch/Delivery read permissions are design work,
not implemented features. The beta checks admin authority on the target Domain
and permits only supported Domain-anchored roles; it does not introduce issuance
by limited-role credentials or a generic delegation subsystem.

Accepted resource path families:

```text
/v1beta1/domains
/v1beta1/domains/{domain}
/v1beta1/domains/{domain}/templates
/v1beta1/domains/{domain}/api-keys
/v1beta1/domains/{domain}/batches
/v1beta1/domains/{domain}/batches/{batch_id}/deliveries
/v1beta1/domains/{domain}/stats
```

### Accepted endpoint inventory

All paths below are relative to `/v1beta1`.

| Methods | Path | Meaning |
| --- | --- | --- |
| POST, GET | `/domains` | Create/list Domains; global admin |
| GET, PATCH | `/domains/{domain}` | Read Domain / update Tracking Policy |
| POST, GET | `/domains/{domain}/templates` | Create/list Persistent Templates |
| GET, PATCH, DELETE | `/domains/{domain}/templates/{template_id}` | Read/update/delete Template |
| POST, GET | `/domains/{domain}/api-keys` | Create/list credentials |
| GET, PATCH | `/domains/{domain}/api-keys/{key_id}` | Read credential / revoke |
| POST, GET | `/domains/{domain}/batches` | Create/list Batches |
| GET | `/domains/{domain}/batches/{batch_id}` | Read Batch |
| GET | `/domains/{domain}/batches/{batch_id}/deliveries` | List Deliveries |
| GET | `/domains/{domain}/batches/{batch_id}/deliveries/{delivery_id}` | Read Delivery |
| GET | `/domains/{domain}/stats/events` | Query detail events |
| GET | `/domains/{domain}/stats/aggregated` | Query Domain counters |
| GET | `/health` | Public minimal readiness result |

Design interview complete: the consolidated contract, concrete schema names,
replay representation and error identifiers are accepted. No handlers, generated
clients or database records are changed by this design work. Implementation
requires a separate task; contract approval does not start it automatically.
