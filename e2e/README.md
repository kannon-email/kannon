# Kannon E2E Testing Suite

This directory contains comprehensive end-to-end tests for the Kannon email system.

## Overview

The e2e tests verify the complete email pipeline from API submission to delivery verification. Kannon is booted through the production startup module (`x/startup`) from a real config file written to a temporary directory, with every listener on port 0, and the suite waits on the instance's readiness signal:

- **PostgreSQL** via the shared test helper (`internal/tests`), prepared by the real migrations
- **NATS** in-process
- **All Kannon services** running in one instance (API, Sender, Dispatcher, Validator, Stats, Tracker, inbound SMTP, Audit)

The outbound Sender is replaced through a startup option by a capturing double, and the retry backoff and Retry Window are compressed the same way.

The suite is a black box (see [CODING_STANDARDS.md](../CODING_STANDARDS.md#the-e2e-suite-is-a-black-box)): scenarios drive Kannon only through REST, a DSN delivered to the inbound SMTP server and requests to the Tracker, and assert only on REST responses, Batch History, stats events, the captured Envelope and `/metrics`.

## Tenant driver

Scenarios are written against a small driver over a REST client generated from the published contract (`api/openapi/v1.yaml`) into `e2e/restclient`. Regenerate it with `go generate ./e2e/restclient`.

```go
tn := e.NewTenant(t)                       // Domain + API Key, provisioned with the Admin Token
b := tn.Send(t, tn.Batch(subject, html, To(rcpt, nil)))
tn.Await(t, rcpt, Delivered)               // a stats event of that outcome for that Recipient
tn.AwaitBatch(t, b, Completed)             // Batch History
tn.Holds(t, "never bounced", func(c *probe) { … })
e.MX.AwaitEnvelope(t, rcpt); e.MX.Attempts(rcpt)
e.DeliverDSN(t, returnPath, rcpt, 450, "…"); e.Track(t, host, path)
e.Metrics(c)                               // a scrape of the metrics listener
```

The capturing MX answers by the Recipient's local-part: `bounce.*` is refused with 550, `transient.x<N>.*` with 451 N times and then accepted, `deferred.*` with 451 on every attempt; anything else is accepted.

## Waiting policy

Every wait polls every 100ms for up to 30s, and every hold lasts 2s. Both scale with `KANNON_E2E_TIMEOUT_SCALE` (for example `KANNON_E2E_TIMEOUT_SCALE=3` on a slow runner). A failing evaluation, transient API errors included, is retried until the timeout; only the last evaluation's failures are reported. A hold fails on the first evaluation that finds its condition false; an evaluation whose read failed (a transport error or a 5xx, through `requireRead` and `requireStatus`) is retried instead.

## Layout

Scenarios are grouped by feature, one file each: `sending_test.go`, `bounce_test.go`, `tracking_test.go`, `intake_test.go`, `audit_test.go` and `metrics_test.go`. The metrics scenario runs first and alone, because it reads process-wide counters. The audit scenarios read the `audit_records` table with SQL, the documented exception until #515.
