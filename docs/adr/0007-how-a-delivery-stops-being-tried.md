# ADR 0007: How a Delivery stops being tried, and what it becomes

## Status

Accepted (2026-08-03).

## Context

Three faults in the retry path were reported together as #378: a retry cap that
nothing could reach, a `permanent` flag suspected of lying, and Pool rows
stranded in an in-flight status with nothing to recover them. Two of the three
turned out not to be what the report said, and the third turned out to be larger.

**The `permanent` flag is correct as it stands.** #433 rewrote what it means
(`CONTEXT.md`, *Bounced*) so that it follows the SMTP reply class on both the
synchronous and the asynchronous path — which is what the code already did. The
report was written against the previous wording, and the fix it proposed would
now be a regression against a spec that is one day old.

**The cap existed too**, as `maxRetry = 10` inside `internal/envelope`, read once
to set the Envelope's `ShouldRetry` flag. It bounds the only loop it can see: a
Delivery that reaches SMTP and keeps being answered transiently. It cannot bound
the loop #403 opened when it stopped stranding claimed Deliveries on dispatch
failure — a Delivery whose Envelope can never be *built* is now rescheduled
forever, because no Envelope means no `ShouldRetry` and so nothing that can ever
bounce. This is reachable without any crash: `GetSendingData` joins Batch,
Template and Domain, there is no foreign key from `messages.template_id`, and so
deleting a Template orphans every pending Delivery of every Batch that
referenced it. Such a Delivery is retried with a doubling backoff until its next
attempt is years away, and the sender is never told — its last stat is
`accepted`, permanently.

**The stranded row is the half of ADR 0004 that was never built.** That ADR
accepted the strand in as many words — "a rare stranded Delivery instead of a
systematic duplicate" — on the understanding that something would eventually
recover it. Nothing does.

Three questions therefore had to be answered together, because each one's answer
constrains the others: what does a Delivery become when Kannon gives up on it,
what does giving up *measure*, and what may a recovery of a stranded row claim?

## Decision

### A Delivery no answer ever came back for is Failed, not Bounced

The discriminator between the two terminal failures is epistemic — it is about
what reached us, not about what happened. **A Bounce always carries a reply
code**, because a Bounce is a remote mail system having spoken; 5xx or 4xx,
synchronous or from a later DSN, there is always a real code and `permanent`
always classifies it. **Failed is the absence of any reply at all**, and so
`StatsDataFailed` deliberately has no `code` field to fill in — only a `reason`,
like Rejected.

Neither of the two existing outcomes could take the case honestly. Rejected is a
judgement Kannon passes on the Recipient *before* attempting anything; Failed is
the absence of an answer *after* attempting, repeatedly. And forcing it into
Bounced would mean inventing `permanent` and `code` values for an event where no
reply class exists — re-opening the exact ambiguity #433 had just closed.

The wire slot already exists: `StatsDataFailed` has occupied field 3 of the
`StatsData` oneof since the proto was written, unemitted and unmapped. Reviving
it costs a `reason` field on a message nobody has ever sent, so nothing is
wire-breaking. Persistence and hourly aggregation come for free — the Stats
worker consumes the `kannon.stats.*` wildcard and `stats.type` is a varchar, not
an enum — so the new outcome needs no migration.

### The Retry Budget is a span of time, not a count of attempts

A Delivery is retried for as long as the next retry would still fall inside a
window measured from the moment its Batch asked for it to be sent, and always
gets at least one attempt however late it is offered one. `RetryWindow` defaults
to **24 hours**.

Two reasons, and the second is the one that matters.

The window makes the give-up decision fall on the **reschedule**, not on the
next claim. Both paths that hand a Delivery back to the Pool — the Dispatcher's
own dispatch failure and the error-stat consumer — run through `Reschedule`, so
one predicate on the entity governs every retry in the system. Under a count,
the give-up lands at the *claim after* the last one: a Delivery rescheduled to
`original + 34h` stays alive for a day and a half before anything notices it is
over, and the sender learns of the outcome long after Kannon decided it.

A span is also indifferent to *what* consumed the attempts. This matters now
that the attempt tally is bumped by three unrelated events — a transient SMTP
answer, a dispatch-side failure, and a reclaim of a stranded row — so under a
count an outage of Kannon's own would spend a sender's chances of delivery. A
span cannot:
every Delivery gets the same 24 hours whoever burned the attempts. The tally
survives, but only to shape the backoff curve.

24 hours is not a new behaviour. Under `DefaultBackoff` the tenth retry is
scheduled at `original + 2m·2⁹` = 17.1h and the eleventh would fall at
`2m·2¹⁰` = 34.1h, so a 24-hour window admits exactly the retries `maxRetry = 10`
admitted and refuses exactly the one it refused. It also coincides with the
`MaxAge` of the `kannon-sending` stream, past which an Envelope cannot survive
anyway.

The window is a wiring point threaded through the Container, alongside the
`BackoffPolicy` it is inseparable from, and **not** a viper key — ADR 0001's
reasoning applies verbatim, and #378's request for an operator knob is declined
for now. A raw count exposed to operators can re-create the very bug being fixed
here (a cap of 60 against a doubling curve is a Delivery retried for centuries);
a window cannot, but no operator has asked for either, and #366 remains its home
if one ever does.

### A reclaim recovers a stranded Delivery; it never terminates one

A row that has sat in an in-flight status past its threshold is handed back to
the Pool with its attempt counter bumped. The reclaim emits no outcome and makes
no claim about what happened, because it genuinely does not know: the Envelope
was published, the SMTP transaction may have completed in full, and the mail may
be in the recipient's inbox. Only the Retry Budget terminates a Delivery — one
place in the whole system decides that it is over.

Recovering rather than closing the books accepts a real risk of a duplicate
email. The send guard cannot suppress it: the guard is keyed on the stream
sequence of the message being delivered, and a recovered Delivery is published
afresh under a new sequence, which is the same thing that makes a legitimate
retry send again. That risk is accepted, and it is the same judgement ADR 0004
made twice — the guard *fails open* precisely because "a duplicate is a smaller
failure than a batch that never leaves". Kannon is a transactional sender whose
core traffic is password resets and receipts; for that traffic a missing email is
far worse than a second copy. What ADR 0004 rejected was a *systematic*
duplicate; a reclaim on an hours-long threshold produces a rare one, which was
not on the table then.

The threshold is measured from a new `claimed_at` column rather than from
`scheduled_time`. `PrepareForSend` does not touch `scheduled_time`, so under a
backlog — exactly when a reclaim matters — that column can be hours in the past on
a row claimed one second ago, and a reclaim reading it would reset live sends and
duplicate them in bulk. An honest column also answers "how long has this been in
flight" for whoever is looking at the table, which is the blindness the reclaim
exists to end.

One mechanism covers every in-flight status, with a per-status threshold, but it
is invoked by the worker that owns that status — the Dispatcher reclaims
`sending`, the Validator reclaims `validating`. A reclaim run from outside both
would have to write to a status it does not own, which is the arrangement
ADR 0004 declined to create when it kept the SMTPSender out of the Pool. The
thresholds differ by two orders of magnitude, because so does the plausible time
in flight: `sending` uses
**1 hour**, taken from `sendGuardTTL`, which is already defined as long enough to
outlast every redelivery window plus the replacement of a dead worker — the
codebase's existing answer to "how long can an Envelope still be alive". A
`validating` row is stuck the moment it is a few minutes old, since the
Validator's whole cycle is bounded at ten seconds, and uses **5 minutes**.

## Consequences

- A new customer-visible outcome. `Failed` appears in stats and in the stats API
  for a Delivery that previously produced no terminal event at all.
- A `Failed` Delivery may in fact have been delivered. Kannon claims only that it
  never learned of an outcome, and `CONTEXT.md` says so where the term is
  defined. Any bounce-rate or deliverability figure built on top must not read it
  as evidence the recipient did not receive the mail.
- `ExponentialBackoff` needs no maximum. The window makes the `math.Pow` overflow
  at ~63 attempts unreachable by construction, so the fix stays one mechanism
  rather than two.
- One dbmate migration: `claimed_at` plus a partial index for the reclaim
  predicate.
- A recovered Delivery can be delivered twice. This is a deliberate widening of
  the trade ADR 0004 took, in the same direction.
- Deleting a Template still orphans the pending Deliveries of every Batch that
  referenced it. The Retry Budget now closes those Deliveries out as Failed
  within 24 hours instead of leaking them forever, but the missing foreign key is
  an upstream cause and is tracked separately.

## Rejected alternatives

- **Fixing the `permanent` flag as #378 asked.** It is not broken; the report
  predates #433's re-specification, and the change would break the e2e
  assertions that pin the current meaning.
- **Terminating an unbuildable Delivery on the first failure**, via a typed
  "cannot build" error. A missing Template row is indistinguishable from one that
  is about to be recreated — `template_id` is a caller-chosen string, so
  recreating it makes the join resolve again — and from replica lag, or from
  pointing at the wrong database. Retrying is right; only the bound was missing.
- **A separate budget for internal failures**, so that Kannon's own faults
  cannot spend a sender's delivery chances. Superseded by measuring the budget in
  time, which achieves the same end with no second counter, no second threshold
  to tune, and no migration. Under a count it would have bought a distinction the
  architecture already draws for free: whoever exhausts the budget is the one
  holding the evidence, and emits the outcome it can prove.
- **Recovering a stranded row event-driven instead of on a timer**, off the
  JetStream advisory for a consumer exceeding `MaxDeliver`. It does not cover the
  case ADR 0004 created, which is the main one: the worker that loses the send
  claim *acknowledges* the message, so the delivery succeeds as far as the server
  is concerned and no advisory fires. Nor does it cover expiry by `MaxAge`. A
  timer is needed regardless, and one mechanism is better than two.
- **Reaping on `scheduled_time` instead of a new column**, as #378 proposed. It
  reads as "claimed long ago" only when there is no backlog, and resets live
  sends when there is.
- **A reclaim that terminates the row it finds.** It would have to assert that a
  Delivery was not transmitted when it may well have been, and it would put the
  give-up decision in a second place.
