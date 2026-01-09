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

package v1alpha1

import (
	"testing"
)

func TestSandbox_ValidatePhaseTransition(t *testing.T) {
	tests := []struct {
		name         string
		currentPhase SandboxPhase
		newPhase     SandboxPhase
		wantErr      bool
	}{
		{
			name:         "initial to pending",
			currentPhase: "",
			newPhase:     SandboxPhasePending,
			wantErr:      false,
		},
		{
			name:         "pending to bound",
			currentPhase: SandboxPhasePending,
			newPhase:     SandboxPhaseBound,
			wantErr:      false,
		},
		{
			name:         "pending to failed",
			currentPhase: SandboxPhasePending,
			newPhase:     SandboxPhaseFailed,
			wantErr:      false,
		},
		{
			name:         "bound to ready",
			currentPhase: SandboxPhaseBound,
			newPhase:     SandboxPhaseReady,
			wantErr:      false,
		},
		{
			name:         "bound to failed",
			currentPhase: SandboxPhaseBound,
			newPhase:     SandboxPhaseFailed,
			wantErr:      false,
		},
		{
			name:         "ready to failed",
			currentPhase: SandboxPhaseReady,
			newPhase:     SandboxPhaseFailed,
			wantErr:      false,
		},
		{
			name:         "same phase (pending to pending)",
			currentPhase: SandboxPhasePending,
			newPhase:     SandboxPhasePending,
			wantErr:      false,
		},
		{
			name:         "same phase (ready to ready)",
			currentPhase: SandboxPhaseReady,
			newPhase:     SandboxPhaseReady,
			wantErr:      false,
		},
		// Invalid transitions
		{
			name:         "bound to pending (invalid)",
			currentPhase: SandboxPhaseBound,
			newPhase:     SandboxPhasePending,
			wantErr:      true,
		},
		{
			name:         "ready to pending (invalid)",
			currentPhase: SandboxPhaseReady,
			newPhase:     SandboxPhasePending,
			wantErr:      true,
		},
		{
			name:         "ready to bound (invalid)",
			currentPhase: SandboxPhaseReady,
			newPhase:     SandboxPhaseBound,
			wantErr:      true,
		},
		{
			name:         "failed to pending (invalid - terminal state)",
			currentPhase: SandboxPhaseFailed,
			newPhase:     SandboxPhasePending,
			wantErr:      true,
		},
		{
			name:         "failed to ready (invalid - terminal state)",
			currentPhase: SandboxPhaseFailed,
			newPhase:     SandboxPhaseReady,
			wantErr:      true,
		},
		{
			name:         "pending to ready (invalid - must go through bound)",
			currentPhase: SandboxPhasePending,
			newPhase:     SandboxPhaseReady,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox := &Sandbox{}
			sandbox.Status.Phase = tt.currentPhase

			err := sandbox.ValidatePhaseTransition(tt.newPhase)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePhaseTransition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
