# ADR 0010: Every authorization decision becomes an Audit Record

## Status

Accepted (2026-08-04). ADR 0008 gives the authority model whose decisions this
one records; ADR 0009 gives the credential that takes them.

## Context

ADR 0009 records an attributed operation as a log line — the smallest form the
record can take: governed by the retention an operator already has for logs, and
needing no schema of its own. What a log line does not do is answer questions for
longer than logs are kept, or join itself to the row it concerns. Persisting the
record instead is a choice between an owner column on the resource and an audit
table, and the two answer different questions. A table also needs a retention
policy, which for a system whose posture is to retain no personal data is not a
neutral decision: an Attribution *is* personal data (ADR 0008).

This ADR settles it: an audit table, with a retention policy, off by default.

Four facts shaped the design.

`Guard` (`internal/authz/guard.go`) is the only place in the system that sees
a Principal, an Action, a Resource and an outcome together, and no production
code calls `Can` directly — every call site goes through `Guard`. There is one
producer, and it needs no discovery.

The volume is the API call rate, not the mail volume. A send is one `Guard` call
per Batch (`pkg/api/mailapi/mailer.go:112`), not one per Delivery, so this is not
an access log with a mail system's traffic behind it.

`pkg/stats` is already the shape this needs: a `Config` carrying a retention read
from viper (`pkg/stats/stats.go:26`), a JetStream consumer, and a cleanup cycle on
a `runner.Run` loop beside it in one errgroup (`pkg/stats/stats.go:83`).

And, before this decision, the `api` runnable did not touch NATS at all
(`pkg/api/api.go`): it held a database handle and nothing else. Every `Guard`
call site lives in that process, so the producer is a process that had to acquire
a dependency it did not have — which is most of the reason the feature is off by
default, and the reason the way it acquires it is settled below.

## Decision

### An Audit Record is one authorization decision, and `Guard` is its only producer

Every check is recorded: permitted, denied, and the `ErrNoPrincipal` case where
nothing authenticated a request that reached a guarded operation. Not only the
attributed ones — and that is the one point where this goes further than ADR
0009, deliberately. ADR 0009 records the attributed operation and nothing else,
as a log line, and it rejected recording every operation attributed or not
because "in one stream at one level there is nothing to select on": the
operations that name a person would be buried among the ones that do not, and an
access log's volume would end up inside the authorization layer.

Both of those are objections to a *stream*, and a table answers them. It has
columns to filter on, so the operations that name a person are one predicate away
and bury nothing — where a log stream has one channel that everything shares. And
the volume this admits into the authorization layer is one row per API call, not
one per Delivery. What ADR 0009 fixed is therefore untouched: a deployment with
no table sees exactly the lines it specifies, and the table records more than a
stream could be asked to carry.

What stays out is the refusal that never reaches `Guard`: a wrong or absent admin
token, a malformed Attribution, a bad API Key. Those are refused at the
interceptor (`internal/authzconnect/authzconnect.go:61-73`) and remain
`slog.Warn` lines. Three reasons. The two columns a reader would come to this
table by — `principal` and `resource` — are both unknown for an authentication
refusal, so admitting it would put two shapes in one table. The system
deliberately keeps the two apart for the caller already: unauthenticated says the
credential was wrong or absent, permission denied says a resolved Principal may
not do this — and the record follows the same line. And with one shared token a
failed authentication is attributable to nobody: it says an address tried, which
is a fact about the network. When per-operator credentials arrive it becomes a
fact about a credential, and worth a row; the table can grow a kind column then.

### The Recorder travels in the context, and its default logs

`authz.Recorder` is an interface, carried in the request context beside the
Principal and installed by one middleware in `api.go` — one place, and it covers
the Mailer API rather than only the surfaces the admin token authenticates.

`Guard`'s signature is unchanged and `Can` stays pure. Four alternatives were
weighed and are recorded below; the deciding argument against all of them is that
this feature must be able to not exist. A recorder passed through six
constructors, or demanded of them, propagates a dependency the domain layer has
no interest in, for something a deployment may switch off entirely.

The default, when no middleware has installed one, is a Recorder that logs:
`slog.Debug` for every check, `slog.Info("attributed operation")` for the
attributed ones that were permitted, and nothing at all when nothing
authenticated the request — the edge that refused it has already logged that, and
a line here would be a second account of one event. It lives in an
implementation of the interface (`internal/authz/recorder.go`) rather than in
`Guard` itself. So a deployment that does not enable the audit trail sees exactly
the log lines ADR 0009 specifies — the decision trace at debug, the attributed
operation at info — and nothing else.

The NATS recorder **decorates** the slog one instead of replacing it. The table
is additive: enabling the audit trail does not take away the lines an operator
already has, and disabling it does not silence the layer.

One change to `internal/authz` follows: `Resource` must expose its segments.
`Resource` is `segments []string` (`internal/authz/resource.go:35-37`) and its
`String()` is declared to be display only — "not a serialisation: nothing parses
a Resource back from it, and a segment holding an identifier that contains a
separator would render ambiguously" (`internal/authz/resource.go:98-100`). A
`Segments()` returning a clone is therefore the honest accessor, and the record
stores what it returns.

### It reaches the table through NATS, fire-and-forget

`nc.Publish` on `kannon.audit.allowed` and `kannon.audit.denied`, exactly as
`PublishStat` does (`internal/publisher/publisher.go`). The outcome is in the
subject so that an operator can alert on refusals alone — which, while the table
is write-only, is the only way anything here becomes visible without a query.

Two subjects for three outcomes, and deliberately (`internal/audit/wire.go:20-29`).
A request nothing authenticated is a refusal, so `no-principal` goes on
`kannon.audit.denied`: the subject exists for the alert, and an alert on refusals
that let this one through would miss the one refusal that says a path into the
system was wired without authentication. The distinction between the two kinds of
refusal is not lost — it is in the `outcome` column, which is where a reader
filters on it, and a subject per outcome would only be a third spelling of what
that column already holds.

A stream `kannon-audit` over `kannon.audit.*`, `FileStorage`, `MaxAge` 7 days.
The stream is a buffer for a consumer that is down over a weekend, not a second
archive: keeping it far below the table's retention avoids two archives with two
expiries and two answers to the same question.

`audit.ConfigureStream` holds the `StreamConfig` — one function — and is called
at startup by both the `api` runnable and the `audit` runnable. It goes through
`utils.ConfigureStream`, as every stream in Kannon does: the configuration stays
with whoever owns the stream, and how hard to try when NATS is not answering yet
is decided in one place, so that a second implementation cannot come to ride out
a shorter outage than the first (`internal/utils/stream.go:34-36`). That is where
the exponential retry lives, and the reason with it — it stands where an
`os.Exit(1)` on the first JetStream error used to, because a NATS pod briefly
slow to come up made the dispatcher crash-loop, which only amplified the load on
NATS (#365, `internal/utils/stream.go:19-21` and `:30-32`). Both runnables call
it because `CreateOrUpdateStream` is idempotent and neither process may depend on
the other's startup order — the producer must not publish into a stream that does
not exist, and the consumer must not require the API to have booted.

`kannon-audit` is deliberately **not** added to `provisionEmbeddedJetStreams`
(`x/container/container.go:371-388`). That list creates streams with no `MaxAge`,
and a second definition of this stream's configuration is exactly what one
function holding it is meant to prevent.

Neither acquiring NATS nor configuring the stream may stop the API from serving.
The producer takes its connection with `TryNatsJetStream` and not
`NatsJetStream`: the latter exits the process, which is right for a worker whose
whole job is that stream and wrong for a process that carries somebody's mail
(`pkg/api/api.go:119-128`, `x/container/container.go:301`). A connection that
cannot be made is logged, no NATS recorder is installed, and the decisions stay
in the log the default Recorder writes — because enabling an audit trail must not
buy an API that crash-loops whenever NATS is slow to come up, which is the shape
of #365 and the requirement of #443. A failed `ConfigureStream` is logged for the
same reason and the Recorder installed anyway: the consumer configures the same
stream, so this having failed does not mean the stream never appears, and
whatever was published in the meantime is the unconfirmed loss a core publish
already costs.

When a publish fails the record goes to the log, through the recorder it
decorates. It never fails the operation: a register that is unavailable must not
stop the mail. So a record is never lost — it changes destination, and the
destination it falls back to is the log line ADR 0009 specifies.

### JSON end to end, and the Resource is stored as its segments

Everything that travels over NATS in Kannon today is proto, defined under
`.proto/kannon/` and generated into the public API. This does not, and the
deviation is deliberate: the payload lands in a `jsonb` column, so proto on the
wire would mean two schemas to keep aligned and a mapping between them, plus a
permanently public type for an event no external client will ever see. One shape
end to end, and evolving the record costs no migration.

The Resource is stored as `text[]` — its segments — and not as the joined path.
Not to dodge a `LIKE`, though it does: `resource LIKE 'domains/test.com%'` also
matches `domains/test.com.evil.com`, and a future read written the obvious way
would authorise the wrong Domain. The reason is that the segments are the
representation the model actually uses — comparison and matching "work on
segments, never on this" — so persisting the joined string would store the
display form of a value whose own type declares that form ambiguous.

### Six columns and a payload

```
id           text  primary key   utils.NewID("audit") → audit_<cuid2>
occurred_at  timestamptz         indexed
principal    text
resource     text[]
action       text
outcome      text                allowed | denied | no-principal
data         jsonb               Attribution, the Principal's Grants, the refusal reason
```

`occurred_at` is generated by the producer, not by the database: it is the
instant of the decision, and a consumer catching up after a stop would otherwise
date every record it writes to the moment it recovered.

The identifier is generated by the producer too, so that a redelivery — a `Nak`
after a database error, or a crash between `Insert` and `Ack` — inserts nothing
the second time (`ON CONFLICT DO NOTHING`). A natural key was rejected: any
tuple of the columns above collapses two genuinely identical operations in the
same instant into one row, which with a shared token and a front-end making two
parallel calls is ordinary rather than exotic. `cuid2` rather than a UUID because
it is what the repository already identifies things with
(`internal/utils/id.go`, `internal/apikeys/id.go`, `internal/batch/id.go`).

The Grants are in the payload because "denied" alone does not say why, and they
carry no personal data. The caller's address is deliberately absent: an IP is
personal data, Kannon retains one only under Tracking Mode Full, and an audit
table collecting one on every operation would contradict that posture in the
place least able to justify it.

One index, on `occurred_at`, which is what the cleanup needs — the same reason
`20260214120001_add_stats_timestamp_idx.sql` exists. Indexes on `principal` and
on a `resource` prefix wait for the read, which will state the shape it queries
by; `CREATE INDEX` is reversible and a speculative schema is not.

### Retention is the operator's, not Kannon's

`audit.retention`, a viper key, default `720h`.

The requirement as stated was "configurable in code". It is a viper key instead,
for a reason taken from the ADRs this one continues: an Audit Record holds an
Attribution, which ADR 0008 and ADR 0009 both call personal data, and the
retention of personal data is the operator's legal obligation rather than
Kannon's preference. The log line of ADR 0009 is governed by the retention an
operator already has for logs; moving the record into a table takes that lever
away, and a lever that only a rebuild can move is not one an operator has.
`stats.retention` is already a viper key over rows carrying email addresses, IPs
and user agents — the same obligation, already answered the same way.

### Off by default

`audit.enabled`, default `false`, read where the retention is
(`internal/audit/config.go`) so that the two processes cannot come to disagree
about whether the feature is on. The producer installs the NATS recorder only when
it is set; the consumer is a runnable, selected by `--run-audit` like every other
Kannon process.

Two reasons, and the second is the load-bearing one. A system whose posture is to
retain no personal data should not begin retaining some because a deployment was
upgraded. And with the audit trail on, the `api` process starts talking to NATS,
which it does not do otherwise: a default of off means no existing deployment
changes its behaviour, its network requirements or its failure modes because this
feature exists.

The consumer reads the same key, and with it unset it warns and stops rather than
starting (`pkg/audit/audit.go:57-73`). Nothing is ever published with the producer
off, so the worker would sit against an empty stream holding a database connection
and a consumer that record nothing; refusing to start says which half of the
configuration is missing, where a process that looks healthy and collects nothing
says the opposite. It returns `nil` and not an error, deliberately: every runnable
shares one errgroup, so an error would take the whole process — the API with it —
down over a feature that is off, and `kannon standalone` turns on every flag while
`audit.enabled` stays false, so an error would make standalone refuse to boot for
something nobody asked for.

Not running the consumer is not a way to disable anything: the records are still
published and still sit on NATS for seven days. Collection stops at the producer
or not at all, which is why the switch is there and not on the runnable.

### A consumer of its own

`pkg/audit`, `--run-audit`, the shape of `pkg/stats`: one consumer
(`kannon-audit-writer`) and the cleanup in the same errgroup. Hourly, one
`DELETE ... WHERE occurred_at < $1`, logging only when it deleted something.
Replicas are harmless — the statement is idempotent and each run deletes an
hour's worth.

A payload that cannot be parsed is `Term`ed and a database error is `Nak`ed,
which is what `pkg/stats` does and for the reason written there: a `Nak` on a
poisoned message reproduces the #396 hot loop (`pkg/stats/stats.go:170-172`).

The sweep is where the shape of `pkg/stats` is deliberately departed from: a
failed `DELETE` is logged and not returned (`pkg/audit/audit.go:155-166`). An
error out of the loop reaches the errgroup and takes the whole process down, and
this worker shares its process with the API whenever an operator co-locates them,
as `standalone` and the Kubernetes manifest both do. A register Kannon could not
prune for an hour must not become an interruption of service for somebody's
customers, and nothing is lost by waiting: the statement takes everything that has
fallen out of the window, so the next run takes what this one left. It is the same
judgement as the warning instead of a failed health check, in the other place an
audit problem could reach a process that carries mail.

Folding this into `stats` was tempting — it is already "the worker that consumes
events and persists them", and every deployment runs it, so the audit trail would
have needed no deployment change. Rejected: the runnable's name would then cover
two domains, and its configuration would read two retentions from two namespaces.

## Consequences

- **The record is an audit table and not an owner column on the resource.** The
  two answer different questions, and only the table can say who *read* the
  statistics last week, since a read leaves no row.
- **A deployment that enables the producer and forgets the consumer loses its
  records.** They are published, they sit on the stream, and they expire after
  seven days. The producer therefore logs a warning when the stream has pending
  messages *and* no consumers — the conjunction, so that it fires when the damage
  exists rather than on every boot. A warning and not a failed health check:
  making the API unhealthy over an audit problem would have a pod killed, turning
  a gap in the register into an interruption of service.
- **Erasing one person's records costs a `jsonb` query.** The Attribution stays in
  the payload, so a request to remove a named person is a scan with `data->>` and
  not a `DELETE` on a column. Accepted, and worth stating because it is the one
  operation a table holding personal data is most likely to be asked for.
- **The record names a credential, and only sometimes a person.** `principalID`
  is the constant `admin-token` (`internal/admintoken/admintoken.go:19`), so the
  `principal` column is that same value for every administrative operation. "All
  operations by X" is answerable for API Keys, and for people only through the
  Attribution in the payload. What fixes this is per-operator credentials, which
  ADR 0009 already owes.
- **With `nc.Publish` a misconfigured stream is an unconfirmed loss.** The publish
  succeeds locally whatever JetStream did with it, so the fallback to logs fires
  only when the connection itself is broken. Both callers of `ConfigureStream` and
  the pending-with-no-consumers warning are what stands in for an ack.
- **This is the first thing in the repository to cross NATS as JSON.** Stated here
  so that it reads as a decision and not as an oversight someone should correct
  towards proto.
- **`authz` gains an accessor it did not have.** `Resource.Segments()` exists
  because a record must store the path; it returns a clone, so a caller cannot
  reach into a Resource and change what a Grant was matched against.
- **The table is write-only, and nothing authorises reading it.** No API, and no
  Resource path naming it — a read surface needs both, and needs to decide whether
  an Audit Record is a Domain's or an operator's. Deliberately not designed here.
- **An Attribution is still never consulted.** It is recorded in a second place
  now, which does not change the property; a claim reaching a decision would still
  be an escalation, and the table is downstream of every decision it describes.

## Rejected alternatives

- **Writing the row straight from `Guard`.** One hop instead of two, no records
  lost, and the producer already holds a database handle. Rejected because it puts
  I/O on a layer that does none, adds a write to the request path of every
  operation, and still needs the cleanup to live somewhere.
- **An in-process buffer with batched inserts.** No NATS, no separate consumer,
  request path untouched. It loses whatever is in flight on every crash and every
  rolling restart — which is precisely when an audit trail is consulted.
- **A recorder as an explicit parameter of `Guard`, held by each service.** The
  dependency is visible at every call site and nothing is hidden in a context. It
  propagates through six constructors and their tests so that the domain layer
  can carry something it never uses, for a feature that is off by default.
- **A recorder demanded by every service constructor.** Same shape, stronger: a
  forgotten wiring would not compile. Rejected for the same reason, and because
  what it protects against — a silently absent recorder — is instead answered by a
  default that logs.
- **A package-level recorder, in the style of `slog.SetDefault`.** The smallest
  change, and honest about the fact that "this process records" is a deployment
  choice rather than a per-request one. Rejected because it makes every test that
  passes through `Guard` share one mutable global, and there are many.
- **An `slog.Handler` recognising the RBAC lines.** Zero changes to `authz`. The
  contract would be the text of a log message, the payload would have to be
  rebuilt from attributes, and the debug level of one line would govern whether
  records exist.
- **Authentication refusals in the same table**, with `principal` and `resource`
  nullable. The most security-relevant event, and two shapes in one table; see
  the Decision for why it waits for per-operator credentials.
- **Authentication refusals in a table of their own.** Two migrations, two repos,
  two retentions, for an event nothing reads yet.
- **Proto on the wire.** Consistent with everything else on NATS and versioned by
  buf. Two schemas to align and a public type for an internal event.
- **Proto end to end with typed columns.** Maximum discipline, and it gives up the
  cheap evolution that is the reason for `jsonb` while nobody yet knows what needs
  reading.
- **`js.Publish` with an ack.** A real guarantee, and a missing stream would show
  up in the logs immediately instead of swallowing records. Rejected for the RTT
  it adds to the request path of every authorized operation; `PublishStat` sets
  the precedent, and the two `ConfigureStream` callers cover the failure it would
  have caught.
- **`js.PublishAsync` with acks watched in the background.** Neither the latency
  nor the blind spot — at the cost of a pending-ack queue with its own sizing and
  shutdown policy. Reach for it if the warning ever proves insufficient.
- **The Resource as a joined `text` path with a prefix index.** One btree serves
  every prefix depth, which `text[]` cannot match. It stores the form the type
  itself calls ambiguous, and the natural query authorises the wrong Domain.
- **`ltree`.** Built for exactly this, with descendant operators and a GiST index.
  Its label alphabet excludes the dot, and a Domain name is required to carry one
  (ADR 0008) — so every path would have to be escaped on the way in and on the way
  out, which is two spellings of one Resource.
- **`bigserial` and duplicates accepted.** The simplest possible schema, and after
  a redelivery the table holds one decision twice with no way to tell that from
  two real operations.
- **The Attribution as its own column.** It is the field that answers "all
  operations by X", and in a column an erasure request is a `DELETE`. Kept in the
  payload to hold the schema to six columns; the cost is recorded above.
- **The caller's address in the payload.** With a shared token it is the only thing
  distinguishing two callers. It is a second piece of personal data under the same
  retention, and a precedent against the posture that keeps an IP out of every
  stat row not taken under Full.
- **The consumer inside the `stats` runnable, or inside `api`.** No new flag, and
  in the first case no deployment change at all. One runnable covering two domains
  under a name that says one; or an HTTP server doing a worker's job, with one
  cleanup loop per replica.
- **On by default.** Useful without being discovered. It starts retaining personal
  data on deployments that never asked, and hands the `api` process a NATS
  dependency on upgrade.
- **No switch at all, only the runnable's flag.** One place to intervene. The
  producer would publish regardless, so turning off the archive would not turn off
  the collection.
