# Upgrading Kannon

Notes for operators upgrading an existing installation, newest first. Only
releases that change behaviour of an installation already in production appear
here; everything else is in [`CHANGELOG.md`](../CHANGELOG.md), which is
generated from commits.

## Unreleased — API errors no longer carry internal detail

### What changes

The Connect `HZService` answers `"ok"` or `"unavailable"` per component, where
it used to answer `"OK"` or the raw error (which named the database user,
database and host). The detail is logged as `health check failed`.

A Connect call that fails on an internal error now answers code `internal`
with the message `internal` and an `X-Request-ID` in the error metadata; the
cause is logged under that ID as `Connect request failed`. A REST 500 logs its
cause under the Problem's `request_id` as `REST request failed`, and every REST
401 logs `REST authentication failed` with the peer address, path and request
ID.

Intake refuses a NUL byte in the Subject, sender alias, HTML or any field with
`validation_failed` (REST 422, Connect `invalid_argument`), where it used to
fail on insert as an internal error. A legacy send Rejects a Recipient whose
fields put a CR or LF into the Subject, with reason `reserved_field`, instead
of accepting a Delivery that could never be built.

### What you have to do

Monitors that compare an `HZService` status with `"OK"` must accept `"ok"`.
Clients that parsed a Connect internal error's message must not: quote its
`X-Request-ID` to find the cause in the API's log.

## Unreleased — Health check on an internal port

### What changes

The REST API no longer serves `GET /v1/health` or `GET /v1beta1/health`, and the
health operation is gone from both OpenAPI contracts. The readiness check moved
to a listener of its own, `api.health_port`, which defaults to `8082`, at
`GET /health`: 200 `{"status":"ready"}` or 503 `{"status":"unavailable"}`.

The API refuses to start when any two of `api.port`, `api.rest_port` and
`api.health_port` are equal.

### What you have to do

Point readiness probes and monitors that called `/v1/health` or
`/v1beta1/health` at `GET /health` on `api.health_port`, adding the port to the
container. Do not route it from outside the cluster. Set `api.health_port` if
`8082` is taken on the host.

## Unreleased — strong Admin Token required

### What changes

A process serving the API refuses to boot when `api.admin_token` is shorter
than 32 characters or is a known placeholder (`change-me`, `changeme`, `admin`,
`secret`), whatever its length. The boot error names the minimum. The
docker-compose example no longer falls back to `change-me`: Compose refuses to
start the stack until `KANNON_ADMIN_TOKEN` is set.

### What you have to do

If your token is shorter than 32 characters or is one of the placeholders,
generate a new one with `openssl rand -hex 32`, store it where `api.admin_token`
reads it (a Secret behind `env://KANNON_ADMIN_TOKEN`), and give it to every
caller of the Admin and Stats APIs before upgrading. With the docker-compose
example, export `KANNON_ADMIN_TOKEN` before `docker compose up`.

## Unreleased — Tracker served only on the Domain's stats host

### What changes

The Tracker answers a click or open only when the request's host, port
stripped and case ignored, is `stats.` followed by the Domain the token was
issued for, the host Kannon writes into tracking links. On any other host it
answers 404 and records nothing. A click now redirects only to an `http` or
`https` URL; a token naming any other kind of URL answers 404.

### What you have to do

Nothing, if every `stats.<domain>` host reaches the Tracker with its `Host`
header intact. If a proxy, Ingress or HTTPRoute in front of `tracker.port`
rewrites the `Host` header, or the Tracker is reached only through a single
shared host, change it so that the original `stats.<domain>` host is passed
through; otherwise every open and click answers 404.

## Unreleased — no delivery to private destinations

### What changes

The sender no longer delivers to an MX host whose address is on loopback, a
private range, the shared address space of carrier-grade NAT (`100.64.0.0/10`),
link-local or unspecified, nor to an IPv6 address that embeds such an IPv4
address (NAT64 `64:ff9b::/96`, IPv4-compatible `::a.b.c.d`, 6to4 `2002::/16`)
or lies in the local-use NAT64 prefix `64:ff9b:1::/48`. Such a Delivery bounces permanently,
with a reason naming the refused address, instead of being handed over.

### What you have to do

Nothing, unless your recipients' mail is relayed through a host on a private
network (a self-hosted MX, a test relay). Then set
`sender.allow_private_destinations: true` on the sender.

## Unreleased — Fields bounded per REST Batch

### What changes

`POST .../batches` refuses a request whose fields would multiply out of
proportion, since global fields are copied into every Recipient and stored with
each Delivery. A Recipient may carry at most 1000 fields, global fields
included (422 `too_many_fields`), and all Recipients' fields, keys and values
plus 64 bytes for each field, with global fields counted once per Recipient,
may total at most 10 MiB (422 `fields_too_large`). The legacy Mailer API is unchanged.

### What you have to do

Nothing, unless a client sends more fields than that: split such a Batch into
smaller ones, or move large shared content from `global_fields` into the HTML or
a Template.

## Unreleased — Prometheus metrics on port 9090

### What changes

Every process that runs a component now serves Prometheus metrics on `GET /metrics`
from a listener of its own, on port `9090` by default. A process whose `api.port`,
`api.rest_port`, `api.health_port` or `tracker.port` is `9090` refuses to boot, naming both keys.

### What you have to do

Nothing, unless port `9090` is taken where Kannon runs: then set `metrics.port`, or
turn the listener off with `metrics.enabled: false`. To scrape it, expose the port
to your scraper only; it is not meant to be published.

## Unreleased — REST API v1

### What changes

The REST API is served under `/v1`, with the same contract as `/v1beta1`: same
operations, request and response bodies, status codes and authentication.
`/v1beta1` keeps working unchanged but is deprecated and will be removed in a
coming release; its contract (`GET /v1beta1/openapi.yaml`) marks every operation
deprecated. A `Location` header points under the version the request was made to.

### What you have to do

Nothing now. Before `/v1beta1` is removed, change client base URLs from
`/v1beta1` to `/v1`, and any Ingress or HTTPRoute that matches `/v1beta1/` only
so that it also routes `/v1/`.

## Unreleased — REST on its own port

### What changes

The REST API (`/v1beta1`) is no longer served on `api.port` beside the Connect
APIs. It has a listener of its own, `api.rest_port`, which defaults to `8081`.
On `api.port`, a request under `/v1beta1/` now answers 404. The Connect, gRPC
and gRPC-Web APIs stay where they were.

The API refuses to start when `api.port` and `api.rest_port` are equal.

### What you have to do

Expose the new port: add it to the container and Service, and point whatever
routes `/v1beta1/` (an Ingress, an HTTPRoute, a client's base URL) at it
instead of `api.port`. Set `api.rest_port` if `8081` is taken on the host.

## Unreleased — Personalised content within api.max_payload

### What changes

`api.max_payload` now bounds a message as it will be sent, after its fields are
substituted, and not only the body and attachments as submitted. A placeholder
repeated through a body against a large field value used to grow at the API or
in the Dispatcher without limit; it is now measured before it is built.

- **The subject counts** toward the limit, beside the HTML and attachments.
- **A Recipient whose fields push its message past the limit is refused alone**,
  with the reason `message_too_large`, on both APIs. On the legacy API, global
  fields that push the body past it fail the call with `message_too_large`, as
  oversized content already did.
- **The legacy Mailer API accepts at most 2000 Recipients per call**, the same
  bound as REST. More fails the call with `InvalidArgument`
  (`too_many_recipients`).
- **The Dispatcher refuses to build** a Delivery queued before this release whose
  content exceeds the NATS server's `max_payload`. It is retried, and ends as
  Failed when its Retry Budget runs out, as a Delivery that cannot be built
  always did.

### What you have to do

If a legacy client sends more than 2000 Recipients in one call, split the Batch
over several calls.

Treat `message_too_large` as a Recipient refusal reason; clients that already
treat an unrecognised reason as a refusal of unknown cause need no change.

## Unreleased — Reply-To on REST Batches

### What changes

A REST Batch may state `reply_to: {email, name}`, written into the `Reply-To`
header of its messages. See the [README](../README.md#reply-to).

Mail that states none changes too: **Kannon no longer writes a `Reply-To`
equal to the Sender**. Every message, from either API, used to carry one. It
said nothing, because a mail client with no `Reply-To` replies to `From` (RFC
5322 §3.6.2), so replies keep going where they went before. A message now
carries `Reply-To` only when its Batch asked for one.

`Reply-To` also joins the header set every DKIM signature covers, whether or
not the message carries one (see the amendment to
[ADR 0005](adr/0005-one-click-unsubscribe-and-signed-header-set.md)). A relay
that *adds* a `Reply-To` to your mail now breaks the signature instead of
redirecting replies unnoticed.

On the REST API, an address that does not parse in `from.email` (and now
`reply_to.email`) is refused with **422 `validation_failed`** instead of 400
`malformed_request`: the JSON is well-formed, the value is not.

### What you have to do

If a REST client branches on the status of a refused `from` address, expect
422 rather than 400.

Nothing else, unless something downstream of Kannon looks for a `Reply-To` header
on every message, or adds one to your mail in transit. If it looks for one,
state `reply_to` on your REST Batches.

## Unreleased — Retention for every table and stream

### What changes

The stats worker's cleanup cycle now bounds every table that used to grow
without limit:

- **Mailer API Batches.** A Batch accepted by the Mailer API, and the Transient
  Template it created, are deleted 30 days after intake once none of its
  Deliveries is left in the Pool. Such a Batch has no history and records no
  completion time, so the clock starts at intake; nothing reads the row once
  its Pool is empty. The migration adds `messages.created_at` and sets it to
  the migration time on existing rows, so Batches accepted before the upgrade
  are deleted 30 days after it. Stats rows are untouched and keep their own
  `stats.retention`.
- **Aggregated counters.** `aggregated_stats` rows older than the new
  `stats.aggregated_retention` (default `8760h`, one year) are deleted. They
  are the only record of anonymous engagement, so **set a longer value before
  upgrading** if you need counters older than a year; they cannot be rebuilt
  once deleted. The key is separate from `stats.retention`, which keeps
  governing per-Delivery stats.
- **Idempotency receipts.** A REST Batch's `batch_intake_receipts` row, until
  now kept forever, is deleted 90 days after the Batch completed (its history
  has gone at 30). From then on its `Idempotency-Key` no longer deduplicates:
  a client reusing the key with the same content creates a new Batch instead of
  replaying the old one. Receipts of Batches that have not completed are kept.
- **Counting markers.** `counted_stat_events`, the aggregate counter's
  deduplication markers, are deleted 8 days after their event was stored: the
  `kannon-stats` max age below, after which the event cannot be redelivered,
  plus a day of margin for clock skew. Do not restore a `kannon-stats` backup
  older than that, or its events are counted again. For the same reason, do
  not raise the stream's max age by hand: the markers would expire while their
  events can still be redelivered (every process resets it to 7 days at
  startup anyway).

And the JetStream streams:

- **`kannon-stats`** is now configured in code with a 7-day max age (limits
  policy, file storage, discard-old) by every process that publishes or
  consumes stat events — stats, dispatcher, sender, validator, tracker and
  smtp. Until now the sender alone set it, with a 24-hour max age, and embedded
  NATS created it with none. A consumer that is down can now catch up on a
  week of events; size the NATS store for a week of stat events. A stream you
  created by hand is updated to this configuration at startup, and a process
  refuses to start if the update is impossible — for instance a stream with
  memory storage, whose storage type cannot be changed: delete it first.
- **`kannon-bounce`** is no longer created. Nothing ever published to or
  consumed it; an existing one is empty and can be deleted with
  `nats stream rm kannon-bounce`.

## Unreleased — Signed bounce return paths

### What changes

The return path of every message Kannon sends now carries an HMAC over the
Recipient and the Batch it names, and the inbound SMTP server reports a bounce
only when the DSN is addressed to a return path that verifies. Before, the
return path was the Recipient's address in base64 next to the Batch ID, both of
which every recipient of the Batch can read off their copy: anyone able to reach
port 25 could forge a DSN that made the Dispatcher drop any other Recipient's
queued or retrying Delivery, and record a bounce against any address of any
Domain.

### What you have to do

**Set `bounce.secret` before upgrading** on every process running the
dispatcher or the smtp server — the same value on all of them, at least 32
characters. Both refuse to boot without it.

```yaml
bounce:
  secret: env://KANNON_BOUNCE_SECRET
```

```sh
kubectl create secret generic kannon-bounce-secret --from-literal=secret="$(openssl rand -hex 32)"
```

### What you lose

Bounces of messages sent **before** the upgrade arrive on an unsigned return
path and are no longer reported. Their Deliveries have already been handed on,
so the only effect is that a late DSN for one of them is not recorded as a
bounce. The same happens to messages in flight whenever the secret is changed.

## Unreleased — aggregate event counting

The aggregate consumer now counts each stored JetStream message once, including
when an acknowledgement is lost or workers process concurrent redeliveries.
Separate stored Opened and Clicked events still count separately, even when
their payloads match. Anonymous remains aggregate-only.

The migration adds `counted_stat_events`: one marker per counted stored message,
containing only stream name, original server storage timestamp and sequence.
The storage timestamp comes from each message’s metadata, so sequence reuse after
stream recreation does not depend on a worker’s stale stream snapshot. Keep
this table with the aggregate counters when backing up or restoring the database.

Markers do not expire with `stats.retention`: deleting one allows its message
to count again, so a marker must outlive every possible replay of it. Since the
`kannon-stats` stream got a 7-day max age (see *Retention for every table and
stream* above), the stats worker deletes markers 8 days after their message was
stored. Restoring a stream backup older than that would replay messages whose
markers are gone, and count them again.

Existing counters are preserved. Previously counted messages have no markers,
so replaying pre-upgrade history can still add to those existing counters. The
migration cannot reconstruct stored-message identities from hourly totals.

## Unreleased — Pseudonymous tracking

### What changes

The Tracking Mode `pseudonymous` is now selectable. It has been on the scale
since Tracking Policies were introduced, ranked between `anonymous` and
`identified`, but stating it was refused; it is now honoured at Domain, Batch
and Recipient level like every other Mode. Nothing changes for an installation
that does not select it.

It is the rung for a sender who wants open and click *rates* per Batch, and the
ability to tell one recipient's engagement from another's, without recording
who those recipients are. Events are linkable to each other within a single
Batch and to nothing outside it: no recipient address reaches the tracking URL,
the Tracker's access logs, or the `stats` table.

### The `track.<domain>` subdomain is reserved

**Do not deliver mail under `track.<yourdomain.com>`.** Under `pseudonymous` and
`anonymous`, the identity a tracking token carries is a *sentinel address* in
that namespace — `<random>@track.yourdomain.com` and
`anonymous@track.yourdomain.com` respectively — rather than the recipient's own.
A real mailbox under that subdomain would collide with the sentinel space and be
indistinguishable from it in your statistics.

The reservation costs nothing to honour: **no DNS record is required**. These
addresses are identifiers, never envelope recipients — Kannon never delivers to
one, never resolves it, and never looks it up. See
[ADR 0006](adr/0006-tracking-identity-in-the-token-sentinel-addresses.md).

### What your statistics look like

A pseudonymous Opened or Clicked is recorded exactly like an identified one, so
every existing query keeps working — but the `email` it is recorded under is the
pseudonym, not the recipient. Two consequences worth planning for:

- **Counting distinct addresses still counts distinct recipients** within a
  Batch, which is what open and click rates need.
- **A pseudonym is drawn fresh for every Batch**, from `crypto/rand`, stored
  nowhere and derived from nothing. Engagement therefore cannot be joined across
  Batches, or back to a recipient, by anyone — Kannon included. That is the
  point of the rung, and it is not recoverable after the fact: if you need
  per-recipient history over time, state `identified` instead.

Aggregate counters are unaffected: they never looked at the identity.

### Selecting it

Through the Admin API's `SetTrackingPolicy`, per axis, as with any other Mode:

```json
{
  "domain": "yourdomain.com",
  "tracking": {
    "opens": "TRACKING_MODE_PSEUDONYMOUS",
    "links": "TRACKING_MODE_PSEUDONYMOUS"
  }
}
```

As always, a Domain's Policy is a ceiling and a Batch or Recipient may only
restrict it further, and a Policy is frozen onto each Delivery when its Batch is
created — so Deliveries already queued keep the Policy they were accepted under.

One operational note: `pseudonymous` names a different identity per Delivery, so
it signs one token per Delivery like `identified` does, rather than sharing a
single token across the Batch the way `anonymous` can. Expect the same dispatch
cost as `identified`, not the cheaper `anonymous` one.

## Unreleased — One-Click Unsubscribe

### What changes

Kannon can now carry a `List-Unsubscribe` / `List-Unsubscribe-Post` pair
(RFC 8058), which the large receivers require of bulk senders. The endpoint is
**yours**: Kannon personalises the URL, emits it and signs it, and does nothing
else with it — it never calls it, keeps no suppression list, and records no
engagement when a recipient uses it. Nothing is emitted unless you ask for it,
so an installation that ignores this section is unaffected.

Two changes reach mail you are already sending, though:

- **Every DKIM signature now covers a fixed header set**:
  `From, To, Cc, Subject, Message-ID, List-Unsubscribe, List-Unsubscribe-Post`,
  whether or not each header is on the message. Signing a header that is absent
  is what lets a receiver detect one inserted in transit (RFC 6376 §5.4). The
  practical consequence: a relay that *adds* a Cc or an unsubscribe header to
  your mail now breaks the signature instead of having its addition ride along
  unsigned. A forwarder that inserts its own `List-*` headers — a mailing list
  expander, typically — will fail DKIM at the final hop, where before it would
  have passed. See
  [ADR 0005](adr/0005-one-click-unsubscribe-and-signed-header-set.md).
- **`{{ email }}` now resolves in your subject and body**, to the recipient's
  address. Before this release it rendered as literal braces unless you passed a
  field of that name yourself. If you pass your own `email` field, your value
  still wins — nothing you already send changes.

### Using it

State the endpoint per send. It is deliberately not a Domain-wide setting:
Kannon is a transactional sender, and an unsubscribe header on a password reset
offers a recipient something you cannot honour.

```json
{
  "one_click_unsubscribe": {
    "url_template": "https://yourdomain.com/unsub?email={{ email }}"
  }
}
```

Four things the API will hold you to:

1. **The URL must be `https`.** A `mailto:` fallback is not supported, and plain
   `http` would put the recipient's identifier on the wire in the clear. A
   template that is malformed, relative or non-https fails the whole call.
2. **Your endpoint must accept `POST`** with body `List-Unsubscribe=One-Click`
   and unsubscribe with no further confirmation. Supplying a URL here asserts
   that, and Kannon cannot verify it. Advertising one-click without honouring it
   is worse than not advertising it: the recipient presses the button, believes
   they are unsubscribed, and reports your next message as spam.
3. **Pass raw field values, not pre-encoded ones.** Kannon percent-encodes every
   value it substitutes into the URL, so `mario+rossi@example.com` arrives
   intact rather than as `mario rossi@example.com`.
4. **Every placeholder must resolve for every Recipient.** One whose fields
   leave a placeholder unresolved is Rejected on its own, with reason
   `unsubscribe_url_unresolved` in `rejected_recipients`, while the rest of the
   send proceeds. `{{ email }}` always resolves; a custom `{{ token }}` only
   does if you pass it for that row.

The link in your message *body* is a separate matter and is still tracked like
any other. If it points at the same URL, keep it out of click tracking with
`data-no-track` on the `<a>` tag (see below).

## Unreleased — Tracking Policy

### What changes

Before this release Kannon tracked every email it sent and there was no way to
stop it: an open pixel was injected into every Envelope, every link was
rewritten, and every Opened and Clicked event was recorded against the
recipient's address **together with their IP address and user agent**. In the
vocabulary introduced by this release (see [`CONTEXT.md`](../CONTEXT.md)), that
behaviour is the Tracking Mode `full`.

Every Domain — existing ones and newly created ones alike — now starts at
`identified` instead. Opens and clicks are still attributed to the recipient, so
**no dashboard goes dark and no metric resets**, but the IP address and the user
agent are no longer captured or stored. For an existing installation this
upgrade is a *reduction* in what is collected, which is deliberate: see
[ADR 0003](adr/0003-tracking-policy-ceiling-defaults-and-intake-resolution.md).

The migration backfills existing Domains, and existing rows in the Pool, from
the new column default in a single statement — no table rewrite, no downtime.

### Restoring the previous behaviour

Tracking is now a per-Domain decision, so restoring the old behaviour means
putting a Domain back to `full`. Two ways:

Through the Admin API's `SetTrackingPolicy` — the two axes are set
independently:

```json
{
  "domain": "yourdomain.com",
  "tracking": {
    "opens": "TRACKING_MODE_FULL",
    "links": "TRACKING_MODE_FULL"
  }
}
```

Or directly in SQL, for every Domain at once:

```sql
-- Restore the pre-0.6.0 behaviour: attribute engagement to the recipient and
-- retain the IP address and user agent of every open and click.
UPDATE domains SET tracking = '{"opens":"full","links":"full"}'::jsonb;
```

Note that a Tracking Policy is resolved when a Batch is created and frozen onto
each Delivery, so **neither route affects Deliveries already queued** — they
carry the Policy that was in force when they were accepted. The change applies
from the next send onwards.

> **The Admin API has no authentication of its own and is served on the same
> listener and port as the Mailer API.** A Domain's Tracking Policy is a security
> control — it is the ceiling no API caller can exceed — but anyone who can reach
> the API port can raise any Domain's ceiling, and nothing records who changed a
> Policy or when. That is not new (`CreateAPIKey` sits on the same surface), but
> it now bounds what this feature can promise: **the Admin API must not be
> reachable by tenants or from an untrusted network.**

Before choosing `full`, note that retaining an IP address and a user agent is
what makes open and click tracking personal-data processing that needs a lawful
basis; `identified` and the Modes below it exist precisely so that a Domain does
not have to. Kannon does not decide this for you and holds no consent of its own
([ADR 0002](adr/0002-kannon-executes-tracking-instructions-does-not-store-consent.md)).

### Turning tracking down or off instead

The same call takes any Tracking Mode, and the two axes are independent, so a
Domain can rewrite links without embedding a pixel, or the reverse:

| Mode | What is retained |
| --- | --- |
| `off` | Nothing. No pixel is injected, no link is rewritten, and no tracking hostname appears in the delivered message at all. |
| `anonymous` | Aggregate counters only. Nothing that could isolate one recipient from another. |
| `pseudonymous` | A pseudonym, drawn per recipient per Batch. Engagement is linkable within one Batch and nowhere else; no recipient address is retained. |
| `identified` | The recipient's identity. No IP address, no user agent. **The new default.** |
| `full` | The recipient's identity, plus the IP address and user agent of the request. The pre-0.6.0 behaviour. |

A Domain's Policy is a **ceiling**, not a default: an API caller may state a
Tracking Policy per Batch and per Recipient, but only to *restrict* what the
Domain allows. A caller asking for more than the ceiling is refused rather than
silently downgraded, so a policy decision cannot be mistaken for a bug.

### Opting a single link out

A Mode governs a whole Delivery. To keep one link out of click tracking while the
rest of the message stays tracked, say so on the `<a>` tag in the HTML you author:

```html
<a href="https://yourdomain.com/preferences" data-no-track>Manage preferences</a>
```

That link is delivered with its `href` as authored, and the attribute is stripped
before delivery, so it never reaches the recipient. Unsubscribe and preference
links are the reason this exists.

Links no redirect could serve — `mailto:`, `tel:`, `sms:` and in-page anchors —
are now left alone whether or not they carry the attribute. Before this release
they were rewritten too, into a redirect no mail client could follow.

### If you call the API

Nothing is required of existing callers. The new fields on `SendHTML`,
`SendTemplate` and `Recipient` are optional; omitting them states nothing, which
imposes no restriction, which resolves to the Domain's Policy.

Two things are worth adopting when convenient:

- `SendRes` now reports `accepted_count`, `rejected_count` and
  `rejected_recipients`. Recipients that Kannon refuses at intake — an invalid
  address, or a Tracking Policy above the Domain's ceiling — used to be dropped
  with a log line while the call still reported success. They are now returned
  with a stable machine-readable reason.
- A send in which *every* Recipient was refused no longer looks like a success
  with an empty Pool.

One sharp edge worth knowing: a Recipient asking for **more** than the Domain
allows is Rejected, which means that Recipient gets **no email at all** — the
Policy is not quietly clamped down to what the ceiling permits. That is
deliberate (ADR 0003: an instruction above the ceiling is a contradictory
instruction, and silently reinterpreting it would make policy
indistinguishable from a bug), but it means a caller passing `full` for
everybody under a Domain on `identified` stops delivering to everybody. Pass
the Policy you have consent for, or omit it and let the Domain decide.
