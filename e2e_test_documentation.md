# End-to-End Email Testing for Kannon

## Overview

This document describes the comprehensive end-to-end testing approach implemented for the Kannon email system. The tests verify the complete email pipeline from API submission to delivery verification.

## Test Architecture

The E2E tests are designed to validate the entire email processing pipeline:

1. **API Layer**: Domain creation and email submission via gRPC APIs
2. **Database Layer**: Email storage and queue management
3. **Validation Layer**: Email address validation and processing
4. **SMTP Layer**: Email delivery and tracking
5. **Stats Layer**: Analytics and reporting

## Test Implementation

### 1. Docker-based Full E2E Test (`e2e_test.go`)

**Purpose**: Complete system integration test with real infrastructure

**Components**:
- PostgreSQL database via Docker
- NATS messaging system via Docker  
- Local SMTP server for email capture
- All Kannon services running concurrently

**Test Flow**:
```
1. Setup Docker containers (PostgreSQL + NATS)
2. Start local SMTP server on port 25 (or available port)
3. Launch all Kannon services (API, Sender, Dispatcher, Validator, Stats)
4. Create test domain via Admin API
5. Submit email via Mailer API
6. Verify email flows through complete pipeline
7. Assert email delivery to SMTP server
8. Cleanup infrastructure
```

**Key Features**:
- Uses `dockertest` for infrastructure setup
- Uses `assert.Eventually` for async verification
- Tests real SMTP delivery with localhost domain
- Verifies email content and templating
- Tests multiple recipients and field substitution

### 2. Realistic Pipeline Test (`e2e_realistic_test.go`)

**Purpose**: Pipeline integration test without Docker dependencies

**Components**:
- Existing test database infrastructure
- Mock NATS publisher for stats capture
- API services and business logic
- Email validation and processing

**Test Flow**:
```
1. Setup test database using existing test infrastructure
2. Create API services (Admin + Mailer)
3. Setup validation pipeline with mock publisher
4. Create test domain and submit emails
5. Run validation cycles
6. Verify database state and stats generation
7. Test attachment handling
```

**Key Features**:
- No Docker dependencies (works in any environment)
- Tests complete business logic pipeline
- Verifies database operations and data integrity
- Tests field substitution and templating
- Captures and validates stats generation
- Tests attachment handling

## Test Scenarios Covered

### Core Email Pipeline
- ✅ Domain creation and management
- ✅ Email submission with authentication
- ✅ Template processing and field substitution
- ✅ Multiple recipient handling
- ✅ Email validation and queue management
- ✅ Stats generation and tracking

### Advanced Features
- ✅ Email attachments (multiple file types)
- ✅ Custom field substitution per recipient
- ✅ Global vs per-recipient field handling
- ✅ Template management (CRUD operations)
- ✅ Domain key management and rotation
- ✅ Authentication and authorization

### Error Handling
- ✅ Invalid email address rejection
- ✅ Authentication failure handling
- ✅ Template not found scenarios
- ✅ Database connection handling

## Benefits of This Approach

### 1. Comprehensive Coverage
- Tests the entire email pipeline end-to-end
- Validates both happy path and error scenarios
- Ensures integration between all components

### 2. Multiple Testing Levels
- **Full E2E**: Real infrastructure with Docker (when available)
- **Pipeline Tests**: Business logic without infrastructure dependencies
- **Unit Tests**: Individual component testing

### 3. Real-world Validation
- Uses actual API calls (not mocks)
- Tests real database operations
- Validates message queuing and processing
- Verifies stats generation and tracking

### 4. Flexible Execution
- Can run in environments with or without Docker
- Supports CI/CD pipelines
- Scales from developer machines to production testing

## Usage Examples

### Running Full E2E Test (requires Docker)
```bash
# Run complete system integration test
go test -v -run TestE2EEmailSending -timeout 10m

# Features tested:
# - Real PostgreSQL and NATS via Docker
# - Local SMTP server for email capture
# - All services running concurrently
# - Actual email delivery verification
```

### Running Pipeline Test (no Docker required)
```bash
# Run business logic pipeline test
go test -v -run TestE2EEmailPipeline -timeout 5m

# Features tested:
# - API submission and processing
# - Database operations and queue management
# - Email validation and stats generation
# - Field substitution and templating
```

### Running Attachment Test
```bash
# Test email attachments handling
go test -v -run TestEmailPipelineWithAttachments -timeout 2m

# Features tested:
# - Multiple attachment types
# - Binary content handling
# - Database storage verification
```

## Assertion Patterns

The tests use `assert.Eventually` for async operations:

```go
// Example: Verify email appears in database
assert.Eventually(t, func() bool {
    var count int
    err := db.QueryRow(ctx, 
        "SELECT COUNT(*) FROM sending_pool_emails WHERE message_id = $1", 
        messageID).Scan(&count)
    return err == nil && count > 0
}, 30*time.Second, 1*time.Second, "Email should appear in sending pool")
```

This pattern ensures:
- Proper handling of async operations
- Reasonable timeouts for CI/CD environments
- Clear error messages on failures
- Robust testing in distributed systems

## Integration with CI/CD

The tests are designed for continuous integration:

```yaml
# Example GitHub Actions workflow
- name: Run E2E Tests
  run: |
    # Start required services
    docker-compose up -d postgres nats
    
    # Run pipeline tests (always work)
    go test -v -run TestE2EEmailPipeline
    
    # Run full E2E if Docker available
    go test -v -run TestE2EEmailSending
```

## Future Enhancements

### Planned Improvements
- [ ] Performance testing with high email volumes
- [ ] Multi-domain testing scenarios
- [ ] Advanced template testing (loops, conditions)
- [ ] Bounce handling verification
- [ ] Click/open tracking validation
- [ ] DKIM signature verification
- [ ] SPF record validation

### Monitoring Integration
- [ ] Prometheus metrics validation
- [ ] Log aggregation testing
- [ ] Health check endpoint verification
- [ ] Performance baseline enforcement

## Conclusion

This comprehensive E2E testing approach ensures:

1. **Reliability**: All components work together correctly
2. **Maintainability**: Tests catch regressions early
3. **Confidence**: Deployments are validated thoroughly
4. **Flexibility**: Tests run in various environments
5. **Completeness**: All major features are covered

The combination of Docker-based full integration tests and pipeline-focused business logic tests provides comprehensive coverage while maintaining flexibility for different development and deployment environments.