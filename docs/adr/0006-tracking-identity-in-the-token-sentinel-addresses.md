# ADR 0006: Tracking identity travels in the token, as an address — sentinel addresses for the Modes that name nobody

## Status

Accepted (2026-08-02).

## Context

Issue #411 observed that every tracking URL carries the recipient address in a
decodable form: the open and click tokens are RS512-signed JWTs, a JWT payload
is readable base64, and the token *is* the URL. The parties that matter are not
the ones holding the message — a mailbox provider proxying the pixel already
has the address in `To:`, next to the token — but the ones that see the URL
*alone*: the Tracker's access logs and whatever ships them, a CDN or ingress in
front of it, the destination site via `Referer`. A mailbox leak costs one
address; a log leak costs the list. The issue weighed a server-side mapping
table, an encrypted claim, and a keyed hash.

Issue #424 tracked the other half: the **Pseudonymous** Tracking Mode is
defined and ranked in `CONTEXT.md` but reserved — rejected at every API
boundary — pending "the per-Batch identifier work", which was #411's decision
to make. The two issues were mutually blocking.

Constraints fixed by earlier decisions:

- Pool rows are deleted on terminal outcomes, so an engagement arriving months
  later has no Delivery row to resolve against. Whatever the Tracker needs must
  travel in the token, whose life is `tokenExpirePeriod` (three months) —
  `CONTEXT.md`: "The Mode reaches the Tracker as a signed claim in the token,
  not from a database lookup". That sentence names the invariant this ADR
  preserves: the Mode is fixed at send time and the request that carries it
  cannot widen it. It protects against the *Recipient*, not against the
  operator — the operator already owns every table the Policy lives in, and is
  the party responsible for the Mode in the first place (ADR 0002).
- ADR 0003: the Policy is stated per axis, resolved most-restrictive, frozen
  per Delivery.

## Decision

### The tokens stay stateless, and the JWT stays as it is

No identity table, no link catalog, no encrypted claim. The token envelope is
untouched: RS512 over RSA-4096 with `kid` rotation via `stats_keys`, the
channel-binding audiences, `exp` at three months, keys living twice that and
destroyed by the existing cleanup cycle.

The deciding argument is data minimisation as an architectural property. The
complete inventory of recipient addresses at rest in Kannon is:

1. `sending_pool_emails.email` — transient, deleted on the Delivery's terminal
   outcome;
2. `stats.email` — retention-bounded, only for events the Mode attributes.

Tracking adds no third store. Every stateful design evaluated (see Rejected
alternatives) would have created a new at-rest address collection — written on
the build hot path, sized in the tens of gigabytes for a large sender — in the
course of removing addresses from URLs. The audit story of the stateless
design is one sentence: addresses are stored to send, deleted, and stored again
only as retention-bounded statistics.

### The identity claim is decided by the Mode, and is always an address

| Mode | identity claim in the token |
| --- | --- |
| Identified, Full | the recipient address, as today |
| Pseudonymous | `<rand>@track.<sending-domain>` |
| Anonymous | `anonymous@track.<sending-domain>` — and never a stat row |

Because every identity is email-shaped, the Stats worker, the `stats` schema
(`email` is `NOT NULL`) and the API surface change not at all: a Pseudonymous
event flows through the existing per-recipient write path, and counting
distinct addresses keeps meaning distinct recipients. The discriminator is the
Mode, which every stat event already carries.

The pseudonym is **128 bits from `crypto/rand`, lowercase hex local part**
(email pipelines case-fold; a case-sensitive alphabet would let a lowercase in
transit merge pseudonyms), generated **once per Delivery in the Builder**, so
the pixel token and every link token of the Delivery carry the same value —
that intra-Delivery coherence is what makes events linkable within the Batch,
which is the rung's definition. It is regenerated for every Batch, stored
nowhere, and derived from nothing: no one, Kannon included, can link it across
Batches or back to the address. It is deliberately *not* an HMAC of the
address — a deterministic derivation would let any holder of the key confirm a
guessed (address, pseudonym) pairing, the exact weakness #411 charged against
its keyed-hash option.

`track.<sending-domain>` is a **reserved namespace**, and the reservation is
machine-checkable at the one place every token passes through: `statssec`
refuses to mint a Pseudonymous token whose identity does not end in
`@track.<domain>`, so a caller that passes the real address with a Pseudonymous
Mode is caught at the chokepoint rather than shipped. This preserves the
existing design property that no caller can name somebody by forgetting to
blank a field first.

The Anonymous sentinel exists for uniformity of the claim type and nothing
else. It is constant per domain, so the shared per-Batch token — one RSA
signature covering the whole Batch — survives. It is never written to the
`stats` table: the Mode-based skip in the Stats worker keeps `CONTEXT.md`'s
"leaves no per-Delivery record" true, and the "event names nobody yet is not
Anonymous" bug check becomes a sentinel-shape check.

### Identified means identified — #411 is answered by the scale, not by cryptography

Under Identified and Full the address stays in the URL, deliberately. Kannon
executes tracking instructions (ADR 0002): a Domain that attributes engagement
to recipients has stated so, owns the lawful basis for it, and its logs hold
what its instruction implies. The operator that must keep addresses out of
URLs and logs now has a rung of the existing scale that does exactly that —
**Pseudonymous** — instead of a cryptographic wrapper around Identified.
#411 is closed on these terms rather than left half-open.

### The axes stay independent

`opens` and `links` remain independently stated, per ADR 0003. A proposed
coupling (`opens >= links`, on the intuition that click tracking implies open
tracking) was rejected: see Rejected alternatives.

### Erasure, if it ever arrives, is a denylist

Kannon has no per-recipient erasure anywhere today (stat rows expire by
retention only). If an erasure instruction is ever added, the stateless design
answers it with a denylist checked by the Tracker — keyed as
`HMAC(key, address)` so the list of the erased is not itself a cleartext
address collection. State proportional to the exceptions, not to the sends.

## Consequences

- **#424 is unblocked and fully specified**; #411 is closed as answered by the
  Pseudonymous rung.
- **Zero schema, migration, or API changes.** The Stats worker and `stats`
  table are untouched.
- The implementation surface is the mint and the Tracker: the Builder
  generates the per-Delivery pseudonym; `statssec` validates the reserved
  namespace; `retained()` keeps the pseudonym while still dropping IP and user
  agent (Pseudonymous sits below Identified on the scale, but its events must
  carry the pseudonym or the rung records nothing); `toStatedMode` stops
  rejecting Pseudonymous.
- **Pseudonymous costs like Identified at the mint.** Per-recipient identity
  means per-Delivery tokens: the shared-Batch signature that Anonymous enjoys
  does not apply. The RSA-4096 bill this keeps (~4.2 ms per signature,
  measured) is today's bill, accepted knowingly; reducing it belongs to the
  dispatch-performance work (#380) and is orthogonal to this decision.
- **Addresses under Identified/Full remain readable in access logs.** That is
  the operator's stated instruction. Operators for whom that is not acceptable
  state Pseudonymous.
- The longest-lived address store in Kannon is now the `stats` table's default
  retention (one year). Any privacy audit should look there first.
- Legacy tokens minted before this change (blank identity under non-identifying
  Modes) keep verifying until they expire; the empty-identity path cannot be
  deleted for one `tokenExpirePeriod`.
- When #424 lands, `CONTEXT.md`'s Pseudonymous entry loses its *reserved* note
  and gains the sentinel format as part of the Mode's definition, and the
  `track.<domain>` reservation must be documented for operators (a real mailbox
  under that subdomain would collide with the sentinel space).

## Rejected alternatives

- **Identity rows in the database** (a row per (Batch, Recipient, axis),
  resolved by an opaque URL id, plus a per-Batch link catalog). Designed in
  full before rejection. It solves #411's letter and hands #424 its identifier
  for free, but it creates the third at-rest address store — two rows per
  Delivery written on the exact hot path the dispatch-performance work is
  trying to relieve, ~60 GB at steady state for a 1M/day sender — and its
  read-side advantage evaporated on inspection: the verify path already pays a
  per-hit key lookup today.
- **An encrypted identity claim** (AEAD value inside the existing JWT, or a
  compact AEAD token replacing it). Solves #411's letter with no new tables,
  and defeats URL-only observers. Rejected because the ciphertext travels in
  every sent message and every log line as a copy that can never be rotated:
  one key compromise retroactively unmasks every archived tracking URL held by
  anyone, and the key must live at least as long as the tokens. The chosen
  design achieves the same operator-facing goal — no address in the URL — by
  Mode selection, with no key custody at all.
- **A pseudonym derived as `HMAC(key, batch‖address)`.** Deterministic, hence
  confirmable by any key holder; randomness with no stored mapping is strictly
  stronger and simpler.
- **Coupling the axes (`opens >= links`).** Forbids the most defensible
  configuration in practice (opens off or anonymous, links identified — clicks
  are deliberate acts, opens are proxy-inflated noise, see #412), fails to
  eliminate mixed-axis Policies (the allowed direction still mixes), and is
  inexpressible in ADR 0003's per-axis most-restrictive cascade: a Recipient
  stating only `opens=off` under a permissive Domain would either be rejected
  for restricting, or have a restriction applied to an axis nobody stated.
- **Bare-domain sentinels** (`anonymous@<sending-domain>`). `anonymous@` can
  be a real mailbox; the reserved subdomain exists so the sentinel space and
  the deliverable space cannot collide.
- **Keeping Pseudonymous reserved.** The rung was blocked on #411's identifier
  decision; this ADR is that decision, and the rung is the answer to #411.
