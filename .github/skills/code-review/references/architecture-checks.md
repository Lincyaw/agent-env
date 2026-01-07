# Architecture Validation Checklist

Comprehensive guide for validating software architecture health, robustness, and maintainability.

## Table of Contents

- [Layered Architecture](#layered-architecture)
- [Dependency Architecture](#dependency-architecture)
- [Domain-Driven Design](#domain-driven-design)
- [Error Handling Architecture](#error-handling-architecture)
- [Testing Architecture](#testing-architecture)
- [Performance Architecture](#performance-architecture)
- [Security Architecture](#security-architecture)
- [Observability Architecture](#observability-architecture)
- [Deployment Architecture](#deployment-architecture)
- [Documentation Architecture](#documentation-architecture)

---

## Layered Architecture

### Layer Separation

**Principle**: Clear boundaries between presentation, business logic, and data layers.

**Validation Checklist**:
- [ ] **Presentation Layer** (API/UI) contains only request/response handling
- [ ] **Business Layer** (Services/Domain) contains all business rules
- [ ] **Data Layer** (Repositories) contains only persistence logic
- [ ] No business logic in handlers/controllers
- [ ] No SQL queries in services
- [ ] No HTTP-specific code in domain

**Good Structure**:
```
project/
├── api/                    # Presentation Layer
│   ├── handlers/          # HTTP handlers
│   ├── middleware/        # HTTP middleware
│   └── dto/               # Request/Response DTOs
├── domain/                 # Business Layer
│   ├── models/            # Domain entities
│   ├── services/          # Business logic
│   ├── repositories/      # Repository interfaces
│   └── errors/            # Domain errors
├── infrastructure/         # Data Layer
│   ├── database/          # DB implementations
│   ├── cache/             # Cache implementations
│   └── external/          # External API clients
└── cmd/                    # Application entry points
```

**Code Example**:
```go
// ✅ Good: Clear layer separation

// Presentation Layer (api/handlers/user_handler.go)
type UserHandler struct {
    service *domain.UserService
}

func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
    var req CreateUserRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respondError(w, http.StatusBadRequest, "invalid request")
        return
    }
    
    // Call business layer
    user, err := h.service.CreateUser(r.Context(), req.Email, req.Name)
    if err != nil {
        handleServiceError(w, err)
        return
    }
    
    respondJSON(w, http.StatusCreated, user)
}

// Business Layer (domain/services/user_service.go)
type UserService struct {
    repo UserRepository
    emailService EmailService
}

func (s *UserService) CreateUser(ctx context.Context, email, name string) (*User, error) {
    // Business validation
    if err := validateEmail(email); err != nil {
        return nil, NewValidationError("email", err)
    }
    
    // Business logic
    user := &User{
        ID:    generateID(),
        Email: email,
        Name:  name,
        CreatedAt: time.Now(),
    }
    
    // Delegate to data layer
    if err := s.repo.Save(ctx, user); err != nil {
        return nil, fmt.Errorf("save user: %w", err)
    }
    
    // Side effects
    go s.emailService.SendWelcome(user)
    
    return user, nil
}

// Data Layer (infrastructure/database/user_repository.go)
type PostgresUserRepository struct {
    db *sql.DB
}

func (r *PostgresUserRepository) Save(ctx context.Context, user *User) error {
    _, err := r.db.ExecContext(ctx,
        "INSERT INTO users (id, email, name, created_at) VALUES ($1, $2, $3, $4)",
        user.ID, user.Email, user.Name, user.CreatedAt,
    )
    return err
}
```

### Dependency Direction

**Principle**: Dependencies point inward toward domain core.

**Validation**:
- [ ] Domain layer has NO external dependencies
- [ ] Infrastructure depends on domain (not reverse)
- [ ] Presentation depends on domain (not reverse)
- [ ] All external libraries isolated in infrastructure
- [ ] Domain defines interfaces, infrastructure implements

**Dependency Flow**:
```
Presentation → Domain ← Infrastructure
     ↓           ↑           ↓
   HTTP      Interfaces    Database
  handlers                 External APIs
```

**Anti-Pattern to Avoid**:
```go
// ❌ Bad: Domain depends on infrastructure
package domain

import "github.com/lib/pq"  // PostgreSQL driver

type User struct {
    ID string
}

func (u *User) Save() error {
    db := pq.Connect(...)  // Domain knows about Postgres!
    // ...
}

// ✅ Good: Domain defines interface, infrastructure implements
package domain

type User struct {
    ID string
}

type UserRepository interface {
    Save(ctx context.Context, user *User) error
}

// Infrastructure implements interface
package infrastructure

import "github.com/lib/pq"

type PostgresUserRepository struct {
    db *sql.DB
}

func (r *PostgresUserRepository) Save(ctx context.Context, user *User) error {
    // Postgres-specific code isolated here
}
```

---

## Dependency Architecture

### Dependency Injection

**Validation Checklist**:
- [ ] All dependencies injected through constructors
- [ ] No `init()` functions creating dependencies
- [ ] No global variables for stateful dependencies
- [ ] Dependencies declared as interfaces
- [ ] Constructor parameters are minimal (< 7)
- [ ] All dependencies required, no optionals in constructor

**Example**:
```go
// ✅ Good: Constructor injection
type OrderService struct {
    repo      OrderRepository
    payment   PaymentService
    inventory InventoryService
    events    EventPublisher
}

func NewOrderService(
    repo OrderRepository,
    payment PaymentService,
    inventory InventoryService,
    events EventPublisher,
) *OrderService {
    return &OrderService{
        repo:      repo,
        payment:   payment,
        inventory: inventory,
        events:    events,
    }
}

// Wire dependencies in main
func main() {
    db := connectDatabase()
    
    orderRepo := infrastructure.NewOrderRepository(db)
    paymentService := infrastructure.NewStripeService(cfg.Stripe)
    inventoryService := infrastructure.NewInventoryService(db)
    eventBus := infrastructure.NewEventBus()
    
    orderService := domain.NewOrderService(
        orderRepo,
        paymentService,
        inventoryService,
        eventBus,
    )
    
    handler := api.NewOrderHandler(orderService)
    // ...
}
```

### Circular Dependencies

**Validation**:
- [ ] No import cycles between packages
- [ ] No mutual dependencies between services
- [ ] Clear component initialization order
- [ ] Use interfaces to break cycles if needed

**Breaking Cycles**:
```go
// ❌ Bad: Circular dependency
// package user imports package order
// package order imports package user

// ✅ Good: Use shared interfaces package
package domain

type UserRepository interface {
    FindByID(id string) (*User, error)
}

type OrderRepository interface {
    FindByUserID(userID string) ([]*Order, error)
}

// package user implements and uses domain interfaces
// package order implements and uses domain interfaces
// No circular dependency
```

---

## Domain-Driven Design

### Bounded Contexts

**Validation**:
- [ ] Clear domain boundaries identified
- [ ] Each context has its own models
- [ ] Contexts communicate via well-defined interfaces
- [ ] No shared database tables between contexts
- [ ] Context maps documented

**Example Structure**:
```
project/
├── users/              # User Management Context
│   ├── domain/
│   ├── repository/
│   └── service/
├── orders/             # Order Management Context
│   ├── domain/
│   ├── repository/
│   └── service/
├── inventory/          # Inventory Context
│   ├── domain/
│   ├── repository/
│   └── service/
└── shared/             # Shared kernel
    └── types/
```

### Aggregates and Entities

**Validation**:
- [ ] Aggregates enforce invariants
- [ ] Aggregate roots control access to entities
- [ ] Entities have identity
- [ ] Value objects are immutable
- [ ] Aggregates are consistency boundaries

**Example**:
```go
// Aggregate Root
type Order struct {
    id          string
    customerID  string
    items       []OrderItem
    total       decimal.Decimal
    status      OrderStatus
    createdAt   time.Time
}

// Enforce invariants in aggregate
func (o *Order) AddItem(item OrderItem) error {
    if o.status != OrderStatusDraft {
        return errors.New("cannot add items to non-draft order")
    }
    
    // Business rule
    if len(o.items) >= 100 {
        return errors.New("order cannot have more than 100 items")
    }
    
    o.items = append(o.items, item)
    o.recalculateTotal()
    return nil
}

func (o *Order) Submit() error {
    if len(o.items) == 0 {
        return errors.New("cannot submit empty order")
    }
    
    o.status = OrderStatusPending
    return nil
}

// Private method ensures invariants
func (o *Order) recalculateTotal() {
    total := decimal.Zero
    for _, item := range o.items {
        total = total.Add(item.Price.Mul(decimal.NewFromInt(int64(item.Quantity))))
    }
    o.total = total
}

// Entity
type OrderItem struct {
    id       string
    productID string
    quantity int
    price    decimal.Decimal
}

// Value Object (immutable)
type Money struct {
    amount   decimal.Decimal
    currency string
}

func NewMoney(amount decimal.Decimal, currency string) (Money, error) {
    if amount.IsNegative() {
        return Money{}, errors.New("amount cannot be negative")
    }
    return Money{amount: amount, currency: currency}, nil
}
```

---

## Error Handling Architecture

### Error Strategy

**Validation Checklist**:
- [ ] Consistent error handling across codebase
- [ ] Errors wrapped with context
- [ ] Domain errors defined
- [ ] Error types distinguish recoverable vs non-recoverable
- [ ] Errors logged at appropriate level
- [ ] Stack traces available for debugging

**Error Hierarchy**:
```go
// Domain errors
type DomainError struct {
    Code    string
    Message string
    Err     error
}

func (e *DomainError) Error() string {
    return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *DomainError) Unwrap() error {
    return e.Err
}

// Specific domain errors
var (
    ErrNotFound      = &DomainError{Code: "NOT_FOUND", Message: "resource not found"}
    ErrValidation    = &DomainError{Code: "VALIDATION", Message: "validation failed"}
    ErrUnauthorized  = &DomainError{Code: "UNAUTHORIZED", Message: "unauthorized"}
    ErrConflict      = &DomainError{Code: "CONFLICT", Message: "resource conflict"}
)

// Create specific error with context
func NewValidationError(field string, err error) error {
    return fmt.Errorf("%w: field %s: %v", ErrValidation, field, err)
}

// Handle errors consistently
func (h *Handler) handleError(w http.ResponseWriter, err error) {
    switch {
    case errors.Is(err, ErrNotFound):
        respondError(w, http.StatusNotFound, err.Error())
    case errors.Is(err, ErrValidation):
        respondError(w, http.StatusBadRequest, err.Error())
    case errors.Is(err, ErrUnauthorized):
        respondError(w, http.StatusUnauthorized, err.Error())
    case errors.Is(err, ErrConflict):
        respondError(w, http.StatusConflict, err.Error())
    default:
        log.Error("internal error", "error", err)
        respondError(w, http.StatusInternalServerError, "internal server error")
    }
}
```

### Panic Recovery

**Validation**:
- [ ] Panics recovered at application boundaries
- [ ] Panics logged with stack traces
- [ ] Panics converted to errors
- [ ] No naked panics in business logic
- [ ] Recovery middleware in HTTP handlers

```go
// Recovery middleware
func RecoveryMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer func() {
            if err := recover(); err != nil {
                log.Error("panic recovered",
                    "error", err,
                    "stack", string(debug.Stack()),
                    "path", r.URL.Path,
                )
                
                w.WriteHeader(http.StatusInternalServerError)
                json.NewEncoder(w).Encode(map[string]string{
                    "error": "internal server error",
                })
            }
        }()
        
        next.ServeHTTP(w, r)
    })
}
```

---

## Testing Architecture

### Test Organization

**Validation Checklist**:
- [ ] Unit tests for business logic (domain/)
- [ ] Integration tests for repositories (infrastructure/)
- [ ] API tests for handlers (api/)
- [ ] Test files alongside production code (*_test.go)
- [ ] Test coverage > 80% for business logic
- [ ] Critical paths have tests
- [ ] Edge cases covered

**Test Structure**:
```
project/
├── domain/
│   ├── user.go
│   ├── user_test.go        # Unit tests
│   ├── service.go
│   └── service_test.go
├── infrastructure/
│   ├── repository.go
│   └── repository_test.go  # Integration tests
└── api/
    ├── handler.go
    └── handler_test.go     # API tests
```

### Test Quality

**Validation**:
- [ ] Tests are independent (no shared state)
- [ ] Tests use table-driven pattern
- [ ] Tests have clear arrange-act-assert structure
- [ ] Mocks used for external dependencies
- [ ] Tests don't depend on execution order
- [ ] Fast unit tests (< 100ms each)
- [ ] Slow integration tests isolated

**Example**:
```go
func TestUserService_CreateUser(t *testing.T) {
    tests := []struct {
        name    string
        email   string
        wantErr error
    }{
        {
            name:    "valid email",
            email:   "user@example.com",
            wantErr: nil,
        },
        {
            name:    "invalid email",
            email:   "invalid",
            wantErr: ErrValidation,
        },
        {
            name:    "duplicate email",
            email:   "existing@example.com",
            wantErr: ErrConflict,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Arrange
            repo := &MockUserRepository{}
            if tt.email == "existing@example.com" {
                repo.ExistingEmails = []string{tt.email}
            }
            service := NewUserService(repo)
            
            // Act
            _, err := service.CreateUser(context.Background(), tt.email, "Test User")
            
            // Assert
            if !errors.Is(err, tt.wantErr) {
                t.Errorf("got error %v, want %v", err, tt.wantErr)
            }
        })
    }
}
```

---

## Performance Architecture

### Caching Strategy

**Validation**:
- [ ] Cache layer defined
- [ ] Cache invalidation strategy clear
- [ ] Cache keys consistent
- [ ] TTL configured appropriately
- [ ] Cache warming strategy for cold starts
- [ ] Cache hit/miss metrics tracked

**Example**:
```go
type CachedUserRepository struct {
    repo  UserRepository
    cache Cache
    ttl   time.Duration
}

func (r *CachedUserRepository) FindByID(ctx context.Context, id string) (*User, error) {
    // Try cache first
    key := fmt.Sprintf("user:%s", id)
    if data, err := r.cache.Get(ctx, key); err == nil {
        var user User
        if err := json.Unmarshal(data, &user); err == nil {
            return &user, nil
        }
    }
    
    // Cache miss - fetch from DB
    user, err := r.repo.FindByID(ctx, id)
    if err != nil {
        return nil, err
    }
    
    // Update cache
    if data, err := json.Marshal(user); err == nil {
        _ = r.cache.Set(ctx, key, data, r.ttl)
    }
    
    return user, nil
}

func (r *CachedUserRepository) Save(ctx context.Context, user *User) error {
    // Save to DB
    if err := r.repo.Save(ctx, user); err != nil {
        return err
    }
    
    // Invalidate cache
    key := fmt.Sprintf("user:%s", user.ID)
    _ = r.cache.Delete(ctx, key)
    
    return nil
}
```

### Database Query Optimization

**Validation**:
- [ ] Indexes on frequently queried columns
- [ ] N+1 queries eliminated
- [ ] Pagination implemented
- [ ] Query timeout configured
- [ ] Connection pooling configured
- [ ] Prepared statements used

**Example**:
```go
// ✅ Good: Efficient query with join
func (r *OrderRepository) FindWithItems(ctx context.Context, orderID string) (*Order, error) {
    query := `
        SELECT 
            o.id, o.customer_id, o.status, o.created_at,
            oi.id, oi.product_id, oi.quantity, oi.price
        FROM orders o
        LEFT JOIN order_items oi ON oi.order_id = o.id
        WHERE o.id = $1
    `
    
    rows, err := r.db.QueryContext(ctx, query, orderID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    // Process results...
}

// Pagination
func (r *OrderRepository) List(ctx context.Context, opts ListOptions) ([]*Order, error) {
    query := `
        SELECT * FROM orders
        WHERE status = $1
        ORDER BY created_at DESC
        LIMIT $2 OFFSET $3
    `
    
    rows, err := r.db.QueryContext(ctx, query, opts.Status, opts.Limit, opts.Offset)
    // ...
}
```

### Concurrency Patterns

**Validation**:
- [ ] Goroutines have bounded lifecycle
- [ ] Context used for cancellation
- [ ] Worker pools for bounded concurrency
- [ ] Rate limiting implemented
- [ ] No data races (`go test -race` passes)

**Example**:
```go
// Worker pool pattern
func (s *ProcessingService) ProcessBatch(ctx context.Context, items []Item) error {
    const maxWorkers = 10
    
    itemsCh := make(chan Item, len(items))
    errCh := make(chan error, len(items))
    
    // Start workers
    var wg sync.WaitGroup
    for i := 0; i < maxWorkers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for item := range itemsCh {
                if err := s.processItem(ctx, item); err != nil {
                    errCh <- err
                }
            }
        }()
    }
    
    // Send items
    for _, item := range items {
        select {
        case itemsCh <- item:
        case <-ctx.Done():
            return ctx.Err()
        }
    }
    close(itemsCh)
    
    // Wait for completion
    wg.Wait()
    close(errCh)
    
    // Collect errors
    var errors []error
    for err := range errCh {
        errors = append(errors, err)
    }
    
    if len(errors) > 0 {
        return fmt.Errorf("processing errors: %v", errors)
    }
    
    return nil
}
```

---

## Security Architecture

### Authentication & Authorization

**Validation**:
- [ ] Authentication middleware implemented
- [ ] Authorization checks at service layer
- [ ] Role-based access control (RBAC) defined
- [ ] JWT tokens validated properly
- [ ] Session management secure
- [ ] Password hashing (bcrypt/argon2)

**Example**:
```go
// Authentication middleware
func AuthMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        token := extractToken(r)
        
        claims, err := validateJWT(token)
        if err != nil {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        
        // Add user to context
        ctx := context.WithValue(r.Context(), "user_id", claims.UserID)
        ctx = context.WithValue(ctx, "roles", claims.Roles)
        
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

// Authorization in service layer
func (s *OrderService) DeleteOrder(ctx context.Context, orderID string) error {
    userID := ctx.Value("user_id").(string)
    roles := ctx.Value("roles").([]string)
    
    order, err := s.repo.FindByID(ctx, orderID)
    if err != nil {
        return err
    }
    
    // Check authorization
    if order.CustomerID != userID && !hasRole(roles, "admin") {
        return ErrUnauthorized
    }
    
    return s.repo.Delete(ctx, orderID)
}
```

### Input Validation

**Validation**:
- [ ] All user input validated
- [ ] SQL injection prevented (parameterized queries)
- [ ] XSS prevention (output encoding)
- [ ] CSRF tokens used
- [ ] File upload validation (size, type, content)
- [ ] Rate limiting on endpoints

**Example**:
```go
// Input validation
type CreateUserRequest struct {
    Email    string `json:"email" validate:"required,email"`
    Name     string `json:"name" validate:"required,min=2,max=100"`
    Password string `json:"password" validate:"required,min=8"`
}

func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
    var req CreateUserRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respondError(w, http.StatusBadRequest, "invalid request")
        return
    }
    
    // Validate
    if err := validator.Validate(req); err != nil {
        respondError(w, http.StatusBadRequest, err.Error())
        return
    }
    
    // Sanitize
    req.Name = html.EscapeString(req.Name)
    
    // Process...
}

// Parameterized queries (prevent SQL injection)
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
    query := "SELECT * FROM users WHERE email = $1"  // Parameterized
    row := r.db.QueryRowContext(ctx, query, email)
    // ...
}
```

### Secrets Management

**Validation**:
- [ ] No secrets in code
- [ ] Environment variables or secret manager
- [ ] Secrets encrypted at rest
- [ ] Secrets rotated regularly
- [ ] API keys have appropriate scopes

---

## Observability Architecture

### Logging

**Validation**:
- [ ] Structured logging used
- [ ] Log levels appropriate (DEBUG, INFO, WARN, ERROR)
- [ ] Request ID propagated through stack
- [ ] No sensitive data in logs
- [ ] Logs aggregated centrally
- [ ] Log rotation configured

**Example**:
```go
// Structured logging
log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

log.Info("user created",
    "user_id", user.ID,
    "email", user.Email,
    "request_id", requestID,
)

log.Error("database error",
    "error", err,
    "query", query,
    "duration_ms", duration.Milliseconds(),
)

// Context-aware logging
func (s *Service) Process(ctx context.Context) error {
    requestID := ctx.Value("request_id").(string)
    
    log := s.logger.With("request_id", requestID)
    log.Info("processing started")
    
    // ...
    
    log.Info("processing completed", "duration", duration)
}
```

### Metrics

**Validation**:
- [ ] Key business metrics tracked
- [ ] HTTP metrics (requests, duration, status codes)
- [ ] Database metrics (query duration, connections)
- [ ] Error rates tracked
- [ ] Custom metrics for domain events
- [ ] Metrics exported (Prometheus format)

**Example**:
```go
var (
    httpRequestsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "http_requests_total",
            Help: "Total number of HTTP requests",
        },
        []string{"method", "path", "status"},
    )
    
    httpRequestDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "http_request_duration_seconds",
            Help: "HTTP request duration in seconds",
        },
        []string{"method", "path"},
    )
    
    ordersCreated = promauto.NewCounter(
        prometheus.CounterOpts{
            Name: "orders_created_total",
            Help: "Total number of orders created",
        },
    )
)

// Instrument code
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    start := time.Now()
    
    rw := &responseWriter{ResponseWriter: w}
    h.next.ServeHTTP(rw, r)
    
    duration := time.Since(start).Seconds()
    
    httpRequestsTotal.WithLabelValues(
        r.Method,
        r.URL.Path,
        fmt.Sprintf("%d", rw.status),
    ).Inc()
    
    httpRequestDuration.WithLabelValues(
        r.Method,
        r.URL.Path,
    ).Observe(duration)
}
```

### Tracing

**Validation**:
- [ ] Distributed tracing implemented
- [ ] Spans created for key operations
- [ ] Context propagated across services
- [ ] Trace IDs in logs
- [ ] Tracing backend configured (Jaeger, Zipkin)

---

## Deployment Architecture

### Configuration Management

**Validation**:
- [ ] Environment-specific configs
- [ ] Secrets separate from config
- [ ] Config validation on startup
- [ ] Config changes don't require rebuild
- [ ] Default values provided

### Health Checks

**Validation**:
- [ ] Liveness endpoint (`/health/live`)
- [ ] Readiness endpoint (`/health/ready`)
- [ ] Dependency checks (DB, cache, external APIs)
- [ ] Graceful shutdown implemented
- [ ] Startup probes configured

**Example**:
```go
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
    checks := []struct {
        name  string
        check func() error
    }{
        {"database", h.checkDatabase},
        {"cache", h.checkCache},
        {"external_api", h.checkExternalAPI},
    }
    
    var errors []string
    for _, c := range checks {
        if err := c.check(); err != nil {
            errors = append(errors, fmt.Sprintf("%s: %v", c.name, err))
        }
    }
    
    if len(errors) > 0 {
        w.WriteHeader(http.StatusServiceUnavailable)
        json.NewEncoder(w).Encode(map[string]interface{}{
            "status": "unhealthy",
            "errors": errors,
        })
        return
    }
    
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{
        "status": "healthy",
    })
}

// Graceful shutdown
func (s *Server) Shutdown(ctx context.Context) error {
    log.Info("shutting down server")
    
    // Stop accepting new requests
    if err := s.httpServer.Shutdown(ctx); err != nil {
        return err
    }
    
    // Close database connections
    if err := s.db.Close(); err != nil {
        return err
    }
    
    // Close cache connections
    if err := s.cache.Close(); err != nil {
        return err
    }
    
    log.Info("server shutdown complete")
    return nil
}
```

---

## Documentation Architecture

### Code Documentation

**Validation**:
- [ ] Public APIs documented (godoc/docstring)
- [ ] Complex algorithms explained
- [ ] Architecture decisions recorded (ADRs)
- [ ] API documentation (OpenAPI/Swagger)
- [ ] README with setup instructions
- [ ] CONTRIBUTING guide for developers

### Architecture Documentation

**Validation**:
- [ ] System architecture diagram
- [ ] Component interaction diagram
- [ ] Data flow diagrams
- [ ] Deployment diagram
- [ ] Decision records (ADRs)
- [ ] API contracts documented

---

## Summary Checklist

### Critical (Must Have)

- [ ] Clear layer separation
- [ ] Dependencies injected, not global
- [ ] Errors handled consistently
- [ ] Tests cover critical paths
- [ ] No data races
- [ ] Security validations in place
- [ ] Logging and metrics implemented
- [ ] Health checks configured
- [ ] No secrets in code

### Important (Should Have)

- [ ] Domain model well-defined
- [ ] Caching strategy implemented
- [ ] Database queries optimized
- [ ] API versioning support
- [ ] Graceful shutdown
- [ ] Comprehensive error handling
- [ ] Monitoring dashboards
- [ ] Documentation updated

### Nice to Have

- [ ] Distributed tracing
- [ ] Feature flags
- [ ] Plugin architecture
- [ ] Auto-scaling configured
- [ ] Chaos engineering tests
- [ ] Load testing automated
- [ ] Performance benchmarks

---

## Architecture Review Process

1. **High-Level Review**
   - Understand system purpose and requirements
   - Review architecture diagrams
   - Identify major components and boundaries

2. **Layer Analysis**
   - Verify layer separation
   - Check dependency direction
   - Validate component responsibilities

3. **Code Quality**
   - Review error handling strategy
   - Check testing coverage and quality
   - Validate security practices

4. **Performance & Scalability**
   - Review caching strategy
   - Check query optimization
   - Validate concurrency patterns

5. **Operational Readiness**
   - Verify observability (logs, metrics, traces)
   - Check health checks and graceful shutdown
   - Review deployment configuration

6. **Documentation**
   - Verify code documentation
   - Check architecture documentation
   - Validate API documentation

**Output**: Report with critical/major/minor issues and specific recommendations for improvement.
