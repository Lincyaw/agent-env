package gateway

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultPolicyTenant = "default"
	defaultPoolProfile  = "default"

	poolProfileAnnotation = "arl.infra.io/profile"
)

// RequestScope identifies the policy boundary for a session request. Tenant is
// intentionally defaulted today so future multi-tenant policy can be introduced
// without changing the selector/admission interfaces.
type RequestScope struct {
	Namespace string
	Principal string
	Tenant    string
}

func (s RequestScope) normalized() RequestScope {
	if s.Namespace == "" {
		s.Namespace = "default"
	}
	if s.Tenant == "" {
		s.Tenant = defaultPolicyTenant
	}
	return s
}

// ResourceIntent is the normalized "what do I need?" input to pool policy.
type ResourceIntent struct {
	Scope            RequestScope
	Profile          string
	Image            string
	PinnedPoolName   string
	Managed          bool
	ExperimentID     string
	ColdStartAllowed bool
}

// PoolSnapshot is the selector/admission view of a SandboxWarmPool.
type PoolSnapshot struct {
	Name              string
	Namespace         string
	Profile           string
	Image             string
	DesiredReplicas   int32
	ReadyReplicas     int32
	AllocatedReplicas int32
}

func (p PoolSnapshot) WarmAvailable() int32 {
	available := p.ReadyReplicas - p.AllocatedReplicas
	if available < 0 {
		return 0
	}
	return available
}

// PoolSelection records which pool policy selected and why.
type PoolSelection struct {
	PoolName  string
	Namespace string
	Reason    string
	Pool      PoolSnapshot
}

// PoolSelector selects a pool for a normalized resource intent.
type PoolSelector interface {
	SelectPool(ctx context.Context, intent ResourceIntent, pools []PoolSnapshot) (PoolSelection, error)
}

// DefaultPoolSelector filters by profile/image and chooses the pool with the
// most warm capacity. A pinned pool name is only used by internal admin flows
// that created infrastructure with request-specific settings.
type DefaultPoolSelector struct{}

func (DefaultPoolSelector) SelectPool(_ context.Context, intent ResourceIntent, pools []PoolSnapshot) (PoolSelection, error) {
	scope := intent.Scope.normalized()
	profile := strings.TrimSpace(intent.Profile)
	image := strings.TrimSpace(intent.Image)

	if intent.PinnedPoolName != "" {
		for _, pool := range pools {
			if pool.Namespace == scope.Namespace && pool.Name == intent.PinnedPoolName {
				if profile != "" && pool.Profile != profile {
					return PoolSelection{}, fmt.Errorf("pool %q profile %q does not match requested profile %q", intent.PinnedPoolName, pool.Profile, profile)
				}
				if image != "" && pool.Image != image {
					return PoolSelection{}, fmt.Errorf("pool %q image %q does not match requested image %q", intent.PinnedPoolName, pool.Image, image)
				}
				return PoolSelection{
					PoolName:  pool.Name,
					Namespace: pool.Namespace,
					Reason:    "pinned_pool",
					Pool:      pool,
				}, nil
			}
		}
		return PoolSelection{}, fmt.Errorf("pool %q not found in namespace %q", intent.PinnedPoolName, scope.Namespace)
	}

	var candidates []PoolSnapshot
	for _, pool := range pools {
		if pool.Namespace != scope.Namespace {
			continue
		}
		if profile != "" && pool.Profile != profile {
			continue
		}
		if image != "" && pool.Image != image {
			continue
		}
		candidates = append(candidates, pool)
	}
	if len(candidates) == 0 {
		if profile != "" {
			return PoolSelection{}, fmt.Errorf("no pool found for profile %q in namespace %q", profile, scope.Namespace)
		}
		return PoolSelection{}, fmt.Errorf("no pool found in namespace %q", scope.Namespace)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.WarmAvailable() != right.WarmAvailable() {
			return left.WarmAvailable() > right.WarmAvailable()
		}
		if left.ReadyReplicas != right.ReadyReplicas {
			return left.ReadyReplicas > right.ReadyReplicas
		}
		if left.DesiredReplicas != right.DesiredReplicas {
			return left.DesiredReplicas > right.DesiredReplicas
		}
		return left.Name < right.Name
	})

	selected := candidates[0]
	return PoolSelection{
		PoolName:  selected.Name,
		Namespace: selected.Namespace,
		Reason:    "profile_capacity",
		Pool:      selected,
	}, nil
}

// AdmissionDecision captures the bounded decision made before creating a Claim.
type AdmissionDecision struct {
	Admitted          bool
	Reason            string
	WarmAvailable     int32
	ColdStartExpected bool
}

// AdmissionController decides whether a selected pool should accept a request.
type AdmissionController interface {
	Admit(ctx context.Context, intent ResourceIntent, selection PoolSelection) (AdmissionDecision, error)
}

// DefaultAdmissionController preserves today's behavior: warm capacity is
// preferred, but cold start is allowed when the warm pool is saturated.
type DefaultAdmissionController struct {
	AllowColdStart bool
}

func NewDefaultAdmissionController() DefaultAdmissionController {
	return DefaultAdmissionController{AllowColdStart: true}
}

func (a DefaultAdmissionController) Admit(_ context.Context, intent ResourceIntent, selection PoolSelection) (AdmissionDecision, error) {
	warmAvailable := selection.Pool.WarmAvailable()
	if warmAvailable > 0 {
		return AdmissionDecision{
			Admitted:      true,
			Reason:        "warm_capacity_available",
			WarmAvailable: warmAvailable,
		}, nil
	}
	if intent.ColdStartAllowed && a.AllowColdStart {
		return AdmissionDecision{
			Admitted:          true,
			Reason:            "cold_start_allowed",
			WarmAvailable:     0,
			ColdStartExpected: true,
		}, nil
	}
	return AdmissionDecision{
		Admitted:      false,
		Reason:        "pool_at_capacity",
		WarmAvailable: 0,
	}, ErrPoolAtCapacity
}

func (g *Gateway) resourceIntentFromCreateSession(ctx context.Context, req CreateSessionRequest, namespace string) ResourceIntent {
	principal, _ := KeyHashFromContext(ctx)
	return ResourceIntent{
		Scope: RequestScope{
			Namespace: namespace,
			Principal: principal,
			Tenant:    defaultPolicyTenant,
		},
		Profile:          normalizeProfile(req.Profile),
		Image:            normalizedOptionalImage(req.Image),
		PinnedPoolName:   req.PoolName,
		Managed:          req.Managed,
		ExperimentID:     req.ExperimentID,
		ColdStartAllowed: !g.gwConfig.AdmissionDisableColdStart,
	}
}

func (g *Gateway) ensureImageBackedSessionPool(ctx context.Context, req CreateSessionRequest, namespace string) (CreateSessionRequest, error) {
	req.Image = normalizedOptionalImage(req.Image)
	if req.Image == "" {
		if req.Profile != "" {
			req.Profile = normalizeProfile(req.Profile)
		}
		return req, nil
	}
	req.Profile = normalizeProfile(req.Profile)
	if req.PoolName != "" {
		return req, nil
	}

	snapshots, err := g.snapshotPools(ctx, namespace)
	if err != nil {
		return req, err
	}
	for _, pool := range snapshots {
		if pool.Namespace == namespace && pool.Profile == req.Profile && pool.Image == req.Image {
			return req, nil
		}
	}

	poolName, err := managedPoolName(req.Image, namespace, req.Profile, nil)
	if err != nil {
		return req, err
	}
	if err := g.CreatePool(ctx, CreatePoolRequest{
		Name:      poolName,
		Image:     req.Image,
		Profile:   req.Profile,
		Replicas:  1,
		Namespace: namespace,
	}); err != nil && !k8serrors.IsAlreadyExists(err) {
		return req, fmt.Errorf("ensure sandbox pool for image %q profile %q: %w", req.Image, req.Profile, err)
	}
	req.PoolName = poolName
	return req, nil
}

func (g *Gateway) planSessionAllocation(ctx context.Context, intent ResourceIntent) (PoolSelection, AdmissionDecision, error) {
	selection, decision, err := g.tryPlanSessionAllocation(ctx, intent)
	if err == nil || g.gwConfig.AdmissionQueueTimeout <= 0 || ctx.Err() != nil {
		return selection, decision, err
	}
	if !errors.Is(err, ErrPoolAtCapacity) {
		return selection, decision, err
	}

	queueKey := types.NamespacedName{Name: selection.PoolName, Namespace: selection.Namespace}
	g.incrementAdmissionQueue(queueKey)
	defer g.decrementAdmissionQueue(queueKey)

	timeout := g.gwConfig.AdmissionQueueTimeout
	poll := g.gwConfig.AdmissionQueuePollInterval
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			if waitCtx.Err() == context.DeadlineExceeded {
				return selection, decision, fmt.Errorf("%w: queued for %s waiting for warm capacity", ErrPoolAtCapacity, timeout)
			}
			return selection, decision, waitCtx.Err()
		case <-ticker.C:
			nextSelection, nextDecision, nextErr := g.tryPlanSessionAllocation(waitCtx, intent)
			if nextErr == nil {
				return nextSelection, nextDecision, nil
			}
			selection, decision = nextSelection, nextDecision
			if !errors.Is(nextErr, ErrPoolAtCapacity) {
				return nextSelection, nextDecision, nextErr
			}
		}
	}
}

func (g *Gateway) incrementAdmissionQueue(key types.NamespacedName) {
	if key.Name == "" {
		return
	}
	g.admissionQueueMu.Lock()
	defer g.admissionQueueMu.Unlock()
	if g.admissionQueueDepth == nil {
		g.admissionQueueDepth = make(map[types.NamespacedName]int32)
	}
	g.admissionQueueDepth[key]++
}

func (g *Gateway) decrementAdmissionQueue(key types.NamespacedName) {
	if key.Name == "" {
		return
	}
	g.admissionQueueMu.Lock()
	defer g.admissionQueueMu.Unlock()
	if g.admissionQueueDepth == nil {
		return
	}
	next := g.admissionQueueDepth[key] - 1
	if next <= 0 {
		delete(g.admissionQueueDepth, key)
		return
	}
	g.admissionQueueDepth[key] = next
}

func (g *Gateway) admissionQueueSnapshot() map[types.NamespacedName]int32 {
	g.admissionQueueMu.Lock()
	defer g.admissionQueueMu.Unlock()
	result := make(map[types.NamespacedName]int32, len(g.admissionQueueDepth))
	for key, value := range g.admissionQueueDepth {
		result[key] = value
	}
	return result
}

func (g *Gateway) tryPlanSessionAllocation(ctx context.Context, intent ResourceIntent) (PoolSelection, AdmissionDecision, error) {
	scope := intent.Scope.normalized()
	snapshots, err := g.snapshotPools(ctx, scope.Namespace)
	if err != nil {
		return PoolSelection{}, AdmissionDecision{}, err
	}

	selector := g.poolSelector
	if selector == nil {
		selector = DefaultPoolSelector{}
	}
	selection, err := selector.SelectPool(ctx, intent, snapshots)
	if err != nil {
		return PoolSelection{}, AdmissionDecision{}, err
	}

	admission := g.admissionController
	if admission == nil {
		defaultAdmission := NewDefaultAdmissionController()
		admission = defaultAdmission
	}
	decision, err := admission.Admit(ctx, intent, selection)
	if err != nil {
		return selection, decision, err
	}
	if !decision.Admitted {
		return selection, decision, fmt.Errorf("%w: %s", ErrPoolAtCapacity, decision.Reason)
	}
	return selection, decision, nil
}

func (g *Gateway) snapshotPools(ctx context.Context, namespace string) ([]PoolSnapshot, error) {
	if namespace == "" {
		namespace = "default"
	}

	var poolList extensionsv1beta1.SandboxWarmPoolList
	if err := g.k8sClient.List(ctx, &poolList, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("list sandbox warm pools: %w", err)
	}

	claimCounts := make(map[types.NamespacedName]int32)
	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("list sandbox claims: %w", err)
	}
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.DeletionTimestamp != nil || claim.Spec.WarmPoolRef.Name == "" {
			continue
		}
		key := types.NamespacedName{Name: claim.Spec.WarmPoolRef.Name, Namespace: claim.Namespace}
		claimCounts[key]++
	}

	snapshots := make([]PoolSnapshot, 0, len(poolList.Items))
	for i := range poolList.Items {
		pool := &poolList.Items[i]
		template := &extensionsv1beta1.SandboxTemplate{}
		templateKey := types.NamespacedName{Name: pool.Spec.TemplateRef.Name, Namespace: pool.Namespace}
		image := ""
		templateProfile := ""
		if err := g.k8sClient.Get(ctx, templateKey, template); err == nil {
			image = primarySandboxTemplateImage(template)
			templateProfile = profileFromObjectMeta(template.ObjectMeta)
		}

		key := types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}
		profile := firstNonEmpty(profileFromObjectMeta(pool.ObjectMeta), templateProfile, defaultPoolProfile)
		snapshots = append(snapshots, PoolSnapshot{
			Name:              pool.Name,
			Namespace:         pool.Namespace,
			Profile:           profile,
			Image:             image,
			DesiredReplicas:   desiredSandboxWarmPoolReplicas(pool),
			ReadyReplicas:     pool.Status.ReadyReplicas,
			AllocatedReplicas: claimCounts[key],
		})
	}
	return snapshots, nil
}

func profileFromObjectMeta(meta metav1.ObjectMeta) string {
	if meta.Annotations == nil {
		return ""
	}
	return strings.TrimSpace(meta.Annotations[poolProfileAnnotation])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeProfile(profile string) string {
	return firstNonEmpty(profile, defaultPoolProfile)
}

func normalizedOptionalImage(image string) string {
	if strings.TrimSpace(image) == "" {
		return ""
	}
	return normalizeImage(strings.TrimSpace(image))
}
