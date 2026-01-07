# Anti-Patterns Reference Guide

Comprehensive guide for identifying and addressing common anti-patterns in software development.

## Table of Contents

- [Structural Anti-Patterns](#structural-anti-patterns)
- [Behavioral Anti-Patterns](#behavioral-anti-patterns)
- [Architectural Anti-Patterns](#architectural-anti-patterns)
- [Concurrency Anti-Patterns](#concurrency-anti-patterns)
- [Database Anti-Patterns](#database-anti-patterns)
- [Go-Specific Anti-Patterns](#go-specific-anti-patterns)
- [Python-Specific Anti-Patterns](#python-specific-anti-patterns)

---

## Structural Anti-Patterns

### God Object / God Class

**Description**: Single class/object that knows too much or does too much.

**Symptoms**:
- Class with 1000+ lines
- Dozens of methods and fields
- Multiple unrelated responsibilities
- High coupling with many other classes

**Impact**: Unmaintainable, untestable, hard to understand

**Fix**:
```go
// ❌ Bad: God object
type UserManager struct {
    db *sql.DB
    cache *redis.Client
    emailService *EmailService
    smsService *SMSService
    paymentProcessor *PaymentProcessor
}

func (m *UserManager) CreateUser(...) { }
func (m *UserManager) DeleteUser(...) { }
func (m *UserManager) SendEmail(...) { }
func (m *UserManager) ProcessPayment(...) { }
func (m *UserManager) GenerateReport(...) { }
func (m *UserManager) HandleWebhook(...) { }

// ✅ Good: Separated responsibilities
type UserRepository struct {
    db *sql.DB
}

type NotificationService struct {
    email *EmailService
    sms   *SMSService
}

type PaymentService struct {
    processor *PaymentProcessor
}
```

### Tight Coupling

**Description**: Components depend directly on concrete implementations rather than abstractions.

**Symptoms**:
- Direct instantiation of dependencies
- Hard-coded class names
- Cannot test in isolation
- Changes cascade across components

**Impact**: Difficult to test, extend, or modify

**Fix**:
```go
// ❌ Bad: Tight coupling
type OrderService struct {
    db *PostgresDB  // Concrete type
}

func NewOrderService() *OrderService {
    return &OrderService{
        db: NewPostgresDB(),  // Hard-coded
    }
}

// ✅ Good: Dependency injection with interface
type OrderService struct {
    repo OrderRepository  // Interface
}

type OrderRepository interface {
    Save(order *Order) error
    FindByID(id string) (*Order, error)
}

func NewOrderService(repo OrderRepository) *OrderService {
    return &OrderService{repo: repo}
}
```

### Circular Dependencies

**Description**: A depends on B, and B depends on A (directly or transitively).

**Symptoms**:
- Import cycles in Go
- Initialization deadlocks
- Complex bootstrapping logic
- Hard to reason about component lifecycle

**Impact**: Compile errors, runtime deadlocks, fragile initialization

**Fix**:
```go
// ❌ Bad: Circular dependency
// package user imports package order
// package order imports package user

// ✅ Good: Introduce abstraction layer
// package user uses interface from package domain
// package order uses interface from package domain
// package domain has no dependencies

package domain

type UserRepository interface {
    FindByID(id string) (*User, error)
}

type OrderRepository interface {
    FindByUserID(userID string) ([]*Order, error)
}
```

### Shotgun Surgery

**Description**: Single change requires modifications in many places.

**Symptoms**:
- Same logic duplicated across files
- Adding feature touches 10+ files
- Bug fixes require changing multiple files
- Inconsistent implementations of same concept

**Impact**: Error-prone changes, missed updates, inconsistent behavior

**Fix**:
```go
// ❌ Bad: Duplicated validation logic everywhere
func CreateUser(email string) error {
    if !strings.Contains(email, "@") {
        return errors.New("invalid email")
    }
    // ...
}

func UpdateUser(email string) error {
    if !strings.Contains(email, "@") {
        return errors.New("invalid email")
    }
    // ...
}

// ✅ Good: Centralized validation
type Email string

func NewEmail(s string) (Email, error) {
    if !strings.Contains(s, "@") {
        return "", errors.New("invalid email")
    }
    return Email(s), nil
}

func CreateUser(email Email) error {
    // Validation already done by type constructor
}
```

---

## Behavioral Anti-Patterns

### Error Swallowing

**Description**: Ignoring or hiding errors without proper handling.

**Symptoms**:
- `_ = someFunc()` without justification
- Empty catch blocks
- Errors logged but not propagated
- Silent failures

**Impact**: Bugs hidden, debugging nightmares, data corruption

**Fix**:
```go
// ❌ Bad: Swallowing errors
func ProcessData() {
    data, _ := fetchData()  // Error ignored
    _ = saveData(data)       // Error ignored
}

// ✅ Good: Proper error handling
func ProcessData() error {
    data, err := fetchData()
    if err != nil {
        return fmt.Errorf("fetch data: %w", err)
    }
    
    if err := saveData(data); err != nil {
        return fmt.Errorf("save data: %w", err)
    }
    
    return nil
}
```

### Callback Hell

**Description**: Deeply nested callbacks making code hard to follow.

**Symptoms**:
- Indentation levels > 5
- Arrow-shaped code
- Hard to track control flow
- Error handling scattered

**Impact**: Unreadable, unmaintainable, error-prone

**Fix**:
```python
# ❌ Bad: Callback hell
def process_order(order_id):
    get_order(order_id, lambda order:
        validate_order(order, lambda valid:
            if valid:
                charge_payment(order, lambda charged:
                    if charged:
                        update_inventory(order, lambda updated:
                            if updated:
                                send_confirmation(order)
                        )
                )
        )
    )

# ✅ Good: Linear async/await
async def process_order(order_id: str) -> None:
    order = await get_order(order_id)
    
    if not await validate_order(order):
        raise ValidationError("Invalid order")
    
    if not await charge_payment(order):
        raise PaymentError("Payment failed")
    
    await update_inventory(order)
    await send_confirmation(order)
```

### Magic Numbers / Magic Strings

**Description**: Hard-coded values without explanation.

**Symptoms**:
- Unexplained numeric literals
- String literals scattered throughout code
- Duplicate values in multiple places
- No clear meaning

**Impact**: Hard to understand, modify, and maintain

**Fix**:
```go
// ❌ Bad: Magic numbers
func ProcessBatch(items []Item) {
    for i := 0; i < len(items); i += 100 {
        batch := items[i:min(i+100, len(items))]
        if len(batch) > 50 {
            // ...
        }
    }
    time.Sleep(5 * time.Second)
}

// ✅ Good: Named constants
const (
    BatchSize = 100
    BatchThreshold = 50
    ProcessingDelay = 5 * time.Second
)

func ProcessBatch(items []Item) {
    for i := 0; i < len(items); i += BatchSize {
        batch := items[i:min(i+BatchSize, len(items))]
        if len(batch) > BatchThreshold {
            // ...
        }
    }
    time.Sleep(ProcessingDelay)
}
```

### Premature Optimization

**Description**: Optimizing before knowing there's a performance problem.

**Symptoms**:
- Complex code for marginal gains
- Micro-optimizations everywhere
- No benchmarks to justify complexity
- Sacrificing readability for speed

**Impact**: Reduced maintainability, wasted effort, often slower

**Fix**:
```go
// ❌ Bad: Premature optimization
func SumValues(data []int) int {
    // Complex bit manipulation "optimization"
    sum := 0
    for i := 0; i < len(data); i++ {
        sum += data[i] ^ ((data[i] >> 31) & 1)
    }
    return sum
}

// ✅ Good: Clear, simple code (optimize if proven slow)
func SumValues(data []int) int {
    sum := 0
    for _, v := range data {
        sum += v
    }
    return sum
}

// Only add complexity if benchmarks show need:
// BenchmarkSumValues-8  1000000  1234 ns/op
```

---

## Architectural Anti-Patterns

### Big Ball of Mud

**Description**: System with no discernible architecture.

**Symptoms**:
- No clear package/module structure
- Everything depends on everything
- No separation of concerns
- Ad-hoc code organization

**Impact**: Impossible to understand, modify, or extend

**Fix**: Implement layered architecture with clear boundaries:
```
domain/      # Business logic, no external dependencies
  models/
  services/
  repositories/  # Interfaces only

infrastructure/  # External dependencies
  database/
  http/
  cache/

application/  # Use cases, orchestration
  handlers/
  commands/
  queries/
```

### Leaky Abstraction

**Description**: Implementation details leak through abstraction layer.

**Symptoms**:
- Database types in API responses
- SQL errors bubbling to UI
- Framework-specific types in domain logic
- Cannot swap implementations

**Impact**: Tight coupling, hard to test, fragile

**Fix**:
```go
// ❌ Bad: Leaky abstraction
type UserRepository interface {
    FindByID(id string) (*sql.Row, error)  // SQL-specific
}

// ✅ Good: Clean abstraction
type UserRepository interface {
    FindByID(id string) (*User, error)  // Domain type
}

// Implementation handles SQL details
type PostgresUserRepository struct {
    db *sql.DB
}

func (r *PostgresUserRepository) FindByID(id string) (*User, error) {
    row := r.db.QueryRow("SELECT * FROM users WHERE id = $1", id)
    var u User
    if err := row.Scan(&u.ID, &u.Name, &u.Email); err != nil {
        if err == sql.ErrNoRows {
            return nil, ErrNotFound  // Domain error
        }
        return nil, fmt.Errorf("scan user: %w", err)
    }
    return &u, nil
}
```

### Missing Abstraction Layer

**Description**: Business logic directly coupled to infrastructure.

**Symptoms**:
- HTTP handlers contain business logic
- Controllers query database directly
- Business rules mixed with persistence
- Cannot test without database

**Impact**: Untestable, inflexible, hard to change

**Fix**:
```go
// ❌ Bad: No abstraction
func CreateUserHandler(w http.ResponseWriter, r *http.Request) {
    var req CreateUserRequest
    json.NewDecoder(r.Body).Decode(&req)
    
    // Business logic in handler!
    if len(req.Email) < 3 || !strings.Contains(req.Email, "@") {
        http.Error(w, "invalid email", 400)
        return
    }
    
    // Direct DB access!
    _, err := db.Exec("INSERT INTO users ...")
    // ...
}

// ✅ Good: Layered architecture
// Handler
func CreateUserHandler(w http.ResponseWriter, r *http.Request) {
    var req CreateUserRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid request", 400)
        return
    }
    
    user, err := userService.Create(r.Context(), req.Email, req.Name)
    if err != nil {
        handleError(w, err)
        return
    }
    
    json.NewEncoder(w).Encode(user)
}

// Service (business logic)
func (s *UserService) Create(ctx context.Context, email, name string) (*User, error) {
    if err := validateEmail(email); err != nil {
        return nil, err
    }
    
    user := &User{Email: email, Name: name}
    if err := s.repo.Save(ctx, user); err != nil {
        return nil, err
    }
    
    return user, nil
}

// Repository (persistence)
func (r *UserRepository) Save(ctx context.Context, user *User) error {
    _, err := r.db.ExecContext(ctx, "INSERT INTO users ...")
    return err
}
```

---

## Concurrency Anti-Patterns

### Goroutine Leak

**Description**: Goroutines that never terminate.

**Symptoms**:
- Goroutines waiting on channels forever
- No context cancellation
- Memory usage grows over time
- Go runtime shows increasing goroutine count

**Impact**: Memory leaks, resource exhaustion

**Fix**:
```go
// ❌ Bad: Goroutine leak
func ProcessStream() {
    ch := make(chan Data)
    
    go func() {
        for data := range ch {  // Never exits if ch never closes
            process(data)
        }
    }()
    
    // If we return here, goroutine leaks
}

// ✅ Good: Context-based cancellation
func ProcessStream(ctx context.Context) {
    ch := make(chan Data)
    
    go func() {
        for {
            select {
            case data := <-ch:
                process(data)
            case <-ctx.Done():
                return  // Clean exit
            }
        }
    }()
}
```

### Race Condition

**Description**: Multiple goroutines accessing shared state without synchronization.

**Symptoms**:
- `go test -race` reports races
- Intermittent test failures
- Inconsistent results
- Data corruption

**Impact**: Undefined behavior, data corruption, crashes

**Fix**:
```go
// ❌ Bad: Race condition
type Counter struct {
    value int
}

func (c *Counter) Increment() {
    c.value++  // Race!
}

// ✅ Good: Synchronized access
type Counter struct {
    mu    sync.Mutex
    value int
}

func (c *Counter) Increment() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.value++
}

// ✅ Even better: Atomic operations for simple cases
type Counter struct {
    value atomic.Int64
}

func (c *Counter) Increment() {
    c.value.Add(1)
}
```

---

## Database Anti-Patterns

### N+1 Query Problem

**Description**: Executing one query, then N additional queries for related data.

**Symptoms**:
- Query count grows with result set size
- Slow response for large data sets
- Database connection pool exhaustion

**Impact**: Severe performance degradation

**Fix**:
```go
// ❌ Bad: N+1 queries
func GetUsersWithOrders() ([]*UserWithOrders, error) {
    users, err := db.Query("SELECT * FROM users")
    // ...
    
    for _, user := range users {
        orders, err := db.Query("SELECT * FROM orders WHERE user_id = ?", user.ID)
        // N queries!
    }
}

// ✅ Good: Join or eager loading
func GetUsersWithOrders() ([]*UserWithOrders, error) {
    rows, err := db.Query(`
        SELECT u.*, o.*
        FROM users u
        LEFT JOIN orders o ON o.user_id = u.id
    `)
    // Single query with join
}
```

### Missing Indexes

**Description**: Queries without proper indexes scan entire tables.

**Symptoms**:
- Slow queries on large tables
- `EXPLAIN` shows full table scan
- High database CPU usage
- Queries slow down as data grows

**Impact**: Poor performance, scalability issues

**Fix**:
```sql
-- ❌ Bad: No index on frequently queried column
SELECT * FROM orders WHERE user_id = ?;
-- Full table scan on millions of rows

-- ✅ Good: Add index
CREATE INDEX idx_orders_user_id ON orders(user_id);

-- ✅ Better: Composite index for common query patterns
CREATE INDEX idx_orders_user_status ON orders(user_id, status);
```

### Missing Transactions

**Description**: Related operations not wrapped in transaction.

**Symptoms**:
- Partial updates on errors
- Data inconsistency
- Race conditions between operations
- Cannot rollback failed operations

**Impact**: Data corruption, inconsistent state

**Fix**:
```go
// ❌ Bad: No transaction
func TransferMoney(fromID, toID string, amount decimal.Decimal) error {
    if err := debitAccount(fromID, amount); err != nil {
        return err
    }
    
    if err := creditAccount(toID, amount); err != nil {
        // First debit succeeded, second failed - inconsistent!
        return err
    }
    
    return nil
}

// ✅ Good: Atomic transaction
func TransferMoney(fromID, toID string, amount decimal.Decimal) error {
    tx, err := db.Begin()
    if err != nil {
        return err
    }
    defer tx.Rollback()  // No-op if already committed
    
    if err := debitAccount(tx, fromID, amount); err != nil {
        return err
    }
    
    if err := creditAccount(tx, toID, amount); err != nil {
        return err
    }
    
    return tx.Commit()  // All or nothing
}
```

---

## Go-Specific Anti-Patterns

### Not Using Context

**Description**: Long-running operations without context for cancellation.

**Symptoms**:
- Cannot cancel operations
- Goroutines don't respect timeouts
- No way to propagate deadlines
- Resources not released on cancellation

**Impact**: Resource leaks, unresponsive system

**Fix**:
```go
// ❌ Bad: No context
func FetchData(url string) (*Data, error) {
    resp, err := http.Get(url)  // No timeout
    // ...
}

// ✅ Good: Context-aware
func FetchData(ctx context.Context, url string) (*Data, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }
    
    resp, err := http.DefaultClient.Do(req)
    // Respects context cancellation/timeout
}
```

### Ignoring Error Returns

**Description**: Not checking error return values.

**Symptoms**:
- `_ = someFunc()` without comment
- Proceeding with nil values
- Silent failures

**Impact**: Bugs, crashes, data corruption

**Fix**:
```go
// ❌ Bad: Ignored error
func Process() {
    file, _ := os.Open("data.txt")  // Could be nil!
    defer file.Close()               // Crash if file is nil
    // ...
}

// ✅ Good: Proper error checking
func Process() error {
    file, err := os.Open("data.txt")
    if err != nil {
        return fmt.Errorf("open file: %w", err)
    }
    defer file.Close()
    // ...
    return nil
}
```

### Not Closing Resources

**Description**: File handles, connections not properly closed.

**Symptoms**:
- File descriptor leaks
- Connection pool exhaustion
- Memory leaks
- "Too many open files" errors

**Impact**: Resource exhaustion, system instability

**Fix**:
```go
// ❌ Bad: Resource leak
func ReadFile(path string) ([]byte, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    
    // Missing file.Close()!
    return io.ReadAll(file)
}

// ✅ Good: Defer close
func ReadFile(path string) ([]byte, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()  // Always closes
    
    return io.ReadAll(file)
}
```

---

## Python-Specific Anti-Patterns

### Using Mutable Default Arguments

**Description**: List/dict as function default argument.

**Symptoms**:
- Function behavior changes between calls
- Shared state between invocations
- Unexpected mutations

**Impact**: Subtle bugs, non-deterministic behavior

**Fix**:
```python
# ❌ Bad: Mutable default
def add_item(item: str, items: list = []) -> list:
    items.append(item)  # Mutates shared default!
    return items

# ✅ Good: None as default
def add_item(item: str, items: list | None = None) -> list:
    if items is None:
        items = []
    items.append(item)
    return items
```

### Bare Except Clauses

**Description**: Catching all exceptions without specificity.

**Symptoms**:
- `except:` without exception type
- Catches KeyboardInterrupt, SystemExit
- Hides unexpected errors

**Impact**: Hard to debug, masks real issues

**Fix**:
```python
# ❌ Bad: Bare except
try:
    result = risky_operation()
except:  # Catches EVERYTHING including Ctrl+C!
    result = None

# ✅ Good: Specific exceptions
try:
    result = risky_operation()
except (ValueError, KeyError) as e:
    logger.error("Operation failed: %s", e)
    result = None
```

### Missing Type Hints

**Description**: No type annotations on function signatures.

**Symptoms**:
- No IDE autocomplete
- Runtime type errors
- Hard to understand function contracts
- No static type checking

**Impact**: Poor maintainability, runtime errors

**Fix**:
```python
# ❌ Bad: No type hints
def process_user(user_id, callback):
    user = get_user(user_id)
    return callback(user)

# ✅ Good: Full type hints
def process_user(
    user_id: str,
    callback: Callable[[User], dict[str, Any]]
) -> dict[str, Any]:
    user = get_user(user_id)
    return callback(user)
```

### Using Raw Dicts for Business Data

**Description**: Passing dictionaries instead of typed models.

**Symptoms**:
- `data: dict` everywhere
- String key typos cause runtime errors
- No validation
- Hard to know what fields exist

**Impact**: Runtime errors, no type safety, poor maintainability

**Fix**:
```python
# ❌ Bad: Raw dictionaries
def create_user(data: dict) -> dict:
    user = {
        "email": data["email"],  # Typo = runtime error
        "name": data.get("name"),  # Optional?
    }
    return user

# ✅ Good: Pydantic models
from pydantic import BaseModel, EmailStr

class UserCreate(BaseModel):
    email: EmailStr  # Validated
    name: str

class User(BaseModel):
    id: str
    email: EmailStr
    name: str

def create_user(data: UserCreate) -> User:
    # Type-safe, validated
    user = User(
        id=generate_id(),
        email=data.email,
        name=data.name,
    )
    return user
```

---

## Detection Strategy

### Automated Detection Tools

**Go**:
```bash
# Run these regularly
go vet ./...                    # Built-in checks
staticcheck ./...               # Static analysis
golangci-lint run              # Multiple linters
go test -race ./...            # Race detection
```

**Python**:
```bash
# Run these regularly
ruff check .                   # Fast linter
mypy .                         # Type checking
bandit -r .                    # Security issues
```

### Manual Review Checklist

- [ ] Every error is handled or explicitly ignored with comment
- [ ] All resources (files, connections) are closed
- [ ] No duplicated code (DRY principle)
- [ ] Clear separation of concerns
- [ ] No hard-coded magic values
- [ ] Proper abstraction layers
- [ ] Context propagation for cancellation
- [ ] Thread-safe concurrent access
- [ ] Proper transaction boundaries
- [ ] Efficient database queries with indexes

### Code Smell Indicators

- **Long functions** (>50 lines): Likely doing too much
- **Many parameters** (>5): Consider parameter object
- **Deep nesting** (>4 levels): Extract functions
- **Long parameter lists**: Use configuration object
- **Duplicate code**: Extract and reuse
- **Complex conditionals**: Use strategy pattern or lookup table
- **Large classes** (>500 lines): Split responsibilities

---

## Summary

Anti-patterns indicate design problems that make code:
- Hard to understand and maintain
- Prone to bugs and errors
- Difficult to test
- Inflexible to change
- Poor performing

**Key Actions**:
1. **Detect early**: Use linters and code review
2. **Refactor aggressively**: Fix anti-patterns immediately
3. **Prevent**: Follow SOLID principles and design patterns
4. **Document**: Explain complex decisions to prevent misunderstanding
5. **Test**: Comprehensive tests catch regressions during refactoring
