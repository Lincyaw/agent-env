package interfaces

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// ControllerRegistrar defines the interface for registering controllers with the manager
type ControllerRegistrar interface {
	// SetupWithManager sets up the controller with the Manager
	SetupWithManager(mgr ctrl.Manager) error

	// Name returns the name of the controller for logging purposes
	Name() string
}

// ReconcilerHook defines lifecycle hooks for reconciliation
type ReconcilerHook interface {
	// Before is called before reconciliation starts
	// Return error to abort reconciliation
	Before(ctx interface{}, resource interface{}) error

	// After is called after reconciliation completes
	// Error from After won't affect reconciliation result
	After(ctx interface{}, resource interface{}, err error)
}

// Middleware defines a reusable middleware function
type Middleware func(next HandlerFunc) HandlerFunc

// HandlerFunc is the core handler signature
type HandlerFunc func(ctx interface{}, resource interface{}) error
