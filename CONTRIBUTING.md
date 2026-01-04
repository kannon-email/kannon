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

### 5. Run E2E Tests

```sh
make test-e2e
```

### 6. Run Linters

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

## Submitting Pull Requests (PRs)

1. Fork the repository and create your branch from `main` or the relevant feature branch.
2. Make your changes, following the code style guidelines.
3. Add or update tests as needed.
4. Run all tests and linters locally.
5. Push your branch and open a PR against the main repository.
6. Fill out the PR template, describing your changes and motivation.
7. Participate in the code review process—respond to feedback and make necessary changes.
8. PRs are merged after passing CI and review.

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

- **`dependabot-auto-merge.yaml`**: Automatically merges Dependabot PRs for minor/patch updates after CI passes

- **`release.yaml`**: Manages release drafts using release-drafter

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
