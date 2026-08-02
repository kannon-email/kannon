# Upgrading Kannon

Notes for operators upgrading an existing installation, newest first. Only
releases that change behaviour of an installation already in production appear
here; everything else is in [`CHANGELOG.md`](../CHANGELOG.md), which is
generated from commits.

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
