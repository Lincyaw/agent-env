# Extensibility Patterns Reference Guide

Comprehensive guide for designing extensible, maintainable software systems.

## Table of Contents

- [Core Principles](#core-principles)
- [SOLID Principles](#solid-principles)
- [Design Patterns for Extensibility](#design-patterns-for-extensibility)
- [Dependency Management](#dependency-management)
- [Configuration Management](#configuration-management)
- [API Design for Extensibility](#api-design-for-extensibility)
- [Plugin Architecture](#plugin-architecture)
- [Go-Specific Patterns](#go-specific-patterns)
- [Python-Specific Patterns](#python-specific-patterns)

---

## Core Principles

### Open/Closed Principle (OCP)

**Definition**: Software entities should be open for extension but closed for modification.

**Goal**: Add new functionality without changing existing code.

**Implementation**:
```go
// ❌ Bad: Must modify existing code to add behavior
type PaymentProcessor struct{}

func (p *PaymentProcessor) Process(method string, amount float64) error {
    switch method {
    case "credit_card":
        // Credit card logic
    case "paypal":
        // PayPal logic
    // Must modify this function to add new methods!
    }
    return nil
}

// ✅ Good: Extension without modification
type PaymentMethod interface {
    Process(amount float64) error
}

type PaymentProcessor struct {
    methods map[string]PaymentMethod
}

func (p *PaymentProcessor) Register(name string, method PaymentMethod) {
    p.methods[name] = method
}

func (p *PaymentProcessor) Process(method string, amount float64) error {
    pm, ok := p.methods[method]
    if !ok {
        return fmt.Errorf("unknown payment method: %s", method)
    }
    return pm.Process(amount)
}

// New payment methods just implement interface
type CreditCardPayment struct{}
func (c *CreditCardPayment) Process(amount float64) error { /* ... */ }

type PayPalPayment struct{}
func (p *PayPalPayment) Process(amount float64) error { /* ... */ }

// Register at initialization
processor.Register("credit_card", &CreditCardPayment{})
processor.Register("paypal", &PayPalPayment{})
```

### Dependency Inversion Principle (DIP)

**Definition**: Depend on abstractions, not concretions.

**Goal**: Reduce coupling between components.

**Implementation**:
```go
// ❌ Bad: High-level depends on low-level concrete type
type OrderService struct {
    db *PostgresDB  // Concrete dependency
}

func NewOrderService() *OrderService {
    return &OrderService{
        db: NewPostgresDB(),  // Tight coupling
    }
}

// ✅ Good: Depend on abstraction
type OrderRepository interface {
    Save(order *Order) error
    FindByID(id string) (*Order, error)
}

type OrderService struct {
    repo OrderRepository  // Abstract dependency
}

// Dependency injected from outside
func NewOrderService(repo OrderRepository) *OrderService {
    return &OrderService{repo: repo}
}

// Easy to swap implementations
type PostgresOrderRepository struct {
    db *sql.DB
}

type MongoOrderRepository struct {
    client *mongo.Client
}

// Both implement OrderRepository
```

### Interface Segregation Principle (ISP)

**Definition**: Clients should not depend on interfaces they don't use.

**Goal**: Keep interfaces focused and cohesive.

**Implementation**:
```go
// ❌ Bad: Fat interface
type Worker interface {
    Work()
    Eat()
    Sleep()
    GetSalary() float64
    GetVacationDays() int
}

type Robot struct{}
func (r *Robot) Work() { /* ... */ }
func (r *Robot) Eat() { /* robots don't eat! */ }
func (r *Robot) Sleep() { /* robots don't sleep! */ }
func (r *Robot) GetSalary() float64 { return 0 }
func (r *Robot) GetVacationDays() int { return 0 }

// ✅ Good: Segregated interfaces
type Workable interface {
    Work()
}

type Biological interface {
    Eat()
    Sleep()
}

type Employee interface {
    GetSalary() float64
    GetVacationDays() int
}

// Robot only implements what it needs
type Robot struct{}
func (r *Robot) Work() { /* ... */ }

// Human implements all
type Human struct{}
func (h *Human) Work() { /* ... */ }
func (h *Human) Eat() { /* ... */ }
func (h *Human) Sleep() { /* ... */ }
func (h *Human) GetSalary() float64 { /* ... */ }
func (h *Human) GetVacationDays() int { /* ... */ }
```

---

## SOLID Principles

### Single Responsibility Principle (SRP)

**Definition**: A class should have only one reason to change.

**Example**:
```go
// ❌ Bad: Multiple responsibilities
type User struct {
    ID    string
    Email string
}

func (u *User) Save() error {
    // Database logic
}

func (u *User) SendEmail() error {
    // Email logic
}

func (u *User) GenerateReport() ([]byte, error) {
    // Report logic
}

// ✅ Good: Separated responsibilities
type User struct {
    ID    string
    Email string
}

type UserRepository struct {
    db *sql.DB
}

func (r *UserRepository) Save(user *User) error {
    // Database logic only
}

type EmailService struct {
    client *smtp.Client
}

func (s *EmailService) SendToUser(user *User, message string) error {
    // Email logic only
}

type ReportGenerator struct {
    template *template.Template
}

func (g *ReportGenerator) GenerateUserReport(user *User) ([]byte, error) {
    // Report logic only
}
```

### Liskov Substitution Principle (LSP)

**Definition**: Subtypes must be substitutable for their base types.

**Example**:
```go
// ❌ Bad: Violates LSP
type Rectangle struct {
    width, height float64
}

func (r *Rectangle) SetWidth(w float64) {
    r.width = w
}

func (r *Rectangle) SetHeight(h float64) {
    r.height = h
}

func (r *Rectangle) Area() float64 {
    return r.width * r.height
}

type Square struct {
    Rectangle
}

// Violates LSP: changes behavior of base class
func (s *Square) SetWidth(w float64) {
    s.width = w
    s.height = w  // Side effect!
}

func (s *Square) SetHeight(h float64) {
    s.width = h   // Side effect!
    s.height = h
}

// ✅ Good: Proper abstraction
type Shape interface {
    Area() float64
}

type Rectangle struct {
    width, height float64
}

func (r *Rectangle) Area() float64 {
    return r.width * r.height
}

type Square struct {
    side float64
}

func (s *Square) Area() float64 {
    return s.side * s.side
}
```

---

## Design Patterns for Extensibility

### Strategy Pattern

**Purpose**: Define family of algorithms, encapsulate each, make them interchangeable.

**Use When**: Multiple ways to perform operation, need to select at runtime.

```go
// Strategy interface
type CompressionStrategy interface {
    Compress(data []byte) ([]byte, error)
}

// Concrete strategies
type GzipCompression struct{}
func (g *GzipCompression) Compress(data []byte) ([]byte, error) {
    // Gzip compression
}

type ZstdCompression struct{}
func (z *ZstdCompression) Compress(data []byte) ([]byte, error) {
    // Zstd compression
}

// Context uses strategy
type FileCompressor struct {
    strategy CompressionStrategy
}

func (f *FileCompressor) SetStrategy(s CompressionStrategy) {
    f.strategy = s
}

func (f *FileCompressor) CompressFile(path string) error {
    data, err := os.ReadFile(path)
    if err != nil {
        return err
    }
    
    compressed, err := f.strategy.Compress(data)
    if err != nil {
        return err
    }
    
    return os.WriteFile(path+".compressed", compressed, 0644)
}

// Usage
compressor := &FileCompressor{}
compressor.SetStrategy(&GzipCompression{})  // Easy to switch
compressor.CompressFile("data.txt")
```

### Factory Pattern

**Purpose**: Create objects without specifying exact class.

**Use When**: Object creation logic is complex or should be centralized.

```go
// Product interface
type Database interface {
    Connect() error
    Query(sql string) ([]Row, error)
}

// Concrete products
type PostgresDB struct {
    connStr string
}

func (p *PostgresDB) Connect() error { /* ... */ }
func (p *PostgresDB) Query(sql string) ([]Row, error) { /* ... */ }

type MySQLDB struct {
    connStr string
}

func (m *MySQLDB) Connect() error { /* ... */ }
func (m *MySQLDB) Query(sql string) ([]Row, error) { /* ... */ }

// Factory
type DatabaseFactory struct{}

func (f *DatabaseFactory) Create(dbType, connStr string) (Database, error) {
    switch dbType {
    case "postgres":
        return &PostgresDB{connStr: connStr}, nil
    case "mysql":
        return &MySQLDB{connStr: connStr}, nil
    default:
        return nil, fmt.Errorf("unsupported database: %s", dbType)
    }
}

// Usage
factory := &DatabaseFactory{}
db, err := factory.Create("postgres", "postgres://...")
if err != nil {
    return err
}
db.Connect()
```

### Decorator Pattern

**Purpose**: Add behavior to objects without modifying their structure.

**Use When**: Need to add responsibilities dynamically.

```go
// Component interface
type Handler interface {
    Handle(ctx context.Context, req *Request) (*Response, error)
}

// Concrete component
type BaseHandler struct{}

func (h *BaseHandler) Handle(ctx context.Context, req *Request) (*Response, error) {
    // Core logic
    return &Response{Data: "processed"}, nil
}

// Decorators
type LoggingHandler struct {
    next Handler
}

func (h *LoggingHandler) Handle(ctx context.Context, req *Request) (*Response, error) {
    log.Printf("Request: %+v", req)
    resp, err := h.next.Handle(ctx, req)
    log.Printf("Response: %+v, Error: %v", resp, err)
    return resp, err
}

type MetricsHandler struct {
    next Handler
}

func (h *MetricsHandler) Handle(ctx context.Context, req *Request) (*Response, error) {
    start := time.Now()
    resp, err := h.next.Handle(ctx, req)
    duration := time.Since(start)
    metrics.RecordDuration("handler", duration)
    return resp, err
}

type AuthHandler struct {
    next Handler
}

func (h *AuthHandler) Handle(ctx context.Context, req *Request) (*Response, error) {
    if !isAuthenticated(req) {
        return nil, errors.New("unauthorized")
    }
    return h.next.Handle(ctx, req)
}

// Usage: Compose decorators
handler := &BaseHandler{}
handler = &AuthHandler{next: handler}
handler = &LoggingHandler{next: handler}
handler = &MetricsHandler{next: handler}

// Easy to add/remove decorators
```

### Observer Pattern

**Purpose**: Define one-to-many dependency; notify observers of state changes.

**Use When**: Changes in one object trigger updates in multiple objects.

```go
// Subject interface
type EventBus interface {
    Subscribe(event string, handler EventHandler)
    Publish(event string, data interface{})
}

type EventHandler func(data interface{})

// Concrete subject
type SimpleEventBus struct {
    handlers map[string][]EventHandler
    mu       sync.RWMutex
}

func NewEventBus() *SimpleEventBus {
    return &SimpleEventBus{
        handlers: make(map[string][]EventHandler),
    }
}

func (b *SimpleEventBus) Subscribe(event string, handler EventHandler) {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.handlers[event] = append(b.handlers[event], handler)
}

func (b *SimpleEventBus) Publish(event string, data interface{}) {
    b.mu.RLock()
    handlers := b.handlers[event]
    b.mu.RUnlock()
    
    for _, handler := range handlers {
        go handler(data)  // Async notification
    }
}

// Usage
bus := NewEventBus()

// Subscribe multiple observers
bus.Subscribe("user.created", func(data interface{}) {
    user := data.(*User)
    sendWelcomeEmail(user)
})

bus.Subscribe("user.created", func(data interface{}) {
    user := data.(*User)
    trackAnalytics("user_created", user.ID)
})

bus.Subscribe("user.created", func(data interface{}) {
    user := data.(*User)
    updateSearchIndex(user)
})

// Publish event
bus.Publish("user.created", newUser)
```

---

## Dependency Management

### Constructor Injection

**Best Practice**: Inject dependencies through constructor.

```go
// ✅ Good: Dependencies explicit and required
type OrderService struct {
    repo      OrderRepository
    payment   PaymentService
    inventory InventoryService
    logger    Logger
}

func NewOrderService(
    repo OrderRepository,
    payment PaymentService,
    inventory InventoryService,
    logger Logger,
) *OrderService {
    return &OrderService{
        repo:      repo,
        payment:   payment,
        inventory: inventory,
        logger:    logger,
    }
}

// Cannot create without dependencies - compile-time safety
```

### Interface-Based Dependencies

**Best Practice**: Depend on minimal interfaces.

```go
// ❌ Bad: Depend on concrete type with many methods
type UserService struct {
    cache *RedisCache  // 50+ methods
}

// ✅ Good: Depend on minimal interface
type UserCache interface {
    Get(key string) (string, error)
    Set(key string, value string) error
}

type UserService struct {
    cache UserCache  // Only what we need
}

// Redis implements UserCache
type RedisCache struct {
    client *redis.Client
}

func (r *RedisCache) Get(key string) (string, error) {
    return r.client.Get(context.Background(), key).Result()
}

func (r *RedisCache) Set(key string, value string) error {
    return r.client.Set(context.Background(), key, value, 0).Err()
}
```

### Avoiding Global State

**Best Practice**: No package-level variables for dependencies.

```go
// ❌ Bad: Global state
var DB *sql.DB

func init() {
    DB = connectDatabase()  // Hidden dependency
}

func GetUser(id string) (*User, error) {
    // Uses global DB
    return queryUser(DB, id)
}

// ✅ Good: Explicit dependencies
type UserRepository struct {
    db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
    return &UserRepository{db: db}
}

func (r *UserRepository) GetUser(id string) (*User, error) {
    // Explicit dependency, easy to test
    return queryUser(r.db, id)
}
```

---

## Configuration Management

### Environment-Based Configuration

**Pattern**: Externalize configuration, support multiple environments.

```go
// Config structure
type Config struct {
    Server   ServerConfig
    Database DatabaseConfig
    Cache    CacheConfig
}

type ServerConfig struct {
    Host string `env:"SERVER_HOST" default:"localhost"`
    Port int    `env:"SERVER_PORT" default:"8080"`
}

type DatabaseConfig struct {
    URL         string `env:"DATABASE_URL" required:"true"`
    MaxConns    int    `env:"DB_MAX_CONNS" default:"10"`
    MaxIdleTime string `env:"DB_MAX_IDLE_TIME" default:"5m"`
}

// Load from environment
func LoadConfig() (*Config, error) {
    var cfg Config
    if err := env.Parse(&cfg); err != nil {
        return nil, fmt.Errorf("parse config: %w", err)
    }
    return &cfg, nil
}

// Usage
func main() {
    cfg, err := LoadConfig()
    if err != nil {
        log.Fatal(err)
    }
    
    // Pass config to components
    db := NewDatabase(cfg.Database)
    cache := NewCache(cfg.Cache)
    server := NewServer(cfg.Server, db, cache)
    
    server.Start()
}
```

### Feature Flags

**Pattern**: Toggle features without code changes.

```go
type FeatureFlags struct {
    EnableNewUI    bool `env:"FEATURE_NEW_UI" default:"false"`
    EnableAnalytics bool `env:"FEATURE_ANALYTICS" default:"true"`
    MaxUploadSize  int  `env:"MAX_UPLOAD_SIZE_MB" default:"10"`
}

type Config struct {
    Features FeatureFlags
}

// Use in code
func (s *Server) HandleUpload(w http.ResponseWriter, r *http.Request) {
    maxSize := int64(s.config.Features.MaxUploadSize) * 1024 * 1024
    r.Body = http.MaxBytesReader(w, r.Body, maxSize)
    
    // ...
    
    if s.config.Features.EnableAnalytics {
        s.analytics.Track("upload", metadata)
    }
}
```

---

## API Design for Extensibility

### Versioned APIs

**Pattern**: Support multiple API versions simultaneously.

```go
// Version in URL or header
type Router struct {
    v1 *V1Handler
    v2 *V2Handler
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    switch {
    case strings.HasPrefix(req.URL.Path, "/api/v1/"):
        r.v1.ServeHTTP(w, req)
    case strings.HasPrefix(req.URL.Path, "/api/v2/"):
        r.v2.ServeHTTP(w, req)
    default:
        http.NotFound(w, req)
    }
}

// Each version independent
type V1Handler struct {
    userService *UserServiceV1
}

type V2Handler struct {
    userService *UserServiceV2
}
```

### Backward Compatibility

**Pattern**: Add fields, never remove or change existing.

```go
// ✅ Good: Additive changes only
type UserV1 struct {
    ID    string `json:"id"`
    Email string `json:"email"`
}

type UserV2 struct {
    ID        string  `json:"id"`
    Email     string  `json:"email"`
    Name      string  `json:"name"`       // Added
    AvatarURL *string `json:"avatar_url"` // Added, optional
}

// Old clients still work with V2 endpoint
// New clients get additional fields
```

### Pagination and Filtering

**Pattern**: Design APIs for extensibility.

```go
type ListOptions struct {
    // Pagination
    Limit  int    `json:"limit"`
    Offset int    `json:"offset"`
    Cursor string `json:"cursor,omitempty"`
    
    // Filtering (extensible)
    Filters map[string]interface{} `json:"filters,omitempty"`
    
    // Sorting
    SortBy    string `json:"sort_by,omitempty"`
    SortOrder string `json:"sort_order,omitempty"`
}

type ListResponse struct {
    Items      []interface{} `json:"items"`
    Total      int           `json:"total"`
    NextCursor string        `json:"next_cursor,omitempty"`
}

// Easy to add new filter types without breaking API
```

---

## Plugin Architecture

### Plugin Interface

**Pattern**: Load external plugins at runtime.

```go
// Plugin interface
type Plugin interface {
    Name() string
    Init(config map[string]interface{}) error
    Execute(ctx context.Context, input interface{}) (interface{}, error)
}

// Plugin registry
type PluginRegistry struct {
    plugins map[string]Plugin
    mu      sync.RWMutex
}

func (r *PluginRegistry) Register(plugin Plugin) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.plugins[plugin.Name()] = plugin
}

func (r *PluginRegistry) Get(name string) (Plugin, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    plugin, ok := r.plugins[name]
    return plugin, ok
}

// Load plugins
func LoadPlugins(dir string) error {
    files, err := os.ReadDir(dir)
    if err != nil {
        return err
    }
    
    for _, file := range files {
        if filepath.Ext(file.Name()) == ".so" {
            plugin, err := loadSharedObject(filepath.Join(dir, file.Name()))
            if err != nil {
                return err
            }
            registry.Register(plugin)
        }
    }
    
    return nil
}
```

---

## Go-Specific Patterns

### Functional Options

**Pattern**: Flexible, extensible configuration.

```go
type Server struct {
    host    string
    port    int
    timeout time.Duration
    logger  Logger
}

type Option func(*Server)

func WithHost(host string) Option {
    return func(s *Server) {
        s.host = host
    }
}

func WithPort(port int) Option {
    return func(s *Server) {
        s.port = port
    }
}

func WithTimeout(timeout time.Duration) Option {
    return func(s *Server) {
        s.timeout = timeout
    }
}

func WithLogger(logger Logger) Option {
    return func(s *Server) {
        s.logger = logger
    }
}

func NewServer(opts ...Option) *Server {
    // Defaults
    s := &Server{
        host:    "localhost",
        port:    8080,
        timeout: 30 * time.Second,
        logger:  &DefaultLogger{},
    }
    
    // Apply options
    for _, opt := range opts {
        opt(s)
    }
    
    return s
}

// Usage: Easy to extend without breaking existing code
server := NewServer(
    WithHost("0.0.0.0"),
    WithPort(3000),
    WithTimeout(60*time.Second),
)
```

### Interface Composition

**Pattern**: Build complex interfaces from simple ones.

```go
// Small interfaces
type Reader interface {
    Read(p []byte) (n int, err error)
}

type Writer interface {
    Write(p []byte) (n int, err error)
}

type Closer interface {
    Close() error
}

// Compose for complex needs
type ReadWriter interface {
    Reader
    Writer
}

type ReadWriteCloser interface {
    Reader
    Writer
    Closer
}

// Accept minimal interface
func Copy(dst Writer, src Reader) error {
    // Works with any Reader/Writer, not just files
}
```

---

## Python-Specific Patterns

### Protocol Classes (Structural Subtyping)

**Pattern**: Duck typing with type checking.

```python
from typing import Protocol

class Serializable(Protocol):
    def serialize(self) -> bytes: ...
    def deserialize(self, data: bytes) -> None: ...

# Any class with these methods is Serializable
class User:
    def serialize(self) -> bytes:
        return json.dumps(self.__dict__).encode()
    
    def deserialize(self, data: bytes) -> None:
        self.__dict__ = json.loads(data)

def save_to_file(obj: Serializable, path: str) -> None:
    data = obj.serialize()
    with open(path, 'wb') as f:
        f.write(data)

# Works without explicit inheritance
```

### Abstract Base Classes

**Pattern**: Enforce interface implementation.

```python
from abc import ABC, abstractmethod

class PaymentGateway(ABC):
    @abstractmethod
    def charge(self, amount: Decimal, source: str) -> str:
        """Charge payment and return transaction ID."""
        pass
    
    @abstractmethod
    def refund(self, transaction_id: str) -> bool:
        """Refund transaction."""
        pass

class StripeGateway(PaymentGateway):
    def charge(self, amount: Decimal, source: str) -> str:
        # Implementation
        return transaction_id
    
    def refund(self, transaction_id: str) -> bool:
        # Implementation
        return success

# Cannot instantiate without implementing all abstract methods
```

### Dependency Injection with Pydantic

**Pattern**: Type-safe configuration and DI.

```python
from pydantic import BaseModel
from typing import Protocol

class Cache(Protocol):
    def get(self, key: str) -> str | None: ...
    def set(self, key: str, value: str) -> None: ...

class RedisCache:
    def __init__(self, url: str):
        self.client = redis.from_url(url)
    
    def get(self, key: str) -> str | None:
        return self.client.get(key)
    
    def set(self, key: str, value: str) -> None:
        self.client.set(key, value)

class ServiceConfig(BaseModel):
    cache_url: str = "redis://localhost"
    api_key: str

class UserService:
    def __init__(self, cache: Cache, config: ServiceConfig):
        self.cache = cache
        self.config = config
    
    def get_user(self, user_id: str) -> User | None:
        # Use cache and config
        cached = self.cache.get(f"user:{user_id}")
        if cached:
            return User.model_validate_json(cached)
        # ...

# Wire dependencies
config = ServiceConfig(
    cache_url=os.getenv("CACHE_URL", "redis://localhost"),
    api_key=os.getenv("API_KEY"),
)
cache = RedisCache(config.cache_url)
service = UserService(cache=cache, config=config)
```

---

## Summary

**Key Principles for Extensibility**:

1. **Program to interfaces, not implementations**
2. **Inject dependencies, don't create them**
3. **Favor composition over inheritance**
4. **Keep interfaces small and focused**
5. **Make components replaceable**
6. **Externalize configuration**
7. **Use plugins for major extensions**
8. **Version APIs for compatibility**
9. **Follow SOLID principles**
10. **Design for testability**

**Checklist**:
- [ ] Can add features without modifying existing code?
- [ ] Are dependencies injected, not hard-coded?
- [ ] Do interfaces have single, clear purpose?
- [ ] Can components be tested in isolation?
- [ ] Is configuration external and environment-aware?
- [ ] Can implementations be swapped easily?
- [ ] Are there clear extension points?
- [ ] Is the system plugin-friendly?
- [ ] Do APIs support versioning?
- [ ] Is backward compatibility maintained?
