package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
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
)

const (
	PoolLabelKey    = "arl.infra.io/pool"
	SandboxLabelKey = "arl.infra.io/sandbox"
	StatusLabelKey  = "arl.infra.io/status"
	StatusIdle      = "idle"
	StatusAllocated = "allocated"
)

// WarmPoolReconciler reconciles a WarmPool object
type WarmPoolReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Config     *config.Config
	Metrics    interfaces.MetricsCollector
	Middleware *middleware.Chain
}

// +kubebuilder:rbac:groups=arl.infra.io,resources=warmpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arl.infra.io,resources=warmpools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arl.infra.io,resources=warmpools/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete

// Reconcile manages the WarmPool lifecycle
func (r *WarmPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Execute middleware chain if enabled
	if r.Middleware != nil {
		if err := r.Middleware.ExecuteBefore(ctx, req); err != nil {
			return ctrl.Result{}, err
		}
		defer r.Middleware.ExecuteAfter(ctx, req, nil)
	}

	return r.reconcile(ctx, req)
}

func (r *WarmPoolReconciler) reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the WarmPool instance
	pool := &arlv1alpha1.WarmPool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// List all pods belonging to this pool
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(req.Namespace),
		client.MatchingLabels{PoolLabelKey: pool.Name}); err != nil {
		return ctrl.Result{}, err
	}

	// Count idle and allocated pods
	var readyIdle, totalIdle, allocated, totalPods int32
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		totalPods++ // Count all non-deleted pods
		status := pod.Labels[StatusLabelKey]
		if status == StatusIdle {
			totalIdle++ // Count all idle pods (including pending/creating)
			if pod.Status.Phase == corev1.PodRunning {
				readyIdle++
			}
		} else if status == StatusAllocated {
			allocated++
		}
	}

	// Calculate how many pods to create - only create if total idle < desired
	needed := pool.Spec.Replicas - totalIdle

	logger.Info("Pool status",
		"pool", pool.Name,
		"desired", pool.Spec.Replicas,
		"ready", readyIdle,
		"totalIdle", totalIdle,
		"allocated", allocated,
		"totalPods", totalPods,
		"needed", needed)

	// Record metrics
	if r.Metrics != nil {
		r.Metrics.RecordPoolUtilization(pool.Name, readyIdle, allocated)
	}

	// Create new pods if needed
	if needed > 0 {
		for i := int32(0); i < needed; i++ {
			pod := r.constructPod(pool)
			if err := r.Create(ctx, pod); err != nil {
				logger.Error(err, "Failed to create pod")
				continue
			}
			logger.Info("Created pod", "pod", pod.Name)
		}
	} else if needed < 0 {
		// Delete excess pods (scale down)
		toDelete := -needed
		logger.Info("Scaling down pool", "toDelete", toDelete)

		// Get idle pods to delete
		var idlePods corev1.PodList
		if err := r.List(ctx, &idlePods, client.InNamespace(pool.Namespace),
			client.MatchingLabels{
				PoolLabelKey:   pool.Name,
				StatusLabelKey: StatusIdle,
			}); err != nil {
			logger.Error(err, "Failed to list idle pods for deletion")
		} else {
			// Delete the excess idle pods
			deleted := int32(0)
			for i := range idlePods.Items {
				if deleted >= toDelete {
					break
				}
				pod := &idlePods.Items[i]
				if err := r.Delete(ctx, pod); err != nil {
					logger.Error(err, "Failed to delete pod", "pod", pod.Name)
					continue
				}
				logger.Info("Deleted excess pod", "pod", pod.Name)
				deleted++
			}
		}
	}

	// Update status
	pool.Status.ReadyReplicas = readyIdle
	pool.Status.AllocatedReplicas = allocated
	if err := r.Status().Update(ctx, pool); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue to maintain the pool
	requeueDelay := r.Config.DefaultRequeueDelay
	return ctrl.Result{RequeueAfter: requeueDelay}, nil
}

// constructPod creates a Pod from the WarmPool template
func (r *WarmPoolReconciler) constructPod(pool *arlv1alpha1.WarmPool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: pool.Name + "-",
			Namespace:    pool.Namespace,
			Labels: map[string]string{
				PoolLabelKey:   pool.Name,
				StatusLabelKey: StatusIdle,
			},
		},
		Spec: pool.Spec.Template.Spec,
	}

	// Ensure sidecar container exists
	hasSidecar := false
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "sidecar" {
			hasSidecar = true
			break
		}
	}

	if !hasSidecar {
		// Add default sidecar container
		sidecarContainer := corev1.Container{
			Name:            "sidecar",
			Image:           "arl-sidecar:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Ports: []corev1.ContainerPort{
				{
					ContainerPort: int32(r.Config.SidecarPort),
					Protocol:      corev1.ProtocolTCP,
				},
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "workspace",
					MountPath: r.Config.WorkspaceDir,
				},
			},
		}
		pod.Spec.Containers = append(pod.Spec.Containers, sidecarContainer)
	}

	// Add shared workspace volume if not exists
	hasWorkspace := false
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == "workspace" {
			hasWorkspace = true
			break
		}
	}

	if !hasWorkspace {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: "workspace",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	// Set owner reference
	ctrl.SetControllerReference(pool, pod, r.Scheme)

	return pod
}

// SetupWithManager sets up the controller with the Manager
func (r *WarmPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&arlv1alpha1.WarmPool{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

// Name returns the controller name for logging
func (r *WarmPoolReconciler) Name() string {
	return "WarmPool"
}
