# ADR 0001: Configurable Delivery Backoff

## Status

Accepted (2026-05-03).

## Context

`Delivery` (per `CONTEXT.md`) is the per-recipient transmission unit. When a
send attempt fails transiently, the repository's `Reschedule` call rolls the
row's `scheduled_time` forward by `Delivery.NextRetryAt() - now`. Until now
that delay was hardcoded inside `internal/delivery`:

```
delay = max(5m, 2m * 2^attempts)
```

This made the e2e suite's "fail twice then succeed" scenario take ~13 minutes
of wall-clock to converge naturally — outside any reasonable CI wall-clock
budget. Two approaches were on the table:

1. **Don't observe retry end-to-end.** Verify transient classification at the
   unit level only (the SMTPSender chooses Bounced vs. Errored correctly),
   and never run the reschedule loop in e2e.
2. **Test-only DB hack.** Mutate `sending_pool_emails.scheduled_time`
   directly from the e2e harness after each failed attempt to skip the wait.

(1) loses the cross-component coverage that is the entire point of the e2e
suite — the boundary between SMTPSender, dispatcher, and pool reschedule is
exactly where retry regressions hide. (2) couples test code to internal
schema and bypasses the production code path under test.

## Decision

Make backoff a `BackoffPolicy` interface threaded through the Container as a
single canonical instance. Production wires `delivery.DefaultBackoff` (the
existing 2m/5m curve, unchanged). Tests opt in to a faster curve via the
`WithBackoff(p) TestOption` on `container.NewForTest`.

### The policy lives on the entity, not as a `NextRetryAt(p)` parameter

`Delivery` already owns the retry-shaped state — `sendAttempts`,
`originalScheduledTime`, `scheduledTime`. The backoff curve is the same
shape of state: the policy was hardcoded inside `NextRetryAt` precisely
because it _belonged_ next to those fields. Lifting it to a parameter on
every caller would force every callsite to learn what the policy is and
re-pass it on every reschedule. Keeping it as a field on the entity matches
the existing pattern and keeps `NextRetryAt()` parameter-free, which is
what every caller wants.

`NewParams` and `LoadParams` carry a `Backoff BackoffPolicy` field. If
omitted (nil), `delivery.DefaultBackoff` is substituted — this defensive
fallback exists so a missing wire-up degrades to "production curve" rather
than a nil-pointer panic, but the convention is that every callsite passes
the policy explicitly.

### `NewDeliveryRepository(q, p)` is an explicit required parameter

Functional options (`WithBackoff(p)`) on the repo factory were considered and
rejected: the e2e suite wants the test container's policy used everywhere a
Delivery is rehydrated, and a silent fallback to `DefaultBackoff` when the
caller forgot to pass the option would produce hard-to-debug 5-minute waits
in tests. An explicit positional parameter makes a missing wire-up a compile
error, not a 5-minute timeout.

### No viper key

Backoff stays a wiring point, not an operator-facing knob. Exposing
`backoff.base` / `backoff.min` viper keys is one config block on top of this
seam if it ever becomes wanted, but it's not adopted now — operators
historically have not asked for it, and adding the surface area before the
need is hypothetical.

## Consequences

- Production behaviour is byte-for-byte identical: `delivery.DefaultBackoff`
  reproduces the old `2m / 5m` curve exactly. The `TestNextRetryAt` and
  `TestDefaultBackoffCurve` tests pin this.
- The e2e suite (sub-issue #353) can shrink retry waits to milliseconds via
  `container.WithBackoff(...)` and reach the "delivered after N transient
  failures" assertion in seconds.
- `BackoffPolicy` is unit-testable in isolation — the `internal/delivery`
  tests do not boot a Container.

## Rejected alternatives

- **Don't observe retry end-to-end.** Loses the cross-component coverage
  that motivates the e2e suite.
- **Test-only DB mutation of `sending_pool_emails.scheduled_time`.** Couples
  tests to internal schema; bypasses the production reschedule path under
  test.
- **Functional-option backoff on the repo factory.** A missing wire-up would
  silently fall back to `DefaultBackoff`, producing 5-minute test waits;
  positional param surfaces the mistake at compile time.
- **Viper key for backoff up front.** Adds operator-facing surface area for
  a knob no operator has asked for; the seam itself is what the e2e change
  needs.
