# Coding Standards

Rules every change to Kannon follows. Vocabulary comes from [CONTEXT.md](CONTEXT.md); decisions behind the rules live in [docs/adr/](docs/adr/).

## Testing

A good test drives code through its public interface and asserts only observable behaviour. It fails because a behaviour changed, not because the CI runner was slow or the schema was refactored.

### The e2e suite is a black box

The suite in `e2e/` boots Kannon as a whole and talks to it only from the outside, the way a sender, a remote MX and a recipient do.

**Inputs** come only through:

- the REST API;
- a DSN delivered to the inbound SMTP server (the SMTPServer);
- HTTP requests to the Tracker.

**The outside world** is simulated only at the edges:

- a capturing Sender stands in for the remote MX;
- time is compressed with the backoff and Retry Window options ([ADR 0001](docs/adr/0001-configurable-delivery-backoff.md)).

**Assertions** read only public outputs:

- REST responses;
- Batch History;
- stats events read back through the API;
- the Envelope captured by the Sender stand-in;
- the `/metrics` endpoint.

An e2e test never reads or writes the database, never publishes to or subscribes on NATS subjects, never reads an in-process registry, and never depends on when a component happens to run. If a behaviour can only be observed or provoked through internal state, its test does not belong in the e2e suite.

**Temporary exception:** the audit scenarios read the `audit_records` table with SQL, because there is no API for reading Audit Records yet. The exception ends with [#515](https://github.com/kannon-email/kannon/issues/515), and no other scenario may rely on it.

### Where each kind of test belongs

| What the test pins | Where it lives |
| --- | --- |
| A domain rule of one type (Delivery, Batch, Retry Budget, Tracking Policy, …) | Unit test next to the type in `internal/`, no infrastructure |
| A query, a repository or a state transition stored in Postgres | Package integration test with real Postgres (`internal/tests.TestPostgresInit`) |
| A component's behaviour on internal state: Pool rows, attempt counters, a broken Template, the NATS subject a stat travels on | Package integration test of that component (`pkg/dispatcher`, `pkg/smtp`, `pkg/smtpsender`, …), with real Postgres or NATS where the behaviour depends on them |
| An HTTP or RPC surface: status codes, error shapes, authorization | Package test of that surface (`pkg/api`, `pkg/tracker`, …) |
| Kannon startup: configuration, refusals, listeners, readiness, shutdown | Tests of `cmd` and the startup module |
| A user-visible flow end to end: send, deliver, bounce, track, read stats | The e2e suite, under the black-box rule above |
