# ADR 0008: The authority model — typed Roles anchored on the Resource tree

## Status

Accepted (2026-08-04). ADR 0009 authenticates the surfaces this model
protects; ADR 0010 records what it decides.

## Context

When this ADR was written, Kannon had no authorization. `adminapi` and both
`statsapi` versions were mounted with no authentication at all
(`pkg/api/api.go`); only `mailapi` authenticated, with HTTP Basic carrying
`<domain>:<key>` (`pkg/api/mailapi/mailer.go:361`).

Because `CreateDomain` and `CreateAPIKey` sat on that unauthenticated
surface, the authentication `mailapi` *did* perform protected nothing:
anyone who could reach the listener created a Domain, minted a key for it,
and sent with a perfectly valid credential. ADR 0003 had already recorded the
shape of this — "a Tracking Policy is only as strong as access to the Admin
API" — and deferred it. This ADR is that deferral coming due.

This ADR settles the model of authority and the decision procedure every
guarded operation runs through. It publishes no public API of its own:
authenticating the surfaces the model protects is ADR 0009, and recording
what the model decides is ADR 0010. `CONTEXT.md` §Access control holds the
resulting language.

Three requirements framed the design. First, a Role that can do everything
on every Domain. Second, room to extend today's Domain-bound API Keys so
that a key can be issued for reading statistics or administering one Domain
rather than only for sending — deferred (see Consequences), but the model
has to express it without changing shape. Third, surfaced by reviewing the
first cut of this ADR: the unit an operator issues must be a single *name*
fixed to a single *place*. "Domain administration" spans Templates, API
Keys, statistics and the Tracking Policy; a model in which that is four
separately attached grants turns every issuance into hand-assembly, and an
assembly mistake is a silent misconfiguration that surfaces only when the
fourth operation is attempted. Kubernetes and GCP converge on the same
answer — a role binds verbs to *kinds* of resources, and the binding
supplies the place — and the model below follows them.

A fourth requirement comes from above Kannon rather than from inside it. The
intended architecture puts a closed-source UI in front of Kannon core. That
UI has its own people and its own sessions; Kannon has neither, and knows
only Principals resolved from credentials. The UI therefore holds a
Principal with wide authority and hands on requests made by users who, as
far as Kannon is concerned, do not exist. Two things are wanted from that
arrangement: that the UI be able to act with one user's reach rather than
its own for a single operation, and that the record of an operation — who
created this API Key — name that user rather than the UI. They were
originally conceived as one thing: an `impersonate` permission letting a
Principal replace itself with a different, more specific one, provided the
replacement's Resources were a subset of the original's. They are two
mechanisms here, and the split is what inverts which half is dangerous.

## Decision

### Authority is a set of Grants — a Role name fixed to an Anchor — on a resolved Principal

A **Principal** is a value object produced by whatever authenticated the
request, never a stored record. Each authentication method populates one in
its own way — an API Key by looking it up, a signed token by reading its
claims — so what a Principal *is* does not depend on how it arrived. It
carries an identifier naming the credential it came from and a set of
**Grants**, each a Role name fixed to an **Anchor**. Its authority is their
union. A Principal describes authority and does not decide; `Can` decides.

The Principal carries Role *names*, not expanded rules. The expansion
happens at the moment of the check, against the catalogue. This matters
because a credential may outlive a change to what a Role means: an expanded
snapshot would freeze the semantics of every token already issued, so
widening or narrowing a Role would require finding and reissuing them.
Carrying the name means one edit takes effect everywhere at once.

`Can(principal, action, resource) bool` is a pure function. It takes no
`context.Context`, performs no I/O, and consults no repository. The
Principal travels from the authentication edge to the call site in the
context, but that is transport: the decision procedure never sees it.

### Resources are paths, and authority over a path extends to everything beneath it

A **Resource** is a hierarchical path:

```
domains
domains/<name>                      update = SetTrackingPolicy
domains/<name>/batches              create = SendHTML / SendTemplate
domains/<name>/templates/<id>
domains/<name>/apikeys/<id>
domains/<name>/stats                per-Delivery rows
domains/<name>/stats/aggregated     counters
```

Matching is **prefix domination**: a pattern reaches a Resource when it
equals it or a prefix of it, so authority on `domains/example.com` reaches
that Domain's Templates, Batches and statistics without naming them.

Prefix domination replaces the two-tier `System` / `Domain` scope that was
considered first. The domain-less operations that motivated a `System` tier
are only `CreateDomain` and `GetDomains`, and both are Actions on the
`domains` collection, which is simply a shorter path. One mechanism covers
what two would have.

Reach and power stay independent: the Role's rules bound what may be done
and to which kinds of things, and the Anchor bounds where. A Grant anchored
on `*` therefore confers unbounded *reach*, not unbounded *power* — a
`viewer` anchored on `*` sees everything and can change nothing.

Two things are deliberately inexpressible. Holding a path without what lies
under it: there is no non-recursive Grant. And subtraction: see below.

**Statistics nest rather than sit as siblings.** `stats/aggregated` is a
child of `stats`, so authority over the per-Delivery rows implies authority
over the counters. That implication is semantically true — anyone who can
read every event can count them — and the nesting makes the incoherent Grant
"detail but not aggregate" impossible to write, rather than writable and
unenforceable. The line is not one of granularity but of personal data:
`stats` carries `email` and, under Full, `ip` and `user_agent`, while
`aggregated_stats` carries only `(domain, timestamp, type, count)`. It is
the same line the Tracking Mode scale already draws, and under Anonymous the
aggregate is the *only* place an engagement event is recorded.

The cost is an admitted abuse of the hierarchy: a path normally denotes
containment, and the aggregate is a derived view rather than a contained
child. The implication earned is worth it.

### A Role is a named set of typed rules, completed by the Grant's Anchor

A **Role** is a set of rules, each pairing Actions with the *kind* of thing
they act on, stated relative to the Anchor of the Grant that places the
Role: `at(...)` names the anchored Resource itself, `on(kind, ...)` names a
child kind beneath it. The catalogue seeds two:

```
admin    at(create, read, list, update, delete, attribute)
sender   on(batches, create)
```

A rule's effective pattern under a Grant is the concatenation of the Anchor
and the rule's suffix, matched by the same prefix domination as everything
else — there is one matcher in the system. `on(templates, update)` anchored
at `domains/example.com` composes `domains/example.com/templates`, which
dominates every `templates/<id>` beneath it: item identifiers never appear
in rules, because domination already reaches them.

`admin` is Kubernetes' `resources: '*', verbs: '*'` — beneath its Anchor —
for free: one `at()` rule holding the whole Action vocabulary, extended to
every kind by domination, with no wildcard token anywhere in the rule.
Anchored on `*` it is the Role that can do everything on every Domain;
anchored on one Domain it is that Domain's owner. Same Role, two reaches.

**`attribute` is in that rule deliberately.** Naming a person is not an
administrative power over Kannon's resources; it is the capability of a
front-end that has people to name — and the credential such a front-end
holds is `admin` on the root (ADR 0009). The Action and the Role meet in the
same place whether or not the catalogue says so.

The alternative is a second Role of pure shape — `at(attribute)` — composed
as a second Grant beside `admin`, which would keep `admin` free of the
Action. Nothing would ever be issued that Role alone: it would exist only to
accompany `admin`, so the separation would be nominal while every front-end
credential became two Grants — the hand-assembly this ADR exists to remove.

Widening who may make a claim does not widen what anyone may do. An
Attribution is recorded and never consulted (below), so it cannot enter a
decision; the power it adds is over the record, and the record names the
authenticated credential beside the claim, so a false name cannot displace
the fact that a credential holder acted.

Scoping survives intact. `attribute` is an Action carried on a Resource like
any other, so `admin` on one Domain may name a person only within that
Domain: putting the Action in a Role gives up nothing of "permit attribution
only within some part of the tree".

Typed rules are what let one *name* span kinds. The intended vocabulary —
recorded here as documentation, deliberately not seeded until the grants
table gives anything a way to issue it — reads:

```
domain-admin     at(read, update)                    GetDomain, SetTrackingPolicy
                 on(templates, create, read, list, update, delete)
                 on(apikeys, read, list)             sees keys; cannot mint or revoke
                 on(stats, read, list)
template-editor  on(templates, create, read, list, update)
key-manager      on(apikeys, create, read, list, delete)
analyst          on(stats, read, list)               detail and, by nesting, aggregate
metrics-reader   on(stats/aggregated, read, list)    counters only — no personal data
viewer           at(read, list)                      everything beneath the Anchor
```

Two properties of this vocabulary are deliberate. Every rule holding
`create` also holds `read`, visibly, so a credential can always read back
what it just created. And composition replaces role multiplication: "a
domain administrator who also manages keys" is one credential carrying two
Grants on the same Anchor, not a third Role.

The cost of `attribute` living in `admin` is paid here, and accepted: a
per-operator `admin` credential inherits the Action and may write names into
records within its Anchor, which a human administering Kannon directly has
no use for. The place to withhold it is the vocabulary above — none of
`domain-admin`, `template-editor`, `key-manager`, `analyst`,
`metrics-reader` or `viewer` holds `attribute`, and issuing one of those
rather than `admin` is how an operator gets an administrator who speaks for
nobody.

`sender` holding none of it is the load-bearing half. A customer's API Key
may create a Batch and must not be able to claim somebody else asked for it,
so the catalogue's two Roles divide exactly on the line Attribution draws
below.

A root-scoped `provisioner` (`on(domains, create, list)`) is deliberately
absent: `create` composed at `domains` dominates every `create` beneath it —
the Batches and API Keys of every Domain — so an onboarding Role, if ever
needed, requires its own design rather than a line in this list.

### Anchors come in two kinds, and NewGrant refuses everything else

A Grant is issued on either the **root** — written `*`: the whole tree — or
a **Domain**: one (`domains/example.com`) or every (`domains/*`). Each Role
declares the kind its typed rules were written against — `on(domains, ...)`
composes only at the root, `on(templates, ...)` only beneath a Domain — and
a Role of pure shape, with `at()` rules only, accepts either kind.
`NewGrant` rejects a mismatch, and rejects everything that is neither kind,
loudly: *"anchor `domains` is not grantable; did you mean `domains/*`?"*.

The refusal is deliberate rather than defensive, for two reasons beyond
readability:

- Concatenating onto the wrong node does not produce an error, it produces
  a different *meaning*. `on(templates, ...)` anchored at `domains`
  composes `domains/templates` — not a dead path but the Domain whose name
  is literally `templates`, an alias no reader would spot. The anchor-kind
  check refuses the Grant before that can matter, and the dot rule on a
  domain name below removes the class entirely — belt and suspenders.
- One intent, one spelling. "Every Domain" is `domains/*`, and the wildcard
  shouts that the reach is unbounded, future Domains included. Admitting
  `domains` as a synonym would put two spellings of one authority in the
  grants table, and the equivalence would be a rewrite inside the one layer
  that must never normalise anything.

The `*` of an authored Anchor is structural, not textual: the root is
represented explicitly rather than as an empty pattern, so the zero value
of a pattern still covers nothing and a programming error keeps failing
closed.

### Allow-only: there are no deny rules

Authority is the union of a Principal's Grants and nothing ever removes
from it. "Everything except `vip.example.com`" is inexpressible.

Deny rules would buy that one phrase and cost a precedence order between
allow and deny, which is where authorization models stop being readable by
inspection and where their worst bugs live — a deny that does not fire
because a more specific allow outranked it. A model with no subtraction can
be verified by reading it.

### A closed Action vocabulary that never names what it acts on

`create`, `read`, `list`, `update`, `delete`, `attribute`. Six verbs, and
the set is closed: giving Kannon a new kind of thing to manage adds a path
segment and, where a Role needs it, a rule — never an Action.

Because the rule and the path already carry the kind, the verb must not.
There is no `create-template` that could be granted over API Keys, and
sending mail is `create` on a Domain's `batches` rather than a `send` of
its own: `CONTEXT.md` already defines a **Batch** as the aggregate one
Mailer API call creates, so a send *is* a creation, and a dedicated verb
would be the only type-bound one in the vocabulary.

`list` is separate from `read` because prefix domination makes a collection
and its items one Resource family. With a single `read`, knowing *which*
things exist could never be granted apart from inspecting them. Enumeration
discloses something different from inspection — which Domains exist is not
the same as what is inside one — and recovering that distinction costs one
word.

### Roles are code, not data

The catalogue is a static map in Go, seeded with exactly `admin` and
`sender`: the only Roles today's two Principal producers can resolve to —
the API Key adapter and the operator token of ADR 0009. A Role that nothing
can issue would be dead vocabulary, and would churn when the grants table is
designed; the intended vocabulary above is documentation until then.

Three reasons. The vocabulary becomes compiler-checked, so a rule cannot
name an Action that does not exist. Every change to what a Role may do
arrives as a reviewable diff, which for a security decision is the right
place for it to be seen. And the check stays a pure function with no
catalogue lookup, no cache and nothing to invalidate.

The catalogue can later grow a second, database-backed source behind an
interface without the Principal or `Can` changing. Until someone actually
needs a Role tailored per customer, it does not exist.

### A Domain's name must be canonical — and carry a dot — or none of this is sound

Path authority requires that a Domain have exactly one spelling.

When this ADR was written it did not have one. Nothing rejected a domain name
beyond its being empty, uniqueness was on the raw string
(`db/schema.sql:478`), and every key lookup compared it byte-wise
(`internal/db/apikeys.sql:18`). Two consequences, both fatal to the model
above:

- A domain name could contain `/`, which is the path separator. A Domain
  named `a.com/templates` makes `domains/a.com/templates` — a Grant that
  collides with a real path. A name containing `*` forges a wildcard.
  Since anyone could create a Domain, anyone could inject into the
  authority model.
- `test.com` and `TEST.com` were two Domains with two DKIM keypairs and two
  sets of keys, though DNS, SMTP and DKIM all consider them one mail domain.

The fix is a canonical domain-name type — lower-cased; rejecting empty,
`/`, `@` and `*`; and requiring at least one dot — that can only be
constructed through validation, and a Resource constructor that accepts
nothing else. The dot closes a subtler hole than the separator does: a
single-label name can equal a segment of the Resource tree itself —
`templates`, `apikeys`, `batches` and `stats` are all valid hostnames —
and a Domain so named turns a composed path into an alias for another
node. Real mail domains carry a dot; nothing real is lost.

Note what canonicalisation means here: the authorization layer must
**never** normalise. If it lower-cased domain names while two
case-differing Domains could coexist, the normalisation would itself be
the escalation, handing a Grant on `TEST.com` the other Domain's
Templates, keys and statistics. Canonicalisation belongs to the Domain, so
that authorization compares values that are already canonical and guesses
at nothing.

### Attenuation and Attribution are orthogonal, and only one of them needs an Action

Narrowing authority and naming a person are independent. A Principal can
narrow itself while the record still says the UI acted; it can name a person
while acting with undiminished authority. Fusing them into one permission
gated on one check protects the half that is safe and leaves the half that
is not unexamined.

- **Attenuation** — narrowing the Anchors of a Principal's Grants — is
  verified, and needs **no** Action. Giving up authority one already holds
  is always safe.
- **Attribution** — naming who asked — cannot be verified at all, and
  **requires** the `attribute` Action.

### Attenuation intersects, so widening is not a mistake that can be made

Attenuation computes the intersection of the requested Resources with those
already held, rather than validating a requested set and substituting it.
Under substitution, the subset check is the only thing standing between
derivation and privilege escalation — which makes it a check that can be
forgotten. Under intersection, a request for more than one holds yields less
rather than more. The failure mode becomes empty authority, which fails
closed.

Requested Resources must be **concrete paths**, without wildcards. This is
what keeps the operation cheap: "is this covered?" is answered by the same
matcher that answers authorization requests, so there is one piece of
matching logic in the system rather than a second one deciding inclusion
between glob patterns. Prefix domination is what makes concrete paths
sufficient — narrowing to `domains/example.com` still reaches that Domain's
Templates.

What narrows is each Grant's **Anchor**; the Role's rules travel unchanged,
so narrowing where authority lands never changes what it is. Typed rules
make one old hazard inexpressible — no Anchor names a kind, so no
attenuation can trade one kind's authority for another's — and impose one
restriction of their own: a typed Role does not narrow beneath the kind its
rules were written against. `template-editor` narrows to one Domain, never
to one Template, because beneath the Domain its rules would compose paths
that name the wrong things rather than smaller authority. The check is the
same Anchor-kind declaration `NewGrant` enforces above. A Role of pure
shape — `at()` rules only — narrows to any concrete path it covers, which
is what lets a front-end scope one request down to a single Template.

Attenuation returns the requested paths it dropped. Silent narrowing is
safe but hides mistakes: a front-end with a typo would see its user refused
rather than learn it asked for something it does not hold.

### Attenuation preserves identity

An attenuated Principal keeps the identifier of the credential it came from.
Narrowing changes what may be done and never who did it. This is what keeps
Attenuation from quietly becoming the other mechanism — if narrowing also
changed the identity, then the identity in the record would sometimes be
authenticated and sometimes asserted, with nothing to distinguish them.

### An Attribution is recorded and never consulted

The people an Attribution names exist only in the system that sent it.
Kannon cannot check that `alice@corp.com` is real, and no future version can:
there is nothing to check against. An Attribution is therefore an unverified
claim, recorded because the calling Principal is trusted to be honest about
it, and it never enters an authorization decision. It can no more widen what
a Principal may do than it can be verified.

The Principal that actually called is recorded **whether or not** an
Attribution accompanies it, so the record never leaves the question of who
acted unanswered, and an authenticated identity is never confused with an
asserted one.

### Naming a person is a power; giving up authority is not

Writing an arbitrary name into the record of who did what is itself
something not every credential should be able to do — an ordinary customer
key must not be able to claim a Batch was sent on someone else's behalf. So
`attribute` is an Action, carried on a Resource like any other, rather than a
flag outside the permission model. Keeping it inside costs nothing and yields,
unasked, the ability to permit attribution only within some part of the tree.

### Neither is called impersonation

In Kubernetes and in GCP, impersonation means acquiring *another principal's*
authority, which may be greater than or simply different from one's own.
Attenuation is the opposite: authority can only shrink, and never moves
sideways. Using the word would mislead precisely the readers who know those
systems. "Actor" and "Subject" are rejected for a subtler reason: RFC 8693
uses both for exactly this shape, and its `actor` is the *calling* party —
the UI — while colloquially the word reads as the human who clicked. A
glossary term that means the opposite of how it reads is worse than a new
one.

## Consequences

- **Every guarded operation passes through one `Guard` at the service seam.**
  Each operation of the Admin API, of both Stats APIs and of the send is
  wrapped in `authz.Guard` (`internal/authz/guard.go:35`) inside the domain
  service that performs it — the `service.go` of `apikeys`, `domains`,
  `templates` and `stats`, and `pkg/api/mailapi/mailer.go:112` for the send —
  so a handler states no authority of its own and an operation cannot run
  ahead of its check. A decorator rather than a bare check, because a
  forgotten `return` after a check authorizes everything. Two properties
  follow, and both ADRs after this one rest on them: `Guard` is the only place
  that sees a Principal, an Action, a Resource and an outcome together, which
  is what makes recording a decision a single seam (ADR 0010); and no
  production code calls `Can` directly — its only caller is `Require`, whose
  only caller is `Guard`. The service methods deliberately outside a guard
  are the ones no request reaches: `InsertStat`, `IncrementAggregatedStat`
  and `Cleanup`, whose callers are Kannon's own workers, and
  `ValidateForAuth`, which decides who the caller is and so cannot require
  the answer before the question.
- **The model's soundness rests on a unit table over `Can`.** Coverage is
  owed by unit tests over a table of Principals, Grants and Resources rather
  than by integration tests through the handlers, because what has to be
  proved is a set of refusals the transport cannot produce on demand. The
  table owes the model three cases in particular: a typed Role granted off
  its declared Anchor kind is refused; Attenuation never widens, and never
  narrows a typed Role beneath its kind; the root Anchor is not the zero
  value of a pattern, so a zero value still covers nothing.
- **Changing a Tracking Policy and rewriting Templates are not separable.**
  `SetTrackingPolicy` is `update` on `domains/<name>`, which dominates that
  Domain's Templates. Accepted: both are things a Domain administrator does,
  and the alternative — a `domains/<name>/tracking` path corresponding to no
  entity in the language — buys a Role nobody has asked for. The same
  domination gives `domain-admin`'s `at(update)` formal reach over kinds no
  operation updates today; it is stated once here rather than worked around.
- **Authority over detailed statistics implies authority over the
  aggregates.** By design, and true anyway.
- **A Role tailored to one customer requires a deploy.** The accepted cost of
  a code catalogue.
- **Domain-name canonicalisation is enforced with a `CHECK` constraint on
  `domains.domain`**, not a data migration: pre-v1, the table holds no rows a
  fold or a human adjudication could be needed for, so the constraint
  (`domains_domain_check`, lower-case, dot-separated labels of `[a-z0-9_-]`,
  no leading, trailing or empty label) is added directly. `api_keys.domain`,
  `templates.domain`, `messages.domain`, `sending_pool_emails.domain`,
  `stats.domain` and `aggregated_stats.domain` copy the same value and are
  compared byte-wise, but carry no constraint of their own; each is written
  only from a value that already passed the canonical type's constructor, so
  the single `CHECK` on the source of the name is enough. Should this ever
  need revisiting after real data exists, the fold and the single-label
  refusal become adjudication problems again, not migrations.
- **Three Template operations carry no Domain in their request.**
  `GetTemplateReq`, `UpdateTemplateReq` and `DeleteTemplateReq` hold only a
  `template_id`, which the handlers passed straight to the unscoped
  repository methods. Since the proto is not being changed, the Domain is
  recovered by parsing the
  identifier, whose format already embeds it (`newTemplateID`,
  `internal/templates/templates.go:124`), and the handlers hand the parsed
  value to the scoped service method (`pkg/api/adminapi/templates.go:27`,
  `:43` and `:59`). Authorizing on a value parsed out of caller-supplied
  input is only sound because the Domain service then loads through
  `FindByDomain` with that same value (`internal/templates/repo.go:41`,
  cross-Domain isolation already asserted by the `WrongDomain` case at
  `internal/templates/repospec.go:151`): a fabricated Domain authorizes
  the caller for something the load will not find, so the parse can only
  narrow. It is sound *only* under the canonical domain name above —
  otherwise `template_x@a.com@b.com` parses to one Domain and loads from
  another. The parse therefore lives beside `newTemplateID` as its inverse
  (`DomainFromID`, `internal/templates/templates.go:136`), and returns an
  error rather than an empty string.
- **API Keys keep exactly one fixed Grant.** A key resolves to `sender`
  anchored on its own Domain — the rule pins the kind (`batches`), the
  Anchor pins the place — and nothing else; no schema change. Keys for
  reading statistics or administering a Domain need a grants table and are
  deferred. The deferral is cheap precisely because Roles are code and the
  Principal is resolved rather than stored: what a grants table will store
  is exactly what a Grant already is — a Role name and an Anchor — and the
  key-to-Principal adapter is the only code that will change.
- **A `jti` denylist, if token revocation is ever needed, does not violate
  allow-only.** Invalidating a credential is a different layer from denying
  a permission, the same distinction as `is_active` on an API Key.
- **Where an Attribution is persisted is settled by ADR 0010:** an audit
  table, holding the claim in its payload. The retention such a table needs
  is the operator's, because an Attribution *is* personal data, and that too
  is ADR 0010's.
- **Attenuation can never be a route to escalation, and never a route to
  anything sideways.** A front-end cannot derive a Principal able to do
  something it cannot do itself. Any use case needing that is not
  Attenuation and must be designed separately.
- **Pattern subsumption is not implemented.** Attenuating to a wildcard is
  refused, not because it is unsound but because deciding inclusion between
  glob patterns is a second matcher. Deferred until something needs it.
- **The "recorded, never consulted" property is load-bearing and easy to
  erode.** A future reader may see an identity in a request and reach for it
  to make a decision. If an Attribution ever reaches an authorization check,
  a caller with `attribute` gains the ability to choose its own authority,
  which is a privilege escalation. That is why the term is defined against
  the words that invite the mistake.

## Rejected alternatives

- **Type-free Roles: a Role as a bare set of Actions.** The first cut of
  this ADR. It kept reach and power perfectly orthogonal and the catalogue
  tiny, but no single named thing could span resource kinds — "domain
  administration" was four separately attached grants, a hand-assembly
  whose mistakes surface as silent misconfiguration — and a Role could only
  be named honestly for its shape, never for its intent: `template-editor`
  granted over `.../apikeys` *is* an API-key editor. Kubernetes binds verbs
  to resource kinds inside a role's rules and GCP's permissions are
  `service.resource.verb`; with the kind inside the Role, the bundling
  problem and the naming problem dissolve together.
- **A profile catalogue above type-free Roles, expanded at resolution
  time.** Solves the bundling without touching the engine, but splits the
  language in two: what an operator picks (a profile) is not what the
  engine reasons about (Grants), and two catalogues of names must be kept
  honest against each other. No prior art needs the second concept.
- **A second Role of pure shape, `at(attribute)`, granted alongside
  `admin`.** Keeps `admin` free of the Action, and would allow issuing the
  capability on its own. Rejected because nothing would ever be issued it
  alone, so the separation would be nominal at the cost of two Grants per
  front-end credential. It remains the shape to reach for if something ever
  needs to name people without administering anything.
- **Collection Anchors as sugar** — `template-editor` on `domains` meaning
  `domains/*`, the analogue of binding a namespaced role cluster-wide. It
  reads naturally, but it is two spellings for one authority, a rewrite
  inside the layer that must never normalise, and it leaves
  `domains/templates` — the Domain named `templates` — one typo away from
  an alias.
- **Rule suffixes that match at any depth.** An implicit `**`: when the
  tree grows, a future `domains/<name>/<node>/templates` would silently
  enter the reach of Grants issued years earlier. Reach must not change
  meaning when the tree does.
- **A two-tier `System` / `Domain(name)` scope.** Dissolves into the path
  model: the only domain-less operations are Actions on the `domains`
  collection. Keeping it would have meant a wildcard at the Domain tier as
  well, which is prefix domination in disguise.
- **Resource paths with explicit `**` and no implicit domination.** More
  precise, and it would have preserved "this node but not its children". It
  also makes attenuation a language-inclusion problem over glob patterns
  instead of a reuse of the same matcher, which is where that machinery
  breaks.
- **Coarse `read` / `write` / `admin` tiers.** `write` would fuse
  `CreateAPIKey` with `DeactivateAPIKey`, so neither a provisioner that
  cannot revoke nor an incident responder that cannot mint is expressible —
  on a credential system, the distinction most worth having. Structurally
  worse: `admin ⊃ write ⊃ read` adds a second ordering on top of prefix
  domination, so every question requires reasoning about two interacting
  lattices.
- **RPC method names as permissions.** Zero design and perfectly precise,
  and the role definitions would fail closed on every new method. But it
  couples authority to the transport, which is exactly what this layer must
  sit beneath: two transports over one domain would need two vocabularies.
- **Business-capability verbs** (`send-mail`, `read-stats`,
  `manage-templates`). This is how the requirement was originally phrased,
  and the legibility it offers is real — but it belongs to the Role, whose
  name and typed rules already provide it. Putting intent in the verbs
  would state the kind in two places.
- **Roles as database rows.** Would let an operator compose a Role without a
  deploy. Rejected until someone needs it: it turns the check into a
  component with a repository dependency and a cache, and moves a security
  decision out of code review.
- **Deny rules.** One phrase gained, a precedence order lost.
- **`stats/aggregated` as a sibling of `stats/deliveries`.** Structurally
  more honest, but it permits the Grant "detail but not aggregate", which
  the holder can defeat by counting.
- **A dedicated `send` Action.** The only type-bound verb in an otherwise
  type-free vocabulary.
- **Normalising a domain name inside the authorization layer.** Actively
  dangerous while two case-differing Domains can coexist: the normalisation
  becomes the privilege escalation.
- **One fused `impersonate` permission gated on a subset check.** The
  original proposal. It guards the safe half — narrowing, which needs no
  permission — and leaves unguarded the half that is an unverifiable
  assertion about a person.
- **Kubernetes- and GCP-style impersonation**, where a Principal acquires
  another's authority. Requires Kannon to know of principals it could become,
  which under this model it does not, and gives up the property that
  authority can only shrink.
- **Attenuation by validate-then-substitute.** Correct when the check runs,
  escalation when it does not. Intersection makes the check unforgettable by
  removing it.
- **Requiring an Action to attenuate.** Buys nothing: the operation is safe
  for anyone, always.
- **Replacing the Principal's identifier on attenuation.** Would merge the
  two mechanisms back together and make the recorded identity sometimes
  authenticated and sometimes asserted.
- **`actor` / `subject` per RFC 8693.** Standard, and inverted relative to
  how the words read.
- **Verifying the Attribution.** Impossible in principle, not merely
  unimplemented — the named people exist only in the calling system. Any
  design that appears to verify one is checking something else.
