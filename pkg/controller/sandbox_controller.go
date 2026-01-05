package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
)

// SandboxReconciler reconciles a Sandbox object
type SandboxReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=arl.infra.io,resources=sandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arl.infra.io,resources=sandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arl.infra.io,resources=sandboxes/finalizers,verbs=update

// Reconcile manages the Sandbox lifecycle
func (r *SandboxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Sandbox instance
	sandbox := &arlv1alpha1.Sandbox{}
	if err := r.Get(ctx, req.NamespacedName, sandbox); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// If already bound and ready, nothing to do
	if sandbox.Status.Phase == arlv1alpha1.SandboxPhaseReady {
		return ctrl.Result{}, nil
	}

	// If pending, try to bind to a pod
	if sandbox.Status.Phase == "" || sandbox.Status.Phase == arlv1alpha1.SandboxPhasePending {
		// Find an idle pod from the pool
		podList := &corev1.PodList{}
		if err := r.List(ctx, podList,
			client.InNamespace(req.Namespace),
			client.MatchingLabels{
				PoolLabelKey:   sandbox.Spec.PoolRef,
				StatusLabelKey: StatusIdle,
			}); err != nil {
			return ctrl.Result{}, err
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
			logger.Info("No idle pods available", "pool", sandbox.Spec.PoolRef)
			sandbox.Status.Phase = arlv1alpha1.SandboxPhasePending
			if err := r.Status().Update(ctx, sandbox); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// Mark pod as allocated
		if selectedPod.Labels == nil {
			selectedPod.Labels = make(map[string]string)
		}
		selectedPod.Labels[StatusLabelKey] = StatusAllocated
		selectedPod.Labels[SandboxLabelKey] = sandbox.Name

		if err := r.Update(ctx, selectedPod); err != nil {
			return ctrl.Result{}, err
		}

		// Update sandbox status
		sandbox.Status.Phase = arlv1alpha1.SandboxPhaseBound
		sandbox.Status.PodName = selectedPod.Name
		sandbox.Status.PodIP = selectedPod.Status.PodIP
		sandbox.Status.WorkDir = WorkspaceDir

		if err := r.Status().Update(ctx, sandbox); err != nil {
			return ctrl.Result{}, err
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
				sandbox.Status.Phase = arlv1alpha1.SandboxPhaseFailed
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
			sandbox.Status.Phase = arlv1alpha1.SandboxPhaseReady
			sandbox.Status.PodIP = pod.Status.PodIP
			
			if err := r.Status().Update(ctx, sandbox); err != nil {
				return ctrl.Result{}, err
			}

			logger.Info("Sandbox is ready", "sandbox", sandbox.Name)
			return ctrl.Result{}, nil
		}

		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *SandboxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&arlv1alpha1.Sandbox{}).
		Complete(r)
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
