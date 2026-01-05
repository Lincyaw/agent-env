package webhook

import (
	"context"
	"fmt"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// SandboxValidator validates Sandbox resources
type SandboxValidator struct {
	// Add dependencies here as needed
}

// NewSandboxValidator creates a new Sandbox validator
func NewSandboxValidator() interfaces.Validator {
	return &SandboxValidator{}
}

// ValidateCreate validates Sandbox creation
func (v *SandboxValidator) ValidateCreate(ctx context.Context, obj interface{}) error {
	sandbox, ok := obj.(*arlv1alpha1.Sandbox)
	if !ok {
		return fmt.Errorf("expected Sandbox object, got %T", obj)
	}

	// TODO: Implement validation logic
	// Example validations:
	// - Ensure PoolRef exists
	// - Validate resource requirements

	if sandbox.Spec.PoolRef == "" {
		return fmt.Errorf("poolRef is required")
	}

	return nil
}

// ValidateUpdate validates Sandbox updates
func (v *SandboxValidator) ValidateUpdate(ctx context.Context, oldObj, newObj interface{}) error {
	_, ok := oldObj.(*arlv1alpha1.Sandbox)
	if !ok {
		return fmt.Errorf("expected Sandbox object for oldObj, got %T", oldObj)
	}

	newSandbox, ok := newObj.(*arlv1alpha1.Sandbox)
	if !ok {
		return fmt.Errorf("expected Sandbox object for newObj, got %T", newObj)
	}

	// TODO: Implement update validation

	if err := v.ValidateCreate(ctx, newSandbox); err != nil {
		return err
	}

	return nil
}

// ValidateDelete validates Sandbox deletion
func (v *SandboxValidator) ValidateDelete(ctx context.Context, obj interface{}) error {
	// TODO: Implement deletion validation if needed
	return nil
}
