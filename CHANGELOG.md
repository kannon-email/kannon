# Changelog

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
