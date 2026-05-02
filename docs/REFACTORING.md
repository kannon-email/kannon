# Refactoring Backlog

Refactoring targets identified during the language-grilling session. These are *findings*, not yet *decisions* — promote individual items to ADRs in `docs/adr/` when committed to.

---

## 1. Sending pool state machine cleanup

**Source:** `internal/db/pool.go`, `internal/db/pool.sql`, `pkg/validator/validator.go`, `pkg/dispatcher/disp.go`, migrations `20210406191606_dbinit.sql` and `20220830073617_sending-pool-type-improvements.sql`.

### Current state

`SendingPoolStatus` declares seven values: `initializing`, `to_validate`, `validating`, `scheduled`, `sending`, `sent`, `error`. Three are dead, two pairs are concurrency leaks.

| State | Reality |
|---|---|
| `initializing` | **Dead.** Column default only; `CreatePool` always inserts as `to_validate`, so no row ever sits in `initializing`. |
| `to_validate` | Real. Insert state. |
| `validating` | Real, but functions as the **Validator's claim flag** — flipped by `PrepareForValidate`, never read as a "state". |
| `scheduled` | Real and **domain-meaningful** — "validated, awaiting `scheduled_time`". The only state that carries semantics beyond claim-locking. |
| `sending` | Real, but functions as the **Dispatcher's claim flag** — flipped by `PrepareForSend`, never read as a "state". |
| `sent` | **Dead.** `SetSendingPoolDelivered` SQL is defined but no Go code calls it. Happy path deletes the row instead. |
| `error` | **Dead.** Never set anywhere. Transmission errors call `RescheduleEmail` (back to `scheduled`). |

### Actual lifecycle in code

```
INSERT(to_validate) → [claim] validating ─ok→ scheduled ─time arrives & claim→ sending
                                          │                                       │
                                  invalid │                                       │
                                          ↓                              transient│error
                                         DELETE                                   ↓
                                       (+rejected stat)                  scheduled (retry, ++attempts)
                                                                                  │
                                                                  permanent error/│delivered/bounce
                                                                                  ↓
                                                                                DELETE
                                                                              (+stat event)
```

### Why this matters

- Dead enum values clutter the schema and mislead new contributors who reasonably assume `sent`/`error` are reachable.
- The `to_validate`/`validating` and `scheduled`/`sending` doublings express row-level claim-locking through enum mutation. This conflates "where in the workflow is this Delivery" with "is some worker currently holding it".
- Pool ≠ domain. The pool is internal mechanics; terminal outcomes (Accepted, Rejected, Delivered, Bounced, Failed) live in **stats**, not in the pool. Pool's terminal action is `DELETE`.

### Proposed cleanup (to be ratified by ADR)

1. **Drop dead enum values:** remove `initializing`, `sent`, `error` from `SENDING_POOL_STATUS`. Migration must drop the column default and rewrite the type.
2. **Replace claim-flip pairs with a claim column:** introduce `claimed_at TIMESTAMP NULL` (and optionally `claimed_by VARCHAR`) and reduce the enum to `to_validate` and `scheduled`. The SELECT-FOR-UPDATE-style claim becomes `WHERE status='X' AND claimed_at IS NULL` + set `claimed_at = NOW()`.
3. **Rename remaining states to neutral nouns:** `to_validate` → `pending`, `scheduled` → `ready` (or keep `scheduled`, since it carries domain meaning about deferred sends).
4. **Sender Go struct in `internal/pool/`:** resolved under PRD #322. The local `pool.Sender` struct has been removed; "Sender" now has exactly one meaning in code — the proto type for the visible from-identity of a Batch (mirrored as `batch.Sender`).

### Open questions

- Should the claim mechanism use `claimed_at` only, or include `claimed_by` (worker ID) for observability?
- Is keeping `scheduled` worth the carry-over, or should we rename it `ready` and store the deferral as `send_after` only?

---

## 2. Stats vocabulary cleanup

**Source:** `.proto/kannon/stats/types/stats.proto`, `internal/db/statistics.go`, `pkg/validator/validator.go`, `pkg/dispatcher/disp.go`.

### Current state

`StatsType` declares: `accepted`, `rejected`, `delivered`, `bounced`, `opened`, `clicked`, `error`, `failed`, `unknown`. Two are dead, one is misnamed, one is internal-only.

| Stat | Reality |
|---|---|
| `accepted` | Real. Emitted by Validator. **Misnamed** — collides with the SMTP sense of "remote MX accepted". Should be `validated`. |
| `rejected` | Real. Emitted by Validator with `reason`. Keep. |
| `delivered` | Real. Means "remote MX accepted handoff" (industry-standard loose meaning). Keep, document explicitly. |
| `bounced` | Real. Carries `permanent`, `code`, `msg`. Two source paths (sync from SMTPSender, async DSN from SMTPServer). Keep. |
| `opened` | Real. Engagement event from Tracker. Keep. |
| `clicked` | Real. Engagement event from Tracker. Keep. |
| `error` | Real internal-only signal (transient retry). Demote: drop from the public stats vocabulary; keep as an internal Dispatcher signal (NATS topic or log). |
| `failed` | **Dead.** Declared in proto, no publisher. Remove. |
| `unknown` | DB-side fallback. Not a real event. Remove or keep as defensive default only. |

### Proposed cleanup (to be ratified by ADR — wire-breaking)

1. Rename **`accepted` → `validated`** in proto, DB enum, NATS subject, and Go code. Wire-breaking; align with the refactor.
2. **Drop `failed`** from proto and DB enum.
3. **Drop or downgrade `error`** to a non-public internal signal. The retry-pending notion is plumbing, not an outcome.
4. Document **`delivered`** as "remote MX accepted handoff, not inbox placement" in user-facing API docs.
5. Decide whether **`bounced`'s `permanent` flag** still has a non-permanent path; if all bounces are permanent, drop the flag.

### Open questions

- Do we want a terminal **`failed_after_retries`** stat to record "we gave up" distinctly from "remote rejected us" (`bounced`)? Today `RescheduleEmail` has no visible max-attempts ceiling — needs verification.
- Should engagement events (`opened`, `clicked`) live in a separate NATS stream / DB table from delivery outcomes? They have different cardinality (1 Delivery → N Opens) and different retention needs.

---

## 3. Rename `Bump` → `Tracker` — DONE

Completed under PRD #322. The package is now `pkg/tracker/`, the `tracker:` config / `K_TRACKER_PORT` env var are canonical, and `bump:` / `K_BUMP_PORT` continue to work as deprecated aliases that log a warning at startup. HTTP routes were preserved, so previously-emitted tracking links remain valid.

---

## 4. Domain entity: clarify the string-vs-entity boundary

**Source:** `internal/db/models.go` (`Domain` struct), `.proto/kannon/admin/apiv1/adminapiv1.proto` (`Domain` message), `db/migrations/20210406191606_dbinit.sql`.

### Current state

The `Domain` table and proto both use `domain string` as the FQDN field on a `Domain` record. The entity name and the field name collide, making `domain.domain` patterns common in code.

### Proposed cleanup

1. Keep the entity name **`Domain`** (decision pinned in `CONTEXT.md`) — wire-stable.
2. Rename the FQDN string field from `domain` → `fqdn` in the proto, DB column, and Go struct. This is wire-breaking; align with the larger refactor.
3. Update SQL queries (`WHERE domain = ?` → `WHERE fqdn = ?`) and downstream call sites.

### Open questions

- Should the rename be paired with a UNIQUE constraint review on the FQDN column? (Today it's already `UNIQUE` per the dbinit migration — verify it survived later migrations.)

---

## 5. Template type rename and source-format dimension

**Source:** `internal/db/models.go`, `internal/templates/impl.go`, `db/migrations/20220809092503_add_template_type.sql`.

### Current state

`template_type` enum values are `transient` and `template`. The latter value name is confusing — every row in the `templates` table is a template. The enum encodes **lifetime**, not type.

### Proposed cleanup

1. Rename `template_type` enum to `template_lifetime` (or similar). Values: `transient` and `persistent` (renaming `template` → `persistent`).
2. Wire-breaking in the Admin proto (`Template.type` field). Align with the refactor.

### Future addition (not blocking)

Eventually, a separate `source_format` axis (e.g. `html`, `mjml`) may be added so external authoring tooling can re-transpile on update. Out of scope for the current refactor — pinning the lifetime axis first.

### Use case to preserve through the rename

Transient Templates exist to support a "split a million-recipient Batch across multiple API calls without re-uploading the body" workflow: the first call inlines the HTML (creating a Transient Template); subsequent calls reference it by ID via `SendTemplate`. Verify whether `SendTemplate` can in fact reference a Transient Template today — if not, this is a feature gap to flag separately, not a vocabulary issue.

---

