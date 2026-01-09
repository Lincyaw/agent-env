// Copyright 2024 ARL-Infra Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
