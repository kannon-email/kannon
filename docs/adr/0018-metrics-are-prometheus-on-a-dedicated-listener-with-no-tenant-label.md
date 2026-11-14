# ADR 0018: Metrics are Prometheus, on a dedicated listener per process, with no tenant label

## Status

Accepted (2026-11-14).

## Context

Kannon had no operational metrics. What an operator could chart came from the Stats API and the
`aggregated_stats` table, which answer a tenant's questions — how did my Batches do — and not an
operator's: is the API answering, how fast, and is mail flowing through the platform right now.

Kannon is one binary started as several workloads (ADR 0011). The API serves Connect on `api.port`
and REST on `api.rest_port`, the tracker serves HTTP on `tracker.port`, and the sender, dispatcher,
validator, stats and audit workers open no HTTP listener at all. Whatever exposes metrics has to
cover all of them.

## Decision

**Prometheus text format**, through `github.com/prometheus/client_golang`. Every scraper an operator
is likely to run reads it natively, and it costs a pull endpoint rather than a push pipeline.

**One registry per process**, owned by `x/container` (`Container.Metrics()`), with the Go and
process collectors on it. Each Component registers its own collectors there when it is built, and
counts what it decides itself, where the decision is taken. A Component built twice on one
container reuses the collectors already registered (`metrics.Register`) rather than panicking.

**A dedicated listener** serving `GET /metrics`, configured by a top-level section:

```yaml
metrics:
  enabled: true   # the default
  port: 9090      # the default
```

It is not a Component and has no entry under `services`: it runs in every process that runs at
least one Component, and `metrics.enabled: false` opens no listener at all. It shuts down through
`container.ServeHTTP` like every other listener. A `metrics.port` equal to the port of a listener
the same process opens — `api.port`, `api.rest_port` or `tracker.port` — refuses the boot, naming
both keys.

**No `domain` label, nor any other unbounded one.** A label is allowed only if its values are a
bounded enum written down in the code: the API, the procedure or route pattern, an outcome. Never a
Domain, a Batch, a Delivery, an address or a raw URL path. The REST `operation` label is the pattern
the ServeMux matched (`GET /domains/{domain}`), and a request no route matches is
`operation="unmatched"`.

**Pool statuses are exposed, as an accepted exception.** CONTEXT.md treats the Pool's statuses as
implementation detail, but the backlog in each is what an operator needs to see a stuck worker, so
`kannon_pool_deliveries{status}` reports them: `to_validate`, `validating`, `scheduled` and
`sending`, the bounded `pool.Status` enum, each on every scrape. They may change between releases,
and the metric's help text says so.

## Why not the Connect or REST ports

Those ports are meant to be published — the REST one to the internet. Metrics on them would either
be public or need an authentication story of their own, and a route on a published listener is one
HTTPRoute rule away from being exposed by accident. Only the API process has them, too: the workers
have no HTTP listener at all, so a dedicated listener is the only option covering every workload,
and once it exists the API process might as well use it the same way.

## Why not OpenTelemetry

OpenTelemetry metrics would add an SDK, an exporter and, in practice, a collector to run, to arrive
at the same Prometheus series the scraper reads directly. Kannon has no tracing today, and nothing
here forecloses adding it later; the metrics need none of that machinery.

## Why no `domain` label

A Domain is a tenant, and an open-source installation can have thousands. Every label multiplies the
series of every metric carrying it, so a per-Domain label makes the cost of monitoring grow with the
customer base, and lets one tenant's traffic shape decide how much the operator's Prometheus stores.
Per-Domain figures already exist where they belong — `aggregated_stats` and the Stats API — and are
read by the tenant they describe. Metrics are the operator's signal about the platform as a whole.

## Consequences

Every process opens port 9090 by default. An installation where that port is taken sets
`metrics.port`, or turns metrics off. A scraper has to be pointed at each workload's metrics port,
which is deployment configuration rather than Kannon's.

Counters are an operational signal and not a record. A JetStream redelivery can count one decision
twice; the Stats API stays the record of what happened to a Batch.
