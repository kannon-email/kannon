# Contributing to Kannon

Thank you for your interest in contributing to Kannon! We welcome all contributions—bug reports, feature requests, documentation improvements, and code changes. Kannon is an open-source, community-driven project, and your input helps make it better for everyone.

## Project Philosophy

- **Cloud-native**: Designed for scalable, containerized, and distributed environments.
- **Reliability**: Robust email delivery and observability.
- **Modularity**: Decoupled components for easy extension and maintenance.
- **Open Collaboration**: All contributions are welcome!

## Getting Started

### 1. Clone the Repository

```sh
git clone https://github.com/kannon-email/kannon.git
cd kannon
```

### 2. Build the Project

```sh
go build -o kannon .
```

### 3. Run Locally

You will need a running PostgreSQL and NATS instance. You can use Docker Compose:

```sh
docker-compose -f examples/docker-compose/docker-compose.yaml up
```

Then, in another terminal:

```sh
./kannon --run-api --run-smtp --run-sender --run-dispatcher --config ./examples/docker-compose/kannon.yaml
```

### 4. Run Tests

```sh
make test
```

Both `make test` and `make test-e2e` run with Go's race detector (`-race`)
enabled, matching CI. Race-detector builds are slower and use more memory, but
catch concurrency bugs (Kannon uses `errgroup`, NATS consumers, and worker
pools) at PR time instead of in production. If you hit a `DATA RACE` report
locally, treat it as a real bug.

### 5. Run E2E Tests

```sh
make test-e2e
```

### 6. Run Benchmarks

```sh
make bench
```

Runs all `Benchmark*` functions across the module (without `-race`, since the
race detector dramatically inflates timings). Benchmarks that need Postgres
(e.g. `internal/db`) will spin up a container via testcontainers, so a Docker
daemon must be running.

### 7. Run Linters

```sh
make lint
```

## Code Style & Best Practices

- Follow idiomatic Go (gofmt, goimports).
- Use clear, descriptive names for variables, functions, and types.
- Write unit and integration tests for new features and bug fixes.
- Keep functions small and focused.
- Document exported functions and types.
- Prefer composition over inheritance.
- Avoid breaking backward compatibility unless necessary (discuss in an issue first).

## Submitting Issues & Feature Requests

- Use [GitHub Issues](https://github.com/kannon-email/kannon/issues) for bugs, enhancements, and questions.
- For feature requests, describe the use case and proposed solution.
- For bugs, include steps to reproduce, expected vs. actual behavior, and environment details.

## Commit Messages

Releases are automated with [release-please](https://github.com/googleapis/release-please): it reads the commit history of `main`, computes the next version and writes `CHANGELOG.md`. A commit that does not follow the convention never shows up in the release notes, so **every commit that lands on `main` must be a [Conventional Commit](https://www.conventionalcommits.org/en/v1.0.0/)**:

```
<type>[(optional scope)][!]: <description>

[optional body]

[optional footer(s)]
```

- `<type>` is mandatory and lowercase; see the table below.
- `(scope)` is optional but recommended: the package or component touched, e.g. `dispatcher`, `sender`, `smtp`, `db`, `envelope`, `deps`, `ci`.
- `<description>` is a short imperative summary, lowercase (proper nouns such as `NATS` or `Postgres` excepted) and without a trailing period.
- Append `!` after the type/scope (and/or add a `BREAKING CHANGE:` footer explaining the migration) for anything that breaks compatibility. release-please gives it its own changelog section and bumps the version accordingly.
- Reference issues in the body or footer (`Fixes #123`), not in the type.

| Type       | Use for                                                     | Release effect        |
| ---------- | ----------------------------------------------------------- | --------------------- |
| `feat`     | New user-visible functionality                              | Minor bump, changelog |
| `fix`      | Bug fixes                                                   | Patch bump, changelog |
| `perf`     | Performance improvements without behaviour changes          | Patch bump, changelog |
| `revert`   | Reverting a previous commit                                 | Patch bump, changelog |
| `docs`     | Documentation only                                          | No bump               |
| `refactor` | Restructuring with no behaviour change                      | No bump               |
| `test`     | Tests only                                                  | No bump               |
| `build`    | Build system, Makefile, Dockerfile, dependency bumps        | No bump               |
| `ci`       | GitHub Actions workflows and CI configuration               | No bump               |
| `chore`    | Everything else (also used by release-please's own commits) | No bump               |

Examples taken from the actual history:

```
feat(sender): add per-domain rate limit
fix(dispatcher): reschedule claimed deliveries on dispatch failure
build(deps): bump github.com/nats-io/nats-server/v2
feat!: tracking Policy: per-Domain/Batch/Recipient control over open and link tracking
```

PRs are squash-merged, therefore **the PR title is the commit message that ends up on `main`** and must follow the same convention. This is enforced in CI by the `PR Title` workflow, which fails when the type is unknown or the description ends with a period; if a PR contains a single commit, that commit message is validated too, because GitHub pre-fills the squash message with it. Individual commits inside a branch should follow the convention as well — keep them clean, so the history stays readable if a PR is rebased instead of squashed.

## Submitting Pull Requests (PRs)

1. Fork the repository and create your branch from `main` or the relevant feature branch.
2. Make your changes, following the code style guidelines.
3. Add or update tests as needed.
4. Run all tests and linters locally.
5. Push your branch and open a PR against the main repository.
6. Title the PR as a Conventional Commit (see [Commit Messages](#commit-messages)).
7. Fill out the PR template, describing your changes and motivation.
8. Participate in the code review process—respond to feedback and make necessary changes.
9. PRs are merged after passing CI and review.

## Continuous Integration (CI)

- All PRs are automatically tested via GitHub Actions.
- PRs must pass all tests and linters before merging.

### CI Workflows

The project uses several GitHub Actions workflows:

- **`ci.yaml`**: Main CI pipeline that runs on all PRs and pushes to `main`
  - **Unit Tests**: Runs `make test` with Go module caching for faster builds
  - **E2E Tests**: Runs `make test-e2e` in a separate job
  - **Docker Build**: Builds and pushes Docker images with layer caching (only on `main` and tags)
  - Includes concurrency management to cancel outdated builds

- **`golang.yaml`**: Runs `golangci-lint` on all PRs with Go module caching

- **`pr-title.yaml`**: Validates that the PR title (and the single commit of one-commit PRs) is a Conventional Commit, since that title becomes the squashed commit on `main`

- **`dependabot-auto-merge.yaml`**: Automatically merges Dependabot PRs for minor/patch updates after CI passes

- **`release-please.yml`**: On every push to `main`, opens/updates the release PR (version bump + `CHANGELOG.md`) from the Conventional Commit history; merging it tags the release

- **`release.yaml`**: On `v*` tags, builds and publishes the release artifacts with GoReleaser

### Build Optimization

- **Go module caching**: CI workflows cache `~/go/pkg/mod` and `~/.cache/go-build` to speed up builds
- **Docker layer caching**: Docker builds use GitHub Actions cache for faster image builds
- **Concurrency control**: Outdated builds are automatically cancelled when new commits are pushed

## Community & Contact

- For questions, join the discussion on GitHub or open an issue.
- See the [README](./README.md) for more information about the project.
- We welcome all contributors—thank you for helping make Kannon better!

---

Happy hacking! 🚀
