# ADR 0011: The config file is the contract, and the environment is referenced from it

## Status

Accepted (2026-08-11). Supersedes the `K_` environment prefix and the
`--run-*` flags as the way a deployment is described; both keep working,
deprecated. ADR 0009 specified `api.admin_token` and its `K_API_ADMIN_TOKEN`
spelling, which this ADR generalises rather than replaces.

## Context

Kannon's configuration had two halves that did not meet.

Values came from a YAML file, read section by section: each runnable calls
`UnmarshalKey` on its own key and gets a struct. Viper can also answer from
the environment, and Kannon turned that on with a `K_` prefix — but
`AutomaticEnv` only applies to `Get`, and `UnmarshalKey` never consults the
environment at all. So `K_DATABASE_URL` worked, because `database_url` is a
top-level key read with `GetString` and bound by hand, and `K_API_PORT` did
nothing whatsoever. Neither did `K_SENDER_HOSTNAME`, `K_SMTP_ADDRESS` or
`K_TRACKER_PORT` — one of which sat in a live deployment's manifest, setting
nothing, for months. Which variables worked was not derivable from the
outside: the four that did looked exactly like the ones that did not.
`api.admin_token` was the exception, and it took a paragraph of comment and a
hand-written binding to be one.

Which components a process ran came from somewhere else entirely: eight
`--run-*` flags on the command line. A deployment of Kannon is one image
started eight ways, so the thing that differs most between its pods was the
thing least able to be written down — a ConfigMap could describe every
setting an installation shared, and then each Deployment still carried its
own `args` list. Adding a component to an installation meant editing YAML in
a manifest, not configuration.

The pressure came from operating several installations. What is wanted is one
file per installation, mounted by every pod, with each pod contributing only
the few things that are genuinely its own.

## Decision

### The file is the contract, and it may name environment variables

Every setting is written in the config file. Any value in it may be a
reference to an environment variable instead of a literal:

```yaml
database_url: env://KANNON_DATABASE_URL             # required
nats_url: env://KANNON_NATS_URL:-nats://nats:4222   # with a fallback
api:
  admin_token: env://KANNON_ADMIN_TOKEN
```

`env://NAME` is required: unset, it stops the boot with a message naming both
the key and the variable. `env://NAME:-default` falls back, and treats a
variable set to the empty string as unset — POSIX `${NAME:-default}`, because
that is the syntax this borrows and because in a container an empty variable
is a hole in the deployment far more often than a considered value. The
reference must span the whole leaf value, so a URL that happens to contain
the scheme is untouched, and `\env://NAME` escapes a literal.

This is a mapstructure decode hook (`x/config/envref`), so it applies to
every unmarshal Kannon performs: nested keys included, which is the half
viper could not do. Resolution is therefore the operator's decision and is
visible in one file, and the variable can keep whatever name the deployment
already gives it — including `K_DATABASE_URL`.

### Which components run is configuration

A `services` section, one entry per runnable, all off unless enabled:

```yaml
services:
  api:
    enabled: true
  stats:
    enabled: env://KANNON_ENABLE_STATS:-false
```

Its own block rather than an `enabled` key inside each runnable's section,
for two reasons. `audit.enabled` already means something else — whether
authorization decisions are published at all (ADR 0010) — and the writer that
consumes them is a runnable, so per-section `enabled` would have collided on
exactly the pair that most needs to stay distinct. And one block is the thing
an operator reads to answer "what is this process", which was the question
the flags answered badly.

A misspelled name under `services` is refused rather than ignored — this is
the one section where a typo is the difference between a working process and
one that starts nothing. A process with nothing enabled is refused too: it
used to log `Starting Kannon runnables: []` and exit 0, which reads as a
clean shutdown in every dashboard there is.

### The flags stay, OR-ed, deprecated

`--run-<component>` still selects a component, and is OR-ed with the section
rather than ranked against it: both are an operator asking for that
component, and nobody passes `--run-stats` meaning "not stats". OR-ing is
what makes the migration free of order — an installation can move one
Deployment at a time to the file while the rest still pass flags, with no
combination that turns something off unexpectedly. pflag names the
replacement the first time a flag is used.

### The `K_` prefix stays, deprecated, and says so

The four keys it reached keep working, now bound explicitly so they survive
being read through a struct. At startup Kannon warns once, naming **every**
`K_` variable in the environment — not only the keys it reads, because a
deployment carrying `K_TRACKER_PORT` has been setting nothing for as long as
it has existed and its operator has no other way to find out.

`api.admin_token` remains the one key read through viper's accessors rather
than through a struct, so that `K_API_ADMIN_TOKEN` keeps working; the
reference in the file is resolved for it by hand.

### Reading configuration is one package

`x/config` locates the file, resolves references, holds the deprecations and
hands each runnable its section. It was in `x/container`, which is dependency
injection and had no business also being the configuration layer — and the
audit config, which cannot import `x/container` without a cycle, had to
duplicate the two lines that read a section. `x/config` depends on viper and
nothing of Kannon's, so everything can use it.

## Consequences

Configuration failures move earlier and get louder. A reference to a variable
nobody set stops the boot instead of yielding a zero value, which is the
point, but it also means a config file that was silently half-ignored may now
refuse to start — an operator who wrote `env://` into a key expecting the old
best-effort behaviour gets an error. Nothing that worked before stops
working: the prefix, the flags and every plain value are untouched.

The root configuration is read once on the boot path and handed to
`container.New`, so `database_url` and `nats_url` are reported as a message
rather than a panic. Sections a runnable reads still panic, which is the
existing contract for a malformed file, and the message names the key and the
variable.

`services.audit.enabled` and `audit.enabled` are two keys one letter apart in
meaning, which is a naming cost this ADR accepts in exchange for not moving
`audit.enabled` — a key already documented, already deployed, and already
carrying a data-retention obligation.

Nothing in Kannon reads a configuration value through `viper.Get*` any more,
apart from the admin token: the hook only runs while something is being
decoded, so an accessor added back for a key an operator may write a
reference into would hand that reference to the code as a value. A test in
`x/config/envref` pins that boundary down.

## Rejected alternatives

**An `enabled` key inside each runnable's section.** The shape first asked
for, and the one that collides with `audit.enabled`. Renaming that key would
have broken a documented setting to make room for a spelling that is no
clearer.

**Resolving `env://` eagerly over the whole tree at boot, writing the results
back with `viper.Set`.** Simpler in one respect — every accessor would see
resolved values, including `Get` — but `viper.Set` on a nested key puts a
partial map in the override layer, and `UnmarshalKey` returns that layer
alone rather than merging it, so promoting `api.admin_token` that way would
cost the operator their `api.port`. The decode hook has no such reach into
what it did not resolve.

**Binding every nested key to an environment variable by hand.** What viper
asks for, and it is a list that has to be kept in step with every struct
field in the codebase — the failure mode being exactly the silence this ADR
set out to remove. It also fixes the variable's name, where a reference lets
a deployment keep the names it has.

**One root struct unmarshalled once, with a field per section.** More robust
than per-section reads — viper's `Unmarshal` merges every layer key by key,
where `UnmarshalKey` does not — and it would let a nested key be bound to an
environment variable after all. It needs the root struct to reach every
runnable's `Config` type, which inverts the dependency that has each runnable
own its own configuration. Worth revisiting; not worth coupling `x/config` to
every `pkg/` for.
