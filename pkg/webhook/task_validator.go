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

// TaskValidator validates Task resources
type TaskValidator struct {
	// Add dependencies here as needed
}

// NewTaskValidator creates a new Task validator
func NewTaskValidator() interfaces.Validator {
	return &TaskValidator{}
}

// ValidateCreate validates Task creation
func (v *TaskValidator) ValidateCreate(ctx context.Context, obj interface{}) error {
	task, ok := obj.(*arlv1alpha1.Task)
	if !ok {
		return fmt.Errorf("expected Task object, got %T", obj)
	}

	// TODO: Implement validation logic
	// Example validations:
	// - Ensure SandboxRef is not empty
	// - Validate Timeout is reasonable (e.g., < 1 hour)
	// - Ensure at least one step exists
	// - Validate step types and content

	if task.Spec.SandboxRef == "" {
		return fmt.Errorf("sandboxRef is required")
	}

	if len(task.Spec.Steps) == 0 {
		return fmt.Errorf("at least one step is required")
	}

	return nil
}

// ValidateUpdate validates Task updates
func (v *TaskValidator) ValidateUpdate(ctx context.Context, oldObj, newObj interface{}) error {
	_, ok := oldObj.(*arlv1alpha1.Task)
	if !ok {
		return fmt.Errorf("expected Task object for oldObj, got %T", oldObj)
	}

	newTask, ok := newObj.(*arlv1alpha1.Task)
	if !ok {
		return fmt.Errorf("expected Task object for newObj, got %T", newObj)
	}

	// TODO: Implement update validation
	// Example validations:
	// - Prevent modification of immutable fields after task starts
	// - Validate state transitions

	if err := v.ValidateCreate(ctx, newTask); err != nil {
		return err
	}

	return nil
}

// ValidateDelete validates Task deletion
func (v *TaskValidator) ValidateDelete(ctx context.Context, obj interface{}) error {
	// TODO: Implement deletion validation if needed
	// Example: prevent deletion of running tasks
	return nil
}
