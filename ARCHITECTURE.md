# Architecture Design

## Overview

The ARL-Infra operator has been redesigned with a focus on **extensibility**, **pluggability**, and **maintainability**. The new architecture introduces clear interfaces, dependency injection, and middleware support.

## Key Design Principles

### 1. **Interface-Based Design**
All core components are defined as interfaces in `pkg/interfaces/`:

- `ControllerRegistrar`: Controllers that can be registered with the manager
- `SidecarClient`: Communication with sidecar containers
- `MetricsCollector`: Metrics collection abstraction
- `Validator`: Resource validation (for webhooks)
- `ReconcilerHook`: Lifecycle hooks for reconciliation

**Benefits:**
- Easy to mock for testing
- Supports multiple implementations (HTTP, gRPC, mock)
- Decouples interface from implementation

### 2. **Dependency Injection**
Controllers receive all dependencies through their struct fields:

```go
type TaskReconciler struct {
    client.Client
    Scheme        *runtime.Scheme
    Config        *config.Config
    SidecarClient interfaces.SidecarClient
    Metrics       interfaces.MetricsCollector
    Middleware    *middleware.Chain
}
```

**Benefits:**
- Easy to test with mock dependencies
- Clear dependency graph
- No hidden global state

### 3. **Middleware Chain**
Middleware hooks execute before/after reconciliation:

```go
middleware := middleware.NewChain().
    AddBefore(LoggingHook).
    AddAfter(MetricsHook)
```

**Available Hooks:**
- `LoggingHook`: Request/response logging
- `MetricsHook`: Metrics collection
- `ValidationHook`: Pre-reconciliation validation
- `RetryHook`: Retry logic (placeholder)

**Add Custom Middleware:**
```go
type CustomHook struct {}

func (h *CustomHook) Before(ctx interface{}, resource interface{}) error {
    // Your custom logic here
    return nil
}

func (h *CustomHook) After(ctx interface{}, resource interface{}, err error) {
    // Cleanup or post-processing
}
```

### 4. **Controller Registration**
Controllers are registered using the registrar pattern:

```go
controllers := []interfaces.ControllerRegistrar{
    &controller.WarmPoolReconciler{...},
    &controller.SandboxReconciler{...},
    &controller.TaskReconciler{...},
}

for _, c := range controllers {
    c.SetupWithManager(mgr)
}
```

**Adding a New Controller:**
1. Implement `ControllerRegistrar` interface
2. Add to the `controllers` slice in `main.go`
3. Done! No other code changes needed

### 5. **Configuration Management**
Configuration is centralized in `pkg/config/`:

- Supports environment variables
- Type-safe with validation
- Feature flags for enabling/disabling features

**Example:**
```bash
export SIDECAR_PORT=9090
export ENABLE_METRICS=true
export ENABLE_WEBHOOKS=false
```

## Architecture Layers

```
┌─────────────────────────────────────────────────┐
│              CRD Layer (API)                    │
│  WarmPool, Sandbox, Task                        │
└─────────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────────┐
│           Controller Layer                       │
│  - Middleware Chain (hooks)                     │
│  - Dependency Injection                         │
│  - Reconciliation Logic                         │
└─────────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────────┐
│          Service Layer (Interfaces)             │
│  - SidecarClient (HTTP/gRPC/Mock)              │
│  - MetricsCollector (Prometheus/NoOp)          │
│  - Validators (Webhook)                         │
└─────────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────────┐
│        Infrastructure Layer                      │
│  - Kubernetes API                               │
│  - Sidecar HTTP Server                          │
│  - Prometheus Metrics                           │
└─────────────────────────────────────────────────┘
```

## Package Structure

```
pkg/
├── interfaces/          # Core interfaces
│   ├── controller.go    # Controller & middleware interfaces
│   ├── client.go        # SidecarClient interface
│   ├── metrics.go       # MetricsCollector interface
│   └── webhook.go       # Validator interface
│
├── middleware/          # Middleware implementations
│   └── chain.go         # Middleware chain + built-in hooks
│
├── client/              # SidecarClient implementations
│   ├── http.go          # HTTP client
│   └── mock.go          # Mock for testing
│
├── config/              # Configuration management
│   └── config.go        # Config struct + env loading
│
├── controller/          # Controller implementations
│   ├── warmpool_controller.go
│   ├── sandbox_controller.go
│   └── task_controller.go
│
├── webhook/             # Webhook validators (reserved)
│   ├── task_validator.go
│   ├── sandbox_validator.go
│   └── warmpool_validator.go
│
└── metrics/             # Metrics implementations (reserved)
    ├── prometheus.go    # Prometheus collector
    └── doc.go           # Usage documentation
```

## Extensibility Examples

### Adding Authentication/Authorization Middleware

```go
// pkg/middleware/auth.go
type AuthHook struct {
    rbacClient rbac.Client
}

func (h *AuthHook) Before(ctx interface{}, resource interface{}) error {
    // Check if user has permission
    if !h.rbacClient.HasPermission(...) {
        return errors.New("unauthorized")
    }
    return nil
}

// In main.go
taskMiddleware.AddBefore(middleware.NewAuthHook(rbacClient))
```

### Adding Rate Limiting

```go
// pkg/middleware/ratelimit.go
type RateLimitHook struct {
    limiter *rate.Limiter
}

func (h *RateLimitHook) Before(ctx interface{}, resource interface{}) error {
    if !h.limiter.Allow() {
        return errors.New("rate limit exceeded")
    }
    return nil
}

// In main.go
taskMiddleware.AddBefore(middleware.NewRateLimitHook(limiter))
```

### Switching to gRPC SidecarClient

```go
// pkg/client/grpc.go
type GRPCSidecarClient struct {
    conn *grpc.ClientConn
}

func (c *GRPCSidecarClient) Execute(ctx context.Context, podIP string, req interfaces.ExecRequest) (interfaces.ExecResponse, error) {
    // gRPC implementation
}

// In main.go
sidecarClient := client.NewGRPCSidecarClient(cfg.SidecarPort)
```

## Feature Flags

| Flag | Description | Default |
|------|-------------|---------|
| `ENABLE_METRICS` | Enable Prometheus metrics | `true` |
| `ENABLE_MIDDLEWARE` | Enable middleware chain | `true` |
| `ENABLE_WEBHOOKS` | Enable admission webhooks | `false` |
| `ENABLE_VALIDATION` | Enable resource validation | `true` |

## Future Enhancements

### 1. Admission Webhooks
- Implement ValidatingWebhookConfiguration
- Add certificate management
- Enable in production with `ENABLE_WEBHOOKS=true`

### 2. Advanced Metrics
- Custom dashboards
- Alerting rules
- SLO/SLI tracking

### 3. Multi-tenant Support
- Namespace isolation
- Resource quotas per tenant
- RBAC integration

### 4. Event-driven Architecture
- Publish events to message queue
- Asynchronous task processing
- External integrations

## Migration from Old Architecture

The new architecture is **backward compatible**. Existing CRDs and resources work without changes. The main difference is:

**Before:**
```go
// Hard-coded dependencies
controller := &TaskReconciler{
    SidecarClient: NewSidecarClient(), // hard-coded
}
```

**After:**
```go
// Injected dependencies
controller := &TaskReconciler{
    SidecarClient: sidecarClient, // injected
    Metrics:       metricsCollector,
    Config:        cfg,
}
```

## Testing

The new architecture makes testing much easier:

```go
// Test with mock client
func TestTaskReconciler(t *testing.T) {
    mockClient := &client.MockSidecarClient{
        ExecuteFunc: func(...) (...) {
            return &mockResponse, nil
        },
    }
    
    reconciler := &TaskReconciler{
        SidecarClient: mockClient,
        Metrics:       &interfaces.NoOpMetricsCollector{},
    }
    
    // Test reconciliation
}
```

## Summary

The refactored architecture provides:

✅ **Pluggable components** via interfaces  
✅ **Middleware support** for cross-cutting concerns  
✅ **Dependency injection** for testability  
✅ **Configuration management** for flexibility  
✅ **Clear separation of concerns**  
✅ **Easy to extend** with new features  
✅ **Backward compatible** with existing deployments  

This design ensures the operator can **scale with your requirements** without major rewrites.
