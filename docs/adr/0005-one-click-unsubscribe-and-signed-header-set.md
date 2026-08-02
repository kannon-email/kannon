# ADR 0005: One-Click Unsubscribe, and the header set Kannon signs

## Status

Accepted (2026-08-02).

## Context

Kannon emits no `List-Unsubscribe` header of any kind. `batch.Headers` carries
`To` and `Cc` and nothing else, and `buildHeaders` writes a fixed set that has
no room for one. A sender wanting an unsubscribe has only the message body,
where the link is subject to click tracking like any other — mitigated since
#322 by the `data-no-track` attribute, which exists precisely for it.

That is no longer enough. Since February 2024 the large receivers require bulk
senders to offer one-click unsubscribe (RFC 8058) and to honour it promptly.
A sender that cannot emit the header is a sender whose mail is filtered.

ADR 0002 fixes the boundary this feature has to respect: Kannon executes
tracking instructions and does not store consent. Owning an unsubscribe — a
suppression list, a Kannon-minted token, an endpoint of our own — would put
Kannon on the other side of that line and turn it into a list manager.

`CONTEXT.md` defines **One-Click Unsubscribe** as the term for what this ADR
builds.

## Decision

### The endpoint belongs to the sender; Kannon carries it

The caller states the URL of **its own** endpoint. Kannon personalises it,
emits it, signs it, and does nothing else with it: it is never called, never
recorded, and never resolved against any state Kannon holds. There is no
suppression list and no Kannon-issued unsubscribe token.

This follows directly from ADR 0002. It also settles what the Tracker does with
an unsubscribe, which is nothing: the URL travels in a header, and the click
rewriter only ever touches `<a>` tags in the body, so the property holds by
construction rather than by a rule someone has to remember.

### One-click `https` only — no `mailto` branch

RFC 2369 allows a list of URIs and a `mailto:` fallback. Kannon accepts one
`https` URL and always emits both headers:

```
List-Unsubscribe: <https://sender.example/unsub?u=...>
List-Unsubscribe-Post: List-Unsubscribe=One-Click
```

Emitting `List-Unsubscribe-Post` is an assertion about someone else's server —
that it answers `POST` with body `List-Unsubscribe=One-Click` and unsubscribes
with no further confirmation. Kannon cannot verify it, so the assertion has to
be made by the party that knows, and it has to be unmissable. Rather than a
`one_click` boolean beside a general-purpose URL, the field itself *is* the
assertion: it is named `one_click_unsubscribe`, its type is
`OneClickUnsubscribe`, and its documentation states the contract. There is no
way to supply a URL here while meaning something weaker.

The failure this avoids is asymmetric. A missing `List-Unsubscribe-Post` fails
visibly — the client opens the URL in a browser and the recipient completes the
unsubscribe by hand. A present-but-unhonoured one fails silently: the recipient
presses the button, believes they are unsubscribed, is not, and reports the
next message as spam.

### Batch level only

No Domain default, no Recipient override. Kannon is a transactional sender:
a Domain-wide default would attach an unsubscribe to password resets, receipts
and OTPs, where it offers a recipient something the sender cannot honour. A
Recipient-level override would add nothing that personalisation does not
already give.

This is a deliberate asymmetry with the three-level cascade of **Tracking
Policy** (ADR 0003), and the two are different in kind: a Policy is a permission
that is meaningful to narrow by degrees, this is an operational instruction that
is meaningful only where a Batch's intent lives.

### Personalised per Delivery, percent-encoded

The URL is a template. `{{ field }}` placeholders are substituted from the
Recipient's fields, as subject and body already are, and the address of the
Recipient is injected as an `email` field so that the common case needs no
per-row data at all. The injection is a **default**: a caller that supplies its
own `email` field keeps its value, because silently overriding a value the
caller passed would change the meaning of templates already in production. It
is injected when the message is rendered rather than at intake, so nothing is
duplicated into every Pool row.

Substituted values are **percent-encoded**, unlike everywhere else. The body is
left alone because the context of a placeholder there is undecidable — it may be
prose, an attribute, or a URL — while here the context is known by construction.
Without encoding, `mario+rossi@example.com` reaches the endpoint as
`mario rossi@example.com`, and a value containing `&` injects a parameter into
someone else's URL. The contract is therefore that callers pass raw values.

### An unresolvable URL refuses the Recipient, and never ships broken

`ReplaceCustomFields` leaves an unmatched placeholder in the string verbatim.
For a body that is cosmetic; for a DKIM-signed one-click header it is the worst
outcome the feature can produce — a receiver told the destination is
authenticated, a recipient pressing a button, and a `POST` to a URL with braces
in it.

Two checks, at the two levels where the fault actually sits:

- **The call fails** (`INVALID_ARGUMENT`) if the stated template is unparseable
  or not `https`. That is a fault in the request, not in one row of it.
- **The Recipient is Rejected**, with reason `unsubscribe_url_unresolved`,
  if its own fields leave a placeholder unresolved. One bad row does not fail a
  send of thousands, and the caller learns which rows in the send response.

The Builder keeps a backstop: a URL that still fails to resolve there causes the
two headers to be **omitted** for that Delivery. A message without an unsubscribe
is a deliverability cost; a signed and broken one is a complaint.

### A fixed signed header set, oversigned, doubled only where it earns it

`signMessage` built `h=` conditionally, appending `Cc` only when present. It now
signs a fixed list, whether or not each header exists on the message:

```
From:From:To:To:Cc:Cc:Subject:Subject:Message-ID:Message-ID:
List-Unsubscribe:List-Unsubscribe:List-Unsubscribe-Post:List-Unsubscribe-Post
```

RFC 6376 §5.4 makes both halves of this explicit. A signer "MAY contain names of
header fields that do not exist when signed; nonexistent header fields do not
contribute to the signature computation", and the purpose is stated outright:
"By 'signing' header fields that do not actually exist, a Signer can allow a
Verifier to detect insertion of those header fields after signing." §5.4.2 adds
that multiple instances are signed bottom-up, which is why naming a header once
does not protect a message that already has it — a second copy prepended in
transit stays outside the signature while remaining the one a client reads
first. §8.15 covers the threat under "Attacks Involving Extra Header Fields".

Naming every header twice would be the thorough reading, and it was rejected.
Oversigning is **not** common practice: real-world signatures from ordinary
corporate senders list each name once, including `From`, and the public advice
recommending it reads as advocacy for something not yet widely enabled. For
`From`/`To`/`Cc`/`Subject`/`Message-ID` the fixed list already buys protection
against insertion where the header is absent, and the second listing would buy
a divergence from what everyone else emits.

The two `List-*` headers are doubled because there the threat is specific and
the payoff concrete: a second injected copy redirects an unauthenticated `POST`
to an attacker's endpoint, which is the exact attack RFC 8058's signing
requirement exists to prevent.

## Consequences

- **Every outgoing message's DKIM signature changes**, including mail with no
  unsubscribe: `h=` is now fixed rather than derived from what is present. The
  signature stays valid; what changes is that adding any of these headers in
  transit now breaks it.
- **Forwarders that add `List-*` headers will break the signature.** A message
  relayed through a list expander that inserts its own `List-Unsubscribe` will
  fail DKIM at the final hop. Such a path usually breaks DKIM anyway — footers,
  subject tags — and re-signs or ARC-seals, so the marginal cost is small, but
  it is a real narrowing and it is accepted knowingly.
- The `email` field becomes resolvable in **subject and body too**, where today
  `{{ email }}` renders as literal braces. This is a visible behaviour change
  for anyone who wrote that placeholder and never passed the field.
- No migration. The declaration is persisted as a key in the existing `headers`
  JSONB column on `messages`; rows written before this change simply lack it.
- `rejected_recipients` gains a value. Callers were already told to treat an
  unrecognised reason as a refusal of unknown cause, so this is additive.
- **Kannon still cannot tell whether an unsubscribe was honoured.** It does not
  call the endpoint and keeps no list, so a sender that ignores its own
  one-click POSTs looks identical to one that acts on them. That is the sender's
  obligation, not something this ADR can enforce.
- A single definition of "the fields available to this Delivery" has to be
  shared by the intake check and the renderer. If they drift, intake accepts a
  Recipient the Builder cannot resolve, or refuses one it could.

## Rejected alternatives

- **A generic custom-header map.** Cheap, and wrong for this: Kannon applies
  semantics to these headers — personalisation, encoding, validation, signing —
  and semantics hung off an untyped map key is invisible to the caller and
  impossible to validate.
- **A `one_click` boolean beside a neutral `unsubscribe_url`.** Makes the
  strongest claim in the message the easiest field to leave at its default.
- **Supporting the `mailto:` fallback.** Requires the sender to run and parse an
  inbound mailbox, and the receivers that matter want one-click. Reintroducible
  later without a wire break, which is why the field is a message and not a
  string.
- **Auto-excluding body links that match the header URL from click tracking.**
  Considered, then dropped: a one-click `POST` endpoint and a footer's
  confirmation page are different URLs by construction, so the match would find
  nothing in the common case. Where a sender genuinely serves both from one URL,
  `data-no-track` already covers it.
- **A Domain-level default unsubscribe URL.** Silently stamps an unsubscribe on
  transactional mail.
- **Emitting the header with placeholders unresolved.** Produces exactly the
  authenticated-but-broken one-click this feature exists to avoid.
- **Percent-encoding substitutions everywhere.** Would render `{{ name }}` in
  prose as `Mario%20Rossi`.
