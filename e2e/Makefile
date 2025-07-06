.PHONY: test test-short test-verbose test-docker benchmark clean help

# Default target
help:
	@echo "Available targets:"
	@echo "  test        - Run all e2e tests"
	@echo "  test-short  - Run tests in short mode (skips Docker tests)"
	@echo "  test-verbose- Run tests with verbose output"
	@echo "  test-docker - Run full Docker-based tests (requires Docker)"
	@echo "  benchmark   - Run performance benchmarks"
	@echo "  clean       - Clean up test artifacts"
	@echo "  help        - Show this help message"

# Run all e2e tests
test:
	go test -v -timeout 10m

# Run tests in short mode (no Docker)
test-short:
	go test -v -short -timeout 2m

# Run tests with extra verbose output
test-verbose:
	LOGRUS_LEVEL=debug go test -v -timeout 10m

# Run full Docker-based tests
test-docker:
	go test -v -run TestE2EEmailSending -timeout 10m

# Run individual test scenarios
test-single:
	go test -v -run TestE2EEmailSending/SingleRecipientEmail -timeout 5m

test-multiple:
	go test -v -run TestE2EEmailSending/MultipleRecipientsEmail -timeout 5m

test-attachments:
	go test -v -run TestE2EEmailSending/EmailWithAttachments -timeout 5m

test-invalid:
	go test -v -run TestE2EEmailSending/InvalidEmailHandling -timeout 2m

# Run performance benchmarks
benchmark:
	go test -v -bench=BenchmarkE2EEmailThroughput -run=^$$ -timeout 15m

# Run specific benchmark with custom settings
benchmark-short:
	go test -v -bench=BenchmarkE2EEmailThroughput -benchtime=10s -run=^$$ -timeout 5m

# Clean up any test artifacts
clean:
	docker container prune -f
	docker volume prune -f
	go clean -testcache

# Check if Docker is available
check-docker:
	@docker version >/dev/null 2>&1 || (echo "Docker is not available or not running" && exit 1)
	@echo "Docker is available"

# Install dependencies
deps:
	go mod download
	go mod tidy

# Lint the code
lint:
	golangci-lint run || echo "Install golangci-lint for better linting"

# Format the code
fmt:
	go fmt ./...

# Run tests with coverage
coverage:
	go test -v -coverprofile=coverage.out -timeout 10m
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# CI/CD friendly test command
ci:
	go test -v -short -timeout 5m

# Development workflow - format, test short, then full test if Docker available
dev: fmt test-short
	@if docker version >/dev/null 2>&1; then \
		echo "Docker available, running full tests..."; \
		make test-docker; \
	else \
		echo "Docker not available, skipping Docker tests"; \
	fi