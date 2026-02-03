package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SandboxPhase represents the lifecycle phase of a Sandbox
type SandboxPhase string

const (
	SandboxPhasePending SandboxPhase = "Pending"
	SandboxPhaseBound   SandboxPhase = "Bound"
	SandboxPhaseReady   SandboxPhase = "Ready"
	SandboxPhaseFailed  SandboxPhase = "Failed"
)

// SandboxSpec defines the desired state of Sandbox
type SandboxSpec struct {
	// PoolRef is the name of the WarmPool to allocate from
	PoolRef string `json:"poolRef"`

	// KeepAlive indicates whether to keep the pod after task completion
	KeepAlive bool `json:"keepAlive,omitempty"`

	// Resources specifies the resource requirements
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// IdleTimeoutSeconds is the maximum time in seconds a sandbox can be idle before cleanup
	// +kubebuilder:validation:Minimum=0
	// +optional
	IdleTimeoutSeconds *int32 `json:"idleTimeoutSeconds,omitempty"`

	// MaxLifetimeSeconds is the maximum time in seconds a sandbox can exist before cleanup
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxLifetimeSeconds *int32 `json:"maxLifetimeSeconds,omitempty"`
}

// SandboxStatus defines the observed state of Sandbox
type SandboxStatus struct {
	// Phase is the current lifecycle phase
	Phase SandboxPhase `json:"phase,omitempty"`

	// PodName is the name of the bound pod
	PodName string `json:"podName,omitempty"`

	// PodIP is the IP address of the bound pod
	PodIP string `json:"podIP,omitempty"`

	// WorkDir is the working directory in the sandbox
	WorkDir string `json:"workDir,omitempty"`

	// LastTaskTime is when the last task completed on this sandbox
	// +optional
	LastTaskTime *metav1.Time `json:"lastTaskTime,omitempty"`

	// Conditions represent the latest available observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Pod",type=string,JSONPath=`.status.podName`

// Sandbox is the Schema for the sandboxes API
type Sandbox struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SandboxSpec   `json:"spec,omitempty"`
	Status SandboxStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SandboxList contains a list of Sandbox
type SandboxList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Sandbox `json:"items"`
}

// ValidatePhaseTransition validates if a phase transition is allowed
func (s *Sandbox) ValidatePhaseTransition(newPhase SandboxPhase) error {
	// Define valid state transitions
	validTransitions := map[SandboxPhase][]SandboxPhase{
		"":                  {SandboxPhasePending},                   // Initial state
		SandboxPhasePending: {SandboxPhaseBound, SandboxPhaseFailed}, // Can bind or fail
		SandboxPhaseBound:   {SandboxPhaseReady, SandboxPhaseFailed}, // Can become ready or fail
		SandboxPhaseReady:   {SandboxPhaseFailed},                    // Can only fail once ready
		SandboxPhaseFailed:  {},                                      // Terminal state
	}

	currentPhase := s.Status.Phase
	if currentPhase == "" {
		currentPhase = ""
	}

	// Check if transition is valid
	allowedPhases, exists := validTransitions[currentPhase]
	if !exists {
		return fmt.Errorf("unknown current phase: %s", currentPhase)
	}

	// Allow staying in the same phase (idempotent updates)
	if currentPhase == newPhase {
		return nil
	}

	// Check if new phase is in allowed list
	for _, allowed := range allowedPhases {
		if allowed == newPhase {
			return nil
		}
	}

	return fmt.Errorf("invalid phase transition from %s to %s", currentPhase, newPhase)
}
