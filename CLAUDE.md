# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kannon is a cloud-native SMTP mail sender written in Go, designed for Kubernetes and modern infrastructure. It provides gRPC APIs for sending emails, template management, and statistics, using NATS for internal messaging and PostgreSQL for persistence.

## Development Commands

### Building and Running
- `go build -o kannon .` - Build the main binary
- `./kannon --run-api --run-smtp --run-sender --run-dispatcher --config ./config.yaml` - Run with all components locally

### Testing
- `make test` or `go test ./... -v -short` - Run unit and integration tests
- `make test-e2e` or `go test ./e2e -v -timeout 10m` - Run end-to-end tests
- `go test ./path/to/package -v` - Run tests for specific package

### Code Generation
- `make generate` - Generate both DB and proto code
- `make generate-db` or `sqlc generate` - Generate database code from SQL queries
- `make generate-proto` or `buf generate` - Generate protobuf code

### Code Quality
- `make lint` or `golangci-lint run --fix` - Run linters with auto-fix

### Tool Management
The project uses `mise` for tool version management. Tools are defined in `mise.toml`:
- Go 1.24.3
- buf 1.56.0
- sqlc 1.29.0
- golangci-lint 2.2.1

## Architecture Overview

### Core Components
- **API**: gRPC server exposing Admin, Mailer, and Stats APIs
- **SMTP**: SMTP server for incoming mail and bounces
- **Sender**: Worker that performs actual SMTP delivery
- **Dispatcher**: Manages email queue and builds messages for sending
- **Verifier**: Validates emails before sending
- **Stats**: Collects and stores delivery statistics

### Key Directories
- `cmd/` - CLI entrypoint and configuration
- `internal/` - Internal packages (db, mailbuilder, smtp, etc.)
- `pkg/` - Public API and service packages
- `proto/` - Protobuf definitions and generated code
- `db/migrations/` - PostgreSQL schema migrations
- `e2e/` - End-to-end tests

### Database Layer
- Uses `sqlc` to generate type-safe Go code from SQL queries
- SQL queries defined in `internal/db/*.sql` files
- Database models and queries in `internal/db/` (generated)
- PostgreSQL migrations in `db/migrations/`

### NATS Messaging
Components communicate via NATS JetStream topics:
- `kannon.sending` - Emails to be sent
- `kannon.stats.*` - Various statistics events
- `kannon.bounce` - Bounce notifications

### protobuf/gRPC
- Proto files in `proto/kannon/`
- Uses `buf` for code generation
- Connect-RPC for gRPC implementation

## Development Workflow

1. Make changes to Go code, SQL queries, or proto files
2. Run `make generate` if you modified SQL or proto files
3. Run `make test` to ensure tests pass
4. Run `make lint` to check code quality
5. For E2E testing, use `make test-e2e`

## Key Files to Understand
- `kannon.go` - Main application entry point
- `cmd/root.go` - CLI configuration and component orchestration
- `internal/x/container/container.go` - Dependency injection container
- `ARCHITECTURE.md` - Detailed architecture documentation
- `examples/docker-compose/` - Local development setup

## Testing Notes
- Unit tests use `_test.go` suffix
- Some tests require Docker for test databases
- E2E tests in `e2e/` directory test full email pipeline
- Demo sender mode available for testing without actual SMTP delivery

## Configuration
- YAML configuration files supported
- Environment variables with `K_` prefix
- CLI flags available for all options
- See `examples/docker-compose/kannon.yaml` for full configuration example