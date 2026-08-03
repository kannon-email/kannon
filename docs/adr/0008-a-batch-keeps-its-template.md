# ADR 0008: A Batch keeps its Template

## Status

Accepted (2026-08-03).

## Context

`messages.template_id` named a Template with nothing holding it there. The only
foreign key touching the Pool pointed at `messages`, so `DeleteTemplate` was an
unconditional `DELETE`: afterwards `GetSendingData`'s three-way join of Batch ×
Template × Domain returns no rows, and no Envelope can be built for any pending
Delivery of any Batch that referenced that Template. This was reported inside
#378 and left out of its scope, and ADR 0007 recorded it as an upstream cause it
had bounded but not removed.

The bound is real and it stays: the **Retry Budget** now closes such Deliveries
as **Failed** within 24 hours instead of retrying them until their next attempt is
years away. But a bound is not a fix. Every Delivery of every affected Batch still
goes unsent, the sender still learns only that Kannon never got an answer, and
Kannon can no longer say what the body was supposed to be.

Two properties of the existing design decide the shape of the fix.

**Nothing copies the body.** `UpdateTemplate` rewrites the Template row in place
and the Dispatcher renders whatever is in it at the moment it builds each
Envelope. A Template edited halfway through a Batch changes what the rest of that
Batch sends — that is today's behaviour, and it is what makes a Template *part of*
every Batch that names it rather than an input consumed once at intake.

**The intake lookup is not a guard.** `sendTemplate` resolves the Template with
`FindByDomain` and then writes the Batch. A delete landing between the two leaves
exactly the orphan this ADR is about, and no amount of checking before the insert
can close that window.

## Decision

### The reference is a foreign key

`messages.template_id` references `templates (template_id)` with
`ON DELETE RESTRICT`. One constraint answers both directions at once: a Batch
cannot be written naming a Template that is not there, and a Template cannot be
deleted while a Batch names it. The race and the delete are the same fault seen
from two ends, and a key is the only place that holds both.

The key needs `templates.template_id` to be unique, which writes down an
invariant the code already relied on — every `:one` query keyed on that column
assumes it, and the value is generated as `template_<cuid2>@<fqdn>`. The old
non-unique index on it goes; the constraint's own index does that work.

It is installed `NOT VALID`. The rows that violate it are precisely the Batches
this bug has already orphaned, and a migration that refuses to install over them
would leave every *future* Batch unprotected on exactly the databases that have
already been bitten. `NOT VALID` skips the rows that exist and enforces the
constraint in full on every insert into `messages` and every delete from
`templates` from then on.

`messages (template_id)` gets an index. `ON DELETE RESTRICT` asks `messages`
"does any Batch still reference this Template?" on every Template delete, and
Postgres does not index a referencing column of its own accord.

### A Template a Batch references cannot be deleted, and that is the point

Because nothing copies the body, deleting a referenced Template is a request to
destroy part of Batches that already exist — pending ones, whose Deliveries then
have nothing to build from, and finished ones, whose stats would outlive the only
record of what they said. Kannon refuses, and says so as a refusal: the Admin API
answers `FailedPrecondition` (`templates.ErrTemplateInUse`), not `Internal`. The
request is well-formed, names the caller's own Template, and would succeed once
nothing references it; a caller must be able to tell that from a database being
down.

A `SendTemplate` that loses the race is refused the same way — `FailedPrecondition`
with the Template id — rather than being handed a Batch id for a Batch whose
Deliveries could never be built.

The narrower rule, "refuse only while some Delivery is still pending", was
available and is not taken. It would depend on the state of rows the caller cannot
see, it still destroys the body of Batches that have already been sent, and it
would have to be enforced at check-then-act distance from the delete. If deleting
used Templates becomes a real need, the answer is retention on the Batch — remove
the `messages` rows and the same key lets the Template go — not a weaker key.

## Consequences

- `DeleteTemplate` on a Template any Batch has ever referenced now fails with
  `FailedPrecondition`. This is a behaviour change to a public API: a caller that
  deletes Templates as routine cleanup will start seeing refusals, and the way
  through is to stop referencing the Template, not to retry.
- Transient Templates — one per `SendHTML`, one per per-call global-fields
  override — are now permanently undeletable, since each is referenced by the
  Batch it was made for. They already accumulated, and are not exposed for
  deletion at all; this makes their growth a retention question rather than a
  cleanup one.
- Batches orphaned before this migration stay orphaned. `NOT VALID` leaves them
  unchecked, and ADR 0007's Retry Budget remains the thing that closes their
  Deliveries out as **Failed**. That path stays live, and stays tested.
- Two fixtures that wrote a Batch naming a Template that was never created now
  seed one. The Retry Budget tests reach an unbuildable Envelope through the other
  cause ADR 0007 names — a Domain whose DKIM key cannot be parsed — which no key
  can prevent and which therefore keeps that chokepoint honest.
- `messages` carries one more index, paid for on every Batch insert. One row per
  Batch, against a Pool row per Recipient: the cheapest write in the schema.
- One dbmate migration.

## Rejected alternatives

- **A guard in `DeleteTemplate` refusing only Templates with pending Batches**, as
  #378 proposed. Check-then-act: it cannot close the intake race, which is the
  half of this fault that needs no operator to trigger it. It also lets the body
  of a fully-sent Batch be destroyed the moment its last Delivery is terminal,
  leaving stats that describe a message nobody can reconstruct.
- **`ON DELETE CASCADE`.** Deleting a Template would delete every Batch that used
  it, and the Pool rows of those Batches with it — Kannon destroying a sender's
  history in order to honour a cleanup request.
- **Snapshotting the body onto the Batch** (`messages.html`), making a Template a
  true input and deletion free of consequence. Rejected for now rather than on
  principle: it changes what an in-flight Batch sends when its Template is edited,
  which is current behaviour a caller may be relying on; it duplicates every body
  per Batch; and it needs a data migration for every Batch already pending. It is
  the right conversation to reopen if deleting used Templates turns out to matter.
- **Soft-deleting the Template** (`deleted_at`, hidden from listings, still
  joinable by pending Deliveries). The delete becomes a claim every query that
  reads a Template has to keep honouring, and it buys nothing at the end: a Batch
  created a moment before the soft delete still needs the row, so the row can
  still never actually go.
- **Validating the constraint over existing rows.** A migration that installs
  everywhere except on the databases the bug has already reached is the one place
  it must not fail.
