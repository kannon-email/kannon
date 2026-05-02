# Container Architecture

The Container package implements a **fail-fast dependency injection (DI) system** designed specifically for cloud-native applications like Kannon. This architecture follows the principle that **infrastructure failures should immediately terminate the application** rather than attempting graceful degradation.

## Design Philosophy

### Fail-Fast Strategy
The Container uses `logrus.Fatalf()` intentionally when critical infrastructure components fail to initialize:
- **Database connections**
- **NATS messaging system**
- **JetStream setup**

This design ensures that:
1. **No partial state**: Application never runs in a degraded state with missing dependencies
2. **Clear failure signals**: Container orchestrators (Kubernetes) can immediately detect and restart failed instances
3. **Simplified error handling**: No need for complex fallback logic throughout the application
4. **Operational clarity**: Clear distinction between infrastructure failures vs. business logic errors

### Singleton Pattern with Lazy Initialization
All dependencies are created as singletons using the generic `singleton[T]` type:
```go
type singleton[T any] struct {
    once  sync.Once
    value T
}
```

Benefits:
- **Thread-safe**: Uses `sync.Once` for concurrent access
- **Lazy loading**: Dependencies created only when first accessed
- **Memory efficient**: Single instance per dependency type
- **Deterministic**: Same instance returned across the application lifecycle

## Architecture Components

### Core Structure
```go
type Container struct {
    ctx context.Context  // Application context
    cfg Config          // Configuration
    
    // Singleton instances with error handling
    db     *singleton[*pgxpool.Pool]  // Database connection pool
    nats   *singleton[*nats.Conn]     // NATS connection
    sender *singleton[smtp.Sender]    // SMTP sender implementation
    
    closers []CloserFunc  // Enhanced cleanup functions with context
}

// Enhanced closer function signature with context support
type CloserFunc func(context.Context) error
```

### Dependency Graph
```
Container
├── Database (PostgreSQL)
│   └── Queries (SQLC generated)
├── NATS
│   ├── Publisher (with debug logging)
│   └── JetStream (streaming)
└── SMTP Sender
    ├── Production Sender
    └── Demo Sender (for testing)
```

## Usage Patterns

### 1. Service Initialization
Services receive the container and extract only needed dependencies:

```go
// Dispatcher service with improved error handling
func Run(ctx context.Context, cnt *container.Container) error {
    q := cnt.Queries()           // Database queries (fail-fast on connection issues)
    js := cnt.NatsJetStream()    // Message streaming (fail-fast on setup)
    pub := cnt.NatsPublisher()   // Message publishing (fail-fast on connection)
    
    // Initialize service with dependencies
    d := dispatcher{queries: q, jetstream: js, publisher: pub}
    return d.run(ctx)
}
```

### 2. Factory Pattern Integration
The Container supports factory methods for complex dependency construction:

```go
func NewSenderFromContainer(cnt *container.Container, cfg Config) *sender {
    sender := cnt.Sender()        // SMTP implementation
    js := cnt.NatsJetStream()     // Message streaming
    publisher := cnt.NatsPublisher() // Message publishing
    return NewSender(publisher, js, sender, cfg)
}
```

### 3. Enhanced Resource Management
Automatic cleanup with graceful shutdown and timeout handling:

```go
cnt := container.New(ctx, config)
defer cnt.Close()  // Automatically closes all resources with 30s timeout

// For custom timeout control
if err := cnt.CloseWithTimeout(10 * time.Second); err != nil {
    log.Errorf("Shutdown failed: %v", err)
}

// Container tracks cleanup functions with context support
c.addClosers(func(ctx context.Context) error {
    // Graceful shutdown with context cancellation support
    return db.Shutdown(ctx)
})
```

#### Parallel Shutdown
The enhanced closer system provides:
- **Parallel execution**: All closers run concurrently for faster shutdown
- **Timeout protection**: Configurable timeout prevents hanging shutdowns
- **Error aggregation**: Multiple errors are collected and reported together
- **Context propagation**: Shutdown context passed to all closers for cancellation support

## Key Benefits

### 1. **Simplified Service Layer**
Services don't need complex initialization logic - they receive ready-to-use dependencies from the container with automatic error handling.

### 2. **Enhanced Testability**
The Container can be easily mocked or replaced with test implementations:
```go
// Test container with mock dependencies
testContainer := &container.Container{...}
service := NewServiceFromContainer(testContainer)
```

### 3. **Configuration Centralization**
All infrastructure configuration is centralized in the Container config:
```go
type Config struct {
    DBUrl        string
    NatsURL      string
    SenderConfig SenderConfig
}
```

### 4. **Advanced Resource Lifecycle Management**
- **Parallel shutdown**: All resources close concurrently for faster termination
- **Timeout protection**: Configurable timeouts prevent hanging shutdowns
- **Error aggregation**: Multiple shutdown errors collected and reported
- **Context-aware cleanup**: Proper cancellation support in closers
- **Backward compatibility**: Existing `Close()` method maintains compatibility

### 5. **Type Safety and Error Handling**
- **Generic singletons**: Compile-time type safety for all dependencies
- **Dual error patterns**: Both recoverable (`Get`) and fail-fast (`MustGet`) options
- **Clear diagnostics**: Detailed error messages with type information
- **Fail-fast initialization**: Infrastructure failures immediately terminate the application

## Operational Characteristics

### Cloud-Native Compatibility
This architecture is specifically designed for containerized environments:

- **Kubernetes**: Pod restarts on infrastructure failures
- **Docker**: Container restart policies handle failed instances
- **Service Mesh**: Health checks can detect failed containers
- **Monitoring**: Clear failure signals for alerting systems

### Error Boundaries
The Container establishes clear error boundaries:

1. **Infrastructure Layer** (Container): Fatal errors, immediate termination
2. **Service Layer** (pkg/): Recoverable errors, graceful handling
3. **Business Logic**: Domain-specific error handling

### Performance Characteristics
- **Startup Time**: Fast fail for missing infrastructure with detailed error reporting
- **Memory Usage**: Single instance per dependency type
- **Concurrent Access**: Thread-safe singleton access with proper error handling
- **Resource Efficiency**: Lazy initialization reduces startup overhead
- **Shutdown Speed**: Parallel resource cleanup reduces termination time
- **Type Performance**: Zero-cost abstractions with compile-time generic resolution

## Comparison with Alternative Patterns

### vs. Traditional DI Frameworks
| Aspect | Container | Wire/DI Frameworks |
|--------|-----------|-------------------|
| Complexity | Simple, explicit | Complex, magic |
| Build Time | No code generation | Requires generation |
| Runtime Overhead | Minimal | Higher |
| Debugging | Transparent | Often opaque |
| Learning Curve | Low | High |

### vs. Error Handling Patterns
| Pattern | Use Case | Container Choice |
|---------|----------|------------------|
| Graceful Degradation | Optional features | ❌ Not applicable |
| Circuit Breaker | Transient failures | ❌ Not infrastructure |
| Retry Logic | Network hiccups | ❌ Not initialization |
| Fail-Fast | Infrastructure setup | ✅ **Perfect fit** |

## Best Practices

### 1. **Container Usage**
```go
// ✅ Good: Extract only needed dependencies
func NewService(cnt *container.Container) *Service {
    return &Service{
        db: cnt.Queries(),
        publisher: cnt.NatsPublisher(),
    }
}

// ❌ Bad: Pass entire container
func NewService(cnt *container.Container) *Service {
    return &Service{container: cnt}  // Creates tight coupling
}
```

### 2. **Type-Safe Error Handling**
```go
// ✅ Good: Business logic errors are returned
func (s *Service) ProcessEmail(email Email) error {
    if err := s.validate(email); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }
    return nil
}

// ✅ Good: Infrastructure failures are fatal with type information
func (c *Container) DB() *pgxpool.Pool {
    return c.db.MustGet(c.ctx, func(ctx context.Context) (*pgxpool.Pool, error) {
        // Type-safe singleton with detailed error reporting
        db, err := sqlc.Conn(ctx, c.cfg.DBUrl)
        if err != nil {
            // Error will include type name: "*pgxpool.Pool"
            return nil, err
        }
        return db, nil
    })
}

// ✅ Good: Recoverable error pattern when needed
func (c *Container) OptionalService() (Service, error) {
    return c.service.Get(c.ctx, func(ctx context.Context) (Service, error) {
        // Use Get() instead of MustGet() for recoverable failures
        return NewService(ctx)
    })
}
```

### 3. **Type Safety Features**
The enhanced singleton system provides compile-time type safety:

```go
// ✅ Generic singleton with full type safety
type Container struct {
    dbPool    *singleton[*pgxpool.Pool]    // Database connection pool
    msgQueue  *singleton[jetstream.JetStream] // Message queue
    redis     *singleton[*redis.Client]    // Cache client
}

// ✅ Type-safe initialization with error context
func (c *Container) Database() *pgxpool.Pool {
    return c.dbPool.MustGet(c.ctx, func(ctx context.Context) (*pgxpool.Pool, error) {
        // Compiler ensures return type matches singleton type
        pool, err := pgxpool.Connect(ctx, c.cfg.DatabaseURL)
        return pool, err  // Type checked at compile time
    })
}

// ✅ Compile-time prevention of type mismatches
func incorrectUsage() {
    var s singleton[string]
    // This would cause a compile error:
    // s.MustGet(ctx, func(ctx context.Context) (int, error) { return 42, nil })
}
```

### 4. **Testing Strategy**
```go
// Create test container with mock implementations
func newTestContainer() *container.Container {
    return &container.Container{
        // Mock implementations for testing
    }
}
```

## Recent Enhancements ✅

The Container architecture has been significantly improved with the following features:

### 1. **Enhanced Singleton Pattern** ✅ **Implemented**
- Generic type system with compile-time safety
- Dual error handling patterns (`Get` vs `MustGet`)
- Detailed error reporting with type information
- Thread-safe concurrent access

### 2. **Parallel Shutdown System** ✅ **Implemented**
- Concurrent closer execution for faster shutdown
- Configurable timeout protection
- Error aggregation and reporting
- Context-aware cancellation support
- Backward compatibility maintained

### 3. **Future Improvements**
While the architecture is now highly mature, potential future enhancements include:

#### Configuration Validation
```go
func (cfg Config) Validate() error {
    if cfg.DBUrl == "" {
        return errors.New("database URL required")
    }
    return nil
}
```

#### Health Reporting
```go
func (c *Container) Health() map[string]bool {
    return map[string]bool{
        "database": c.db.value != nil,
        "nats":     c.nats.value != nil,
    }
}
```

### 3. **Enhanced Parallel Shutdown** ✅ **Already Implemented**
```go
// Parallel shutdown with timeout and error aggregation
func (c *Container) CloseWithTimeout(timeout time.Duration) error {
    if len(c.closers) == 0 {
        return nil
    }

    errCh := make(chan error, len(c.closers))
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()

    // Close resources in parallel
    for _, closer := range c.closers {
        go func(fn CloserFunc) {
            errCh <- fn(ctx)
        }(closer)
    }

    var errs []error
    for i := 0; i < len(c.closers); i++ {
        select {
        case err := <-errCh:
            if err != nil {
                errs = append(errs, err)
            }
        case <-ctx.Done():
            return fmt.Errorf("shutdown timeout exceeded: %w", ctx.Err())
        }
    }

    if len(errs) > 0 {
        return fmt.Errorf("shutdown errors: %v", errs)
    }
    return nil
}

// Backward-compatible Close method
func (c *Container) Close() {
    if err := c.CloseWithTimeout(30 * time.Second); err != nil {
        logrus.Errorf("Shutdown errors: %v", err)
    }
}
```

#### Key Features:
- **Parallel Execution**: All closers run simultaneously in separate goroutines
- **Timeout Protection**: Configurable timeout prevents indefinite hangs
- **Error Aggregation**: Collects and reports all errors from failed closers
- **Context Support**: Passes timeout context to closers for graceful cancellation
- **Backward Compatibility**: Existing `Close()` method preserved with sensible defaults

The Container architecture represents a **mature, cloud-native approach** to dependency injection that prioritizes **operational simplicity** and **reliability** over complex error recovery mechanisms.