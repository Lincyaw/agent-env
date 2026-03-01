package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WarmPoolSpec defines the desired state of WarmPool
type WarmPoolSpec struct {
	// Replicas is the number of idle pods to maintain
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas"`

	// Template is the pod template for the warm pool
	Template corev1.PodTemplateSpec `json:"template"`

	// Tools configures pre-provisioned tools available in the executor container
	// +optional
	Tools *ToolsSpec `json:"tools,omitempty"`

	// ImageLocality configures image-locality-aware scheduling to minimize
	// redundant image pulls by preferring nodes that should cache the same image.
	// +optional
	ImageLocality *ImageLocalitySpec `json:"imageLocality,omitempty"`
}

// ImageLocalitySpec configures image-locality-aware scheduling to minimize
// redundant image pulls by preferring nodes that should cache the same image.
type ImageLocalitySpec struct {
	// Enabled activates image-locality scheduling.
	// Default: true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// SpreadFactor controls preferred node count: k = ceil(replicas * spreadFactor).
	// Lower values concentrate pods on fewer nodes, maximising image cache hits.
	// Default: 0.25 (e.g. 8 replicas → prefer 2 nodes)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	SpreadFactor *float64 `json:"spreadFactor,omitempty"`

	// Weight for preferred NodeAffinity term (1-100).
	// Default: 100
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Weight *int32 `json:"weight,omitempty"`
}

// ToolsSpec defines the tools to provision in sandbox pods.
// Tools are mounted at /opt/arl/tools/ in the executor container.
type ToolsSpec struct {
	// Images: each is a container image containing tools at /tools/
	// Copied via init container to /opt/arl/tools/
	// +optional
	Images []ToolsImageSource `json:"images,omitempty"`

	// ConfigMaps: each ConfigMap is mounted and copied to /opt/arl/tools/
	// +optional
	ConfigMaps []ToolsConfigMapSource `json:"configMaps,omitempty"`

	// Inline: small tools defined directly in the CRD
	// +optional
	Inline []InlineTool `json:"inline,omitempty"`
}

// ToolsImageSource references a container image containing tools at /tools/.
type ToolsImageSource struct {
	// Image is the container image containing tools at /tools/
	Image string `json:"image"`
}

// ToolsConfigMapSource references a ConfigMap containing tool directories.
type ToolsConfigMapSource struct {
	// Name is the ConfigMap name. Must contain tool directories.
	Name string `json:"name"`
}

// InlineTool defines a small tool directly in the CRD spec.
// The controller auto-generates manifest.json from these fields.
type InlineTool struct {
	// Name is the tool name (used as directory name under /opt/arl/tools/)
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`

	// Description is a human-readable description of the tool
	// +optional
	Description string `json:"description,omitempty"`

	// Parameters is a JSON Schema describing the tool's input parameters
	// +optional
	Parameters *apiextensionsv1.JSON `json:"parameters,omitempty"`

	// Runtime is the tool runtime: bash, python, or binary
	// +kubebuilder:validation:Enum=bash;python;binary
	Runtime string `json:"runtime"`

	// Entrypoint is the script filename to execute (e.g. "run.sh")
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`
	// +kubebuilder:validation:MaxLength=255
	Entrypoint string `json:"entrypoint"`

	// Timeout is the maximum execution time (e.g. "30s")
	// +optional
	Timeout string `json:"timeout,omitempty"`

	// Files maps filename to content for all tool files
	Files map[string]string `json:"files"`
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
