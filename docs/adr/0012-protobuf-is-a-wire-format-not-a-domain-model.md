# ADR 0012: Protobuf is a wire format, not a domain model

## Status

Accepted (2026-08-12). ADR 0010 took this decision once, for one payload, and
had to defend it as a deviation — "this is the first thing in the repository to
cross NATS as JSON. Stated here so that it reads as a decision and not as an
oversight someone should correct towards proto." This ADR states the rule that
was an instance of, so the next one does not have to argue for itself. Nothing
is superseded: every published `.proto` keeps its shape, and no wire contract
with a client changes.

## Context

The boundary this ADR draws is mostly already there, and has never been written
down. Four packages under `internal/` import the generated code:
`internal/trackingpb`, which exists to; `internal/publisher`, which is the NATS
edge and says so on `SendEmail` — "the proto type boundary lives here so the
rest of the dispatcher / builder pipeline stays in domain types";
`internal/stats`; and `internal/db`, which imports it because `sqlc.yaml` tells
it to. Everything else — `internal/tracking`, `internal/delivery`,
`internal/batch`, `internal/domains`, `internal/authz`, `internal/values` —
models Kannon in types Kannon wrote, and the API layer maps onto them.

`internal/trackingpb` states the contract for its one case in its package doc:
"It is the only place that knows both, so that `internal/tracking` stays free of
any protobuf dependency and every API boundary that accepts a Policy refuses the
same wire values." Four packages under `pkg/` call it. That second clause is the
part a per-handler translation could not have delivered — a rule about what
Kannon accepts is a property of one function, not of four that resemble each
other.

`stats` is where the boundary is not held, and it is held nowhere in particular
rather than in one place. The same generated type is at once the domain model,
the storage format and the API payload:

- `internal/stats/stat.go:35` gives the entity a `Data *types.StatsData` field,
  and `DetermineType` (`:63-88`) derives the domain classification — Delivered,
  Bounced, Opened — by type-switching the protobuf oneof wrapper structs. What
  kind of outcome a Stat *is* is read off the encoding of the message that
  carried it.
- `sqlc.yaml:36-41` overrides the `stats.data` column onto that same generated
  type, pointer and all, so `internal/db/models.go:120` and the generated query
  in `internal/db/statistics.sql.go:57` carry it, and `stats_repo.go:31` hands
  `stat.Data` to the insert untouched.
- `proto/kannon/stats/types/db.go` hand-adds `MarshalJSON` and `UnmarshalJSON`
  to the *generated* package, both delegating to `protojson`. It is the only
  hand-written Go file anywhere under `proto/`, sitting in a directory whose
  every other line is rewritten by `make generate-proto`.
- `pkg/statsapi/statsv1/impl.go:88` puts `s.Data` into the response. Not a
  translation of it — the same pointer, out of the database and onto the wire.

`stats.data` is `JSONB NOT NULL`
(`db/migrations/20240421080953_add-stats-to-database.sql:10`), and pgx encodes a
jsonb column through `encoding/json`, which finds those two methods. So the
on-disk format of that column is protojson: lowerCamelCase keys, the oneof as a
single key naming the variant, zero-valued scalars omitted. The on-disk format
of a jsonb column is defined by a `.proto` file, which means a wire-compatible
schema change and a storage migration are the same event, and nobody performing
the first is prompted to think about the second.

That is not a theoretical coupling. Protobuf's own compatibility rules say
renaming a field is safe, because the binary encoding keys on tag numbers.
protojson keys on names, and `db.go:12` calls `protojson.Unmarshal` with no
options, whose default refuses unknown fields. A rename that every client
tolerates therefore turns every row already written into a read error — not a
lost field, a failed query — and the change that causes it is one that proto
itself blesses.

The repository already contains the shape this ADR wants, in the same
configuration file. `tracking.Policy` (`internal/tracking/tracking.go:135-138`)
is a domain struct with plain `json` tags, overridden by `sqlc.yaml:47-55` onto
three jsonb columns, and translated to and from the wire enums by
`internal/trackingpb`. It owns its storage encoding and knows nothing about
protobuf. Even inside the `stats` table the two treatments sit side by side: the
`type` column is overridden onto `sqlc.StatsType` (`internal/db/statistics.go:5`),
a storage-side type written by hand that duplicates the domain's `stats.Type` and
is converted at the repository — and the `data` column, one line further down the
same override list, is the generated proto.

## Decision

### `internal/` models the domain, and does not know the wire exists

No package under `internal/` imports `github.com/kannon-email/kannon/proto/...`
or `google.golang.org/protobuf`.

The reason is not layering hygiene. A proto is a compatibility contract with
clients: what it may do is add fields and reserve tag numbers, its rate of
change is set by who has built against it, and its shape is answerable to
versioning tooling. A domain model is a description of the business: it may be
renamed the day the language is sharpened, it may hold types that refuse values —
`values.DomainName`, `tracking.Mode`, `authz.Resource` all exist to make an
invalid value unrepresentable — and its shape is answerable to `CONTEXT.md`.
They change for different reasons and at different rates. Fusing them means
neither can change alone: a concept cannot be renamed without breaking a client,
and a field cannot be added for a client without a new thing appearing in the
domain that nothing in the domain means.

Two exceptions, and they are the same exception twice. A package whose entire
job is translation — see below. And test support that drives a running Kannon
through its generated client (`internal/tests/adminapi.go`,
`internal/stats/repospec.go`, the integration tests under `internal/envelope`):
those are clients, and a client speaks the wire format by definition.

`internal/db` is generated, so its import is written in `sqlc.yaml` rather than
in Go, and the override list is the thing to read. With `stats.data` moved off
the generated type it holds no proto import at all, and the boundary becomes
exactly "the translation packages and the test clients".

### Translation is a package that does nothing else

`internal/<x>pb`, on the model of `internal/trackingpb`: it imports the one
domain package it serves and the one generated package that carries it, and
holds the mapping in both directions, once. For `stats`, that is an
`internal/statspb` holding what `DetermineType` and `statToPb` do today.

Per concern rather than one `internal/wire`: a single translation package would
import every domain package and every generated package, becoming the one place
in Kannon that knows everything, and every wire change would recompile all of
it. `internal/trackingpb` knows `internal/tracking` and nothing else, which is
what lets its package doc make a claim about the whole system in one sentence.

`internal/publisher` was already such a package for the Envelope, without its
name saying so, and its other half was missing: `PublishStat` took a
`*types.Stats` its caller had assembled, so a stat event was built in proto by
each of the five `pkg/` workers that publish one. Both halves move out —
`internal/envelopepb` and `internal/statspb` — and what stays behind is what a
transport is for: naming the subject, and handing bytes to NATS.

Those two packages own the encoding as well as the field mapping
(`statspb.MarshalEvent`, `envelopepb.MarshalEnvelope`), which is what lets
`internal/publisher` hold no protobuf dependency at all. The alternative was to
except the one package whose job is to put bytes on a topic, and a rule with no
exceptions is worth more than the line it saved. It is a better place for the
split anyway: the bytes and the shape are
one decision, and a caller that wants an Event on the wire should not have to
know it takes two calls.

### `pkg/` speaks proto, and translates at the edge of the handler

`pkg/` is adapters — Connect handlers, NATS consumers, the SMTP workers — and
proto is their vocabulary. Nothing here is forbidden. What is asked for is that
the request type stop at the first function that reads it, so that what travels
inwards is domain values.

`pkg/api/mailapi/mailer.go` mostly does this: `unsubscribeFromRequest` (`:286`),
`senderAddressOf`, `validateHeaders` and `trackingpb.ToPolicy` all convert at
intake. `scheduleBatch` (`:237`) is the counter-example inside the same file — it
takes `[]*mailertypes.Recipient` and reads `r.Email`, `r.Fields` and
`r.Tracking` in the loop that builds Deliveries. It breaks no rule, because it is
in `pkg/`. It is the shape to move away from, for a reason with nothing to do
with layering: a helper that takes the request type can only ever be called by a
request, so the rejection rules it holds — an empty address, an unresolvable
unsubscribe URL, a Mode above the ceiling — cannot be exercised or reused
without building a protobuf message to ask them a question about a Recipient.

### `stats.data` keeps its bytes, and loses its type

The type that defines the column moves into `internal/stats`, carrying the
outcome and its detail as a domain value that states its own kind rather than
having it inferred from a wrapper struct. `sqlc.yaml` points the column at it.
`internal/statspb` translates it to and from `types.StatsData`, and the v1 Stats
API — the only surface that returns per-Delivery detail — calls that instead of
forwarding a pointer.

**The bytes on disk do not change, and stay protojson-shaped.** This is a known
and accepted point rather than something the ADR is unaware of: for as long as
the existing rows exist, a domain type is shaped by a wire encoding — lowerCamelCase
keys, the variant as a single key, zero-valued scalars omitted. It is accepted
because reproducing the encoding is exactly what makes the boundary introducible
with no data migration, on a table holding a row per Delivery outcome for as
long as the retention allows. The encoding is small
and closed — eight outcomes, at most three scalar fields each — so reproducing it
is struct tags plus a marshaller for the one-key envelope, which a Go sum type
needs anyway.

What is bought even so is not nothing. The `.proto` stops being the definition of
the column and becomes one of two things that render it, so a rename on the wire
no longer silently orphans rows; reads become tolerant of unknown keys where
protojson refuses them; and the day the storage format is worth migrating, it is
a change to one Go type and a backfill, rather than a change to a published
schema.

## Consequences

- **The cost of adopting this falls on one package and one column.** The
  boundary already holds everywhere else, which is what makes writing it down
  worth doing now — it is a rule that describes the code rather than a plan to
  change it, with one exception carved out and dated.
- **`stats` gains a type it did not have, and loses one it should not have had.**
  The domain classification stops being derived by type-switching a protobuf
  oneof. A Stat knows what kind of outcome it is because it was built as one.
- **A test pins the encoding.** "The bytes do not change" is a fact only if
  something asserts it: golden payloads produced by the current protojson path,
  read and written by the new type. Without that, this ADR's central concession
  is an intention.
- **Nothing enforces the import rule today.** `.golangci.yaml` runs no
  `depguard`, so the boundary is a review matter — which is precisely how ADR
  0010's deviation ended up needing a paragraph of prose to survive. A
  `depguard` rule over `internal/`, excepting `internal/*pb` and test files, is
  the cheap enforcement and the intended follow-up.
- **One extra allocation per stat row on the read path.** `statToPb` builds a
  message where it forwarded a pointer. The write path costs the same as today:
  a marshal happened either way, in a method on the generated type.
- **Test support keeps its proto imports, and that is the rule working.**
  `internal/tests` exists to call Kannon over Connect. Forbidding proto there
  would be forbidding the test from being a client.
- **The public generated package stops carrying Kannon's storage encoding.**
  `proto/` is not `internal/` and has no module of its own, so `db.go` today
  gives every Go client's `StatsData` a `MarshalJSON` that emits what Kannon
  writes to disk. Deleting it removes a method from a published type — a
  breaking change in the narrow sense, on behaviour that was never anybody's to
  rely on, and worth naming rather than discovering.
- **This does not make the wire a second-class citizen.** Every published
  `.proto` stays the contract it is; the API returns the same messages, and buf
  still governs their evolution. What changes is that they stop also being the
  answer to "what is a Stat".

## Rejected alternatives

**Leave it: one type, no translation.** The fewest types, no mapping to keep in
step, and the compiler guarantees the domain and the wire never drift because
they are the same declaration. It is the current state, and its guarantee is the
problem restated — they cannot drift because neither can move. It also leaves the
storage format defined by a file whose compatibility rules do not describe the
storage.

**Generate the domain types from the `.proto`.** One source of truth, no
translation, and the drift this ADR admits becomes impossible. It makes the
domain model a projection of the public API: a concept Kannon does not expose
could not be modelled, and one it exposes could not be dropped. And generated
messages are structs of scalars with no invariants — `values.Parse` refusing a
trailing dot, an ordered `tracking.Mode`, a `Resource` that matches on segments
because its own `String()` is ambiguous, none of these survive being generated.

**Ban `google.golang.org/protobuf` from `internal/` but allow the generated
types packages.** A softer line that would let `Stat.Data` stay while stopping
the domain from marshalling anything. It draws the boundary at the import graph
rather than at the model: `types.StatsData` is a protobuf message whichever
package names it, and the oneof would still be where the domain reads its
classification from.

**Migrate `stats.data` to a domain-shaped JSON in the same change.** The clean
end state — the column becomes readable without consulting a `.proto`, and the
concession above disappears. It is a rewrite of every row of `stats`, coupled to
the introduction of the boundary, so a fault in either would have to be rolled
back as one. Deferred, and cheap to take later precisely
because the encoding will by then be defined by a Go type with a test around it.

**Store the binary proto in a `bytea` column instead.** Smaller, faster, and
honest about what the column actually holds. It gives up `data->>` on a table
that is queried, rewrites every existing row, and settles the coupling by
deepening it.

**One `internal/wire` package for all translation.** Fewer packages and one
obvious place to look. It imports everything on both sides, so it is the one
package in Kannon that knows the whole system, and any wire change touches all
of it.

**No `internal/*pb` at all — every handler translates for itself.** The
translation would then be duplicated across the four `pkg/` packages that
already call `trackingpb`, and "every API boundary that accepts a Policy refuses
the same wire values" would stop being a property of the system and become four
places that currently agree.
