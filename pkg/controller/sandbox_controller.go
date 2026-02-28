package controller

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	"github.com/Lincyaw/agent-env/pkg/config"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/Lincyaw/agent-env/pkg/middleware"
)

const (
	sandboxFinalizer = "arl.infra.io/sandbox-finalizer"

	// Requeue delays for various scenarios
	DefaultPodWaitRequeueDelay = 5 * time.Second // Wait time when no idle pods available
)

// SandboxReconciler reconciles a Sandbox object
type SandboxReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Config      *config.Config
	Metrics     interfaces.MetricsCollector
	AuditWriter interfaces.AuditWriter
	Middleware  *middleware.Chain
}

// +kubebuilder:rbac:groups=arl.infra.io,resources=sandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arl.infra.io,resources=sandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arl.infra.io,resources=sandboxes/finalizers,verbs=update

// Reconcile manages the Sandbox lifecycle
func (r *SandboxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	// Execute middleware chain if enabled
	if r.Middleware != nil {
		if err := r.Middleware.ExecuteBefore(ctx, req); err != nil {
			return ctrl.Result{}, err
		}
		defer func() { r.Middleware.ExecuteAfter(ctx, req, err) }()
	}

	return r.reconcile(ctx, req)
}

func (r *SandboxReconciler) reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Create tracing span
	tracer := otel.Tracer("sandbox-controller")
	ctx, span := tracer.Start(ctx, "SandboxReconcile",
		trace.WithAttributes(
			attribute.String("sandbox.namespace", req.Namespace),
			attribute.String("sandbox.name", req.Name),
		),
	)
	defer span.End()

	logger := log.FromContext(ctx)

	// Add span trace ID to logger for correlation
	spanContext := span.SpanContext()
	if spanContext.HasTraceID() {
		logger = logger.WithValues("otel.trace_id", spanContext.TraceID().String())
	}

	// Fetch the Sandbox instance
	sandbox := &arlv1alpha1.Sandbox{}
	if err := r.Get(ctx, req.NamespacedName, sandbox); err != nil {
		if errors.IsNotFound(err) {
			// Object not found, could have been deleted after reconcile request
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		return ctrl.Result{}, fmt.Errorf("failed to get Sandbox %s/%s: %w", req.Namespace, req.Name, err)
	}

	span.SetAttributes(
		attribute.String("sandbox.pool_ref", sandbox.Spec.PoolRef),
		attribute.String("sandbox.phase", string(sandbox.Status.Phase)),
	)

	// Handle deletion with finalizer
	if !sandbox.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, sandbox)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(sandbox, sandboxFinalizer) {
		controllerutil.AddFinalizer(sandbox, sandboxFinalizer)
		if err := r.Update(ctx, sandbox); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer to Sandbox %s/%s: %w", sandbox.Namespace, sandbox.Name, err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// If already bound and ready, check for idle timeout and cleanup
	if sandbox.Status.Phase == arlv1alpha1.SandboxPhaseReady {
		return r.handleReadySandbox(ctx, sandbox)
	}

	// If phase is empty, initialize to Pending first
	if sandbox.Status.Phase == "" {
		newPhase := arlv1alpha1.SandboxPhasePending
		if err := r.updateSandboxPhase(ctx, sandbox, newPhase); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to initialize Sandbox %s/%s phase to Pending: %w",
				sandbox.Namespace, sandbox.Name, err)
		}
		if err := r.Status().Update(ctx, sandbox); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update Sandbox %s/%s status: %w",
				sandbox.Namespace, sandbox.Name, err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// If pending, try to bind to a pod
	if sandbox.Status.Phase == arlv1alpha1.SandboxPhasePending {

		// Find an idle pod from the pool
		podList := &corev1.PodList{}
		if err := r.List(ctx, podList,
			client.InNamespace(req.Namespace),
			client.MatchingLabels{
				PoolLabelKey:   sandbox.Spec.PoolRef,
				StatusLabelKey: StatusIdle,
			}); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to list idle pods from pool %s for Sandbox %s/%s: %w",
				sandbox.Spec.PoolRef, sandbox.Namespace, sandbox.Name, err)
		}

		// Find a running pod
		var selectedPod *corev1.Pod
		for i := range podList.Items {
			if podList.Items[i].Status.Phase == corev1.PodRunning &&
				podList.Items[i].DeletionTimestamp == nil {
				selectedPod = &podList.Items[i]
				break
			}
		}

		if selectedPod == nil {
			logger.Info("No idle pods available", "pool", sandbox.Spec.PoolRef, "sandbox", sandbox.Name)
			if r.Metrics != nil {
				r.Metrics.IncrementNoIdlePods(sandbox.Spec.PoolRef)
			}
			return ctrl.Result{RequeueAfter: DefaultPodWaitRequeueDelay}, nil
		}

		// Mark pod as allocated
		if selectedPod.Labels == nil {
			selectedPod.Labels = make(map[string]string)
		}
		selectedPod.Labels[StatusLabelKey] = StatusAllocated
		selectedPod.Labels[SandboxLabelKey] = sandbox.Name

		if err := r.Update(ctx, selectedPod); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update pod %s/%s labels for Sandbox %s: %w",
				selectedPod.Namespace, selectedPod.Name, sandbox.Name, err)
		}

		// Update sandbox status with validation
		newPhase := arlv1alpha1.SandboxPhaseBound
		if err := r.updateSandboxPhase(ctx, sandbox, newPhase); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update Sandbox %s/%s phase to Bound: %w",
				sandbox.Namespace, sandbox.Name, err)
		}

		sandbox.Status.PodName = selectedPod.Name
		sandbox.Status.PodIP = selectedPod.Status.PodIP
		sandbox.Status.WorkDir = r.Config.WorkspaceDir

		if err := r.Status().Update(ctx, sandbox); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update Sandbox %s/%s status: %w",
				sandbox.Namespace, sandbox.Name, err)
		}

		logger.Info("Bound sandbox to pod",
			"sandbox", sandbox.Name,
			"pod", selectedPod.Name)

		return ctrl.Result{Requeue: true}, nil
	}

	// If bound, check if pod is ready
	if sandbox.Status.Phase == arlv1alpha1.SandboxPhaseBound {
		pod := &corev1.Pod{}
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: req.Namespace,
			Name:      sandbox.Status.PodName,
		}, pod); err != nil {
			if errors.IsNotFound(err) {
				newPhase := arlv1alpha1.SandboxPhaseFailed
				if err := r.updateSandboxPhase(ctx, sandbox, newPhase); err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to update Sandbox %s/%s phase to Failed: %w",
						sandbox.Namespace, sandbox.Name, err)
				}
				if err := r.Status().Update(ctx, sandbox); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		// Check if all containers are ready
		allReady := true
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				allReady = false
				break
			}
		}

		if allReady {
			newPhase := arlv1alpha1.SandboxPhaseReady
			if err := r.updateSandboxPhase(ctx, sandbox, newPhase); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update Sandbox %s/%s phase to Ready: %w",
					sandbox.Namespace, sandbox.Name, err)
			}
			sandbox.Status.PodIP = pod.Status.PodIP

			// Initialize LastTaskTime if not set
			if sandbox.Status.LastTaskTime == nil {
				now := metav1.Now()
				sandbox.Status.LastTaskTime = &now
			}

			if err := r.Status().Update(ctx, sandbox); err != nil {
				return ctrl.Result{}, err
			}

			// Record end-to-end sandbox allocation latency: creation → Ready
			if r.Metrics != nil {
				r.Metrics.RecordSandboxE2EReady(
					sandbox.Spec.PoolRef,
					time.Since(sandbox.CreationTimestamp.Time),
				)
			}

			logger.Info("Sandbox is ready", "sandbox", sandbox.Name)
			return ctrl.Result{}, nil
		}

		checkInterval := r.Config.SandboxCheckInterval
		return ctrl.Result{RequeueAfter: checkInterval}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *SandboxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	rl := workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](
			time.Duration(r.Config.WarmPoolBaseDelayMs)*time.Millisecond,
			time.Duration(r.Config.WarmPoolMaxDelayMs)*time.Millisecond,
		),
		&workqueue.TypedBucketRateLimiter[reconcile.Request]{
			Limiter: rate.NewLimiter(rate.Limit(r.Config.WarmPoolRateLimitQPS), r.Config.WarmPoolRateLimitBurst),
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&arlv1alpha1.Sandbox{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.Config.SandboxMaxConcurrent,
			RateLimiter:             rl,
		}).
		Complete(r)
}

// Name returns the controller name for logging
func (r *SandboxReconciler) Name() string {
	return "Sandbox"
}

// handleReadySandbox handles a sandbox that is in Ready phase
func (r *SandboxReconciler) handleReadySandbox(ctx context.Context, sandbox *arlv1alpha1.Sandbox) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Use creation time as fallback if LastTaskTime is not set
	if sandbox.Status.LastTaskTime == nil {
		sandbox.Status.LastTaskTime = &sandbox.CreationTimestamp
		if err := r.Status().Update(ctx, sandbox); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update sandbox LastTaskTime: %w", err)
		}
	}

	// Check for max lifetime (checked before idle timeout)
	maxLifetime := r.getMaxLifetime(sandbox)
	if maxLifetime > 0 {
		sandboxAge := time.Since(sandbox.CreationTimestamp.Time)
		if sandboxAge >= maxLifetime {
			logger.Info("Sandbox max lifetime exceeded, deleting",
				"sandbox", sandbox.Name,
				"age", sandboxAge,
				"maxLifetime", maxLifetime)

			if err := r.Delete(ctx, sandbox); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to delete sandbox after max lifetime: %w", err)
			}
			return ctrl.Result{}, nil
		}
	}

	// Check for idle timeout
	idleTimeout := r.getIdleTimeout(sandbox)
	if idleTimeout > 0 && sandbox.Status.LastTaskTime != nil {
		idleDuration := time.Since(sandbox.Status.LastTaskTime.Time)
		if idleDuration >= idleTimeout {
			logger.Info("Sandbox idle timeout exceeded, deleting",
				"sandbox", sandbox.Name,
				"idleDuration", idleDuration,
				"idleTimeout", idleTimeout)

			// Record idle duration metric
			if r.Metrics != nil {
				r.Metrics.RecordSandboxIdleDuration(sandbox.Spec.PoolRef, sandbox.Namespace, idleDuration)
			}

			if err := r.Delete(ctx, sandbox); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to delete idle sandbox: %w", err)
			}
			return ctrl.Result{}, nil
		}

		// Requeue to check again after remaining timeout
		remaining := idleTimeout - idleDuration
		return ctrl.Result{RequeueAfter: remaining}, nil
	}

	// Requeue periodically to check idle status
	return ctrl.Result{RequeueAfter: r.Config.SandboxCheckInterval}, nil
}

// handleDeletion handles sandbox deletion with finalizer
func (r *SandboxReconciler) handleDeletion(ctx context.Context, sandbox *arlv1alpha1.Sandbox) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(sandbox, sandboxFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("Handling sandbox deletion", "sandbox", sandbox.Name)

	// Delete the pod to ensure complete cleanup (files, processes, state)
	// WarmPool controller will automatically create a new pod to maintain pool size
	if sandbox.Status.PodName != "" {
		pod := &corev1.Pod{}
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: sandbox.Namespace,
			Name:      sandbox.Status.PodName,
		}, pod); err == nil {
			// Delete the pod - this ensures complete cleanup
			if err := r.Delete(ctx, pod); err != nil {
				logger.Error(err, "Failed to delete pod", "pod", pod.Name)
				return ctrl.Result{}, fmt.Errorf("failed to delete pod: %w", err)
			}
			if r.Metrics != nil {
				r.Metrics.IncrementPodDelete(sandbox.Spec.PoolRef, "sandbox_cleanup")
			}
			logger.Info("Deleted pod for complete cleanup", "pod", pod.Name)
		} else if !errors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("failed to get pod: %w", err)
		}
	}

	// Record idle duration metric if applicable
	if r.Metrics != nil && sandbox.Status.LastTaskTime != nil {
		idleDuration := time.Since(sandbox.Status.LastTaskTime.Time)
		r.Metrics.RecordSandboxIdleDuration(sandbox.Spec.PoolRef, sandbox.Namespace, idleDuration)
	}

	// Write audit record
	if r.AuditWriter != nil {
		record := interfaces.SandboxAuditRecord{
			Namespace: sandbox.Namespace,
			Name:      sandbox.Name,
			PoolRef:   sandbox.Spec.PoolRef,
			Phase:     string(sandbox.Status.Phase),
			PodName:   sandbox.Status.PodName,
			Event:     "deleted",
		}
		if err := r.AuditWriter.WriteSandboxEvent(ctx, record); err != nil {
			logger.Error(err, "Failed to write sandbox audit record")
			if r.Metrics != nil {
				r.Metrics.RecordAuditWriteError("sandbox")
			}
		}
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(sandbox, sandboxFinalizer)
	if err := r.Update(ctx, sandbox); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	logger.Info("Sandbox deleted successfully", "sandbox", sandbox.Name)
	return ctrl.Result{}, nil
}

// getIdleTimeout returns the idle timeout for the sandbox
func (r *SandboxReconciler) getIdleTimeout(sandbox *arlv1alpha1.Sandbox) time.Duration {
	// Use sandbox-specific timeout if set
	if sandbox.Spec.IdleTimeoutSeconds != nil {
		return time.Duration(*sandbox.Spec.IdleTimeoutSeconds) * time.Second
	}
	// Fall back to config default
	if r.Config.SandboxIdleTimeoutSeconds > 0 {
		return time.Duration(r.Config.SandboxIdleTimeoutSeconds) * time.Second
	}
	return 0
}

// getMaxLifetime returns the max lifetime for the sandbox
func (r *SandboxReconciler) getMaxLifetime(sandbox *arlv1alpha1.Sandbox) time.Duration {
	// Use sandbox-specific max lifetime if set
	if sandbox.Spec.MaxLifetimeSeconds != nil {
		return time.Duration(*sandbox.Spec.MaxLifetimeSeconds) * time.Second
	}
	// Fall back to config default
	if r.Config.SandboxMaxLifetimeSeconds > 0 {
		return time.Duration(r.Config.SandboxMaxLifetimeSeconds) * time.Second
	}
	return 0
}

// findCondition finds a condition by type
func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// setCondition sets a condition
func setCondition(conditions *[]metav1.Condition, condType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	cond := findCondition(*conditions, condType)
	if cond == nil {
		*conditions = append(*conditions, metav1.Condition{
			Type:               condType,
			Status:             status,
			Reason:             reason,
			Message:            message,
			LastTransitionTime: now,
		})
	} else {
		if cond.Status != status {
			cond.Status = status
			cond.LastTransitionTime = now
		}
		cond.Reason = reason
		cond.Message = message
	}
}

// updateSandboxPhase updates the sandbox phase with validation
func (r *SandboxReconciler) updateSandboxPhase(ctx context.Context, sandbox *arlv1alpha1.Sandbox, newPhase arlv1alpha1.SandboxPhase) error {
	logger := log.FromContext(ctx)

	// Validate phase transition
	if err := sandbox.ValidatePhaseTransition(newPhase); err != nil {
		logger.Error(err, "Invalid phase transition attempt",
			"sandbox", sandbox.Name,
			"currentPhase", sandbox.Status.Phase,
			"newPhase", newPhase)

		// Record event for visibility
		setCondition(&sandbox.Status.Conditions, "PhaseTransition", metav1.ConditionFalse,
			"InvalidTransition", fmt.Sprintf("Invalid phase transition from %s to %s: %v", sandbox.Status.Phase, newPhase, err))

		return err
	}

	sandbox.Status.Phase = newPhase
	return nil
}
