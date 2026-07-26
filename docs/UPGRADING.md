# Upgrading Kannon

Notes for operators upgrading an existing installation, newest first. Only
releases that change behaviour of an installation already in production appear
here; everything else is in [`CHANGELOG.md`](../CHANGELOG.md), which is
generated from commits.

## 0.6.0 — Tracking Policy

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

Through the Admin API's `SetTrackingPolicy`, which is the auditable route — the
two axes are set independently:

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
| `identified` | The recipient's identity. No IP address, no user agent. **The new default.** |
| `full` | The recipient's identity, plus the IP address and user agent of the request. The pre-0.6.0 behaviour. |

A Domain's Policy is a **ceiling**, not a default: an API caller may state a
Tracking Policy per Batch and per Recipient, but only to *restrict* what the
Domain allows. A caller asking for more than the ceiling is refused rather than
silently downgraded, so a policy decision cannot be mistaken for a bug.

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
