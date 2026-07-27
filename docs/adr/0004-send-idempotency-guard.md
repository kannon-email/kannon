# ADR 0004: The SMTPSender guards its own sends, in NATS rather than in the Pool

## Status

Accepted (2026-07-27).

## Context

The **SMTPSender** acknowledges a JetStream message only once the SMTP
transaction has returned — that is what makes a crashed worker's Envelope get
picked up by another one. The flip side is that any redelivery of a message
whose send is still in flight, or already finished, produces a *second* SMTP
transaction: the recipient receives the same email twice.

Issue #425 is that failure in production. A consumer-wide `BackOff` curve gave
every consumer a one-second first ack deadline (`BackOff` overrides `AckWait`),
so essentially every send was redelivered, and roughly one delivery in six was
duplicated. Giving the sending consumer a deadline that outlasts an SMTP
transaction removes the cause, but not the class: a pod restart mid-send, a
relay stuck past the whole backoff curve, or any of the `MaxDeliver` retries
still hands the same stored Envelope to a second worker.

So the send needs to be idempotent. Three designs were on the table.

1. **Check the Pool row before sending.** The Delivery's Pool row is deleted on
   a terminal outcome, so a redelivery arriving after the Delivered stat has
   been processed would find nothing and could be skipped.
2. **Claim the Pool row from the sender**, flipping `sending` → `sent` with a
   conditional `UPDATE` before the transaction, so two workers holding the same
   Envelope cannot both send.
3. **Claim the stored message in NATS**, in a JetStream key/value bucket keyed
   by the stream sequence of the message being delivered.

## Decision

(3). Before handing an Envelope to SMTP, the sender takes an atomic `Create` on
a key derived from the message's stream sequence; the delivery that loses the
race is acknowledged and dropped.

### Why not the Pool

(1) only catches the subset of redeliveries that arrive after the Delivered
stat has been consumed. The duplicates measured in #425 were 1s and 5s apart —
the second send starts while the first is still talking to the relay, with the
row still in `sending` and indistinguishable from a first attempt. A check that
misses the observed failure is not a fix.

(2) closes that race, but at two prices. It puts the Pool's lifecycle in two
hands: today the Dispatcher owns every transition and the sender owns none, and
the Pool's states are explicitly implementation detail (`CONTEXT.md`, *Internal
mechanics*). And it gives the SMTPSender a database dependency it does not have
— `--run-sender` runs today with no `database_url` at all, so this would be a
breaking change for split deployments, in the component that must keep running
when Postgres is unavailable.

### Why the stream sequence is the right key

The unit to deduplicate is *one stored message*, not one Delivery. Every
redelivery of a message carries the same stream sequence, so they collapse onto
one key; a Delivery legitimately dispatched again after a transient failure is
a new message with a new sequence, and is sent again as it should be. Keying on
the Delivery instead (the email ID) would need a time window to distinguish the
two, and no window is correct — `Delivery.NextRetryAt()` is measured from the
Batch's *original* scheduled time, so a retry can be due immediately.

### Why not `Nats-Msg-Id` publish deduplication

The `Duplicates` window on a stream collapses duplicate *publishes*. Redelivery
to a consumer is not a publish, so it does not go through that path at all: a
message id on `kannon.sending` would not have prevented a single one of the
duplicate emails in #425. It remains worth adding against duplicate dispatch,
which is a different failure and needs headers on the publisher interface.

## Consequences

- The sender keeps its shape: NATS in, SMTP out, NATS out. No new dependency,
  no schema change, no migration.
- The guard holds across the worker pool of one process, across replicas, and
  across restarts, which is when redelivery actually happens.
- One more JetStream asset to operate: the `kannon-sent-envelopes` bucket, one
  small entry per Envelope, expiring after an hour.
- The guard **fails open**. If the bucket cannot be reached the send proceeds:
  a duplicate is a smaller failure than a batch that never leaves.
- A delivery that gets past the claim and then fails — a worker dying
  mid-transaction, or a successful send whose outcome stat cannot be published
  — is not retried by its own redelivery any more, because the claim it left
  behind stops it. The Pool row is stranded in `sending` by those failures with
  or without the guard, since no stat reaches the Dispatcher either way; what
  the guard changes is that the email is not sent a second time on the way
  there. That is the trade the issue asks for: a rare stranded Delivery instead
  of a systematic duplicate.
