### Database Migrations (dbmate)

Migrations use [dbmate](https://github.com/amacneil/dbmate).
Always use dbmate to create a new migrations. Please in the same commits do not add multiple migrations

### Commit messages

Conventional Commits are mandatory: `<type>[(scope)][!]: <description>`, with `feat`, `fix`, `perf`, `revert`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`. release-please derives the version and `CHANGELOG.md` from them, and PR titles must follow the same format (PRs are squash-merged). See [CONTRIBUTING.md](CONTRIBUTING.md#commit-messages).

## Agent skills

### Issue tracker

GitHub issues in `kannon-email/kannon`, via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Canonical defaults (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
