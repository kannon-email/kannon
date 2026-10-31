# REST v1beta1 implementation tickets

Approved vertical slices published as sub-issues of [PRD #476](https://github.com/kannon-email/kannon/issues/476).
Every ticket has the prd/subissue and ready-for-agent labels. Dependencies are native GitHub blocking relationships.
The parent issue title, body, state and labels were preserved. No feature implementation has started.

| Slice | Ticket | Blocked by |
| --- | --- | --- |
| 1 | [#477: Extract transport-independent Batch intake](https://github.com/kannon-email/kannon/issues/477) | None |
| 2 | [#478: Expose REST Domain administration and readiness](https://github.com/kannon-email/kannon/issues/478) | None |
| 3 | [#479: Manage Domain credentials with five-role authorization](https://github.com/kannon-email/kannon/issues/479) | [#478](https://github.com/kannon-email/kannon/issues/478) |
| 4 | [#480: Manage Persistent Templates through REST](https://github.com/kannon-email/kannon/issues/480) | [#479](https://github.com/kannon-email/kannon/issues/479) |
| 5 | [#481: Create inline-HTML Batches atomically and idempotently](https://github.com/kannon-email/kannon/issues/481) | [#477](https://github.com/kannon-email/kannon/issues/477), [#479](https://github.com/kannon-email/kannon/issues/479) |
| 6 | [#482: Send from Templates with consistent personalization](https://github.com/kannon-email/kannon/issues/482) | [#481](https://github.com/kannon-email/kannon/issues/481) |
| 7 | [#483: Schedule Batches and send attachments](https://github.com/kannon-email/kannon/issues/483) | [#481](https://github.com/kannon-email/kannon/issues/481) |
| 8 | [#484: Apply Tracking Policy and One-Click Unsubscribe at REST intake](https://github.com/kannon-email/kannon/issues/484) | [#482](https://github.com/kannon-email/kannon/issues/482) |
| 9 | [#485: Retain and expose individual Delivery outcomes](https://github.com/kannon-email/kannon/issues/485) | [#481](https://github.com/kannon-email/kannon/issues/481) |
| 10 | [#486: Browse Batch progress and paginated Delivery history](https://github.com/kannon-email/kannon/issues/486) | [#485](https://github.com/kannon-email/kannon/issues/485) |
| 11 | [#487: Expire completed Batch history after 30 days](https://github.com/kannon-email/kannon/issues/487) | [#486](https://github.com/kannon-email/kannon/issues/486) |
| 12 | [#488: Query individual Stats events through REST](https://github.com/kannon-email/kannon/issues/488) | [#479](https://github.com/kannon-email/kannon/issues/479) |
| 13 | [#489: Query complete Domain aggregate time series](https://github.com/kannon-email/kannon/issues/489) | [#479](https://github.com/kannon-email/kannon/issues/479) |

The initial frontier is slices 1 and 2. Prioritize the intake prefactoring on the sending path; Domain/readiness work has no technical dependency on it.
Afterward, start a ticket only when all of its native blockers are complete. Tests and persistence changes belong to their behavior slice, not separate horizontal tickets.
