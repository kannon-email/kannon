# Kannon E2E Testing Suite

This directory contains comprehensive end-to-end tests for the Kannon email system.

## Overview

The e2e tests verify the complete email pipeline from API submission to delivery verification using real infrastructure components:

- **PostgreSQL** database via Docker
- **NATS** messaging system via Docker
- **Test SMTP server** for email capture and verification
- **All Kannon services** running concurrently (API, Sender, Dispatcher, Validator, Stats)

## Test Structure

### Files

- `e2e_test.go` - Main test suite with comprehensive scenarios
- `infrastructure.go` - Docker-based test infrastructure setup
- `smtp_server.go` - Advanced SMTP server implementation for testing
- `README.md` - This documentation

### Test Scenarios

The test suite includes the following scenarios:

1. **SingleRecipientEmail** - Tests basic email sending to a single recipient
2. **MultipleRecipientsEmail** - Tests bulk email sending with personalized content
3. **EmailWithAttachments** - Tests email delivery with file attachments
4. **InvalidEmailHandling** - Tests error handling for invalid email addresses
5. **BenchmarkE2EEmailThroughput** - Performance benchmarking

## Requirements

### System Requirements

- Docker Engine installed and running
- Go 1.21 or later
- Access to Docker socket (`/var/run/docker.sock`)
- Available ports for PostgreSQL, NATS, and API services

### Dependencies

The tests use the following key dependencies:

- `github.com/ory/dockertest/v3` - Docker container management
- `github.com/stretchr/testify` - Testing assertions
- `github.com/jackc/pgx/v5` - PostgreSQL driver
- `github.com/nats-io/nats.go` - NATS messaging
- `connectrpc.com/connect` - gRPC/Connect API clients

## Running the Tests

### Basic Test Execution

```bash
# Run all e2e tests
go test -v ./e2e

# Run tests with timeout
go test -v -timeout 10m ./e2e

# Run specific test
go test -v -run TestE2EEmailSending ./e2e
```

### Running Individual Test Scenarios

```bash
# Run single recipient test
go test -v -run TestE2EEmailSending/SingleRecipientEmail ./e2e

# Run multiple recipients test
go test -v -run TestE2EEmailSending/MultipleRecipientsEmail ./e2e

# Run attachment test
go test -v -run TestE2EEmailSending/EmailWithAttachments ./e2e

# Run invalid email handling test
go test -v -run TestE2EEmailSending/InvalidEmailHandling ./e2e
```

### Running Performance Benchmarks

```bash
# Run throughput benchmark
go test -v -run BenchmarkE2EEmailThroughput -bench=. ./e2e

# Run benchmark with custom parameters
go test -v -bench=BenchmarkE2EEmailThroughput -benchtime=30s ./e2e
```

### Skip Tests Without Docker

```bash
# Run tests in short mode (skips Docker-dependent tests)
go test -v -short ./e2e
```

## Test Environment

### Docker Infrastructure

The tests automatically set up the following Docker containers:

- **PostgreSQL 15-alpine** - Database server
- **NATS 2.10-alpine** - Message broker with JetStream

### Test SMTP Server

A comprehensive SMTP server implementation that:

- Handles multiple concurrent connections
- Implements proper SMTP protocol
- Parses email headers and attachments
- Provides debugging and inspection capabilities
- Supports email searching and filtering

### Service Configuration

The tests start all Kannon services with test-specific configurations:

- **API Server** - HTTP/gRPC API on random port
- **Sender** - Email sending service with localhost hostname
- **Dispatcher** - Message routing and queuing
- **Validator** - Email validation service
- **Stats** - Analytics and reporting service

## Test Verification

### Email Delivery Verification

The tests verify email delivery by:

1. **SMTP Capture** - Emails are captured by the test SMTP server
2. **Content Verification** - Email content is checked for proper templating
3. **Database State** - Database records are verified
4. **Stats Generation** - Statistics are confirmed

### Database Assertions

The tests verify database state including:

- Email records in `sending_pool_emails` table
- Statistics in `stats` table
- Message ID tracking
- Delivery status updates

### Async Verification

The tests use `assert.Eventually` patterns for async operations:

```go
assert.Eventually(t, func() bool {
    return smtpServer.GetReceivedEmailCount() >= expectedCount
}, 60*time.Second, 2*time.Second, "Emails should be received")
```

## Troubleshooting

### Common Issues

1. **Docker Not Available**
   - Tests are skipped gracefully if Docker is not available
   - Error message: "Docker not available, skipping E2E test"

2. **Port Conflicts**
   - Tests use random ports to avoid conflicts
   - Check for conflicting services if tests fail

3. **Timeout Issues**
   - Increase timeout values if tests are slow
   - Check Docker resource allocation

4. **Permission Issues**
   - Ensure Docker socket is accessible
   - On Linux, user may need to be in `docker` group

### Debug Output

Enable debug logging:

```bash
# Run with verbose output
go test -v ./e2e

# Enable logrus debug level
LOGRUS_LEVEL=debug go test -v ./e2e
```

### Test Cleanup

The tests automatically clean up resources:

- Docker containers are removed after tests
- Temporary files are cleaned up
- Network resources are released

## Configuration

### Environment Variables

- `DOCKER_HOST` - Docker daemon URL (default: unix:///var/run/docker.sock)
- `LOGRUS_LEVEL` - Logging level (debug, info, warn, error)

### Test Timeouts

- **Overall Test Timeout**: 5 minutes
- **Service Startup**: 3 seconds
- **Email Delivery**: 60 seconds
- **Database Operations**: 30 seconds

## Performance Characteristics

### Typical Performance

- **Single Email**: < 5 seconds end-to-end
- **Bulk Emails (3)**: < 30 seconds
- **Attachment Emails**: < 10 seconds
- **Database Operations**: < 1 second

### Benchmark Results

The benchmark test measures:

- Emails per second throughput
- Memory usage
- CPU utilization
- Database performance

## Integration with CI/CD

### GitHub Actions Example

```yaml
name: E2E Tests
on: [push, pull_request]

jobs:
  e2e-tests:
    runs-on: ubuntu-latest
    services:
      docker:
        image: docker:dind
        options: --privileged
    
    steps:
    - uses: actions/checkout@v3
    - uses: actions/setup-go@v4
      with:
        go-version: '1.21'
    
    - name: Run E2E Tests
      run: |
        go test -v -timeout 10m ./e2e
```

### GitLab CI Example

```yaml
e2e-tests:
  image: golang:1.21
  services:
    - docker:dind
  variables:
    DOCKER_HOST: tcp://docker:2376
    DOCKER_TLS_CERTDIR: ""
  script:
    - go test -v -timeout 10m ./e2e
```

## Contributing

### Adding New Tests

1. Create test functions in `e2e_test.go`
2. Use the established patterns for infrastructure setup
3. Include proper cleanup and error handling
4. Add documentation for new test scenarios

### Improving Infrastructure

1. Enhance `infrastructure.go` for new services
2. Update `smtp_server.go` for additional SMTP features
3. Maintain backward compatibility
4. Add comprehensive error handling

### Best Practices

1. **Always use `assert.Eventually`** for async operations
2. **Include detailed logging** for debugging
3. **Test both success and failure scenarios**
4. **Verify database state** after operations
5. **Use meaningful test data** for better debugging

## Support

For issues or questions:

1. Check the troubleshooting section
2. Review test output for error messages
3. Enable debug logging for more details
4. Create an issue with full test output