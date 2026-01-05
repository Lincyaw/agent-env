# AI Coding Agent Instructions

## Language & Code Style

- **Code files**: English only for all code, comments, and variable names
- **Documentation**: Can use Chinese in markdown files if appropriate
- **Do not create**: Test files unless explicitly requested or absolutely necessary
- **Format before commit**: Run `make fmt`, `make vet`, `make tidy`
- **Go version**: Using Go 1.25.0 - follow latest best practices

## Go 1.24/1.25 Best Practices

### Error Handling

```go
// ✅ Always wrap errors with context
if err := doSomething(); err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}

// ✅ Use errors.Join for multiple errors (Go 1.20+)
if err1 != nil || err2 != nil {
    return errors.Join(err1, err2)
}

// ❌ Don't ignore errors silently
defer resp.Body.Close()  // Wrong

// ✅ Handle defer errors appropriately
defer func() {
    if closeErr := resp.Body.Close(); closeErr != nil {
        // Log or handle the error
        _ = closeErr
    }
}()
```

### HTTP Best Practices

```go
// ✅ Use http.Server for graceful shutdown
server := &http.Server{
    Addr:              ":8080",
    Handler:           mux,
    ReadHeaderTimeout: 10 * time.Second,
    ReadTimeout:       30 * time.Second,
    WriteTimeout:      30 * time.Second,
    IdleTimeout:       60 * time.Second,
}

// ✅ Always set timeouts for HTTP clients
client := &http.Client{
    Timeout: 30 * time.Second,
}

// ✅ Use context for HTTP requests
req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
```

### Concurrency Patterns

```go
// ✅ Use sync.WaitGroup to wait for goroutines
var wg sync.WaitGroup
wg.Add(2)
go func() {
    defer wg.Done()
    // work
}()
wg.Wait()

// ✅ Use context-aware channel sends to prevent goroutine leaks
select {
case ch <- value:
case <-ctx.Done():
    return ctx.Err()
}

// ✅ Always close channels in the sender
defer close(ch)
```

### Resource Management

```go
// ✅ Check for nil before dereferencing
if cmd.Process != nil {
    cmd.Process.Kill()
}

// ✅ Use defer for cleanup
timer := time.AfterFunc(timeout, cleanup)
defer timer.Stop()

// ✅ Clean up maps to prevent memory leaks
delete(s.processes, pid)
```

### Constants and Configuration

```go
// ✅ Define constants for magic values
const (
    DefaultTimeout    = 30 * time.Second
    MaxRetries        = 3
    DefaultPort       = 8080
)

// ❌ Don't use magic numbers
time.Sleep(5 * time.Second)  // What does 5 mean?

// ✅ Use named constants
time.Sleep(DefaultRequeueDelay)
```

### Signal Handling and Graceful Shutdown

```go
// ✅ Use signal.NotifyContext (Go 1.16+)
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

// Start server in goroutine
go func() {
    if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        log.Fatal(err)
    }
}()

// Wait for signal
<-ctx.Done()

// Graceful shutdown with timeout
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
server.Shutdown(shutdownCtx)
```

## Development Workflow

### Build & Validation

```bash
make build              # Build binaries
make docker-build       # Build Docker images
```

### Deployment

```bash
# Local (Minikube)
make minikube-start && make minikube-load-images && make deploy

# Standard K8s
make k8s-build-push && make k8s-deploy
```

### Registry Override

```bash
REGISTRY=your-registry.com make k8s-build-push
```

## Kubernetes Operator Patterns

### CRD Development

After modifying types in `api/v1alpha1/`:

```bash
make generate    # Update generated code
make manifests   # Update CRD YAML files
```

Required kubebuilder markers on all CRD types:

```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
```

### Controller Reconciliation

Standard pattern for all controllers:

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch resource, return early if NotFound with wrapped error
    obj := &MyType{}
    if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
        if errors.IsNotFound(err) {
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, fmt.Errorf("failed to get resource: %w", err)
    }
    
    // 2. Check terminal states, skip reconciliation if done
    if obj.Status.Phase == PhaseReady || obj.Status.Phase == PhaseFailed {
        return ctrl.Result{}, nil
    }
    
    // 3. Perform state transitions
    // ...
    
    // 4. Update status separately from spec
    if err := r.Status().Update(ctx, obj); err != nil {
        return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
    }
    
    // 5. Use named constants for requeue delays
    return ctrl.Result{RequeueAfter: DefaultRequeueDelay}, nil
}
```

### Status Updates

Always update status subresource separately:

```go
// ✅ Correct - separate status update
obj.Status.Phase = Ready
if err := r.Status().Update(ctx, obj); err != nil {
    return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
}

// ❌ Wrong - don't mix spec and status updates
obj.Spec.SomeField = value
obj.Status.Phase = Ready
r.Update(ctx, obj)  // This won't update status properly
```

## Code Quality Rules

1. **Error wrapping**: Always wrap errors with `fmt.Errorf("context: %w", err)` for better debugging
2. **No blocking operations** in controllers - use async patterns or external workers
3. **Validate YAML** - all config files must pass `kubectl apply --dry-run=client`
4. **Constants** - use constants for magic strings/numbers (ports, paths, labels, timeouts)
5. **Context usage** - pass context to all I/O operations and respect cancellation
6. **Resource cleanup** - use `defer` for cleanup, check nil before dereferencing
7. **Goroutine safety** - use `sync.WaitGroup`, context-aware channel operations
8. **HTTP timeouts** - always set timeouts on servers and clients
9. **Graceful shutdown** - implement signal handling and graceful shutdown for servers
10. **Run validation** - execute `make fmt && make vet && make tidy` before committing

## Project Structure

```
cmd/operator/    # Operator entry point
cmd/sidecar/     # Sidecar HTTP server (with graceful shutdown)
pkg/controller/  # Reconciliation logic
pkg/sidecar/     # Sidecar service implementation
api/v1alpha1/    # CRD type definitions
config/crd/      # Generated CRD manifests (don't edit manually)
config/samples/  # Example resources
```

## Common Commands

```bash
# Format & lint
make fmt && make vet && make tidy

# Build everything
make build && make docker-build

# Deploy to minikube
make minikube-start minikube-load-images deploy

# Clean up
make undeploy
```
