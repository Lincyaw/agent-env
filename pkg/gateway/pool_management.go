package gateway

import (
	"context"
	"fmt"
	"log"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/labels"
	"github.com/Lincyaw/agent-env/pkg/scheduling"
)

const (
	defaultSandboxRequestCPU    = "500m"
	defaultSandboxRequestMemory = "512Mi"
	defaultSandboxLimitCPU      = "8"
	defaultSandboxLimitMemory   = "16Gi"
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

	workspaceDir := req.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = "/workspace"
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
	templateAnnotations := map[string]string{}
	poolAnnotations := map[string]string{}
	podAnnotations := map[string]string{}
	if profile := strings.TrimSpace(req.Profile); profile != "" {
		templateAnnotations[poolProfileAnnotation] = profile
		poolAnnotations[poolProfileAnnotation] = profile
	}
	if req.Managed {
		templateAnnotations[labels.ManagedPoolAnnotation] = "true"
		poolAnnotations[labels.ManagedPoolAnnotation] = "true"
	}
	if replicas > 0 {
		poolAnnotations[labels.PoolStateAnnotation] = labels.PoolStateRunning
	} else {
		poolAnnotations[labels.PoolStateAnnotation] = labels.PoolStateStopped
		poolAnnotations[scheduling.PoolAutoscaleAnnotation] = "false"
	}
	imageLocalityEnabled := g.gwConfig.ImageLocalityEnabled || hasJSONPayload(req.ImageLocality)
	if imageLocalityEnabled {
		templateAnnotations[scheduling.ImageLocalityAnnotation] = scheduling.ImageLocalityEnabledValue
		poolAnnotations[scheduling.ImageLocalityAnnotation] = scheduling.ImageLocalityEnabledValue
		podAnnotations[scheduling.ImageLocalityAnnotation] = scheduling.ImageLocalityEnabledValue
	}
	if req.Image != "" && (imageLocalityEnabled || strings.TrimSpace(g.gwConfig.SchedulerName) != "") {
		templateAnnotations[scheduling.ExecutorImageAnnotation] = req.Image
		podAnnotations[scheduling.ExecutorImageAnnotation] = req.Image
	}
	if len(templateAnnotations) > 0 {
		templateMeta.Annotations = templateAnnotations
	}
	if len(poolAnnotations) > 0 {
		poolMeta.Annotations = poolAnnotations
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
				Spec:       g.sandboxPodSpec(req.Image, workspaceDir, *resources, req.PrivateContainers),
			},
		},
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

	if req.Image != "" {
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
	if pool.Annotations == nil {
		pool.Annotations = make(map[string]string)
	}
	if req.Replicas > 0 {
		pool.Annotations[labels.PoolStateAnnotation] = labels.PoolStateRunning
		delete(pool.Annotations, scheduling.PoolAutoscaleAnnotation)
	} else {
		pool.Annotations[labels.PoolStateAnnotation] = labels.PoolStateStopped
		pool.Annotations[scheduling.PoolAutoscaleAnnotation] = "false"
	}

	if err := g.k8sClient.Update(ctx, pool); err != nil {
		return nil, fmt.Errorf("update pool: %w", err)
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
	if pool.Annotations == nil {
		pool.Annotations = make(map[string]string)
	}
	pool.Spec.Replicas = int32Ptr(replicas)
	pool.Annotations[labels.PoolStateAnnotation] = state
	if disableAutoscale {
		pool.Annotations[scheduling.PoolAutoscaleAnnotation] = "false"
	}
	if err := g.k8sClient.Patch(ctx, pool, client.MergeFrom(before)); err != nil {
		return fmt.Errorf("patch pool %s/%s lifecycle: %w", namespace, name, err)
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

	return fmt.Sprintf("pool=%s desired=%d replicas=%d ready=%d template=%s",
		poolRef, desiredSandboxWarmPoolReplicas(pool), pool.Status.Replicas, pool.Status.ReadyReplicas, pool.Spec.TemplateRef.Name)
}

// ListPools returns SandboxWarmPool CRDs in the gateway namespace.
// Uses batch queries to avoid N+1 API calls.
func (g *Gateway) ListPools(ctx context.Context, namespace string) ([]PoolInfo, error) {
	namespace, err := g.resolveNamespace(namespace)
	if err != nil {
		return nil, err
	}

	var poolList extensionsv1beta1.SandboxWarmPoolList
	if err := g.k8sClient.List(ctx, &poolList, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	templatesByName := make(map[string]*extensionsv1beta1.SandboxTemplate)
	var templateList extensionsv1beta1.SandboxTemplateList
	if err := g.k8sClient.List(ctx, &templateList, client.InNamespace(namespace)); err == nil {
		for i := range templateList.Items {
			templatesByName[templateList.Items[i].Name] = &templateList.Items[i]
		}
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

	pools := make([]PoolInfo, 0, len(poolList.Items))
	for i := range poolList.Items {
		pool := &poolList.Items[i]
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
	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(pool.Namespace)); err == nil {
		for i := range claims.Items {
			if claims.Items[i].Spec.WarmPoolRef.Name == pool.Name && claims.Items[i].DeletionTimestamp == nil {
				info.AllocatedReplicas++
			}
		}
	}
	return info
}
