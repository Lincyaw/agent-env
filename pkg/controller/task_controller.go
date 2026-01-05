package controller

import (
	"context"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

// TaskReconciler reconciles a Task object
type TaskReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	SidecarClient *SidecarClient
}

// +kubebuilder:rbac:groups=arl.infra.io,resources=tasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arl.infra.io,resources=tasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arl.infra.io,resources=tasks/finalizers,verbs=update

// Reconcile manages the Task lifecycle
func (r *TaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Task instance
	task := &arlv1alpha1.Task{}
	if err := r.Get(ctx, req.NamespacedName, task); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// If already completed, nothing to do
	if task.Status.State == arlv1alpha1.TaskStateSucceeded || 
	   task.Status.State == arlv1alpha1.TaskStateFailed {
		return ctrl.Result{}, nil
	}

	// Get the sandbox
	sandbox := &arlv1alpha1.Sandbox{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      task.Spec.SandboxRef,
	}, sandbox); err != nil {
		logger.Error(err, "Failed to get sandbox", "sandbox", task.Spec.SandboxRef)
		task.Status.State = arlv1alpha1.TaskStateFailed
		task.Status.Stderr = "sandbox not found"
		if err := r.Status().Update(ctx, task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Check if sandbox is ready
	if sandbox.Status.Phase != arlv1alpha1.SandboxPhaseReady {
		logger.Info("Sandbox not ready", "sandbox", sandbox.Name, "phase", sandbox.Status.Phase)
		task.Status.State = arlv1alpha1.TaskStatePending
		if err := r.Status().Update(ctx, task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	// Mark task as running
	if task.Status.State == "" || task.Status.State == arlv1alpha1.TaskStatePending {
		now := metav1.Now()
		task.Status.State = arlv1alpha1.TaskStateRunning
		task.Status.StartTime = &now
		if err := r.Status().Update(ctx, task); err != nil {
			return ctrl.Result{}, err
		}
	}

	logger.Info("Executing task", 
		"task", task.Name,
		"sandbox", sandbox.Name,
		"pod", sandbox.Status.PodName,
		"podIP", sandbox.Status.PodIP)

	// Execute steps
	var stdout, stderr strings.Builder
	exitCode := int32(0)

	for i, step := range task.Spec.Steps {
		logger.Info("Executing step", "step", i, "name", step.Name, "type", step.Type)

		switch step.Type {
		case arlv1alpha1.StepTypeFilePatch:
			// Update files
			resp, err := r.SidecarClient.UpdateFiles(sandbox.Status.PodIP, &sidecar.FileRequest{
				BasePath: sandbox.Status.WorkDir,
				Files:    parseFiles(step.Content),
				Patch:    step.Content,
			})
			if err != nil {
				stderr.WriteString("Failed to update files: " + err.Error() + "\n")
				exitCode = 1
				break
			}
			if !resp.Success {
				stderr.WriteString("File update failed: " + resp.Message + "\n")
				exitCode = 1
				break
			}
			stdout.WriteString("Files updated: " + resp.Message + "\n")

		case arlv1alpha1.StepTypeCommand:
			// Execute command
			timeout := int32(30)
			if task.Spec.Timeout.Duration > 0 {
				timeout = int32(task.Spec.Timeout.Duration.Seconds())
			}

			resp, err := r.SidecarClient.Execute(sandbox.Status.PodIP, &sidecar.ExecRequest{
				Command:        step.Command,
				Env:            step.Env,
				WorkingDir:     sandbox.Status.WorkDir,
				TimeoutSeconds: timeout,
			})
			if err != nil {
				stderr.WriteString("Failed to execute command: " + err.Error() + "\n")
				exitCode = 1
				break
			}
			stdout.WriteString(resp.Stdout)
			stderr.WriteString(resp.Stderr)
			exitCode = resp.ExitCode
			if exitCode != 0 {
				break
			}
		}

		if exitCode != 0 {
			break
		}
	}

	// Update task status
	now := metav1.Now()
	task.Status.CompletionTime = &now
	task.Status.ExitCode = exitCode
	task.Status.Stdout = stdout.String()
	task.Status.Stderr = stderr.String()

	if task.Status.StartTime != nil {
		duration := now.Time.Sub(task.Status.StartTime.Time)
		task.Status.Duration = metav1.Duration{Duration: duration}
	}

	if exitCode == 0 {
		task.Status.State = arlv1alpha1.TaskStateSucceeded
	} else {
		task.Status.State = arlv1alpha1.TaskStateFailed
	}

	if err := r.Status().Update(ctx, task); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Task completed",
		"task", task.Name,
		"state", task.Status.State,
		"exitCode", exitCode)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *TaskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&arlv1alpha1.Task{}).
		Complete(r)
}

// parseFiles parses file content from a simple format
// TODO: Implement proper patch file parsing for production use
// Current implementation: This is a placeholder that returns empty map.
// File content should be provided via the Files map in FileRequest instead.
func parseFiles(content string) map[string]string {
	files := make(map[string]string)
	if content == "" {
		return files
	}

	// Placeholder implementation
	// In production, this should parse unified diff format or custom patch format
	// Example expected format:
	//   --- a/file.py
	//   +++ b/file.py
	//   @@ content @@
	
	return files
}
