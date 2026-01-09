package webhook

import (
	"context"
	"fmt"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// WarmPoolValidator validates WarmPool resources
type WarmPoolValidator struct {
	// Add dependencies here as needed
}

// NewWarmPoolValidator creates a new WarmPool validator
func NewWarmPoolValidator() interfaces.Validator {
	return &WarmPoolValidator{}
}

// ValidateCreate validates WarmPool creation
func (v *WarmPoolValidator) ValidateCreate(ctx context.Context, obj interface{}) error {
	pool, ok := obj.(*arlv1alpha1.WarmPool)
	if !ok {
		return fmt.Errorf("expected WarmPool object, got %T", obj)
	}

	// TODO: Implement validation logic
	// Example validations:
	// - Ensure Replicas > 0
	// - Validate PodTemplate

	if pool.Spec.Replicas <= 0 {
		return fmt.Errorf("replicas must be greater than 0")
	}

	return nil
}

// ValidateUpdate validates WarmPool updates
func (v *WarmPoolValidator) ValidateUpdate(ctx context.Context, oldObj, newObj interface{}) error {
	_, ok := oldObj.(*arlv1alpha1.WarmPool)
	if !ok {
		return fmt.Errorf("expected WarmPool object for oldObj, got %T", oldObj)
	}

	newPool, ok := newObj.(*arlv1alpha1.WarmPool)
	if !ok {
		return fmt.Errorf("expected WarmPool object for newObj, got %T", newObj)
	}

	// TODO: Implement update validation

	if err := v.ValidateCreate(ctx, newPool); err != nil {
		return err
	}

	return nil
}

// ValidateDelete validates WarmPool deletion
func (v *WarmPoolValidator) ValidateDelete(ctx context.Context, obj interface{}) error {
	// TODO: Implement deletion validation if needed
	// Example: prevent deletion if pool has allocated sandboxes
	return nil
}
