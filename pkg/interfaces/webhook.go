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
