package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Chain manages a chain of reconciler hooks
type Chain struct {
	before []interfaces.ReconcilerHook
	after  []interfaces.ReconcilerHook
}

// NewChain creates a new middleware chain
func NewChain() *Chain {
	return &Chain{
		before: make([]interfaces.ReconcilerHook, 0),
		after:  make([]interfaces.ReconcilerHook, 0),
	}
}

// AddBefore adds a hook to execute before reconciliation
func (c *Chain) AddBefore(hook interfaces.ReconcilerHook) *Chain {
	c.before = append(c.before, hook)
	return c
}

// AddAfter adds a hook to execute after reconciliation
func (c *Chain) AddAfter(hook interfaces.ReconcilerHook) *Chain {
	c.after = append(c.after, hook)
	return c
}

// ExecuteBefore executes all before hooks
func (c *Chain) ExecuteBefore(ctx context.Context, resource interface{}) error {
	for _, hook := range c.before {
		if err := hook.Before(ctx, resource); err != nil {
			return fmt.Errorf("before hook failed: %w", err)
		}
	}
	return nil
}

// ExecuteAfter executes all after hooks
func (c *Chain) ExecuteAfter(ctx context.Context, resource interface{}, reconcileErr error) {
	for _, hook := range c.after {
		hook.After(ctx, resource, reconcileErr)
	}
}

// LoggingHook logs reconciliation start and end
type LoggingHook struct {
	controllerName string
}

// NewLoggingHook creates a new logging hook
func NewLoggingHook(controllerName string) *LoggingHook {
	return &LoggingHook{controllerName: controllerName}
}

func (h *LoggingHook) Before(ctx interface{}, resource interface{}) error {
	logger := log.FromContext(ctx.(context.Context))
	logger.V(1).Info("Reconciliation started", "controller", h.controllerName)
	return nil
}

func (h *LoggingHook) After(ctx interface{}, resource interface{}, err error) {
	logger := log.FromContext(ctx.(context.Context))
	if err != nil {
		logger.Error(err, "Reconciliation failed", "controller", h.controllerName)
	} else {
		logger.V(1).Info("Reconciliation completed", "controller", h.controllerName)
	}
}

// MetricsHook records reconciliation metrics
type MetricsHook struct {
	controllerName string
	collector      interfaces.MetricsCollector
	startTime      time.Time
}

// NewMetricsHook creates a new metrics hook
func NewMetricsHook(controllerName string, collector interfaces.MetricsCollector) *MetricsHook {
	return &MetricsHook{
		controllerName: controllerName,
		collector:      collector,
	}
}

func (h *MetricsHook) Before(ctx interface{}, resource interface{}) error {
	h.startTime = time.Now()
	return nil
}

func (h *MetricsHook) After(ctx interface{}, resource interface{}, err error) {
	result := "success"
	if err != nil {
		result = "error"
	}
	h.collector.IncrementReconcileTotal(h.controllerName, result)
}

// ValidationHook validates resources before reconciliation
type ValidationHook struct {
	validator interfaces.Validator
}

// NewValidationHook creates a new validation hook
func NewValidationHook(validator interfaces.Validator) *ValidationHook {
	return &ValidationHook{validator: validator}
}

func (h *ValidationHook) Before(ctx interface{}, resource interface{}) error {
	// Validation logic can be added here if needed
	// For now, validation is handled by webhooks
	return nil
}

func (h *ValidationHook) After(ctx interface{}, resource interface{}, err error) {
	// No-op for validation hook
}

// RetryHook provides retry logic for transient errors
type RetryHook struct {
	maxRetries int
	retryDelay time.Duration
}

// NewRetryHook creates a new retry hook
func NewRetryHook(maxRetries int, retryDelay time.Duration) *RetryHook {
	return &RetryHook{
		maxRetries: maxRetries,
		retryDelay: retryDelay,
	}
}

func (h *RetryHook) Before(ctx interface{}, resource interface{}) error {
	return nil
}

func (h *RetryHook) After(ctx interface{}, resource interface{}, err error) {
	// Retry logic can be implemented here
	// For controller-runtime, retries are handled via Result.Requeue
}

// Wrap wraps a reconcile function with middleware chain
func (c *Chain) Wrap(reconcileFn func(context.Context, ctrl.Request) (ctrl.Result, error)) func(context.Context, ctrl.Request) (ctrl.Result, error) {
	return func(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
		// Execute before hooks
		if err := c.ExecuteBefore(ctx, req); err != nil {
			return ctrl.Result{}, err
		}

		// Execute reconcile function
		result, err := reconcileFn(ctx, req)

		// Execute after hooks
		c.ExecuteAfter(ctx, req, err)

		return result, err
	}
}
