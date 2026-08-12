# Migrating to v1

For installations running **0.5.x**. v1 changes how Kannon is configured, closes
the API surfaces that used to answer anybody, and ships five schema migrations —
one of which can refuse to apply to data 0.5.x allowed you to create.

Read [Before you start](#before-you-start) first: it is the part that has to
happen while the old version is still running.

| Area                                    | What changes                                                                             |
| --------------------------------------- | ---------------------------------------------------------------------------------------- |
| [Configuration](#1-configuration)       | The config file is the only contract: `--run-*` flags and `K_` variables are gone         |
| [Access control](#2-access-control)     | The Admin and Stats APIs authenticate; a pod serving the API needs `api.admin_token`      |
| [Database](#3-database)                 | Five migrations, one constraint that can fail, one backfill that changes tracking         |
| [APIs](#4-apis)                         | Hourly aggregated stats, Tracking Policy on send, one-click unsubscribe                   |
| [Audit trail](#5-audit-trail-optional)  | New, off by default, holds personal data when on                                          |

## Before you start

**1. Find the domains the new schema will refuse.** v1 constrains `domains.domain`
to a canonical form — lower-case, at least two dot-separated labels of
`[a-z0-9_-]`. 0.5.x let you store anything, and the migration fails on the first
row that does not fit, taking the whole `kannon migrate main` with it:

```sql
SELECT domain FROM domains WHERE domain !~ '^[a-z0-9_-]+(\.[a-z0-9_-]+)+$';
```

Fix or delete every row this returns before upgrading. `MAIL.Example.COM` is the
common case; lower-casing it is safe as long as nothing outside Kannon joins on
the old spelling — the domain is what a sender authenticates as, so an API Key
holder sending as `MAIL.Example.COM` has to be told the new spelling.

**2. Inventory what your manifests pass.** Every `--run-*` argument and every
`K_*` variable has to be rewritten; the sections below say what into. Both are
removed, not deprecated — a flag stops the boot with an unknown-flag error, and a
variable is simply not read.

**3. Know where the admin token will come from.** A pod serving the API refuses to
boot without one in v1. Generate it now and put it in a Secret.

## 1. Configuration

### The config file is the contract

Every setting is written in one file, and any value in it can name an environment
variable instead of holding a literal:

```yaml
database_url: env://KANNON_DATABASE_URL # required: an unset variable stops the boot
nats_url: env://KANNON_NATS_URL:-nats://nats:4222 # with a fallback
```

`env://NAME` is required and refuses the boot when unset. `env://NAME:-default`
falls back, and treats an empty variable as unset. A value that opens with the
scheme but is malformed — `env:/NAME`, `ENV://NAME`, `env://my-name` — is refused
rather than passed through as a literal.

This works for **every** key, including nested ones. That is the whole reason the
`K_` prefix is gone: viper could only answer top-level keys from the environment,
so `K_DATABASE_URL` worked while `K_API_PORT`, `K_SENDER_HOSTNAME` and
`K_TRACKER_PORT` were silently ignored — and nothing distinguished the two from
the outside.

### Which components a process runs

`--run-*` flags are gone. A process is described by the `services` section:

```yaml
services:
  api:
    enabled: true
  stats:
    enabled: env://KANNON_ENABLE_STATS:-false
```

| 0.5.x                             | v1                                  |
| --------------------------------- | ----------------------------------- |
| `--run-sender`                    | `services.sender.enabled`           |
| `--run-dispatcher`                | `services.dispatcher.enabled`       |
| `--run-validator`, `--run-verifier` | `services.validator.enabled`       |
| `--run-tracker`, `--run-bounce`   | `services.tracker.enabled`          |
| `--run-stats`                     | `services.stats.enabled`            |
| `--run-api`                       | `services.api.enabled`              |
| `--run-smtp`                      | `services.smtp.enabled`             |
| `--run-audit`                     | `services.audit.enabled`            |
| `--viper`                         | nothing — it never did anything     |

Two refusals are new, and both are deliberate:

- a process with **nothing** enabled is an error. In 0.5.x it logged
  `Starting Kannon runnables: []` and exited 0, which reads as a clean shutdown in
  every dashboard there is.
- a **misspelled** name under `services` is an error rather than a silently
  ignored key, since the difference is a pod that runs nothing.

`kannon standalone` is unaffected: it is every component by definition and reads
no `services` section.

### Environment variables

| 0.5.x                            | v1                                                |
| -------------------------------- | ------------------------------------------------- |
| `K_DATABASE_URL`                 | `database_url: env://K_DATABASE_URL`              |
| `K_NATS_URL`                     | `nats_url: env://K_NATS_URL`                      |
| `K_USE_EMBEDDED_NATS`            | `use_embedded_nats: env://K_USE_EMBEDDED_NATS`    |
| `K_DEBUG`                        | `debug: env://K_DEBUG`                            |
| `K_API_ADMIN_TOKEN`              | `api.admin_token: env://K_API_ADMIN_TOKEN`        |
| `K_API_PORT`, `K_SENDER_HOSTNAME`, `K_TRACKER_PORT`, `K_SMTP_ADDRESS`, `K_BUMP_PORT` | `api.port`, `sender.hostname`, `tracker.port`, `smtp.address`, `tracker.port` — none of these variables ever worked |

The variable does not have to be renamed: the file can go on referring to whatever
your deployment already sets. What changes is that the reference is written down.

The `bump:` section is gone too — it was an alias for `tracker:`, named after a
package removed before 0.5.0. Move `bump.port` to `tracker.port`.

### A deployment, before and after

0.5.x, with the component in the args and the settings in the environment:

```yaml
args: ['--run-api', '--run-stats']
env:
  - name: K_DATABASE_URL
    valueFrom: { secretKeyRef: { name: kannon-database, key: url } }
  - name: K_API_ADMIN_TOKEN
    valueFrom: { secretKeyRef: { name: kannon-admin-token, key: token } }
```

v1, with one ConfigMap shared by every Deployment and each pod naming only what it
is:

```yaml
# ConfigMap, mounted unchanged by every Deployment
config.yaml: |
  database_url: env://KANNON_DATABASE_URL
  nats_url: env://KANNON_NATS_URL:-nats://nats:4222

  services:
    api:
      enabled: env://KANNON_ENABLE_API:-false
    stats:
      enabled: env://KANNON_ENABLE_STATS:-false
    # …one entry per component, all defaulting to false

  api:
    admin_token: env://KANNON_ADMIN_TOKEN
  sender:
    hostname: env://KANNON_SENDER_HOSTNAME
---
# Deployment
args: ['--config', '/etc/kannon/config.yaml']
env:
  - name: KANNON_ENABLE_API
    value: 'true'
  - name: KANNON_ENABLE_STATS
    value: 'true'
  - name: KANNON_DATABASE_URL
    valueFrom: { secretKeyRef: { name: kannon-database, key: url } }
  - name: KANNON_ADMIN_TOKEN
    valueFrom: { secretKeyRef: { name: kannon-admin-token, key: token } }
```

[`k8s/deployment.yaml`](../k8s/deployment.yaml) is the complete reference manifest.

## 2. Access control

In 0.5.x the Admin API and both Stats APIs were mounted with **no authentication
at all**: anyone who could reach the listener could create a Domain, mint an API
Key for it and read any Domain's statistics. v1 closes them.

**The admin token.** Set `api.admin_token` — as a reference to a Secret, not as a
literal in the ConfigMap. A pod with `services.api.enabled` and no token **refuses
to boot**, rather than come up answering every request with `unauthenticated`.
Workers that serve no API need no token, and should not be given one.

**Calling the closed surfaces.** The Admin API and both Stats APIs take a header of
their own:

```
X-Kannon-Admin-Token: <api.admin_token>
```

Every existing client of those APIs has to be updated. The health service
(`HZService`) stays open, so probes need no change.

**The Mailer API is unchanged**: Basic Auth with `base64(<domain>:<api key>)`.
Existing API Keys keep working.

**Attribution (optional).** A front-end holding the admin token can name the person
it is acting for, on the same three surfaces:

```
X-Kannon-Attribution: alice@corp.com
```

It is recorded, never checked, and can never widen what a request may do. Malformed
claims (over 256 bytes of UTF-8, or carrying control characters) are refused with
`invalid_argument` rather than dropped.

> The admin token authorizes **everything on every Domain** and names no operator.
> It is revoked by changing it and restarting. Keep the API listener off untrusted
> networks.

## 3. Database

Run `kannon migrate main --config ./config.yaml` before starting v1 pods. Five
migrations apply:

| Migration                              | What it does                                                                 |
| -------------------------------------- | ---------------------------------------------------------------------------- |
| `add_tracking_policy`                  | Adds `tracking` to `domains`, `sending_pool_emails` and `messages`            |
| `switch_aggregated_stats_to_hourly_buckets` | Rewrites `aggregated_stats.timestamp` from day to hour                   |
| `add_sending_pool_emails_claimed_at`   | Records when a Delivery was claimed; rows already in flight get one full grace period |
| `require_canonical_domain_name`        | Constrains `domains.domain` — **can fail**, see [Before you start](#before-you-start) |
| `add_audit_records`                    | New `audit_records` table, written only when the audit trail is on            |

Three of them are worth knowing about beyond the fact that they ran.

**Tracking is backfilled to `identified`.** Existing Domains and Deliveries track
effectively "full" today; the backfill keeps per-recipient attribution and stops
retaining IP address and user agent. It is a reduction — if you depend on that
data, set the Domain's ceiling explicitly after migrating. See
[ADR 0003](adr/0003-tracking-policy-ceiling-defaults-and-intake-resolution.md).

**Aggregated stats change meaning.** `aggregated_stats.timestamp` was a UTC day at
midnight and is now a UTC hour. The migration rebuilds the window that `stats`
fully covers; older rows are left as day buckets at `00:00`, which sum correctly on
a daily axis and pile onto hour 0 on an hourly one. That is the honest rendering of
"we know the day, not the hour" — history is not destroyed to make the axis
uniform, because `stats` is pruned by retention while `aggregated_stats` never is.
The rewrite runs in one transaction with a 300s statement timeout; on a large
installation, run the migration in a maintenance window rather than from an init
container racing a rollout.

**The canonical-domain constraint can fail.** Check for offending rows before you
start, not after the migration has rolled back.

## 4. APIs

**Stats API v2** (`kannon.stats.apiv2.StatsApiV2/GetAggregatedStats`) serves the
hourly buckets. Stats API v1 still exists and still answers; a client rolling
buckets up into days of its own timezone should move to v2, which is the version
whose granularity makes that possible.

**Tracking Policy on send.** `SendHTML` / `SendTemplate` accept an optional
`tracking` policy at Batch level and per Recipient. A Batch may only *narrow* the
Domain's ceiling; asking for more fails the call, and the per-Recipient version
rejects that Recipient with reason `tracking_above_ceiling`.

**One-click unsubscribe (RFC 8058).** The optional `one_click_unsubscribe` field
carries your own unsubscribe endpoint. Kannon emits the headers; the endpoint is
yours, and setting the field asserts that a POST to it unsubscribes with no
confirmation step.

**Rejected recipients** are reported per send with a stable `reason` token —
`invalid_email`, `tracking_above_ceiling`, `unsupported_tracking_mode`,
`unsubscribe_url_unresolved`. The set grows, so treat an unrecognised value as a
refusal of unknown cause rather than as an error in your client.

## 5. Audit trail (optional)

New in v1 and **off by default**. Two keys, one letter apart in meaning, and both
are needed:

```yaml
audit:
  enabled: true # publish authorization decisions at all
  retention: 720h # how long a record is kept — your obligation, not Kannon's
services:
  audit:
    enabled: true # run the writer that turns them into rows
```

`audit.enabled` alone publishes records nobody writes down, and they expire off the
NATS stream after seven days — the API warns when it sees that happening. The
writer alone consumes nothing and stops rather than idling.

An Audit Record holds the credential that acted, the Action, the Resource path, the
outcome, the instant, the Grants held, and — when the request carried
`X-Kannon-Attribution` — the person that header named. **That claim is personal
data**, which is why its retention is yours to set. The caller's IP address is
deliberately not collected. See
[ADR 0010](adr/0010-every-authorization-decision-becomes-an-audit-record.md).

## Upgrade order

1. Fix non-canonical domains (query above), on 0.5.x, while it is still running.
2. Create the admin-token Secret.
3. Write the config file, mount it as a ConfigMap, and update every Deployment:
   `--config`, the `KANNON_ENABLE_*` variables, and the Secret references. Do not
   roll it out yet.
4. Run `kannon migrate main` against the new image. The new columns carry defaults
   and the new table is unread by 0.5.x, so the old pods keep running while it
   applies.
5. Update the clients of the Admin and Stats APIs to send `X-Kannon-Admin-Token`.
   Do this **before** rolling out: 0.5.x ignores the header, so an updated client
   works against both versions, while a client that has not been updated stops
   working the moment the new API pod serves it.
6. Roll out the new pods.

## If something goes wrong

Every migration has a `migrate:down`, but two of them lose data on the way back.
Rolling back `add_tracking_policy` drops the tracking columns, so every explicit
Tracking Policy goes with them. Rolling back the hourly-bucket rewrite sums the
hourly rows back into day buckets, which is correct and irreversible: the hour is
gone, and re-applying the migration can only rebuild it for the window `stats`
still covers. Take a database backup before step 4.

A pod that fails to boot says why, and in v1 that message is the whole diagnosis:
an unknown flag is a `--run-*` left in a manifest, `this process was asked to run
nothing` is a missing or misspelled `services` entry, a message naming
`api.admin_token` is the Secret, and one naming a variable is an `env://` reference
nobody set.
