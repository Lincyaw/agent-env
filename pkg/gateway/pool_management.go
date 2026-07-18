package gateway

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	sandboxcontrollers "sigs.k8s.io/agent-sandbox/controllers"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/labels"
	"github.com/Lincyaw/agent-env/pkg/scheduling"
)

const (
	defaultSandboxRequestCPU    = "500m"
	defaultSandboxRequestMemory = "512Mi"
	defaultSandboxLimitCPU      = "8"
	defaultSandboxLimitMemory   = "32Gi"
)

// CreatePool creates an agent-sandbox SandboxTemplate and SandboxWarmPool.
func (g *Gateway) CreatePool(ctx context.Context, req CreatePoolRequest) error {
	ns, err := g.resolveNamespace(req.Namespace)
	if err != nil {
		return err
	}

	replicas := req.Replicas
	if replicas < 0 {
		replicas = 0
	}

	resources := req.Resources
	if resources == nil {
		defaultResources, err := g.defaultSandboxResources()
		if err != nil {
			return err
		}
		resources = &defaultResources
	}

	if hasJSONPayload(req.ConfigEnv) {
		return fmt.Errorf("pool configEnv is not supported by SandboxWarmPool-backed pools; pass configEnv when creating a session")
	}
	if hasJSONPayload(req.Tools) {
		return fmt.Errorf("tools are not supported by SandboxWarmPool-backed pools yet")
	}
	if err := validatePrivateContainers(req.PrivateContainers); err != nil {
		return err
	}

	templateName := sandboxTemplateName(req.Name)
	existingPool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: ns}, existingPool); err == nil {
		return fmt.Errorf("create sandbox warm pool: %w", errors.NewAlreadyExists(extensionsv1beta1.Resource("sandboxwarmpools"), req.Name))
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("get sandbox warm pool before create: %w", err)
	}
	if err := g.ensureSandboxRuntimeSecret(ctx, ns); err != nil {
		return err
	}

	templateMeta := metav1.ObjectMeta{
		Name:      templateName,
		Namespace: ns,
	}
	poolMeta := metav1.ObjectMeta{
		Name:      req.Name,
		Namespace: ns,
	}
	podAnnotations := map[string]string{}
	if profile := strings.TrimSpace(req.Profile); profile != "" {
		applyPoolProfileMetadata(&templateMeta, profile)
		applyPoolProfileMetadata(&poolMeta, profile)
	}
	if req.Managed {
		applyManagedPoolMetadata(&templateMeta, true)
		applyManagedPoolMetadata(&poolMeta, true)
	}
	if replicas > 0 {
		applyPoolStateMetadata(&poolMeta, labels.PoolStateRunning)
	} else {
		applyPoolStateMetadata(&poolMeta, labels.PoolStateStopped)
		applyPoolLastUsedMetadata(&poolMeta, time.Now())
		ensureObjectAnnotations(&poolMeta)[scheduling.PoolAutoscaleAnnotation] = "false"
	}
	imageLocalityEnabled := g.gwConfig.ImageLocalityEnabled || hasJSONPayload(req.ImageLocality)
	if imageLocalityEnabled {
		ensureObjectAnnotations(&templateMeta)[scheduling.ImageLocalityAnnotation] = scheduling.ImageLocalityEnabledValue
		ensureObjectAnnotations(&poolMeta)[scheduling.ImageLocalityAnnotation] = scheduling.ImageLocalityEnabledValue
		podAnnotations[scheduling.ImageLocalityAnnotation] = scheduling.ImageLocalityEnabledValue
	}
	if req.Image != "" && (imageLocalityEnabled || strings.TrimSpace(g.gwConfig.SchedulerName) != "") {
		ensureObjectAnnotations(&templateMeta)[scheduling.ExecutorImageAnnotation] = req.Image
		podAnnotations[scheduling.ExecutorImageAnnotation] = req.Image
	}
	podMetadata := sandboxv1beta1.PodMetadata{}
	if len(podAnnotations) > 0 {
		podMetadata.Annotations = podAnnotations
	}
	template := &extensionsv1beta1.SandboxTemplate{
		ObjectMeta: templateMeta,
		Spec: extensionsv1beta1.SandboxTemplateSpec{
			NetworkPolicyManagement:    g.sandboxNetworkPolicyManagement(),
			EnvVarsInjectionPolicy:     extensionsv1beta1.EnvVarsInjectionPolicyOverrides,
			VolumeClaimTemplatesPolicy: extensionsv1beta1.VolumeClaimTemplatesPolicyAllowed,
			Service:                    boolPtr(false),
			PodTemplate: sandboxv1beta1.PodTemplate{
				ObjectMeta: podMetadata,
				Spec:       g.sandboxPodSpec(req.Image, *resources, req.PrivateContainers),
			},
		},
	}
	if req.AllowInternet != nil && !*req.AllowInternet {
		template.Spec.NetworkPolicyManagement = extensionsv1beta1.NetworkPolicyManagementManaged
		template.Spec.NetworkPolicy = denyInternetEgressPolicy(g.egressAllowCIDRs())
	}
	createdTemplate := false
	if err := g.k8sClient.Create(ctx, template); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("create sandbox template: %w", err)
		}
	} else {
		createdTemplate = true
	}

	pool := &extensionsv1beta1.SandboxWarmPool{
		ObjectMeta: poolMeta,
		Spec: extensionsv1beta1.SandboxWarmPoolSpec{
			Replicas:    int32Ptr(replicas),
			TemplateRef: extensionsv1beta1.SandboxTemplateRef{Name: templateName},
			UpdateStrategy: &extensionsv1beta1.SandboxWarmPoolUpdateStrategy{
				Type: extensionsv1beta1.RecreateSandboxWarmPoolUpdateStrategyType,
			},
		},
	}
	if err := g.k8sClient.Create(ctx, pool); err != nil {
		if createdTemplate {
			if cleanupErr := g.k8sClient.Delete(ctx, template); cleanupErr != nil && !errors.IsNotFound(cleanupErr) {
				log.Printf("Warning: failed to cleanup sandbox template %s/%s after pool create failure: %v", ns, templateName, cleanupErr)
			}
		}
		return fmt.Errorf("create sandbox warm pool: %w", err)
	}
	if g.poolIndex != nil {
		g.poolIndex.upsertTemplate(template)
		g.poolIndex.upsertPool(pool)
	}

	if replicas == 0 && req.Image != "" {
		go g.runImagePrefetch(req.Name, ns, req.Image)
	}

	return nil
}

func (g *Gateway) ensureClaimEnvInjectionPolicy(ctx context.Context, poolName, namespace string) error {
	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolName, Namespace: namespace}, pool); err != nil {
		return fmt.Errorf("get sandbox warm pool %s/%s for configEnv: %w", namespace, poolName, err)
	}
	templateName := pool.Spec.TemplateRef.Name
	if templateName == "" {
		return fmt.Errorf("sandbox warm pool %s/%s has no templateRef for configEnv", namespace, poolName)
	}
	template := &extensionsv1beta1.SandboxTemplate{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: templateName, Namespace: namespace}, template); err != nil {
		return fmt.Errorf("get sandbox template %s/%s for configEnv: %w", namespace, templateName, err)
	}
	if template.Spec.EnvVarsInjectionPolicy == extensionsv1beta1.EnvVarsInjectionPolicyOverrides {
		return nil
	}
	patch := client.MergeFrom(template.DeepCopy())
	template.Spec.EnvVarsInjectionPolicy = extensionsv1beta1.EnvVarsInjectionPolicyOverrides
	if err := g.k8sClient.Patch(ctx, template, patch); err != nil {
		return fmt.Errorf("patch sandbox template %s/%s env injection policy: %w", namespace, templateName, err)
	}
	return nil
}

func (g *Gateway) defaultSandboxResources() (corev1.ResourceRequirements, error) {
	requestCPU, err := parseDefaultSandboxQuantity("sandbox default request cpu", g.gwConfig.DefaultSandboxRequestCPU, defaultSandboxRequestCPU)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	requestMemory, err := parseDefaultSandboxQuantity("sandbox default request memory", g.gwConfig.DefaultSandboxRequestMemory, defaultSandboxRequestMemory)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	limitCPU, err := parseDefaultSandboxQuantity("sandbox default limit cpu", g.gwConfig.DefaultSandboxLimitCPU, defaultSandboxLimitCPU)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	limitMemory, err := parseDefaultSandboxQuantity("sandbox default limit memory", g.gwConfig.DefaultSandboxLimitMemory, defaultSandboxLimitMemory)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    requestCPU,
			corev1.ResourceMemory: requestMemory,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    limitCPU,
			corev1.ResourceMemory: limitMemory,
		},
	}, nil
}

func parseDefaultSandboxQuantity(name, configured, fallback string) (resource.Quantity, error) {
	value := strings.TrimSpace(configured)
	if value == "" {
		value = fallback
	}
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("%s must be a valid Kubernetes quantity: %q", name, configured)
	}
	if q.Sign() <= 0 {
		return resource.Quantity{}, fmt.Errorf("%s must be positive: %q", name, value)
	}
	return q, nil
}

// GetPool returns SandboxWarmPool info.
func (g *Gateway) GetPool(ctx context.Context, name, namespace string) (*PoolInfo, error) {
	namespace, err := g.resolveNamespace(namespace)
	if err != nil {
		return nil, err
	}

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pool); err != nil {
		return nil, err
	}

	info := g.poolInfoFromSandboxWarmPool(ctx, pool)
	return &info, nil
}

// ScalePool updates the replica count of a SandboxWarmPool.
func (g *Gateway) ScalePool(ctx context.Context, name string, req ScalePoolRequest) (*PoolInfo, error) {
	ns, err := g.resolveNamespace(req.Namespace)
	if err != nil {
		return nil, err
	}

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, pool); err != nil {
		return nil, fmt.Errorf("get pool: %w", err)
	}

	if req.Resources != nil {
		return nil, fmt.Errorf("updating resources requires updating the SandboxTemplate and is not supported by ScalePool")
	}
	pool.Spec.Replicas = int32Ptr(req.Replicas)
	if req.Replicas > 0 {
		applyPoolStateMetadata(&pool.ObjectMeta, labels.PoolStateRunning)
		delete(pool.Annotations, scheduling.PoolAutoscaleAnnotation)
	} else {
		applyPoolStateMetadata(&pool.ObjectMeta, labels.PoolStateStopped)
		applyPoolLastUsedMetadata(&pool.ObjectMeta, time.Now())
		ensureObjectAnnotations(&pool.ObjectMeta)[scheduling.PoolAutoscaleAnnotation] = "false"
	}

	if err := g.k8sClient.Update(ctx, pool); err != nil {
		return nil, fmt.Errorf("update pool: %w", err)
	}
	if g.poolIndex != nil {
		g.poolIndex.upsertPool(pool)
	}

	return g.GetPool(ctx, name, ns)
}

// DeletePool drains a pool without deleting its SandboxWarmPool or template.
func (g *Gateway) DeletePool(ctx context.Context, name, namespace string) error {
	namespace, err := g.resolveNamespace(namespace)
	if err != nil {
		return err
	}

	if err := g.markPoolDraining(ctx, name, namespace); err != nil {
		return err
	}
	if err := g.deleteSessionsForPool(ctx, name, namespace); err != nil {
		return err
	}
	if err := g.deleteClaimsForPool(ctx, name, namespace); err != nil {
		return err
	}
	return g.markPoolStopped(ctx, name, namespace)
}

// DestroyPool drains a pool, then deletes the SandboxWarmPool and the template.
func (g *Gateway) DestroyPool(ctx context.Context, name, namespace string) error {
	namespace, err := g.resolveNamespace(namespace)
	if err != nil {
		return err
	}

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pool); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get pool for destroy: %w", err)
	}
	templateName := pool.Spec.TemplateRef.Name

	if err := g.DeletePool(ctx, name, namespace); err != nil {
		return err
	}

	if err := g.k8sClient.Delete(ctx, pool); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete sandbox warm pool %s/%s: %w", namespace, name, err)
	}
	if g.poolIndex != nil {
		g.poolIndex.deletePool(pool)
	}
	if templateName == "" {
		return nil
	}
	if err := g.deletePoolTemplateIfOwned(ctx, templateName, name, namespace); err != nil {
		return err
	}
	return nil
}

func (g *Gateway) deleteClaimsForPool(ctx context.Context, poolName, namespace string) error {
	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list sandbox claims for pool delete: %w", err)
	}
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.Spec.WarmPoolRef.Name != poolName {
			continue
		}
		if err := g.k8sClient.Delete(ctx, claim, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete sandbox claim %s/%s for pool %s: %w", namespace, claim.Name, poolName, err)
		}
	}
	return nil
}

func (g *Gateway) markPoolDraining(ctx context.Context, name, namespace string) error {
	return g.patchPoolLifecycle(ctx, name, namespace, 0, labels.PoolStateDraining, true)
}

func (g *Gateway) markPoolStopped(ctx context.Context, name, namespace string) error {
	return g.patchPoolLifecycle(ctx, name, namespace, 0, labels.PoolStateStopped, true)
}

func (g *Gateway) patchPoolLifecycle(ctx context.Context, name, namespace string, replicas int32, state string, disableAutoscale bool) error {
	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pool); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get pool %s/%s: %w", namespace, name, err)
	}
	before := pool.DeepCopy()
	pool.Spec.Replicas = int32Ptr(replicas)
	applyPoolStateMetadata(&pool.ObjectMeta, state)
	if state == labels.PoolStateStopped {
		applyPoolLastUsedMetadata(&pool.ObjectMeta, time.Now())
	}
	if disableAutoscale {
		ensureObjectAnnotations(&pool.ObjectMeta)[scheduling.PoolAutoscaleAnnotation] = "false"
	}
	if err := g.k8sClient.Patch(ctx, pool, client.MergeFrom(before)); err != nil {
		return fmt.Errorf("patch pool %s/%s lifecycle: %w", namespace, name, err)
	}
	if g.poolIndex != nil {
		g.poolIndex.upsertPool(pool)
	}
	return nil
}

func (g *Gateway) deletePoolTemplateIfOwned(ctx context.Context, templateName, poolName, namespace string) error {
	template := &extensionsv1beta1.SandboxTemplate{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: templateName, Namespace: namespace}, template); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get sandbox template %s/%s for pool destroy: %w", namespace, templateName, err)
	}
	managed := strings.EqualFold(strings.TrimSpace(template.Annotations[labels.ManagedPoolAnnotation]), "true")
	defaultName := templateName == sandboxTemplateName(poolName)
	if !managed && !defaultName {
		return nil
	}
	if err := g.k8sClient.Delete(ctx, template); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete sandbox template %s/%s: %w", namespace, templateName, err)
	}
	if g.poolIndex != nil {
		g.poolIndex.deleteTemplate(template)
	}
	return nil
}

func (g *Gateway) deleteSessionsForPool(ctx context.Context, poolName, namespace string) error {
	if g.store == nil {
		return nil
	}

	var sessionIDs []string
	g.store.Range(func(sessionID string, s *session) bool {
		s.mu.RLock()
		closed := s.closed
		allocation := s.runtimeAllocation()
		s.mu.RUnlock()

		if closed || allocation.PoolRef != poolName {
			return true
		}
		if allocation.Namespace != "" && allocation.Namespace != namespace {
			return true
		}
		sessionIDs = append(sessionIDs, sessionID)
		return true
	})

	for _, sessionID := range sessionIDs {
		if err := g.deleteSession(ctx, sessionID, "pool_deleted"); err != nil {
			if isSessionNotFoundError(err, sessionID) {
				continue
			}
			return fmt.Errorf("delete session %s for pool %s: %w", sessionID, poolName, err)
		}
	}
	return nil
}

func isSessionNotFoundError(err error, sessionID string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "session "+sessionID+" not found")
}

// checkPoolHealth returns an error if the SandboxWarmPool cannot be found.
func (g *Gateway) checkPoolHealth(ctx context.Context, poolRef, namespace string) error {
	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolRef, Namespace: namespace}, pool); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("pool %q not found in namespace %q", poolRef, namespace)
		}
		return fmt.Errorf("get pool: %w", err)
	}
	return nil
}

// diagnosePoolHealth returns a diagnostic string about pool health.
func (g *Gateway) diagnosePoolHealth(ctx context.Context, poolRef, namespace string) string {
	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolRef, Namespace: namespace}, pool); err != nil {
		return fmt.Sprintf("unable to check pool health: %v", err)
	}

	summary := fmt.Sprintf("pool=%s desired=%d replicas=%d ready=%d template=%s",
		poolRef, desiredSandboxWarmPoolReplicas(pool), pool.Status.Replicas, pool.Status.ReadyReplicas, pool.Spec.TemplateRef.Name)
	if issues := g.diagnosePoolPodIssues(ctx, poolRef, namespace); issues != "" {
		summary += "; " + issues
	}
	return summary
}

const (
	maxDiagnosedPods         = 3
	maxDiagnosticMessageSize = 240
)

// diagnosePoolPodIssues reports non-ready containers in the pool's warm
// sandbox pods so allocation failures surface the underlying cause (image
// pull errors, crash loops) instead of only replica counts.
func (g *Gateway) diagnosePoolPodIssues(ctx context.Context, poolRef, namespace string) string {
	return diagnoseWarmPoolPodIssues(ctx, g.k8sClient, poolRef, namespace)
}

func diagnoseWarmPoolPodIssues(ctx context.Context, c client.Client, poolRef, namespace string) string {
	var pods corev1.PodList
	if err := c.List(ctx, &pods,
		client.InNamespace(namespace),
		client.MatchingLabels{sandboxv1beta1.SandboxWarmPoolLabel: sandboxcontrollers.NameHash(poolRef)},
	); err != nil {
		return ""
	}

	issues := make([]string, 0, maxDiagnosedPods)
	for i := range pods.Items {
		if len(issues) == maxDiagnosedPods {
			issues = append(issues, "...")
			break
		}
		pod := &pods.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		if containers := describePodContainerIssues(pod); containers != "" {
			issues = append(issues, fmt.Sprintf("pod %s (%s): %s", pod.Name, pod.Status.Phase, containers))
		}
	}
	return strings.Join(issues, "; ")
}

// keepPoolWarmingAfterFailure decides whether a failed session create should
// leave its pool warming. Wait-style failures keep the pool so the client's
// retry lands on the accumulated progress (e.g. an in-flight image pull) —
// unless the pool's pods are provably stuck, in which case the returned
// reason explains why the pool is being torn down.
func (g *Gateway) keepPoolWarmingAfterFailure(err error, poolRef, namespace string) (bool, string) {
	if !provisioningWaitFailure(err) {
		return false, ""
	}
	if poolRef == "" || g.k8sClient == nil {
		return true, ""
	}
	// The request context is typically already expired here.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if reason := poolProvisioningDoomed(ctx, g.k8sClient, poolRef, namespace); reason != "" {
		return false, reason
	}
	return true, ""
}

// poolProvisioningDoomed reports a non-empty reason when the pool's warm pods
// are stuck in a state that cannot resolve on its own (missing image, crash
// loop). Transient pull backoffs are deliberately not treated as doomed.
func poolProvisioningDoomed(ctx context.Context, c client.Client, poolRef, namespace string) string {
	var pods corev1.PodList
	if err := c.List(ctx, &pods,
		client.InNamespace(namespace),
		client.MatchingLabels{sandboxv1beta1.SandboxWarmPoolLabel: sandboxcontrollers.NameHash(poolRef)},
	); err != nil {
		return ""
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		statuses := make([]corev1.ContainerStatus, 0, len(pod.Status.InitContainerStatuses)+len(pod.Status.ContainerStatuses))
		statuses = append(statuses, pod.Status.InitContainerStatuses...)
		statuses = append(statuses, pod.Status.ContainerStatuses...)
		for _, cs := range statuses {
			waiting := cs.State.Waiting
			if waiting == nil {
				continue
			}
			switch waiting.Reason {
			case "CrashLoopBackOff":
				return fmt.Sprintf("pod %s container %s in CrashLoopBackOff", pod.Name, cs.Name)
			case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
				msg := strings.ToLower(waiting.Message)
				if strings.Contains(msg, "not found") || strings.Contains(msg, "manifest unknown") || waiting.Reason == "InvalidImageName" {
					return fmt.Sprintf("pod %s container %s: image cannot be pulled (%s)", pod.Name, cs.Name, waiting.Reason)
				}
			}
		}
	}
	return ""
}

func describePodContainerIssues(pod *corev1.Pod) string {
	var parts []string
	describe := func(cs corev1.ContainerStatus) {
		switch {
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
			detail := cs.Name + " " + cs.State.Waiting.Reason
			if msg := cs.State.Waiting.Message; msg != "" {
				if len(msg) > maxDiagnosticMessageSize {
					msg = msg[:maxDiagnosticMessageSize] + "..."
				}
				detail += ": " + msg
			}
			parts = append(parts, detail)
		case cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0:
			parts = append(parts, fmt.Sprintf("%s exited %d (%s)", cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason))
		}
	}
	for _, cs := range pod.Status.InitContainerStatuses {
		describe(cs)
	}
	for _, cs := range pod.Status.ContainerStatuses {
		describe(cs)
	}
	return strings.Join(parts, ", ")
}

// ListPools returns active SandboxWarmPool CRDs in the gateway namespace.
func (g *Gateway) ListPools(ctx context.Context, namespace string) ([]PoolInfo, error) {
	return g.ListPoolsWithOptions(ctx, PoolListOptions{Namespace: namespace})
}

// ListPoolsWithOptions returns SandboxWarmPool CRDs matching the requested
// shape. By default it omits stopped, unallocated pools so common list/status
// paths do not have to load every historical template.
func (g *Gateway) ListPoolsWithOptions(ctx context.Context, opts PoolListOptions) ([]PoolInfo, error) {
	namespace, err := g.resolveNamespace(opts.Namespace)
	if err != nil {
		return nil, err
	}
	opts.Namespace = namespace
	if readModel, ok := g.syncedPoolReadModel(); ok {
		return readModel.ListPools(opts), nil
	}

	var poolList extensionsv1beta1.SandboxWarmPoolList
	if err := g.k8sClient.List(ctx, &poolList, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	claimCountsByPool := make(map[string]int32)
	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(namespace)); err == nil {
		for i := range claims.Items {
			claim := &claims.Items[i]
			if claim.DeletionTimestamp == nil && claim.Spec.WarmPoolRef.Name != "" {
				claimCountsByPool[claim.Spec.WarmPoolRef.Name]++
			}
		}
	}

	filteredPools := make([]*extensionsv1beta1.SandboxWarmPool, 0, len(poolList.Items))
	for i := range poolList.Items {
		pool := &poolList.Items[i]
		allocated := claimCountsByPool[pool.Name]
		if !opts.IncludeStopped && poolListItemStopped(pool, allocated) {
			continue
		}
		filteredPools = append(filteredPools, pool)
	}

	templatesByName := make(map[string]*extensionsv1beta1.SandboxTemplate)
	if opts.IncludeStopped {
		var templateList extensionsv1beta1.SandboxTemplateList
		if err := g.k8sClient.List(ctx, &templateList, client.InNamespace(namespace)); err == nil {
			for i := range templateList.Items {
				templatesByName[templateList.Items[i].Name] = &templateList.Items[i]
			}
		}
	} else {
		for _, pool := range filteredPools {
			templateName := pool.Spec.TemplateRef.Name
			if templateName == "" {
				continue
			}
			if _, ok := templatesByName[templateName]; ok {
				continue
			}
			template := &extensionsv1beta1.SandboxTemplate{}
			if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: templateName, Namespace: namespace}, template); err == nil {
				templatesByName[templateName] = template
			}
		}
	}

	pools := make([]PoolInfo, 0, len(filteredPools))
	for _, pool := range filteredPools {
		info := PoolInfo{
			Name:              pool.Name,
			Namespace:         pool.Namespace,
			Profile:           firstNonEmpty(profileFromObjectMeta(pool.ObjectMeta), defaultPoolProfile),
			Replicas:          desiredSandboxWarmPoolReplicas(pool),
			ReadyReplicas:     pool.Status.ReadyReplicas,
			State:             firstNonEmpty(pool.Annotations[labels.PoolStateAnnotation], labels.PoolStateRunning),
			CreatedAt:         pool.CreationTimestamp.Time,
			AllocatedReplicas: claimCountsByPool[pool.Name],
		}
		if template, ok := templatesByName[pool.Spec.TemplateRef.Name]; ok {
			info.Image = primarySandboxTemplateImage(template)
			info.Profile = firstNonEmpty(profileFromObjectMeta(pool.ObjectMeta), profileFromObjectMeta(template.ObjectMeta), defaultPoolProfile)
		}
		pools = append(pools, info)
	}
	return pools, nil
}

func poolListItemStopped(pool *extensionsv1beta1.SandboxWarmPool, allocated int32) bool {
	if pool == nil || allocated > 0 {
		return false
	}
	state := strings.ToLower(strings.TrimSpace(pool.Annotations[labels.PoolStateAnnotation]))
	if state == labels.PoolStateStopped {
		return true
	}
	return desiredSandboxWarmPoolReplicas(pool) == 0 && pool.Status.ReadyReplicas == 0
}

func (g *Gateway) poolInfoFromSandboxWarmPool(ctx context.Context, pool *extensionsv1beta1.SandboxWarmPool) PoolInfo {
	info := PoolInfo{
		Name:          pool.Name,
		Namespace:     pool.Namespace,
		Profile:       firstNonEmpty(profileFromObjectMeta(pool.ObjectMeta), defaultPoolProfile),
		Replicas:      desiredSandboxWarmPoolReplicas(pool),
		ReadyReplicas: pool.Status.ReadyReplicas,
		State:         firstNonEmpty(pool.Annotations[labels.PoolStateAnnotation], labels.PoolStateRunning),
		CreatedAt:     pool.CreationTimestamp.Time,
	}
	template := &extensionsv1beta1.SandboxTemplate{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: pool.Spec.TemplateRef.Name, Namespace: pool.Namespace}, template); err == nil {
		info.Image = primarySandboxTemplateImage(template)
		info.Profile = firstNonEmpty(profileFromObjectMeta(pool.ObjectMeta), profileFromObjectMeta(template.ObjectMeta), defaultPoolProfile)
	}
	if readModel, ok := g.syncedPoolReadModel(); ok {
		if snapshot, found := readModel.SnapshotPool(pool.Namespace, pool.Name); found {
			info.AllocatedReplicas = snapshot.AllocatedReplicas
			return info
		}
	}
	if allocated, err := g.claimCountForPool(ctx, pool.Namespace, pool.Name); err == nil {
		info.AllocatedReplicas = allocated
	}
	return info
}
