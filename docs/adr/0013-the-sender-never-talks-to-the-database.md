# ADR 0013: The sender never talks to the database

## Status

Accepted (2026-08-12). Promotes to a rule something
[ADR 0004](./0004-send-idempotency-guard.md) relied on while deciding where the
idempotency guard lives, and which nothing until now stated on its own.

## Context

The **SMTPSender** is the component that turns an Envelope into an SMTP
transaction. Its shape in the code is NATS in, SMTP out, NATS out: it consumes
`kannon.sending`, claims the message in the `kannon-sent-envelopes` key/value
bucket, hands the body to a relay, and publishes what happened on
`kannon.stats.*`. It opens no database connection and issues no query.

Nothing said so, though, and one thing said the opposite: the architecture
diagram in `ARCHITECTURE.md` drew an edge from SMTPSender to PostgreSQL beside
the ones for the API, the Dispatcher, the Validator and Stats. A dependency
that appears in the diagram is a dependency the next change designs against —
and the cheapest version of most send-time features is a query.

ADR 0004 already turned one of those down. Claiming the Pool row from the sender
was the natural way to make a send idempotent, and part of why it was rejected
is that it "gives the SMTPSender a database dependency it does not have […] in
the component that must keep running when Postgres is unavailable". That
sentence was reasoning inside a decision about deduplication. It is really a
constraint about deployment, and it should be able to be cited without citing
the guard.

The constraint earns its keep in three places.

**It is the component that scales on its own axis.** One binary runs every
component and a Deployment says which ones it starts (ADR 0011, ADR 0012). What
sizes a sender pod is outbound volume — how many SMTP transactions can be in
flight, which is `sender.max_jobs` times replicas — and that number is set by
relays and recipients, not by database capacity. A sender that held a pool would
spend Postgres connections, the scarcest thing in the installation, in the one
component whose replica count is expected to move with traffic and to be the
largest.

**It is the component that has to survive the database.** Mail already accepted
is already on `kannon.sending`, and the outcome of sending it is a stat that the
stream holds for 24 hours. So a database outage costs new submissions and
delays the Pool bookkeeping, while the mail Kannon has already promised to send
still goes out. That property exists only as long as the send path touches
nothing but NATS.

**It is the component with the least reason to hold a credential.** A sender pod
that never queries needs no `database_url` at all, so a deployment that splits
the components hands the database password only to the processes that use it.

## Decision

The sender never talks to the database, and this is a constraint on future
changes rather than a description of today's code. `pkg/smtpsender` does not
import `internal/db`, does not ask the `Container` for `DB()` or `Queries()`,
and issues no query — directly or through a helper it reaches for.

`TestSenderNeverTalksToTheDatabase` enforces exactly that, at the boundary where
it is checkable: the imports of the package's own files, and what it asks of the
container. It is deliberately not an assertion about the whole import graph.
Kannon is one binary and the `Container` is shared by every component, so the
pgx driver is linked into every process, sender-only ones included; the rule is
about what the sender *does*, and linking a driver it never calls costs nothing.

## Consequences

Whatever a send needs has to be on the Envelope. A future feature that wants
per-Domain state at send time — a per-Domain relay, a suppression list, a
throttle — puts it there from the Dispatcher, which does hold the database, or
in a JetStream key/value bucket the sender can read. It does not fetch it. That
is a real cost: a decision that would have been one `SELECT` becomes a change to
the Envelope, to the Dispatcher, and to the proto.

Whatever a send learns has to leave as a stat. The sender writes no outcome and
owns no Pool transition; it publishes `kannon.stats.delivered`, `.bounced` or
`.error`, and the Dispatcher turns that into the row change. This is why the
sender can be wrong about nothing but the send itself.

The idempotency guard stays where ADR 0004 put it. The reason to prefer a NATS
key/value claim over a conditional `UPDATE` is now a rule and not a preference,
and it applies the same way to whatever needs to be deduplicated next.

A sender-only process runs with no `database_url`. One wrinkle belongs to the
config file rather than to the code: the root configuration is resolved for
every process (ADR 0011), so a shared file declaring `database_url:
env://KANNON_DATABASE_URL` makes that variable required of a sender pod too, and
the pod stops at boot without it. Writing the reference with an empty fallback —
`env://KANNON_DATABASE_URL:-` — is what lets one file describe an installation
in which the sender pods carry no database credential.
