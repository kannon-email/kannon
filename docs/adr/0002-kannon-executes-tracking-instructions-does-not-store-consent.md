# ADR 0002: Kannon executes tracking instructions; it does not store consent

## Status

Accepted (2026-07-26).

## Context

The Italian DPA's Guidelines of 17 April 2026 on tracking pixels in email
(Provvedimento n. 284, in force 29 April 2026, six months to comply) place
email open and click tracking under Art. 122 of the Italian Privacy Code —
prior informed consent, granular withdrawal, and the ability for a recipient
to keep receiving email *without* being tracked.

Consent under that framework belongs to the **recipient**, not to the Batch.
Within a single Batch of N Recipients, some may have consented and some may
not. That raises the question of where the consent state lives.

Two designs were on the table:

1. **Kannon owns a consent store** — a table keyed on `(domain, email)`
   recording each recipient's tracking consent, consulted at send time.
2. **Kannon executes an instruction** — consent is carried per-Recipient in
   the API call, honoured, and not retained as state of its own.

## Decision

Kannon is an **executor**. The Tracking Policy arrives with each send request
and Kannon applies it. Kannon holds no record of consent, and no code path
consults one.

### Why this matches the roles

The Domain (Kannon's user) is the controller; Kannon is a processor under
Art. 28. A processor's obligation is to make selective disabling
*technically possible* and to act on documented instructions — not to
custody the consent. Consent belongs where it is collected, next to the
context that makes it valid: when it was given, through which form, against
which privacy notice.

### Why a partial store is worse than none

A `consents` table without provenance — timestamp, notice version, method of
collection, proof, expiry, audit trail — would record *that* a flag is set
but not *how* it came to be set. In an inspection that is a liability, not
an asset: it looks like a consent register and cannot function as one.
Building the real thing means building a preference centre, an identity
model for the data subject, and an audit log. That is a product, and it
duplicates what the controller's CRM already has.

Kannon has none of the surfaces such a store would need. It knows a
recipient only as a string in an API payload.

## Consequences

- Every send request must carry the Tracking Policy. A caller that omits it
  gets whatever the Domain permits, which starts at a level the operator set
  deliberately (see ADR 0003).
- Kannon cannot detect a caller that lies about consent. That is the normal
  controller/processor split: the processor acts on documented instructions
  and the controller answers for them.
- There is no state in Kannon on which to hang a recipient-facing withdrawal
  mechanism. When unsubscribe / preference-centre work lands, it will need
  its own decision — this ADR does not pre-empt it, but any such feature
  must not quietly become the consent store rejected here.
- The Domain-level ceiling (ADR 0003) is what protects the operator from a
  buggy or compromised client, since Kannon has no independent record to
  check the instruction against.

## Rejected alternatives

- **A `consents` table keyed on `(domain, email)`.** Requires provenance and
  audit to be meaningful; without them it manufactures false assurance, and
  with them it is a separate product duplicating the controller's CRM.
- **Deriving consent from suppression/unsubscribe state.** Conflates two
  different decisions — "stop emailing me" and "keep emailing me but stop
  tracking me" — which the Guidelines explicitly require to be separable.
