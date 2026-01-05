package interfaces

import (
	"context"
)

// Validator defines the interface for validating resources
type Validator interface {
	// ValidateCreate validates resource creation
	ValidateCreate(ctx context.Context, obj interface{}) error

	// ValidateUpdate validates resource updates
	ValidateUpdate(ctx context.Context, oldObj, newObj interface{}) error

	// ValidateDelete validates resource deletion
	ValidateDelete(ctx context.Context, obj interface{}) error
}

// Defaulter defines the interface for setting default values
type Defaulter interface {
	// Default sets default values for the resource
	Default(ctx context.Context, obj interface{}) error
}

// AdmissionHandler combines validation and defaulting
type AdmissionHandler interface {
	Validator
	Defaulter
}
