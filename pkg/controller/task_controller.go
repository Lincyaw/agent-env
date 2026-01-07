package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	"github.com/Lincyaw/agent-env/pkg/config"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/Lincyaw/agent-env/pkg/middleware"
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

// TaskReconciler reconciles a Task object
type TaskReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Config        *config.Config
	SidecarClient interfaces.SidecarClient
	Metrics       interfaces.MetricsCollector
	AuditWriter   interfaces.AuditWriter
	Middleware    *middleware.Chain
}

// +kubebuilder:rbac:groups=arl.infra.io,resources=tasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arl.infra.io,resources=tasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arl.infra.io,resources=tasks/finalizers,verbs=update

// Reconcile manages the Task lifecycle
func (r *TaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Execute middleware chain if enabled
	if r.Middleware != nil {
		if err := r.Middleware.ExecuteBefore(ctx, req); err != nil {
			return ctrl.Result{}, err
		}
		defer r.Middleware.ExecuteAfter(ctx, req, nil)
	}

	return r.reconcile(ctx, req)
}

func (r *TaskReconciler) reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Task instance
	task := &arlv1alpha1.Task{}
	if err := r.Get(ctx, req.NamespacedName, task); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get Task %s/%s: %w", req.Namespace, req.Name, err)
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
		logger.Error(err, "Failed to get sandbox", "sandbox", task.Spec.SandboxRef, "task", task.Name)
		task.Status.State = arlv1alpha1.TaskStateFailed
		task.Status.Stderr = fmt.Sprintf("sandbox %s not found: %v", task.Spec.SandboxRef, err)
		if err := r.Status().Update(ctx, task); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update Task %s/%s status after sandbox lookup failure: %w",
				task.Namespace, task.Name, err)
		}
		return ctrl.Result{}, nil
	}

	// Check if sandbox is ready
	if sandbox.Status.Phase != arlv1alpha1.SandboxPhaseReady {
		logger.Info("Sandbox not ready", "sandbox", sandbox.Name, "phase", sandbox.Status.Phase, "task", task.Name)
		task.Status.State = arlv1alpha1.TaskStatePending
		if err := r.Status().Update(ctx, task); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update Task %s/%s state to Pending: %w",
				task.Namespace, task.Name, err)
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	// Mark task as running
	if task.Status.State == "" || task.Status.State == arlv1alpha1.TaskStatePending {
		now := metav1.Now()
		task.Status.State = arlv1alpha1.TaskStateRunning
		task.Status.StartTime = &now
		if err := r.Status().Update(ctx, task); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update Task %s/%s state to Running: %w",
				task.Namespace, task.Name, err)
		}

		// Record state change
		if r.Metrics != nil {
			r.Metrics.RecordTaskState(task.Namespace, task.Name, string(arlv1alpha1.TaskStateRunning))
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
			// Create or update file
			if step.Path == "" {
				stderr.WriteString("FilePatch step requires 'path' field\n")
				exitCode = 1
				break
			}

			// Convert absolute path to relative if needed
			filePath := step.Path
			if strings.HasPrefix(filePath, sandbox.Status.WorkDir+"/") {
				filePath = strings.TrimPrefix(filePath, sandbox.Status.WorkDir+"/")
			} else if strings.HasPrefix(filePath, "/") {
				// If it's an absolute path not in workdir, use it as-is
				// and set BasePath to empty
				files := map[string]string{
					filePath: step.Content,
				}
				fileReq := &sidecar.FileRequest{
					BasePath: "",
					Files:    files,
					Patch:    step.Content,
				}
				resp, err := r.SidecarClient.UpdateFiles(ctx, sandbox.Status.PodIP, fileReq)
				if err != nil {
					stderr.WriteString("Failed to update files: " + err.Error() + "\n")
					exitCode = 1
					break
				}
				if !resp.IsSuccess() {
					stderr.WriteString("File update failed: " + resp.GetMessage() + "\n")
					exitCode = 1
					break
				}
				stdout.WriteString("File created: " + step.Path + "\n")
				continue
			}

			files := map[string]string{
				filePath: step.Content,
			}

			fileReq := &sidecar.FileRequest{
				BasePath: sandbox.Status.WorkDir,
				Files:    files,
				Patch:    step.Content,
			}
			resp, err := r.SidecarClient.UpdateFiles(ctx, sandbox.Status.PodIP, fileReq)
			if err != nil {
				stderr.WriteString("Failed to update files: " + err.Error() + "\n")
				exitCode = 1
				break
			}
			if !resp.IsSuccess() {
				stderr.WriteString("File update failed: " + resp.GetMessage() + "\n")
				exitCode = 1
				break
			}
			stdout.WriteString("File created: " + step.Path + "\n")

		case arlv1alpha1.StepTypeCommand:
			// Execute command
			timeout := int32(30)
			if task.Spec.Timeout.Duration > 0 {
				timeout = int32(task.Spec.Timeout.Duration.Seconds())
			}

			workDir := sandbox.Status.WorkDir
			if step.WorkDir != "" {
				workDir = step.WorkDir
			}

			execReq := &sidecar.ExecRequest{
				Command:        step.Command,
				Env:            step.Env,
				WorkingDir:     workDir,
				TimeoutSeconds: timeout,
			}
			resp, err := r.SidecarClient.Execute(ctx, sandbox.Status.PodIP, execReq)
			if err != nil {
				stderr.WriteString("Failed to execute command: " + err.Error() + "\n")
				exitCode = 1
				break
			}
			stdout.WriteString(resp.GetStdout())
			stderr.WriteString(resp.GetStderr())
			exitCode = resp.GetExitCode()
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

	// Update sandbox LastTaskTime
	sandbox.Status.LastTaskTime = &now
	if err := r.Status().Update(ctx, sandbox); err != nil {
		logger.Error(err, "Failed to update sandbox LastTaskTime")
	}

	// Write audit log if no TTL is set (TTL controller handles audit for TTL tasks)
	if r.AuditWriter != nil && task.Spec.TTLSecondsAfterFinished == nil {
		logger.Info("Writing task audit record", "task", task.Name, "state", task.Status.State)
		// Serialize steps (input) to JSON
		inputJSON, err := json.Marshal(task.Spec.Steps)
		if err != nil {
			logger.Error(err, "Failed to marshal task steps to JSON for audit")
		}

		record := interfaces.TaskAuditRecord{
			TraceID:    task.Spec.TraceID,
			Namespace:  task.Namespace,
			Name:       task.Name,
			SandboxRef: task.Spec.SandboxRef,
			State:      string(task.Status.State),
			ExitCode:   task.Status.ExitCode,
			Duration:   task.Status.Duration.Duration.String(),
			StepCount:  len(task.Spec.Steps),
			Input:      string(inputJSON),
			Stdout:     task.Status.Stdout,
			Stderr:     task.Status.Stderr,
		}
		if task.Status.StartTime != nil {
			record.StartTime = task.Status.StartTime.Time
		}
		if task.Status.CompletionTime != nil {
			record.CompletionTime = task.Status.CompletionTime.Time
		}
		if err := r.AuditWriter.WriteTaskCompletion(ctx, record); err != nil {
			logger.Error(err, "Failed to write task audit record")
			if r.Metrics != nil {
				r.Metrics.RecordAuditWriteError("task")
			}
		} else {
			logger.Info("Task audit record written successfully", "task", task.Name)
		}
	} else {
		logger.Info("Skipping task audit", "task", task.Name, "hasAuditWriter", r.AuditWriter != nil, "hasTTL", task.Spec.TTLSecondsAfterFinished != nil)
	}

	// Record metrics
	if r.Metrics != nil {
		if task.Status.StartTime != nil {
			duration := now.Time.Sub(task.Status.StartTime.Time)
			r.Metrics.RecordTaskDuration(task.Namespace, task.Name, duration)
		}
		r.Metrics.RecordTaskState(task.Namespace, task.Name, string(task.Status.State))
	}

	logger.Info("Task completed",
		"task", task.Name,
		"state", task.Status.State,
		"exitCode", exitCode)

	// Handle sandbox cleanup if keepAlive is false
	if !sandbox.Spec.KeepAlive {
		// Check if all tasks for this sandbox are complete
		allComplete, err := r.areAllTasksComplete(ctx, sandbox)
		if err != nil {
			logger.Error(err, "Failed to check task completion status")
		} else if allComplete {
			logger.Info("All tasks complete for non-keepAlive sandbox, marking for deletion",
				"sandbox", sandbox.Name)
			// Note: Actual deletion should be handled by sandbox controller
			// We just update a condition here
			sandbox.Status.Conditions = append(sandbox.Status.Conditions, metav1.Condition{
				Type:               "ReadyForCleanup",
				Status:             metav1.ConditionTrue,
				Reason:             "AllTasksComplete",
				Message:            "All tasks completed, sandbox can be deleted",
				LastTransitionTime: metav1.Now(),
			})
			if err := r.Status().Update(ctx, sandbox); err != nil {
				logger.Error(err, "Failed to update sandbox status")
			}
		}
	}

	return ctrl.Result{}, nil
}

// areAllTasksComplete checks if all tasks referencing a sandbox are complete
func (r *TaskReconciler) areAllTasksComplete(ctx context.Context, sandbox *arlv1alpha1.Sandbox) (bool, error) {
	taskList := &arlv1alpha1.TaskList{}
	if err := r.List(ctx, taskList, client.InNamespace(sandbox.Namespace)); err != nil {
		return false, err
	}

	for _, task := range taskList.Items {
		if task.Spec.SandboxRef == sandbox.Name {
			if task.Status.State != arlv1alpha1.TaskStateSucceeded &&
				task.Status.State != arlv1alpha1.TaskStateFailed {
				return false, nil
			}
		}
	}
	return true, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *TaskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&arlv1alpha1.Task{}).
		Complete(r)
}

// Name returns the controller name for logging
func (r *TaskReconciler) Name() string {
	return "Task"
}
