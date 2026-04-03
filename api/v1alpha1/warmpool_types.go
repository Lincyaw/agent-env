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

	// ConfigEnv declares additional configuration resources and injection rules
	// that must be prepared before the WarmPool pod becomes ready.
	// +optional
	ConfigEnv *ConfigEnvSpec `json:"configEnv,omitempty"`
}

// ConfigEnvSpec declares configuration resources and injection rules for a WarmPool.
type ConfigEnvSpec struct {
	// Vars are template variables available to ConfigEnv resource templates.
	// +optional
	Vars map[string]string `json:"vars,omitempty"`

	// EnvVars are additional environment variables applied to the pod template.
	// +optional
	EnvVars []corev1.EnvVar `json:"envVars,omitempty"`

	// ConfigMaps are ConfigMap resources created and injected for the WarmPool.
	// +optional
	ConfigMaps []ConfigMapTemplate `json:"configMaps,omitempty"`

	// Secrets are Secret resources created and injected for the WarmPool.
	// +optional
	Secrets []SecretTemplate `json:"secrets,omitempty"`
}

// ConfigMapTemplate defines a ConfigMap to create for the WarmPool.
type ConfigMapTemplate struct {
	// +kubebuilder:validation:Optional
	corev1.ConfigMap `json:",inline"`

	// Inject describes how the ConfigMap should be mounted into the pod.
	// +optional
	Inject *VolumeInjection `json:"inject,omitempty"`
}

// SecretTemplate defines a Secret to create for the WarmPool.
type SecretTemplate struct {
	// +kubebuilder:validation:Optional
	corev1.Secret `json:",inline"`

	// Inject describes how the Secret should be mounted or exposed to the pod.
	// +optional
	Inject *SecretInjection `json:"inject,omitempty"`
}

// VolumeInjection describes a file-based mount for a ConfigMap or Secret.
type VolumeInjection struct {
	// Container is the target container name. If omitted, the controller may
	// choose the executor container by default.
	// +optional
	Container string `json:"container,omitempty"`

	// MountPath is the container path where the resource should be mounted.
	// +kubebuilder:validation:MinLength=1
	MountPath string `json:"mountPath"`

	// ReadOnly controls whether the mount should be read-only.
	// +optional
	ReadOnly *bool `json:"readOnly,omitempty"`

	// SubPath optionally mounts a single file or directory path.
	// +optional
	SubPath string `json:"subPath,omitempty"`
}

// SecretInjection describes how a Secret should be exposed to the pod.
type SecretInjection struct {
	// Volume describes file-based mounting for the Secret.
	// +optional
	Volume *VolumeInjection `json:"volume,omitempty"`

	// AsEnv exposes selected secret keys as environment variables.
	// +optional
	AsEnv []SecretEnvVar `json:"asEnv,omitempty"`
}

// SecretEnvVar maps a Secret key to an environment variable name.
type SecretEnvVar struct {
	// Name is the environment variable name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key is the Secret key to read.
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
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

	// ConfigEnv reports the observed state of environment configuration resources.
	// +optional
	ConfigEnv *ConfigEnvStatus `json:"configEnv,omitempty"`
}

// ConfigEnvPhase describes the high-level state of ConfigEnv reconciliation.
type ConfigEnvPhase string

const (
	// ConfigEnvPhasePending indicates that ConfigEnv has not yet been reconciled.
	ConfigEnvPhasePending ConfigEnvPhase = "Pending"
	// ConfigEnvPhaseReady indicates that ConfigEnv resources are ready.
	ConfigEnvPhaseReady ConfigEnvPhase = "Ready"
	// ConfigEnvPhaseFailed indicates that ConfigEnv reconciliation failed.
	ConfigEnvPhaseFailed ConfigEnvPhase = "Failed"
)

// ConfigEnvStatus captures the observed state of ConfigEnv reconciliation.
type ConfigEnvStatus struct {
	// Phase is the current reconciliation phase.
	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	Phase ConfigEnvPhase `json:"phase"`

	// ConfigMapRefs lists created ConfigMaps.
	// +optional
	ConfigMapRefs []ConfigEnvResourceRef `json:"configMapRefs,omitempty"`

	// SecretRefs lists created Secrets.
	// +optional
	SecretRefs []ConfigEnvResourceRef `json:"secretRefs,omitempty"`

	// Conditions provide additional diagnostic detail.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ConfigEnvResourceRef identifies a generated ConfigMap or Secret.
type ConfigEnvResourceRef struct {
	// Name is the resource name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace is the resource namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Kind is the resource kind.
	// +kubebuilder:validation:Enum=ConfigMap;Secret
	Kind string `json:"kind"`
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
