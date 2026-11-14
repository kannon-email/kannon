# The health check is internal, on a listener of its own

`GET /v1/health` was part of the published REST contract, so anyone on the internet
could poll whether the database and NATS were up. We remove it from the REST API and
both OpenAPI contracts, and serve readiness at `GET /health` on `api.health_port`
(default `8082`), a listener meant for probes inside the cluster and never routed
from outside. A separate port rather than a path on `api.rest_port`, so keeping it
private does not depend on every Ingress or HTTPRoute filtering paths correctly. Not
on the metrics listener of ADR 0018 either: that one runs in every process and can be
turned off, while this check reports the API process's own dependencies and its
probes must not disappear with `metrics.enabled: false`.

Removing an operation from `/v1` breaks the compatibility promise of ADR 0017. We
accept it without a `/v2`: the endpoint served infrastructure probes, not
integrators, `/v1` had been out only a few hours, and keeping the operation would
keep the exposure this decision exists to remove. This supersedes point 32 of
`docs/rest-api-design.md`.

The Connect `HZService` on `api.port` is unchanged; it stays open as ADR 0009 records,
and is deprecated with the rest of the Connect surface.
