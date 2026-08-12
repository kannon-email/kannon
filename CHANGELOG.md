# Changelog

## [1.0.0](https://github.com/kannon-email/kannon/compare/v0.5.1...v1.0.0) (2026-08-12)


### ⚠ BREAKING CHANGES

* **config:** remove the K_ prefix, the --run-* flags and the bump alias ([#452](https://github.com/kannon-email/kannon/issues/452))
* **config:** `container.LoadConfig` is now `config.LoadSection` in the new `x/config` package, which also takes over `APIAdminToken`, `APIAdminTokenKey`, `APIAdminTokenEnvVar` and `ApplyDeprecatedAliases`; `container.New` takes the root configuration it used to read itself; and `api.AdminToken` reports a reference it could not resolve. A process with no component enabled is now refused at boot instead of logging an empty runnable list and exiting 0, and the `--run-*` flags no longer appear in `--help`.
* **authz:** the Admin API and both Stats API versions now require the `X-Kannon-Admin-Token` header and answer `unauthenticated` to any request that does not carry it, and a process started with `--run-api` refuses to boot until `api.admin_token` (or `K_API_ADMIN_TOKEN`) is set. Upgrading deployments must configure that secret and give it to every caller of those three APIs before rolling out. The Mailer API's HTTP Basic credential and the health endpoint are unchanged.
* **stats:** GetAggregatedStats (stats API v2) returns one row per hour instead of one per day; a consumer that assumed one bucket per day must sum the hourly buckets itself.
* tracking Policy: per-Domain/Batch/Recipient control over open and link tracking ([#420](https://github.com/kannon-email/kannon/issues/420))

### Features

* **authz:** authorize the Admin, Stats and Mailer APIs from typed Roles anchored by Grants ([#442](https://github.com/kannon-email/kannon/issues/442)) ([d8a7f71](https://github.com/kannon-email/kannon/commit/d8a7f710cd8eb379c012d579f8ae629272e59dff)), closes [#443](https://github.com/kannon-email/kannon/issues/443) [#437](https://github.com/kannon-email/kannon/issues/437)
* **config:** remove the K_ prefix, the --run-* flags and the bump alias ([#452](https://github.com/kannon-email/kannon/issues/452)) ([03ab14d](https://github.com/kannon-email/kannon/commit/03ab14d7771eb90ab7ea63bf5745105dfd19806e))
* **config:** the config file is the contract, and the environment is referenced from it ([#449](https://github.com/kannon-email/kannon/issues/449)) ([ebf5c01](https://github.com/kannon-email/kannon/commit/ebf5c013c16d308fca538ac56df34974a0799206))
* **envelope:** per-link tracking opt-out and skip non-HTTP link schemes ([#421](https://github.com/kannon-email/kannon/issues/421)) ([306bcf6](https://github.com/kannon-email/kannon/commit/306bcf60d00898afda8b3c5bc9b4e10ead560cea))
* **mailer:** carry a one-click unsubscribe endpoint (RFC 8058) ([#429](https://github.com/kannon-email/kannon/issues/429)) ([97c95e1](https://github.com/kannon-email/kannon/commit/97c95e138c36e91965295bc75e8b276e2d2aac3b))
* **stats:** bucket aggregated stats by hour instead of day ([#426](https://github.com/kannon-email/kannon/issues/426)) ([e9713eb](https://github.com/kannon-email/kannon/commit/e9713ebe8e5269e34d2aaa1559866630c36570a4))
* tracking Policy: per-Domain/Batch/Recipient control over open and link tracking ([#420](https://github.com/kannon-email/kannon/issues/420)) ([1de5695](https://github.com/kannon-email/kannon/commit/1de56959cf48adbe7665d69c13cc8a3db61ac8d6))
* **tracking:** implement the pseudonymous Tracking Mode ([9d8b9f4](https://github.com/kannon-email/kannon/commit/9d8b9f416ca9b385c6ad6a2c84279f368202502d))


### Bug Fixes

* **config:** let the file outrank the environment, and refuse a malformed reference ([#450](https://github.com/kannon-email/kannon/issues/450)) ([7274032](https://github.com/kannon-email/kannon/commit/7274032c962cbd6561e2964c3fa691c7315a77ca))
* **db:** strip psql meta-commands from the embedded schema dump ([1f98d55](https://github.com/kannon-email/kannon/commit/1f98d5542fef07688b8140833436075df88df0f7))
* **delivery:** bound the retry path with a time-based Retry Budget ([3130565](https://github.com/kannon-email/kannon/commit/3130565947ccd774e9b4fe284fc75234cdb1184d)), closes [#378](https://github.com/kannon-email/kannon/issues/378)
* **dispatcher:** reschedule claimed deliveries on dispatch failure ([#403](https://github.com/kannon-email/kannon/issues/403)) ([f9ec1ef](https://github.com/kannon-email/kannon/commit/f9ec1ef1dd0af2b27404ff4edab0d0d4d7580bb4))
* **dispatcher:** retry NATS stream configuration with backoff at startup ([10fda0a](https://github.com/kannon-email/kannon/commit/10fda0a3921d6412d20d8af5d50fd8a3b8c23456)), closes [#365](https://github.com/kannon-email/kannon/issues/365)
* **k8s:** make the reference manifest bootable and complete ([9112102](https://github.com/kannon-email/kannon/commit/91121026de19cf586138582d2145f9ea535272f3))
* **sender:** stop physically re-sending emails on JetStream redelivery ([#427](https://github.com/kannon-email/kannon/issues/427)) ([68ef9d2](https://github.com/kannon-email/kannon/commit/68ef9d25caa390865fb741e767fbad61096c8e43)), closes [#425](https://github.com/kannon-email/kannon/issues/425)
* **smtp:** route asynchronous DSN bounces to kannon.stats.bounced ([#433](https://github.com/kannon-email/kannon/issues/433)) ([155c4ec](https://github.com/kannon-email/kannon/commit/155c4ec8b8ff303fc6772f587b3cf05f0ee3042d))
* **stats:** make stat insertion idempotent under JetStream redelivery ([#428](https://github.com/kannon-email/kannon/issues/428)) ([4bace5c](https://github.com/kannon-email/kannon/commit/4bace5c8a1d728c2b1013709f82c92025e9331db))
* **utils:** decode bounce return path with the URL-safe alphabet ([ca8d861](https://github.com/kannon-email/kannon/commit/ca8d861e0b4c83228904ac362eab5d7a4ec7559f)), closes [#432](https://github.com/kannon-email/kannon/issues/432)

## [0.5.1](https://github.com/kannon-email/kannon/compare/v0.5.0...v0.5.1) (2026-05-22)


### Bug Fixes

* **dispatcher:** stop Nak-redelivery hot loop on permanent stat errors ([#396](https://github.com/kannon-email/kannon/issues/396)) ([98c5cd0](https://github.com/kannon-email/kannon/commit/98c5cd03ac5c6c87aae458048a971356d14b7e3e))
* **pool:** race-safe claims with FOR UPDATE SKIP LOCKED ([#384](https://github.com/kannon-email/kannon/issues/384)) ([6734790](https://github.com/kannon-email/kannon/commit/673479008a8f3ef975f9c1dfd823d9eefd2c71a6)), closes [#374](https://github.com/kannon-email/kannon/issues/374)
* **security:** tenant isolation in mailer API ([#386](https://github.com/kannon-email/kannon/issues/386)) ([bf63ee1](https://github.com/kannon-email/kannon/commit/bf63ee1c3248bdb440947392f4e852f9715e47fb))
* **smtp:** guard null-MX domain in Send to avoid nil panic ([#379](https://github.com/kannon-email/kannon/issues/379)) ([#387](https://github.com/kannon-email/kannon/issues/387)) ([cd39628](https://github.com/kannon-email/kannon/commit/cd39628e8a2d7183e3a4ccde4573502339691674))

## [0.5.0](https://github.com/kannon-email/kannon/compare/v0.4.1...v0.5.0) (2026-05-09)


### Features

* add cc and to headers control in sending api ([#305](https://github.com/kannon-email/kannon/issues/305)) ([b0d9c4d](https://github.com/kannon-email/kannon/commit/b0d9c4d7b5fd335c2fcbaa8c0e09fe0cb01d7ce1))
* add stats cleanup in order to remove old data from database ([#311](https://github.com/kannon-email/kannon/issues/311)) ([9cd5f37](https://github.com/kannon-email/kannon/commit/9cd5f37a6c999c5d246a0a47b710bb5e060d6e3c))
* align Go codebase to shared language (CONTEXT.md) — PRD [#322](https://github.com/kannon-email/kannon/issues/322) ([#334](https://github.com/kannon-email/kannon/issues/334)) ([9bae8aa](https://github.com/kannon-email/kannon/commit/9bae8aa775f321689cebde9d037c2857392175c1))
* improve security hash keys ([#310](https://github.com/kannon-email/kannon/issues/310)) ([2cf7a96](https://github.com/kannon-email/kannon/commit/2cf7a96ba95d7512b46002a1fbe59f8e9a3655cb))
* refactor apikey handling ([#302](https://github.com/kannon-email/kannon/issues/302)) ([c845188](https://github.com/kannon-email/kannon/commit/c845188baa88795701780be34c97c8d4bd4fc839))
* unify bootstrap with Runnable/Registry + per-package LoadConfig ([#339](https://github.com/kannon-email/kannon/issues/339)) ([60942e2](https://github.com/kannon-email/kannon/commit/60942e2c58df499d584f684feaeb4e0dda3178dc))


### Bug Fixes

* apply deprecated aliases before reading run flags ([#341](https://github.com/kannon-email/kannon/issues/341)) ([536e857](https://github.com/kannon-email/kannon/commit/536e857de73d76ff704162e02426764eb9fa75f1))
* **db:** add ORDER BY to paginated :many queries ([1023e8f](https://github.com/kannon-email/kannon/commit/1023e8f15e34ab86aacf5d882ea736f382c1c868))
* **mailapi:** tolerate per-recipient errors when scheduling a batch ([1ee25e5](https://github.com/kannon-email/kannon/commit/1ee25e5925ecf94a854d87641db8c0afd97e1e35))


### Performance Improvements

* **db:** index sending_pool_emails on (status, scheduled_time) ([#369](https://github.com/kannon-email/kannon/issues/369)) ([99f1fc7](https://github.com/kannon-email/kannon/commit/99f1fc7429cd85e897d5c2dc18231d444d07367d)), closes [#360](https://github.com/kannon-email/kannon/issues/360)
* **mailapi:** bulk-insert deliveries via pgx CopyFrom ([#358](https://github.com/kannon-email/kannon/issues/358)) ([6a88ce9](https://github.com/kannon-email/kannon/commit/6a88ce920d8ea9d038dd7d14926b88c9fd92381a))
