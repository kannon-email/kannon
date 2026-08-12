# ADR 0012: The deprecated configuration surfaces are removed

## Status

Accepted (2026-08-12). Amends
[ADR 0011](./0011-the-config-file-is-the-contract-and-the-environment-is-referenced-from-it.md),
which kept three things working while deprecated and which this ADR removes: the
`K_` environment prefix, the `--run-*` flags, and the `bump:` section alias for
`tracker:`. ADR 0009's `K_API_ADMIN_TOKEN` goes with the prefix; the key it names,
`api.admin_token`, is untouched.

## Context

ADR 0011 made the config file the contract and kept every older spelling alive
beside it, so that an installation could move one Deployment at a time. That
migration is what the deprecations were for, and holding them costs more than the
code they occupy.

Configuration is read twice over. A component is selected by the `services`
section *or* by a flag, OR-ed; a top-level key is answered by the file *or* by a
`K_` variable, ranked, with the ranking itself a decision that had to be argued
and tested — viper puts the environment above the file, which is backwards for a
fallback, so the promotion bypasses viper's environment layer and merges into its
config layer instead. Every one of those sentences is a thing an operator has to
know before they can predict what their process will do.

It also keeps a hole in the boundary ADR 0011 drew. Nothing in Kannon may read a
configuration value through `viper.Get*`, because the `env://` hook only runs
while something is decoded, so an accessor would hand a reference to the code as
a value. The accessors that remained were exactly the deprecated ones —
`run-stats`, `bump.port` — and the rule was therefore "no accessors, except these,
which are safe because an operator would not write a reference into them". A rule
with an exception list is a rule that gets extended.

The deprecated spellings are also the ones nothing can check. `--run-verifier`
names a component that no longer exists anywhere else in the codebase (PRD #322),
and `bump.port` names a package removed in the same PRD, so both are terms the
glossary tells contributors to avoid while the CLI goes on teaching them.

## Decision

All three are removed. The `services` section is the only way to select a
component, and every setting is either written in the file or referenced from it
with `env://NAME`.

A `--run-*` flag is unregistered rather than hidden. Cobra refuses an unknown flag
and the process exits non-zero, which is the loudest available answer: a flag that
still parsed and selected nothing would give a pod that comes up healthy running
none of what its manifest asked for.

The startup warning that named every `K_` variable in the environment goes too.
Keeping it would have left the prefix alive in `x/config` as a second deprecation
in place of the first — one more thing to explain, and one more thing to decide
when to remove. The removal is announced by the release: a breaking version, a
migration table in the README, and a CHANGELOG entry, which is where a deployment
about to be upgraded is read from.

Nothing about the removal is quiet at run time either, for the half that matters
most: a flag stops the boot outright, and a variable that used to supply
`database_url` leaves the key unset, which the container reports as the
connection it cannot make. What is genuinely lost is the diagnosis for the
variables the prefix never reached — `K_TRACKER_PORT` and its kind, which have
been setting nothing all along and go on doing so.

## Consequences

This is a breaking change for any installation that has not migrated. A pod
passing `--run-api` stops at startup with an unknown-flag error. A pod relying on
`K_DATABASE_URL` starts and then fails on the setting that variable used to carry
— and an operator reading that failure has to know to look at their manifest,
which is what the README's migration table is for.

`x/config` no longer reads a configuration value through any viper accessor, so
ADR 0011's boundary holds without an exception list. Three pieces of machinery go
with the deprecations: the promotion of a `K_` variable onto its key, the
nested-key merge that promotion needed in order not to hide the keys beside the one
it wrote, and `envref.Name`, which existed so the warning could tell a variable the
file refers to from one that was setting nothing.

Nothing in the code knows that `K_` ever meant anything. The prefix survives only
where it belongs — in the migration table and in these ADRs — so a reader of
`x/config` sees one way to configure Kannon rather than one way plus its history.
