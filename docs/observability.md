# Observability

Every Kannon process that runs at least one Component serves Prometheus metrics on `GET /metrics`,
on a listener of its own configured by the top-level `metrics` section (`enabled`, default `true`;
`port`, default `9090`). It is never served on the Connect or REST ports. See
[ADR 0018](adr/0018-metrics-are-prometheus-on-a-dedicated-listener-with-no-tenant-label.md).

Each process exposes only the metrics of the Components it runs, plus the standard Go and process
collectors (`go_*`, `process_*`). A counter series appears once it has been incremented at least once.

## Rules

- **No `domain` label**, nor any other unbounded one: no Batch, Delivery, address or raw URL path.
  Per-Domain figures live in the Stats API. Every label below is a bounded enum.
- **Counted where the decision is taken**, by the Component that takes it. A JetStream redelivery can
  count a decision twice: metrics are an operational signal, the Stats API is the record.
- A label documented as "empty otherwise" is exposed as `""`, which Prometheus treats as absent.

## Metrics

### API calls (API process)

| Metric | Type | Labels |
|---|---|---|
| `kannon_api_requests_total` | counter | `api` = `connect` \| `rest`; `operation` = the Connect procedure (`/pkg.kannon.admin.apiv1.Api/GetDomains`) or the REST route pattern (`GET /domains/{domain}`), `unmatched` when no route matched; `code` = the Connect code (`ok`, `invalid_argument`, ...) for Connect, the status class (`2xx`, `4xx`, `5xx`) for REST |
| `kannon_api_request_duration_seconds` | histogram | `api`, `operation` |

### Mail flow

| Metric | Type | Labels | Counted by |
|---|---|---|---|
| `kannon_batches_total` | counter | `intake` = `mailer` \| `rest` | API (Intake), once per Batch persisted. An idempotent REST replay is not counted, nor a REST Batch refused because every Recipient was rejected. |
| `kannon_recipients_total` | counter | `intake` = `mailer` \| `rest`; `result` = `accepted` \| `rejected` | API (Intake), for every Recipient decided. A Recipient rejected at Intake has no Delivery, so it is not a Delivery outcome. The Recipients of a REST Batch refused because every one was rejected count as `rejected`. |
| `kannon_delivery_outcomes_total` | counter | `outcome` = `validated` \| `rejected` \| `delivered` \| `bounced` \| `failed`; `source` = `sync` \| `async` for `bounced`, empty otherwise; `permanent` = `true` \| `false` for `bounced` (the SMTP reply class: 5xx is permanent), empty otherwise | Validator (`validated`, `rejected`), SMTPSender (`delivered`, `bounced` `sync`), SMTP server (`bounced` `async`, from a DSN), Dispatcher (`failed`, Retry Budget spent with no answer) |
| `kannon_send_attempts_total` | counter | `result` = `delivered` \| `bounced` \| `errored` | SMTPSender, once per SMTP transaction. `errored` is a transient failure retried later: plumbing, never a Delivery outcome. |
| `kannon_smtp_send_duration_seconds` | histogram | `result` = `delivered` \| `bounced` \| `errored` | SMTPSender, the time of one SMTP transaction. Buckets from 50ms to 120s. |
| `kannon_dispatch_total` | counter | `result` = `ok` \| `build_error` \| `publish_error` | Dispatcher, once per dispatch of a claimed Delivery: built and published, failed to build the Envelope, or failed to publish it to NATS |
| `kannon_engagement_events_total` | counter | `kind` = `opened` \| `clicked` | Tracker, once per event published, under every Tracking Mode that publishes (all but `off`, `anonymous` included) |
| `kannon_pool_deliveries` | gauge | `status` = `to_validate` \| `validating` \| `scheduled` \| `sending`, every one on every scrape (0 when no Delivery holds it) | Dispatcher and Validator processes only, read with one `GROUP BY` on the Pool at scrape time, with a 2s timeout. A failed query yields no sample and a log line, not a failed scrape. A row in any other status is not counted. The statuses are internal mechanics, exposed for operators as an exception (ADR 0018), and may change between releases. |

## PromQL examples

Throughput, Deliveries delivered per second:

```promql
sum(rate(kannon_delivery_outcomes_total{outcome="delivered"}[5m]))
```

Bounce rate, the share of Deliveries ending as bounced among those that reached a relay's answer:

```promql
sum(rate(kannon_delivery_outcomes_total{outcome="bounced"}[1h]))
/
sum(rate(kannon_delivery_outcomes_total{outcome=~"delivered|bounced"}[1h]))
```

Backlog, Deliveries waiting in the Pool by status (`max` because the Dispatcher and the Validator can
both report the same Pool):

```promql
max by (status) (kannon_pool_deliveries)
```

Dispatch failures, a symptom of an unbuildable Batch or an unreachable NATS:

```promql
sum by (result) (rate(kannon_dispatch_total{result!="ok"}[5m]))
```

API error ratio:

```promql
sum(rate(kannon_api_requests_total{code=~"5xx|internal|unavailable|unknown"}[5m]))
/
sum(rate(kannon_api_requests_total[5m]))
```
