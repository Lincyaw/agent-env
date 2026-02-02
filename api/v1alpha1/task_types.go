package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TaskState represents the execution state of a Task
type TaskState string

const (
	TaskStatePending   TaskState = "Pending"
	TaskStateRunning   TaskState = "Running"
	TaskStateSucceeded TaskState = "Succeeded"
	TaskStateFailed    TaskState = "Failed"
)

// StepType defines the type of step to execute
type StepType string

const (
	StepTypeFilePatch StepType = "FilePatch"
	StepTypeCommand   StepType = "Command"
)

// TaskStep defines a single step in a task
type TaskStep struct {
	// Name is the step identifier
	Name string `json:"name"`

	// Type is the step type (FilePatch or Command)
	Type StepType `json:"type"`

	// Content is the patch content (for FilePatch type)
	Content string `json:"content,omitempty"`

	// Path is the file path for FilePatch operations
	Path string `json:"path,omitempty"`

	// Command is the command to execute (for Command type)
	Command []string `json:"command,omitempty"`

	// WorkDir is the working directory for the command
	WorkDir string `json:"workDir,omitempty"`

	// Env is the environment variables for the command
	Env map[string]string `json:"env,omitempty"`

	// Container specifies which container to execute the command in
	// If empty, defaults to "sidecar". Use "executor" to run in the executor container.
	// +optional
	Container string `json:"container,omitempty"`
}

// TaskSpec defines the desired state of Task
type TaskSpec struct {
	// SandboxRef is the name of the Sandbox to execute in
	SandboxRef string `json:"sandboxRef"`

	// Timeout is the maximum execution time
	Timeout metav1.Duration `json:"timeout,omitempty"`

	// Steps is the sequence of operations to execute
	Steps []TaskStep `json:"steps"`

	// Retries is the number of times to retry on failure
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	// +optional
	Retries int32 `json:"retries,omitempty"`

	// TTLSecondsAfterFinished is the time to live after completion
	// +kubebuilder:validation:Minimum=0
	// +optional
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`

	// TraceID is an optional identifier for distributed tracing
	// +optional
	TraceID string `json:"traceID,omitempty"`
}

// TaskStatus defines the observed state of Task
type TaskStatus struct {
	// State is the current execution state
	State TaskState `json:"state,omitempty"`

	// ExitCode is the exit code of the last command
	ExitCode int32 `json:"exitCode,omitempty"`

	// Stdout is the standard output
	Stdout string `json:"stdout,omitempty"`

	// Stderr is the standard error output
	Stderr string `json:"stderr,omitempty"`

	// Duration is the execution duration
	Duration metav1.Duration `json:"duration,omitempty"`

	// StartTime is when the task started
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the task completed
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Conditions represent the latest available observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="ExitCode",type=integer,JSONPath=`.status.exitCode`

// Task is the Schema for the tasks API
type Task struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TaskSpec   `json:"spec,omitempty"`
	Status TaskStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TaskList contains a list of Task
type TaskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Task `json:"items"`
}
