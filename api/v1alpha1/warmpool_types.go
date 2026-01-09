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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WarmPoolSpec defines the desired state of WarmPool
type WarmPoolSpec struct {
	// Replicas is the number of idle pods to maintain
	Replicas int32 `json:"replicas"`

	// Template is the pod template for the warm pool
	Template corev1.PodTemplateSpec `json:"template"`
}

// WarmPoolStatus defines the observed state of WarmPool
type WarmPoolStatus struct {
	// ReadyReplicas is the number of ready idle pods
	ReadyReplicas int32 `json:"readyReplicas"`

	// AllocatedReplicas is the number of pods allocated to sandboxes
	AllocatedReplicas int32 `json:"allocatedReplicas"`

	// Conditions represent the latest available observations of the pool's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$')",message="WarmPool name must be a valid DNS label (lowercase alphanumeric and hyphens only, no dots)"

// WarmPool is the Schema for the warmpools API
type WarmPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WarmPoolSpec   `json:"spec,omitempty"`
	Status WarmPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WarmPoolList contains a list of WarmPool
type WarmPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WarmPool `json:"items"`
}
