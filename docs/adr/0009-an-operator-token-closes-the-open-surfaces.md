# ADR 0009: An operator token closes the open surfaces, and names who asked

## Status

Accepted (2026-08-04). ADR 0008 supplies the authority model this token
resolves to a Principal of; ADR 0010 settles whether the record this ADR
specifies is also persisted, and under what retention.

## Context

ADR 0008 built the authority model and put a `Guard` on every operation of
the Admin API and both Stats APIs — and on the Mailer API's send, the one
surface that already authenticates a credential of its own. It authenticated
none of the other three, and nothing else does either, so their guards
protect nothing: `CreateDomain` and `CreateAPIKey` are reachable by anyone
who can reach the listener, which leaves the Mailer API's own authentication
protecting nothing — mint a key and the send is perfectly valid.

What is wanted eventually is per-operator credentials with Grants of their
own — a Domain administrator who is `admin` on `domains/theirs.com` and
nothing else, which the model of ADR 0008 already expresses and which
Attenuation already narrows. That needs issuance, storage, rotation and an
API to manage it, none of which exists.

The same architecture decides who may name a person. The intended deployment
puts a closed-source UI in front of Kannon core: that UI has its own people
and its own sessions, and Kannon has neither — it knows only Principals
resolved from credentials, and naming who asked is an unverifiable claim
gated on the `attribute` Action (ADR 0008). The credential such a UI holds is
therefore the only thing in the system with people to name.

This ADR settles what closes the surfaces, what it may not be allowed to
become, which credential may name a person, how the name reaches Kannon and
what is done with it.

## Decision

### One configured secret, resolving to `admin` on the root

`api.admin_token` — settable as `K_API_ADMIN_TOKEN` — is a single shared
secret. A request presenting it authenticates as a Principal holding one
Grant, `admin` on the root: every Domain, every resource Action, and Domains
that do not exist yet. Nothing narrower would serve these surfaces, since
`CreateDomain` is an Action on the `domains` collection and the Domain an
operator is about to create cannot be named in an Anchor beforehand. Every
operation changes its answer for a caller that does not have the token, and
none changes it for a caller that does.

The Principal's identifier is `admin-token`, naming the *credential* rather
than a person, because that is all a shared secret can honestly say. It is
the property that makes this an interim answer: the record of an operation
can report that a holder of the token acted, never which holder.

It is also the credential a front-end holds, and so the one credential with
people to name. `admin` holds `attribute` (ADR 0008), `sender` holds none of
it, so a request presenting this token may carry a claim naming who asked and
a send key can never make one. The secret itself still names nobody — a claim
belongs to one request, never to the credential.

### A header of Kannon's own, not `Authorization`

The token travels in `X-Kannon-Admin-Token`. The Mailer API already spends
`Authorization` on HTTP Basic `<domain>:<key>`, and one header carrying two
credential schemes is a proxy rule or a log filter away from a send key
being read as an admin token — or from a token being forwarded to the
surface that authenticates the other kind. Separate headers make the
mistake unavailable rather than merely unlikely.

Both sides of the header live in `internal/authzconnect`, handler and
client, so nothing else in the codebase spells the name. A caller that spelt
it itself would keep compiling after a rename here and start failing
authentication instead.

### Refused at the interceptor, as `unauthenticated`

A request without the token, or with the wrong one, is refused before the
operation runs, with `CodeUnauthenticated`. The alternative — install no
Principal and let `Guard` answer `ErrNoPrincipal` — would reach the caller
as `permission_denied`, collapsing "your credential was absent or wrong"
into "your credential may not do this". Those are different problems with
different fixes, and the code is what a client acts on.

A wrong token and no token at all are one error on purpose: the refusal must
not tell a caller whether what it presented was recognised as a token.

### Missing token stops the boot

A process started with `--run-api` and no configured token refuses to boot,
naming both spellings of the key. A process serving an Admin API that answers
`unauthenticated` to everything is not degraded, it is broken: a boot that
fails says so, while one that succeeds and serves nothing usable costs an
operator a debugging session before it tells them the same thing.

The check is gated on the API runnable. A deployment running only the
dispatcher serves none of these endpoints, and requiring the secret there
would put it on hosts that have no use for it.

### The health service stays open

`HZService` discloses no tenant data and is polled by probes that carry no
credential. It is mounted without the interceptor, deliberately.

### The claim travels in a header of its own

`X-Kannon-Attribution`, read by the same interceptor that authenticates the
token, on the Admin API and both Stats API versions.

Not folded into the credential's header, because it is not a credential: it is
unverifiable, it confers nothing, and a request carrying it is authenticated
exactly as far as one without it. The two also have different lifetimes — the
token is configured once for a deployment, the claim belongs to one request —
and one header holding both would put a per-request value in the place
operators have learnt to treat as a secret.

The claim is applied to the Principal *after* authentication, so it can only
ever reach a `Guard` that then requires `attribute` on the Resource. The check
is unchanged, and there is no path that installs a claim without it.

### A malformed claim is refused, not dropped

With `invalid_argument`, which is neither of the refusals already in the
system: the credential is right and the operation permitted, and what arrived
wrong is the claim — the caller's to fix. Dropping it would be worse than
refusing, because the header exists to put a name in a record: a front-end
whose claim was silently discarded would go on believing one was recorded.

What is checked is what a record can hold, and nothing about the person —
there is nobody to check against, which is ADR 0008's Attribution entire. A
claim must be non-empty, at most 256 bytes, valid UTF-8, and free of control
characters:

- 256 because RFC 5321 already caps an address there, so nothing honest is
  refused, while a header of unbounded length would otherwise become a log
  line of unbounded length on every operation it accompanies.
- UTF-8 because a header carries arbitrary bytes and a Postgres text column
  will not take them. Refusing at the boundary beats an operation failing at
  the far end of the request for a reason no caller could read.
- No control characters because they are either a caller's bug or an attempt
  to forge the structure of the record the claim is about to appear in.

Surrounding whitespace is trimmed, for the reason the token's is: a value
copied through a config field or a header arrives padded often enough that
treating the padding as part of the name would record two spellings of one
person. One consequence of the transport is not Kannon's to decide — the HTTP
layer strips the surrounding whitespace of a header value, so a claim of
nothing but spaces arrives as no claim at all. An empty header is therefore
treated as no claim rather than as a claim naming nobody, which is also what
most client libraries need: they cannot tell an unset header from an empty
one, so the two must mean the same thing.

### Recorded at the `Guard`, as a log line at info

At the `Guard`, once the claim is entitled and before the operation runs: an
`attributed operation` log line carrying the authenticated Principal's
identifier, the claim, the Action and the Resource.

Before rather than after, because what is being recorded is who asked, which
is settled by then. A record written only on success would be a record of
outcomes, and one written after the fact is missing exactly when a process
dies mid-operation.

At info rather than debug, because it holds personal data: an operator who has
to answer *what was done in this person's name* cannot be asked to reproduce
the request with debug logging on. The RBAC line every check writes stays at
debug, so the two remain distinguishable — one is a decision trace, the other
is a record. A log line is also the smallest thing that discharges *recorded*:
it asks for no schema, and its retention is one an operator already keeps.

What is left open is only whether the record is *also* persisted — a store of
its own, with a retention of its own — which is ADR 0010's, and is a retention
decision about personal data before it is a schema decision. The line above is
emitted either way. What this ADR fixes is that a claim is recorded, what the
record carries, when it is taken, and at what level it is written.

### The Mailer API does not read the header

The header is read only on the surfaces the admin token authenticates. A send
key resolves to `sender`, which holds no `attribute`, so reading it there
could only turn every attributed send into a refusal. A front-end that wants
a send attributed needs a credential that may attribute — the per-operator
work this ADR defers — rather than a header on this surface.

## Consequences

- **This is a breaking change for every existing deployment.** The Admin API
  and both Stats APIs stop answering requests that carry nothing, and a
  deployment that runs the API must be given a token before it will start
  again.
- **Revocation is a restart.** The token cannot be revoked, rotated per
  caller or narrowed; changing it invalidates it for everyone holding it.
  This is acceptable only because the alternative it replaces is no
  authentication at all, and it is the first thing per-operator credentials
  fix.
- **The record names the credential, and the name a request carried.** Every
  administrative act is recorded against `admin-token`, and anything that
  needs to know *which person* acted needs credentials this token cannot
  become. When a request carries a claim, the record holds that name too — an
  identity that was authenticated and one that was asserted, side by side and
  never confused.
- **The guards can be covered through the API.** The interceptor refuses at
  the edge, so a test can present a wrong token, or none, and watch an
  operation not run: the refusal path is exercised through the handlers
  rather than only as a table over `Can`.
- **Attribution has a producer.** A claim can be presented, entitled, refused
  as malformed and recorded, so the `attribute` Action, the `Guard` that
  requires it and the Principal that carries a claim are reachable end to end.
- **Every holder of the shared token can name anybody.** The claim is
  unverifiable by construction, which is the hazard ADR 0008 names when it
  makes an Attribution a claim rather than a fact. What bounds it is that
  `admin-token` appears beside every claim, so a false name cannot hide who
  acted; a deployment that wants no claims in its records sends no header.
- **Per-operator `admin` credentials will inherit the Action.** The cost and
  the place to withhold it are ADR 0008's, whose intended vocabulary is
  documented — none of it holding `attribute` — before it is seeded.
- **The "recorded, never consulted" property is testable, and still easy to
  erode.** A claim reaching an authorization decision would let a caller
  choose its own authority, and claims now exist in requests, so it is a
  mistake somebody is in a position to make.
- **The seam holds for what replaces it.** `admintoken.Token.Authenticate`
  answers a Principal, and nothing downstream knows how it was obtained. A
  credential store resolving per-operator Grants replaces the inside of that
  method and one interceptor; no service, no Guard and no Role changes.

## Rejected alternatives

- **`Authorization: Bearer <token>`.** Standard, and it would have needed no
  new header. Rejected because it puts two unrelated credential schemes on
  one header name across services that trust them differently — the
  Mailer API resolves a Domain-bound sender from it, these surfaces resolve
  the root.
- **An escape hatch that serves these surfaces unauthenticated.** A flag
  restoring open access would preserve, in a supported form, the exact
  configuration this ADR exists to remove — and its existence would be read
  as a promise not to remove it later.
- **Serving the surfaces while unconfigured, refusing every request.** It
  keeps the process up and makes the failure look like an authorization
  problem at each call site, which is where an operator will then look for
  it. The configuration is what is wrong, and boot is when it is read.
- **Per-operator credentials now.** The right end state, and it is what the
  model of ADR 0008 was built for. It needs issuance, storage, rotation and
  a management API — a body of work that would keep the open surfaces open
  for the whole of its duration.
- **Storing the token hashed, as API Keys are.** API Keys are minted by
  Kannon and can be hashed at the moment they are shown once. This one is
  written by an operator into a config file or a Secret, so Kannon must
  hold what it was given; the comparison is constant-time instead.
- **A second Role of pure shape, `at(attribute)`, granted alongside `admin`.**
  Weighed and rejected where the catalogue is decided (ADR 0008).
- **A flag on the Principal rather than an Action.** It would give up naming a
  person only within part of the tree, which is the property that lets a
  Domain-scoped front-end exist — and is why `attribute` is an Action carried
  on a Resource (ADR 0008).
- **Folding the claim into the admin token's header.** Puts a per-request,
  unverifiable value on the name operators treat as a secret, and invites
  reading the claim as part of the credential.
- **Dropping a malformed claim and serving the request.** The failure would be
  invisible in exactly the place it matters, which is the record.
- **Recording every operation at info, attributed or not.** In one stream at
  one level there is nothing to select on, so it buries the operations that
  name a person among the ones that do not, and it puts the volume of an
  access log inside the authorization layer. What is rejected is the log
  stream and not the coverage: ADR 0010 does record every decision, in a table
  where a column selects what one stream at one level cannot, and it answers
  the volume there — one row per API call, not one per Delivery.
- **Verifying the claim.** Impossible in principle, not merely unimplemented
  (ADR 0008).
- **Reading the header on the Mailer API.** Every attributed send would be
  refused, since `sender` holds no `attribute`; a surface that cannot honour
  the header should not read it.
