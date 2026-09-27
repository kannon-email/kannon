# ADR 0014: Bounce return paths are signed

## Status

Accepted (2026-09-27).

## Context

An asynchronous bounce reaches Kannon as a DSN addressed to the envelope sender
of the message it is about. That return path is how the inbound SMTP server
knows which Delivery the DSN concerns: it names the Recipient and the Batch, and
the server turns what it names into a Bounced event, on which the Dispatcher
drops the Delivery and the stats worker records a bounce.

The return path was `bump_<base64(recipient)>+<batch id>`. Nothing in it was
secret — the Batch ID travels in every message's headers, and base64 is an
encoding — and nothing about an SMTP connection authenticates the DSN it
carries: port 25 is open to anyone, by necessity. So anyone who had received a
single message of a Batch could forge a DSN for any other Recipient of it,
cancelling a queued or retrying Delivery, and could record a bounce against any
address of any Domain, which a sender feeding Kannon's bounces into a
suppression list would act on.

## Decision

The return path carries an HMAC-SHA256, truncated to 128 bits, over the
Recipient and the Batch ID, keyed by an operator-configured secret:
`bump_<base64(recipient)>.<mac>+<batch id>`. The inbound SMTP server reports a
DSN only when the return path it is addressed to verifies, and answers a forged
one exactly as it answers a real one, so the sender learns nothing from the
refusal. `internal/returnpath` is the only code that builds or reads one.

The secret is `bounce.secret`. The Dispatcher, which builds Envelopes, and the
inbound SMTP server, which reads return paths, both need it and refuse to boot
without it; no other process is given it. It must be at least 32 characters.

## Consequences

- Upgrading requires setting `bounce.secret` on every process running either
  runnable, the same value on all of them.
- Bounces of messages sent before the upgrade, or before the secret was
  changed, arrive on a return path that does not verify and are not reported.
  There is no grace period that accepts unsigned return paths: one would keep
  the forgery open for as long as it lasted.
- The secret rotates only by replacing it, at the cost above. A list of secrets
  to verify against would make rotation lossless, and can be added without
  changing the wire format.
- A MAC is not a verdict on the DSN itself: a recipient's own mail server can
  still report their Delivery as bounced, which is what it is for.
