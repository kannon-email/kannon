# ADR 0003: Tracking Policy — ceiling semantics, defaults, and intake resolution

## Status

Accepted (2026-07-26).

## Context

Until now Kannon tracked unconditionally. `envelope.Builder.preparedHTML`
rewrote every link and injected an open pixel on every Delivery, with no
flag anywhere in the API, the schema, or the config. Every open and click
was recorded per-recipient together with the IP address and user agent.

ADR 0002 settles that Kannon executes a Tracking Policy rather than owning
consent. This ADR settles how that Policy gets its effective value, and what
value existing installations wake up to.

`CONTEXT.md` defines **Tracking Mode** as an ordered scale — Off,
Anonymous, Pseudonymous, Identified, Full — and **Tracking Policy** as a
pair of Modes, one for opens and one for links.

## Decision

### A lower level may only restrict, never widen

The Policy is stated at three levels — Domain, Batch, Recipient — and the
effective Policy is the **most restrictive** of the three. A level that
states nothing imposes no restriction of its own.

The alternative was plain override, where the nearest statement wins. The
two rules differ in exactly one case: `Domain=full, Recipient=off` resolves
to `off` under both, but `Domain=off, Recipient=full` resolves to `full`
under override and to `off` here.

That single case is the point. Under ADR 0002 Kannon has no independent
record to validate an instruction against, so the Domain setting is the only
guarantee an operator has. Making it a ceiling means a buggy, careless, or
compromised API client cannot escalate tracking beyond what the operator
configured. Consent can only ever narrow what is permitted — a recipient
consenting does not create a right to collect more than the operator allows.

Because the scale is ordered, this is a rank comparison and not a special
case. The rank function is explicit Go code, **not** the protobuf enum
number, so that a future rung can be inserted mid-scale without renumbering
the wire.

### Exceeding the ceiling is an error, not a silent clamp

A Batch asking for more than its Domain allows fails the call. A Recipient
asking for more than its Domain allows is **Rejected** with a reason,
surfaced through the per-recipient rejection channel proposed in #364.

Silently clamping would make the ceiling indistinguishable from a bug: a
caller passing `full` and observing nothing has no way to tell policy from
breakage. Since the Policy is resolved at intake (below), the API is exactly
where the caller can still act on the information. An instruction above the
ceiling is a contradictory instruction from the controller, and a processor
should say so rather than quietly reinterpret it.

### Resolved at intake and frozen on the Delivery

The cascade is resolved when the Batch is created, and the concrete result is
written onto each Delivery. `sending_pool_emails.tracking` therefore never
holds an unstated Mode. The Builder reads the frozen value; it does not
re-resolve.

- **Consistency.** Resolving at dispatch would let an operator changing the
  Domain policy mid-flight produce a Batch that is half tracked and half not,
  with nothing recording the split.
- **Auditability.** The Delivery records the Policy that actually governed
  it. Resolved at dispatch, that fact is unrecoverable after the fact — only
  the *current* configuration is observable, and it may have changed.
- **Consent semantics.** The instruction that counts is the one in force when
  the controller asked for the send.

`messages.tracking` is retained as provenance. It is strictly redundant, since
the value is already folded into every Delivery, and is kept deliberately for
debugging.

### Defaults: `identified`, for both existing and new Domains

One statement does both, since PostgreSQL 11+ backfills existing rows from
the default without rewriting the table:

```sql
ALTER TABLE domains ADD COLUMN tracking jsonb NOT NULL
  DEFAULT '{"opens":"identified","links":"identified"}'::jsonb;
```

For **existing** installations this is a *reduction*. Today's behaviour is
effectively `full`; `identified` keeps per-recipient attribution and stops
retaining IP and user agent — the fields with the highest risk and the
lowest demonstrated use. Nobody loses a dashboard, and nothing goes dark.
`off` was rejected for existing Domains precisely because it would: an
operator upgrading for an unrelated patch would find their metrics silently
at zero.

For **new** Domains `identified` means a fresh install attributes opens and
clicks by recipient out of the box. This is the weaker half of the decision
and is taken **for now**: the Guidelines' "disabled by default" line is
aimed at platforms that cannot disable selectively, which this one now can,
and the consent obligation sits with the controller regardless. It is the
most likely part of this ADR to be revisited.

On write, an unstated Mode is normalised to `off` before it is persisted on
a Domain, so `""` never appears at rest there. After intake resolution the
same is true of `sending_pool_emails`. `messages.tracking` is the only place
an unstated Mode can be stored.

## Consequences

- **No kill switch.** Flipping a Domain to `off` does not affect Deliveries
  already queued — they carry their frozen Policy. Stopping in-flight
  tracking requires an explicit queue-rewrite operation, which does not
  exist and is not built here.
- Existing API clients are unaffected: they state nothing, therefore impose
  no restriction, therefore resolve to the Domain value.
- `anonymous` becomes materially cheaper than the upper rungs. Its token
  carries no recipient identity, so it is shared across a Batch — one
  signature per Batch for the pixel and one per `(Batch, url)` for links,
  instead of one RSA-4096 signature per link per Delivery.
- The Tracker must learn the resolved Mode from the **JWT claims**, not from
  the database: Pool rows are deleted on terminal outcomes, so by the time an
  open arrives there is no Delivery row to consult. The token is signed, so
  the Mode cannot be tampered with by the recipient.
- `pkg/stats` needs the resolved Mode on the stat event. `handleStats` must
  skip the per-recipient row under `anonymous` while
  `handleAggregatedStats` continues to count — the two are already
  independent NATS consumers, so the aggregate path is unchanged.
- **Under `anonymous`, counter inflation becomes undetectable.** The tracker
  endpoints have no deduplication or rate limiting, so replaying a captured
  tracking URL inflates a Domain's counters. Under `identified` that at least
  leaves anomalous repeated rows against one recipient; under `anonymous` one
  captured URL is shared by the whole Batch and only the aggregate counter
  moves, with nothing retained that could correlate the requests. Accepted as
  inherent: the property that makes the Mode defensible — retaining nothing
  that isolates one Recipient — is the same property that removes the signal.
  Anyone relying on `anonymous` figures for anything but trend should know
  they are unauthenticated counts.
- **A Tracking Policy is only as strong as access to the Admin API.** The
  Domain ceiling is a security control, but `SetTrackingPolicy` sits on the
  same unauthenticated surface as the rest of the Admin API, on the same
  listener as the Mailer API, and nothing records who changed a Policy or
  when. This ADR's claim that the Domain setting is the operator's one
  guarantee holds only where that surface is not reachable by tenants. Fixing
  it is out of scope here; `docs/UPGRADING.md` states the deployment
  requirement.

## Rejected alternatives

- **Plain override (nearest level wins).** Leaves the Domain setting with no
  enforcement power, which under ADR 0002 is the only guarantee an operator
  has.
- **Silent clamp when the ceiling is exceeded.** Indistinguishable from a
  bug from the caller's side.
- **Resolving the cascade at dispatch.** Would gain an in-flight kill switch
  at the cost of intra-Batch consistency and of any durable record of which
  Policy actually applied.
- **Backfilling existing Domains to `off`.** A silent metrics blackout on
  upgrade.
- **Backfilling existing Domains to `full`.** Ships the exact default the
  Guidelines object to, to the entire installed base, in the change made to
  address them.
- **Two columns instead of one `jsonb`.** Rejected in favour of consistency
  with the existing `messages.headers` / `sending_pool_emails.fields`
  pattern and to allow the Policy shape to grow without a migration.
- **A separate `domain_tracking_policy` table.** Adds a fourth join to
  `GetSendingData`, which already joins `domains`, and reintroduces a
  missing-row case that `NOT NULL DEFAULT` makes unrepresentable.
