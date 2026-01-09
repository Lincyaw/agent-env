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
