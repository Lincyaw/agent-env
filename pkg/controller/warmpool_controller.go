package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	"github.com/Lincyaw/agent-env/pkg/config"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/Lincyaw/agent-env/pkg/labels"
	"github.com/Lincyaw/agent-env/pkg/middleware"
	"github.com/Lincyaw/agent-env/pkg/scheduler"
)

// Re-export label constants for backward compatibility within the controller package.
const (
	PoolLabelKey    = labels.PoolLabelKey
	SandboxLabelKey = labels.SandboxLabelKey
	StatusLabelKey  = labels.StatusLabelKey
	StatusIdle      = labels.StatusIdle
	StatusAllocated = labels.StatusAllocated
	StatusRecycling = labels.StatusRecycling
)

// WarmPoolReconciler reconciles a WarmPool object
type WarmPoolReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Config         *config.Config
	Metrics        interfaces.MetricsCollector
	Middleware     *middleware.Chain
	ImageScheduler *scheduler.ImageScheduler

	// in-memory sets for one-time metric recording (keyed by pod UID / pool key)
	recordedPods          sync.Map // types.UID → struct{}
	recordedErrors        sync.Map // "<uid>/<container>/<reason>" → struct{}
	scaleLastTarget       sync.Map // "<ns>/<pool>" → int32
	scaleStartTime        sync.Map // "<ns>/<pool>" → time.Time
	scaleFirstPodRecorded sync.Map // "<ns>/<pool>" → struct{} cleared on each scale-up
}

// +kubebuilder:rbac:groups=arl.infra.io,resources=warmpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arl.infra.io,resources=warmpools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arl.infra.io,resources=warmpools/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile manages the WarmPool lifecycle
func (r *WarmPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	// Execute middleware chain if enabled
	if r.Middleware != nil {
		if err := r.Middleware.ExecuteBefore(ctx, req); err != nil {
			return ctrl.Result{}, err
		}
		defer func() { r.Middleware.ExecuteAfter(ctx, req, err) }()
	}

	return r.reconcile(ctx, req)
}

func (r *WarmPoolReconciler) reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Create tracing span
	tracer := otel.Tracer("warmpool-controller")
	ctx, span := tracer.Start(ctx, "WarmPoolReconcile",
		trace.WithAttributes(
			attribute.String("pool.namespace", req.Namespace),
			attribute.String("pool.name", req.Name),
		),
	)
	defer span.End()

	logger := log.FromContext(ctx)

	// Add span trace ID to logger for correlation
	spanContext := span.SpanContext()
	if spanContext.HasTraceID() {
		logger = logger.WithValues("otel.trace_id", spanContext.TraceID().String())
	}

	// Fetch the WarmPool instance
	pool := &arlv1alpha1.WarmPool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		if errors.IsNotFound(err) {
			if r.Metrics != nil {
				r.Metrics.DeletePoolMetrics(req.Name)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	span.SetAttributes(
		attribute.Int("pool.replicas.desired", int(pool.Spec.Replicas)),
	)

	// Detect scale-out events before processing pod state
	r.detectAndTrackScale(pool)

	cfg, err := r.resolveConfigEnvSpec(pool)
	if err != nil {
		r.setConfigEnvFailureStatus(pool, err.Error())
		_ = r.Status().Update(ctx, pool)
		return ctrl.Result{}, err
	}

	renderedConfig, err := r.reconcileConfigEnv(ctx, pool, cfg)
	if err != nil {
		r.setConfigEnvFailureStatus(pool, err.Error())
		if statusErr := r.Status().Update(ctx, pool); statusErr != nil {
			return ctrl.Result{}, statusErr
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

	// Count idle and allocated pods, detect failures
	var readyIdle, totalIdle, allocated, totalPods, failedPods int32
	var failureMessage string
	var staleConfigPods []*corev1.Pod
	var initTimeoutPods []corev1.Pod
	var prePullPods []corev1.Pod
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		if pod.Labels[labels.RoleLabelKey] == labels.RolePrePull {
			prePullPods = append(prePullPods, pod)
			continue
		}

		currentConfigHash := ""
		if renderedConfig != nil {
			currentConfigHash = renderedConfig.hash
		}
		staleConfig := pod.Annotations[configEnvHashAnnotation] != currentConfigHash
		if staleConfig && pod.Labels[StatusLabelKey] != StatusAllocated {
			staleConfigPods = append(staleConfigPods, pod.DeepCopy())
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

		// Detect failed pods (CrashLoopBackOff, ImagePullBackOff, etc.)
		if pod.Status.Phase == corev1.PodFailed {
			failedPods++
			if pod.Status.Message != "" {
				failureMessage = pod.Status.Message
			}
		}
		for _, cs := range pod.Status.InitContainerStatuses {
			if cs.State.Waiting != nil && (cs.State.Waiting.Reason == "CrashLoopBackOff" ||
				cs.State.Waiting.Reason == "ImagePullBackOff" ||
				cs.State.Waiting.Reason == "ErrImagePull" ||
				cs.State.Waiting.Reason == "CreateContainerError") {
				failedPods++
				failureMessage = fmt.Sprintf("init container %s: %s - %s",
					cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
			}
			if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 && cs.RestartCount > 2 {
				failedPods++
				failureMessage = fmt.Sprintf("init container %s terminated: exit_code=%d reason=%s message=%s",
					cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason, cs.State.Terminated.Message)
			}
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil && (cs.State.Waiting.Reason == "CrashLoopBackOff" ||
				cs.State.Waiting.Reason == "ImagePullBackOff" ||
				cs.State.Waiting.Reason == "ErrImagePull") {
				failedPods++
				failureMessage = fmt.Sprintf("container %s: %s - %s",
					cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
			}
		}

		// Observe image-pull errors (deduplicated per pod+container+reason)
		r.observeImagePullErrors(pool.Name, pod)

		// Detect pods stuck in Pending (e.g. hanging image pull) beyond the init timeout.
		if pod.Status.Phase == corev1.PodPending && r.Config.PodInitTimeout > 0 {
			if time.Since(pod.CreationTimestamp.Time) > r.Config.PodInitTimeout {
				initTimeoutPods = append(initTimeoutPods, pod)
			}
		}
	}

	// Force-delete pods that exceeded the init timeout.
	if len(initTimeoutPods) > 0 {
		logger.Info("Deleting pods stuck in init", "count", len(initTimeoutPods), "timeout", r.Config.PodInitTimeout)
		grace := int64(0)
		delOpts := &client.DeleteOptions{GracePeriodSeconds: &grace}
		g, gCtx := errgroup.WithContext(ctx)
		g.SetLimit(20)
		for i := range initTimeoutPods {
			pod := &initTimeoutPods[i]
			g.Go(func() error {
				age := time.Since(pod.CreationTimestamp.Time).Round(time.Second)
				if err := r.Delete(gCtx, pod, delOpts); err != nil && !errors.IsNotFound(err) {
					logger.Error(err, "Failed to force-delete init-timeout pod", "pod", pod.Name)
					failedPods++
					failureMessage = fmt.Sprintf("pod %s stuck in Pending for %s (init timeout)",
						pod.Name, age)
				} else {
					logger.Info("Force-deleted init-timeout pod", "pod", pod.Name, "age", age)
					if r.Metrics != nil {
						r.Metrics.IncrementPodDelete(pool.Name, "init_timeout")
					}
				}
				return nil
			})
		}
		_ = g.Wait()
	}

	if len(staleConfigPods) > 0 {
		currentConfigHash := ""
		if renderedConfig != nil {
			currentConfigHash = renderedConfig.hash
		}

		g, gCtx := errgroup.WithContext(ctx)
		g.SetLimit(20)
		for _, stalePod := range staleConfigPods {
			pod := stalePod
			g.Go(func() error {
				if err := r.deleteStaleConfigPod(gCtx, pod, currentConfigHash); err != nil {
					logger.Error(err, "Failed to recycle stale config pod", "pod", pod.Name)
				}
				return nil
			})
		}
		_ = g.Wait()
	}

	// Calculate how many pods to create - only create if total pods < desired
	needed := pool.Spec.Replicas - totalPods

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
		// Pods created but not yet in Running state
		r.Metrics.SetPendingPods(pool.Name, totalPods-readyIdle-allocated)
	}

	// One-time per-pod startup latency and scale-complete metrics
	r.observePodMetrics(pool, podList.Items)

	// Prune sync.Maps so entries for deleted pods don't accumulate forever
	r.pruneStaleEntries(podList.Items)

	// Create new pods if needed (parallel)
	if needed > 0 {
		g, gCtx := errgroup.WithContext(ctx)
		g.SetLimit(20) // cap concurrent API calls to avoid overwhelming API server
		for i := int32(0); i < needed; i++ {
			pod := r.constructPod(pool, renderedConfig)
			g.Go(func() error {
				if err := r.Create(gCtx, pod); err != nil {
					logger.Error(err, "Failed to create pod")
					return nil // don't abort other creates
				}
				logger.Info("Created pod", "pod", pod.Name)
				return nil
			})
		}
		_ = g.Wait()
	} else if needed < 0 {
		// Delete excess pods (parallel), preferring least-recently-used first.
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
			// Sort idle pods by LRU: use last-released annotation if present,
			// otherwise fall back to CreationTimestamp. Oldest (least recently
			// used) pods are sorted first and deleted first.
			sort.Slice(idlePods.Items, func(i, j int) bool {
				return podIdleAge(&idlePods.Items[i]).Before(podIdleAge(&idlePods.Items[j]))
			})

			g, gCtx := errgroup.WithContext(ctx)
			g.SetLimit(20)
			for i := range idlePods.Items {
				if int32(i) >= toDelete {
					break
				}
				pod := &idlePods.Items[i]
				podName := pod.Name
				g.Go(func() error {
					if err := r.Delete(gCtx, pod); err != nil {
						logger.Error(err, "Failed to delete pod", "pod", podName)
						return nil
					}
					if r.Metrics != nil {
						r.Metrics.IncrementPodDelete(pool.Name, "scale_down")
					}
					logger.Info("Deleted excess pod", "pod", podName)
					return nil
				})
			}
			_ = g.Wait()
		}
	}

	// Pre-pull: when replicas=0, ensure a lightweight pod exists to cache
	// the image on at least one node. When replicas>0 warm pods handle caching.
	r.reconcilePrePull(ctx, pool, prePullPods)

	// Update status with conditions
	pool.Status.ReadyReplicas = readyIdle
	pool.Status.AllocatedReplicas = allocated

	// Update conditions based on pod status
	if failedPods > 0 {
		logger.Error(fmt.Errorf("pods failing"), "Pool has failing pods",
			"failedPods", failedPods, "message", failureMessage)
		setCondition(&pool.Status.Conditions, "PodsFailing", metav1.ConditionTrue,
			"PodStartupFailed", failureMessage)
	} else {
		setCondition(&pool.Status.Conditions, "PodsFailing", metav1.ConditionFalse,
			"AllPodsHealthy", "")
	}

	if readyIdle >= pool.Spec.Replicas-allocated {
		setCondition(&pool.Status.Conditions, "Ready", metav1.ConditionTrue,
			"PoolReady", fmt.Sprintf("%d/%d pods ready", readyIdle, pool.Spec.Replicas))
	} else {
		setCondition(&pool.Status.Conditions, "Ready", metav1.ConditionFalse,
			"PoolNotReady", fmt.Sprintf("%d/%d pods ready", readyIdle, pool.Spec.Replicas))
	}

	if err := r.Status().Update(ctx, pool); err != nil {
		return ctrl.Result{}, err
	}

	// Emit scale duration when pool reaches desired ready count
	r.checkScaleComplete(pool, readyIdle)

	// Smart requeue: skip for empty pools, longer interval for stable pools
	if pool.Spec.Replicas == 0 && totalPods == 0 {
		// Pool is dormant (replicas=0, no pods). No periodic requeue needed;
		// the Watch on WarmPool spec changes will trigger reconciliation when
		// the pool is scaled up again.
		return ctrl.Result{}, nil
	}

	requeueDelay := r.Config.DefaultRequeueDelay
	if readyIdle >= pool.Spec.Replicas-allocated && failedPods == 0 && needed == 0 {
		// Pool is fully healthy and stable — use a longer requeue as a
		// safety-net drift check. Pod events still trigger immediate reconcile.
		requeueDelay = requeueDelay * 6 // e.g. 10s → 60s
	}
	return ctrl.Result{RequeueAfter: requeueDelay}, nil
}

// detectAndTrackScale stores the time when spec.replicas increases (scale-out event).
func (r *WarmPoolReconciler) detectAndTrackScale(pool *arlv1alpha1.WarmPool) {
	key := pool.Namespace + "/" + pool.Name
	desired := pool.Spec.Replicas
	if prev, ok := r.scaleLastTarget.Load(key); ok {
		if desired > prev.(int32) {
			r.scaleStartTime.Store(key, time.Now())
			r.scaleFirstPodRecorded.Delete(key) // reset so next ready pod marks "first pod"
		}
	}
	r.scaleLastTarget.Store(key, desired)
}

// checkScaleComplete emits arl_warmpool_all_pods_ready_seconds when readyIdle
// reaches spec.replicas after a scale-out event.
func (r *WarmPoolReconciler) checkScaleComplete(pool *arlv1alpha1.WarmPool, readyIdle int32) {
	if r.Metrics == nil {
		return
	}
	key := pool.Namespace + "/" + pool.Name
	startVal, ok := r.scaleStartTime.Load(key)
	if !ok {
		return
	}
	if readyIdle >= pool.Spec.Replicas && pool.Spec.Replicas > 0 {
		r.Metrics.RecordAllPodsReady(pool.Name, time.Since(startVal.(time.Time)))
		r.scaleStartTime.Delete(key)
	}
}

// reconcilePrePull ensures a lightweight pre-pull pod exists when replicas=0
// to cache the primary container image on at least one node. When replicas>0,
// warm pods handle image caching and any leftover pre-pull pod is cleaned up.
// prePullPods is the subset already collected from the main pod list.
func (r *WarmPoolReconciler) reconcilePrePull(ctx context.Context, pool *arlv1alpha1.WarmPool, prePullPods []corev1.Pod) {
	logger := log.FromContext(ctx)

	if pool.Spec.Replicas > 0 {
		for i := range prePullPods {
			if err := r.Delete(ctx, &prePullPods[i]); err != nil && !errors.IsNotFound(err) {
				logger.Error(err, "Failed to delete pre-pull pod", "pod", prePullPods[i].Name)
			}
		}
		return
	}

	// replicas == 0: ensure exactly one pre-pull pod exists.

	// Clean up terminated pre-pull pods (image is already cached regardless
	// of whether the container succeeded or failed).
	var alive bool
	for i := range prePullPods {
		pod := &prePullPods[i]
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			if err := r.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
				logger.Error(err, "Failed to clean up terminated pre-pull pod", "pod", pod.Name)
			}
			continue
		}
		alive = true
	}

	if alive {
		return // pre-pull pod already running or pending
	}

	// Extract primary image from the pool template.
	var image string
	for _, c := range pool.Spec.Template.Spec.Containers {
		if c.Name != "sidecar" {
			image = c.Image
			break
		}
	}
	if image == "" {
		return
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: pool.Name + "-prepull-",
			Namespace:    pool.Namespace,
			Labels: map[string]string{
				PoolLabelKey:        pool.Name,
				labels.RoleLabelKey: labels.RolePrePull,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "pull",
					Image:   image,
					Command: []string{"/bin/true"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1m"),
							corev1.ResourceMemory: resource.MustParse("1Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10m"),
							corev1.ResourceMemory: resource.MustParse("8Mi"),
						},
					},
				},
			},
		},
	}

	ctrl.SetControllerReference(pool, pod, r.Scheme)

	if err := r.Create(ctx, pod); err != nil {
		logger.Error(err, "Failed to create pre-pull pod")
		return
	}
	logger.Info("Created pre-pull pod to cache image", "pod", pod.Name, "image", image)
}

// observePodMetrics records one-time per-pod startup metrics for newly-ready pods.
// It is safe to call on every reconcile; each pod is recorded at most once.
func (r *WarmPoolReconciler) observePodMetrics(pool *arlv1alpha1.WarmPool, pods []corev1.Pod) {
	if r.Metrics == nil {
		return
	}
	for i := range pods {
		pod := &pods[i]
		if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
			continue
		}
		if _, loaded := r.recordedPods.LoadOrStore(pod.UID, struct{}{}); loaded {
			continue // already recorded for this pod
		}
		createdAt := pod.CreationTimestamp.Time
		nodeName := pod.Spec.NodeName

		// Scheduling latency: creation → PodScheduled condition
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionTrue {
				r.Metrics.RecordPodScheduleDuration(pool.Name, cond.LastTransitionTime.Sub(createdAt))
				break
			}
		}

		// Pod ready latency: creation → Ready condition
		// Also emit first-pod-ready for the first pod to become ready after a scale event.
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				readyDuration := cond.LastTransitionTime.Sub(createdAt)
				r.Metrics.RecordPodReadyDuration(pool.Name, nodeName, readyDuration)

				// Emit first-pod-ready once per scale event
				key := pool.Namespace + "/" + pool.Name
				if startVal, ok := r.scaleStartTime.Load(key); ok {
					if _, alreadyRecorded := r.scaleFirstPodRecorded.LoadOrStore(key, struct{}{}); !alreadyRecorded {
						r.Metrics.RecordFirstPodReady(pool.Name, time.Since(startVal.(time.Time)))
					}
				}
				break
			}
		}

		// Per-container start latency: creation → container Running
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Running != nil {
				r.Metrics.RecordContainerStartDuration(pool.Name, cs.Name, cs.State.Running.StartedAt.Sub(createdAt))
			}
		}
	}
}

// observeImagePullErrors increments image pull error counters for a pod.
// Each unique (pod-uid, container, reason) triple is counted only once.
func (r *WarmPoolReconciler) observeImagePullErrors(poolName string, pod corev1.Pod) {
	if r.Metrics == nil {
		return
	}
	track := func(containerName, reason string) {
		key := string(pod.UID) + "/" + containerName + "/" + reason
		if _, loaded := r.recordedErrors.LoadOrStore(key, struct{}{}); !loaded {
			r.Metrics.IncrementImagePullError(poolName, reason)
		}
	}
	for _, cs := range pod.Status.InitContainerStatuses {
		if cs.State.Waiting == nil {
			continue
		}
		switch cs.State.Waiting.Reason {
		case "ImagePullBackOff", "ErrImagePull":
			track(cs.Name, cs.State.Waiting.Reason)
		}
		if strings.Contains(cs.State.Waiting.Message, "pull QPS exceeded") {
			track(cs.Name, "PullQPSExceeded")
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting == nil {
			continue
		}
		switch cs.State.Waiting.Reason {
		case "ImagePullBackOff", "ErrImagePull":
			track(cs.Name, cs.State.Waiting.Reason)
		}
		if strings.Contains(cs.State.Waiting.Message, "pull QPS exceeded") {
			track(cs.Name, "PullQPSExceeded")
		}
	}
}

// pruneStaleEntries removes recordedPods and recordedErrors entries for pods
// that no longer exist, preventing unbounded memory growth.
func (r *WarmPoolReconciler) pruneStaleEntries(currentPods []corev1.Pod) {
	alive := make(map[string]struct{}, len(currentPods))
	for i := range currentPods {
		alive[string(currentPods[i].UID)] = struct{}{}
	}

	r.recordedPods.Range(func(key, _ any) bool {
		if _, ok := alive[string(key.(types.UID))]; !ok {
			r.recordedPods.Delete(key)
		}
		return true
	})

	r.recordedErrors.Range(func(key, _ any) bool {
		k := key.(string)
		// key format: "<uid>/<container>/<reason>" — extract uid prefix
		if idx := strings.Index(k, "/"); idx > 0 {
			uid := k[:idx]
			if _, ok := alive[uid]; !ok {
				r.recordedErrors.Delete(key)
			}
		}
		return true
	})
}

// constructPod creates a Pod from the WarmPool template
func (r *WarmPoolReconciler) constructPod(pool *arlv1alpha1.WarmPool, renderedConfig *renderedConfigEnv) *corev1.Pod {
	// Deep-copy the template spec so that injection helpers don't mutate the WarmPool CR.
	templateSpec := pool.Spec.Template.Spec.DeepCopy()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: pool.Name + "-",
			Namespace:    pool.Namespace,
			Labels: map[string]string{
				PoolLabelKey:   pool.Name,
				StatusLabelKey: StatusIdle,
			},
		},
		Spec: *templateSpec,
	}

	// Inject executor agent into pod
	r.injectExecutorAgent(pod)

	// Inject tools init containers and volumes if tools are configured
	if pool.Spec.Tools != nil {
		r.injectTools(pod, pool.Spec.Tools)
	}

	// Inject image-locality-aware node affinity
	r.injectImageLocality(pod, pool)

	// Ensure sidecar container exists
	hasSidecar := false
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "sidecar" {
			hasSidecar = true
			// Add shared volume mounts to a user-supplied sidecar.
			r.ensureSidecarVolumeMounts(&pod.Spec.Containers[i])
			break
		}
	}

	if !hasSidecar {
		// Resolve the workspace mount path from the executor container so the
		// sidecar and executor agree on the workspace location. Managed
		// sessions can override the default (e.g. /app for terminal-bench).
		sidecarWorkspace := r.Config.WorkspaceDir
		for i := range pod.Spec.Containers {
			c := &pod.Spec.Containers[i]
			if c.Name == "sidecar" {
				continue
			}
			for _, vm := range c.VolumeMounts {
				if vm.Name == "workspace" {
					sidecarWorkspace = vm.MountPath
					break
				}
			}
			break
		}

		sidecarArgs := []string{
			"--workspace=" + sidecarWorkspace,
			"--http-port=" + fmt.Sprintf("%d", r.Config.SidecarHTTPPort),
			"--grpc-port=" + fmt.Sprintf("%d", r.Config.SidecarGRPCPort),
		}

		// Mount the workspace volume at a fixed path that won't collide with
		// the sidecar binary. The sidecar only needs the --workspace argument
		// (passed to the executor agent via gRPC); it doesn't access files
		// directly from its own filesystem.
		sidecarVolumeMounts := []corev1.VolumeMount{
			{Name: "workspace", MountPath: "/var/run/arl/workspace"},
			{Name: "arl-socket", MountPath: "/var/run/arl"},
		}

		httpPort := int32(r.Config.SidecarHTTPPort)
		grpcPort := int32(r.Config.SidecarGRPCPort)

		sidecarContainer := corev1.Container{
			Name:            "sidecar",
			Image:           r.Config.SidecarImage,
			ImagePullPolicy: r.injectedPullPolicy(),
			Args:            sidecarArgs,
			Env:             otelEnvFromOperator(),
			Ports: []corev1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: httpPort,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "grpc",
					ContainerPort: grpcPort,
					Protocol:      corev1.ProtocolTCP,
				},
			},
			VolumeMounts: sidecarVolumeMounts,
			StartupProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/readyz",
						Port: intstr.FromInt32(httpPort),
					},
				},
				PeriodSeconds:    2,
				FailureThreshold: 30, // 60s for slow images
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/readyz",
						Port: intstr.FromInt32(httpPort),
					},
				},
				PeriodSeconds:    5,
				FailureThreshold: 3,
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt32(grpcPort),
					},
				},
				PeriodSeconds:    10,
				FailureThreshold: 3,
			},
		}
		pod.Spec.Containers = append(pod.Spec.Containers, sidecarContainer)
	}

	// Inject the mandatory gRPC auth token into the sidecar (whether
	// user-supplied or default) so it can never run unauthenticated.
	r.injectSidecarAuthEnv(pod)

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

	// Ensure executor containers have ephemeral-storage limits to prevent
	// a single sandbox from filling the node's disk and triggering eviction.
	r.ensureEphemeralStorageLimits(pod)

	// Seed workspace from the executor image: if the image ships files at
	// workspaceDir (e.g. terminal-bench task images COPY project files to
	// /app), the emptyDir mount hides them. An init container running the
	// same image copies the original content into the volume so the
	// executor sees the image's files on startup.
	r.injectWorkspaceSeed(pod)

	// Add shared volumes for executor agent
	r.ensureExecutorVolumes(pod)

	// Inject managed config resources and env vars.
	r.injectConfigEnv(pod, renderedConfig)

	// Inject HTTP proxy env vars into all containers.
	r.injectProxyEnv(pod)

	// Set owner reference
	ctrl.SetControllerReference(pool, pod, r.Scheme)

	return pod
}

// safeNameRe validates tool names, filenames, and entrypoints to prevent shell injection.
var safeNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// injectTools adds init containers, volumes, and volume mounts for tool provisioning.
// Tools are staged in /opt/arl/tools/ via init containers and mounted read-only on the executor.
func (r *WarmPoolReconciler) injectTools(pod *corev1.Pod, tools *arlv1alpha1.ToolsSpec) {
	const toolsMountPath = "/opt/arl/tools"
	const toolsVolumeName = "arl-tools"

	// Early return if tools spec is effectively empty
	if len(tools.Images) == 0 && len(tools.ConfigMaps) == 0 && len(tools.Inline) == 0 {
		return
	}

	// 1. Create EmptyDir volume for tools
	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name: toolsVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	toolsMount := corev1.VolumeMount{Name: toolsVolumeName, MountPath: toolsMountPath}

	// 2. For each tools image, add an init container that copies /tools/* to /opt/arl/tools/
	for i, img := range tools.Images {
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
			Name:    fmt.Sprintf("copy-tools-%d", i),
			Image:   img.Image,
			Command: []string{"sh", "-c", "cp -r /tools/* " + toolsMountPath + "/"},
			VolumeMounts: []corev1.VolumeMount{
				toolsMount,
			},
		})
	}

	// Use executor-agent image for tools init containers (it's busybox-based
	// and guaranteed to be available in the cluster's registry).
	initImage := r.Config.ExecutorAgentImage

	// 3. For each ConfigMap, add a volume + init container that copies files
	for i, cm := range tools.ConfigMaps {
		cmVolName := fmt.Sprintf("arl-tools-cm-%d", i)
		tmpMount := fmt.Sprintf("/tmp/arl-cm-%d", i)

		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: cmVolName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: cm.Name},
				},
			},
		})

		pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
			Name:    fmt.Sprintf("copy-tools-cm-%d", i),
			Image:   initImage,
			Command: []string{"sh", "-c", fmt.Sprintf("cp -r %s/* %s/", tmpMount, toolsMountPath)},
			VolumeMounts: []corev1.VolumeMount{
				{Name: cmVolName, MountPath: tmpMount},
				toolsMount,
			},
		})
	}

	// 4. For inline tools, create an init container that writes files via shell
	if len(tools.Inline) > 0 {
		script := r.buildInlineToolsScript(tools.Inline, toolsMountPath)
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
			Name:    "setup-inline-tools",
			Image:   initImage,
			Command: []string{"sh", "-c", script},
			VolumeMounts: []corev1.VolumeMount{
				toolsMount,
			},
		})
	}

	// 5. Add registry generator init container
	registryScript := r.buildRegistryScript(toolsMountPath)
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
		Name:    "generate-tools-registry",
		Image:   initImage,
		Command: []string{"sh", "-c", registryScript},
		VolumeMounts: []corev1.VolumeMount{
			toolsMount,
		},
	})

	// 6. Mount tools volume read-only on the executor container
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "sidecar" {
			continue
		}
		pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts,
			corev1.VolumeMount{Name: toolsVolumeName, MountPath: toolsMountPath, ReadOnly: true},
		)
		break
	}
}

// buildInlineToolsScript generates a shell script that creates directories and
// writes files for each inline tool, including auto-generated manifest.json.
// Names and filenames are validated against safeNameRe to prevent shell injection.
func (r *WarmPoolReconciler) buildInlineToolsScript(tools []arlv1alpha1.InlineTool, basePath string) string {
	var sb strings.Builder
	sb.WriteString("set -e\n")

	for _, tool := range tools {
		// Validate name to prevent shell injection (also enforced by CRD validation)
		if !safeNameRe.MatchString(tool.Name) {
			sb.WriteString(fmt.Sprintf("echo 'ERROR: invalid tool name: %s' >&2 && exit 1\n", tool.Name))
			continue
		}

		toolDir := basePath + "/" + tool.Name
		sb.WriteString(fmt.Sprintf("mkdir -p %s\n", toolDir))

		// Auto-generate manifest.json
		manifest := map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"runtime":     tool.Runtime,
			"entrypoint":  tool.Entrypoint,
		}
		if tool.Parameters != nil && tool.Parameters.Raw != nil {
			manifest["parameters"] = json.RawMessage(tool.Parameters.Raw)
		} else {
			manifest["parameters"] = map[string]interface{}{}
		}
		if tool.Timeout != "" {
			manifest["timeout"] = tool.Timeout
		}

		manifestJSON, err := json.Marshal(manifest)
		if err != nil {
			sb.WriteString(fmt.Sprintf("echo 'ERROR: failed to marshal manifest for tool %s' >&2 && exit 1\n", tool.Name))
			continue
		}
		sb.WriteString(fmt.Sprintf("cat > %s/manifest.json << 'MANIFEST_EOF'\n%s\nMANIFEST_EOF\n", toolDir, string(manifestJSON)))

		// Write each file
		for filename, content := range tool.Files {
			// Validate filename to prevent path traversal and shell injection
			if !safeNameRe.MatchString(filename) {
				sb.WriteString(fmt.Sprintf("echo 'ERROR: invalid filename: %s in tool %s' >&2 && exit 1\n", filename, tool.Name))
				continue
			}

			// Use a unique heredoc delimiter and ensure content doesn't contain it
			delimiter := "TOOL_FILE_EOF_" + tool.Name + "_" + filename
			for strings.Contains(content, delimiter) {
				delimiter += "_X"
			}
			sb.WriteString(fmt.Sprintf("cat > %s/%s << '%s'\n%s\n%s\n", toolDir, filename, delimiter, content, delimiter))

			// Make entrypoint executable
			if filename == tool.Entrypoint {
				sb.WriteString(fmt.Sprintf("chmod +x %s/%s\n", toolDir, filename))
			}
		}
	}

	return sb.String()
}

// buildRegistryScript generates a shell script that scans /opt/arl/tools/*/manifest.json
// and aggregates them into /opt/arl/tools/registry.json.
func (r *WarmPoolReconciler) buildRegistryScript(basePath string) string {
	// This script uses only POSIX shell + basic tools available in busybox
	return fmt.Sprintf(`set -e
REGISTRY="%s/registry.json"
printf '{"tools":[' > "$REGISTRY"
first=true
for manifest in %s/*/manifest.json; do
  [ -f "$manifest" ] || continue
  if [ "$first" = true ]; then
    first=false
  else
    printf ',' >> "$REGISTRY"
  fi
  cat "$manifest" >> "$REGISTRY"
done
printf ']}' >> "$REGISTRY"
`, basePath, basePath)
}

// injectedPullPolicy returns the ImagePullPolicy for operator-injected
// containers (sidecar + executor-agent init), honoring Config.ImagePullPolicy.
// Defaults to Always when unset/invalid so production behavior is unchanged;
// set IMAGE_PULL_POLICY=IfNotPresent on local kind/minikube clusters where
// images are side-loaded rather than pushed to a registry.
// injectWorkspaceSeed adds an init container that copies the executor image's
// original files at the workspace mount path into the emptyDir workspace volume.
// Without this, the emptyDir mount hides any files the image ships at that path
// (e.g. terminal-bench task images COPY project skeletons to /app).
func (r *WarmPoolReconciler) injectWorkspaceSeed(pod *corev1.Pod) {
	// Find the executor container and its workspace mount.
	var executorImage string
	var workspaceMount string
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		if c.Name == "sidecar" {
			continue
		}
		executorImage = c.Image
		for _, vm := range c.VolumeMounts {
			if vm.Name == "workspace" {
				workspaceMount = vm.MountPath
				break
			}
		}
		break
	}
	if executorImage == "" || workspaceMount == "" {
		return
	}

	// The init container runs the executor image with the workspace volume
	// mounted at a staging path, then copies whatever the image had at the
	// original workspaceMount into the volume. `cp -a` preserves permissions;
	// the trailing dot (src/.) copies directory contents, not the directory.
	stagingPath := "/tmp/arl-workspace-seed"
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
		Name:            "seed-workspace",
		Image:           executorImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{"sh", "-c", fmt.Sprintf(
			"if [ -d %q ] && [ \"$(ls -A %s 2>/dev/null)\" ]; then cp -a %s/. %s/; fi",
			workspaceMount, workspaceMount, workspaceMount, stagingPath,
		)},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "workspace", MountPath: stagingPath},
		},
	})
}

func (r *WarmPoolReconciler) injectedPullPolicy() corev1.PullPolicy {
	switch corev1.PullPolicy(r.Config.ImagePullPolicy) {
	case corev1.PullIfNotPresent:
		return corev1.PullIfNotPresent
	case corev1.PullNever:
		return corev1.PullNever
	default:
		return corev1.PullAlways
	}
}

// injectExecutorAgent adds the init container and modifies the executor container
// to run the executor agent alongside the user process.
func (r *WarmPoolReconciler) injectExecutorAgent(pod *corev1.Pod) {
	// Add init container to copy executor agent binary
	initContainer := corev1.Container{
		Name:            "copy-executor-agent",
		Image:           r.Config.ExecutorAgentImage,
		ImagePullPolicy: r.injectedPullPolicy(),
		Command:         []string{"cp", "/executor-agent", "/arl-bin/executor-agent"},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "arl-bin", MountPath: "/arl-bin"},
		},
	}
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)

	// Modify the first non-sidecar container (executor) to run agent in background
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "sidecar" {
			continue
		}

		c := &pod.Spec.Containers[i]

		// Add volume mounts for agent binary and socket
		c.VolumeMounts = append(c.VolumeMounts,
			corev1.VolumeMount{Name: "arl-bin", MountPath: "/arl-bin"},
			corev1.VolumeMount{Name: "arl-socket", MountPath: "/var/run/arl"},
		)

		// Resolve the actual workspace path from the volume mount (which may
		// differ from the global Config.WorkspaceDir when a managed session
		// overrides it, e.g. workspace_dir=/app for terminal-bench images).
		workspaceArg := r.Config.WorkspaceDir
		for _, vm := range c.VolumeMounts {
			if vm.Name == "workspace" {
				workspaceArg = vm.MountPath
				break
			}
		}
		agentExec := "exec /arl-bin/executor-agent --socket=/var/run/arl/exec.sock --workspace=" + workspaceArg
		if len(c.Command) > 0 {
			originalCmd := ""
			if len(c.Command) >= 3 && (c.Command[0] == "/bin/sh" || c.Command[0] == "sh") && c.Command[1] == "-c" {
				// Already a shell command; combine the shell body with any Args
				parts := append(c.Command[2:], c.Args...)
				originalCmd = strings.Join(parts, " ")
			} else {
				// Build original command from Command + Args combined
				full := append(c.Command, c.Args...)
				originalCmd = joinCmd(full)
			}
			c.Command = []string{"/bin/sh", "-c", originalCmd + " & " + agentExec}
		} else {
			c.Command = []string{"/bin/sh", "-c", agentExec}
		}
		c.Args = nil // Clear args since we've embedded them in command

		break // Only modify the first executor container
	}
}

// sidecarAuthTokenEnv returns the env var that injects the shared gRPC auth
// token from the arl-grpc-token secret. The secret reference is mandatory
// (non-optional): if the token is missing the pod fails to start rather than
// silently running an unauthenticated sidecar.
func sidecarAuthTokenEnv() corev1.EnvVar {
	return corev1.EnvVar{
		Name: "GRPC_AUTH_TOKEN",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "arl-grpc-token"},
				Key:                  "token",
				Optional:             boolPtr(false),
			},
		},
	}
}

// injectSidecarAuthEnv injects the mandatory gRPC auth token into the sidecar
// container, regardless of whether it was user-supplied or operator-injected.
// No-op when the operator has no token configured.
func (r *WarmPoolReconciler) injectSidecarAuthEnv(pod *corev1.Pod) {
	if r.Config.GRPCAuthToken == "" {
		return
	}
	appendEnvVar(pod, "sidecar", sidecarAuthTokenEnv())
}

// injectProxyEnv sets HTTP_PROXY/HTTPS_PROXY/NO_PROXY on executor and init
// containers when PodHTTPProxy is configured. The sidecar is excluded because
// it communicates in-cluster (gRPC to gateway, OTEL export) and routing that
// traffic through an external proxy would break connectivity.
func (r *WarmPoolReconciler) injectProxyEnv(pod *corev1.Pod) {
	if r.Config.PodHTTPProxy == "" {
		return
	}
	noProxy := r.Config.PodNoProxy
	if noProxy == "" {
		noProxy = "localhost,127.0.0.1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,.svc,.svc.cluster.local"
	}
	envVars := []corev1.EnvVar{
		{Name: "HTTP_PROXY", Value: r.Config.PodHTTPProxy},
		{Name: "HTTPS_PROXY", Value: r.Config.PodHTTPProxy},
		{Name: "http_proxy", Value: r.Config.PodHTTPProxy},
		{Name: "https_proxy", Value: r.Config.PodHTTPProxy},
		{Name: "NO_PROXY", Value: noProxy},
		{Name: "no_proxy", Value: noProxy},
	}
	for i := range pod.Spec.InitContainers {
		for _, ev := range envVars {
			upsertEnv(&pod.Spec.InitContainers[i].Env, ev)
		}
	}
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "sidecar" {
			continue
		}
		for _, ev := range envVars {
			appendEnvVar(pod, pod.Spec.Containers[i].Name, ev)
		}
	}
}

func upsertEnv(envs *[]corev1.EnvVar, ev corev1.EnvVar) {
	for i := range *envs {
		if (*envs)[i].Name == ev.Name {
			(*envs)[i] = ev
			return
		}
	}
	*envs = append(*envs, ev)
}

// ensureSidecarVolumeMounts adds executor-related volume mounts to an existing sidecar
func (r *WarmPoolReconciler) ensureSidecarVolumeMounts(c *corev1.Container) {
	hasSocket := false
	for _, vm := range c.VolumeMounts {
		if vm.Name == "arl-socket" {
			hasSocket = true
			break
		}
	}
	if !hasSocket {
		c.VolumeMounts = append(c.VolumeMounts,
			corev1.VolumeMount{Name: "arl-socket", MountPath: "/var/run/arl"},
		)
	}
}

// ensureExecutorVolumes adds shared volumes for executor agent communication
func (r *WarmPoolReconciler) ensureExecutorVolumes(pod *corev1.Pod) {
	volumeNames := map[string]bool{}
	for _, v := range pod.Spec.Volumes {
		volumeNames[v.Name] = true
	}

	if !volumeNames["arl-bin"] {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: "arl-bin",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	if !volumeNames["arl-socket"] {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: "arl-socket",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}
}

// ensureEphemeralStorageLimits sets a default ephemeral-storage limit on executor
// containers that don't already have one, preventing a single sandbox from filling
// the node's disk and triggering node-level eviction.
func (r *WarmPoolReconciler) ensureEphemeralStorageLimits(pod *corev1.Pod) {
	defaultLimit := resource.MustParse("10Gi")
	defaultRequest := resource.MustParse("100Mi")

	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "sidecar" {
			continue
		}
		c := &pod.Spec.Containers[i]
		if c.Resources.Limits == nil {
			c.Resources.Limits = corev1.ResourceList{}
		}
		if _, ok := c.Resources.Limits[corev1.ResourceEphemeralStorage]; !ok {
			c.Resources.Limits[corev1.ResourceEphemeralStorage] = defaultLimit
		}
		if c.Resources.Requests == nil {
			c.Resources.Requests = corev1.ResourceList{}
		}
		if _, ok := c.Resources.Requests[corev1.ResourceEphemeralStorage]; !ok {
			c.Resources.Requests[corev1.ResourceEphemeralStorage] = defaultRequest
		}
	}
}

// shellQuote wraps a string in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// joinCmd joins command parts into a shell-safe string with proper quoting.
func joinCmd(parts []string) string {
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	return strings.Join(quoted, " ")
}

// injectImageLocality appends a PreferredSchedulingTerm to the pod's affinity
// so that pods prefer nodes selected by Rendezvous hashing on the primary image.
func (r *WarmPoolReconciler) injectImageLocality(pod *corev1.Pod, pool *arlv1alpha1.WarmPool) {
	if r.ImageScheduler == nil {
		return
	}

	// Check if explicitly disabled
	spec := pool.Spec.ImageLocality
	if spec != nil && spec.Enabled != nil && !*spec.Enabled {
		return
	}

	// Extract primary image (first non-sidecar container)
	var image string
	for _, c := range pod.Spec.Containers {
		if c.Name != "sidecar" {
			image = c.Image
			break
		}
	}
	if image == "" {
		return
	}

	// Priority: CRD spec > env config > hardcoded default.
	// See Config.ImageLocalitySpreadFactor / ImageLocalityWeight for docs.
	spreadFactor := r.Config.ImageLocalitySpreadFactor
	weight := r.Config.ImageLocalityWeight
	if spec != nil {
		if spec.SpreadFactor != nil {
			spreadFactor = *spec.SpreadFactor
		}
		if spec.Weight != nil {
			weight = *spec.Weight
		}
	}

	k := int(math.Ceil(float64(pool.Spec.Replicas) * spreadFactor))
	if k < 1 {
		k = 1
	}

	preferredNodes := r.ImageScheduler.SelectNodes(image, k)
	if len(preferredNodes) == 0 {
		return
	}

	term := corev1.PreferredSchedulingTerm{
		Weight: weight,
		Preference: corev1.NodeSelectorTerm{
			MatchExpressions: []corev1.NodeSelectorRequirement{
				{
					Key:      "kubernetes.io/hostname",
					Operator: corev1.NodeSelectorOpIn,
					Values:   preferredNodes,
				},
			},
		},
	}

	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &corev1.Affinity{}
	}
	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	}
	pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(
		pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
		term,
	)
}

// SetupWithManager sets up the controller with the Manager
func (r *WarmPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	baseDelay := time.Duration(r.Config.WarmPoolBaseDelayMs) * time.Millisecond
	maxDelay := time.Duration(r.Config.WarmPoolMaxDelayMs) * time.Millisecond
	rl := workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](baseDelay, maxDelay),
		&workqueue.TypedBucketRateLimiter[reconcile.Request]{
			Limiter: rate.NewLimiter(rate.Limit(r.Config.WarmPoolRateLimitQPS), r.Config.WarmPoolRateLimitBurst),
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&arlv1alpha1.WarmPool{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.Config.WarmPoolMaxConcurrent,
			RateLimiter:             rl,
		}).
		Complete(r)
}

// Name returns the controller name for logging
func (r *WarmPoolReconciler) Name() string {
	return "WarmPool"
}

// podIdleAge returns the time a pod has been idle. If the pod has a
// last-released annotation (set when transitioning from allocated → idle),
// that timestamp is returned. Otherwise the pod's CreationTimestamp is used.
func podIdleAge(pod *corev1.Pod) time.Time {
	if ann, ok := pod.Annotations[labels.LastReleasedAnnotation]; ok {
		if t, err := time.Parse(time.RFC3339, ann); err == nil {
			return t
		}
	}
	return pod.CreationTimestamp.Time
}

// findCondition finds a condition by type.
func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// setCondition sets or updates a condition.
func boolPtr(b bool) *bool { return &b }

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
