package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/labels"
	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
)

const runtimeBackendSandboxClaim = "sandboxclaim"

var dnsLabelCleaner = regexp.MustCompile(`[^a-z0-9-]+`)

// SandboxClaimRuntimeAllocator allocates execution runtimes through
// agent-sandbox SandboxClaim resources.
type SandboxClaimRuntimeAllocator struct {
	k8sClient    client.Client
	namespace    string
	pollInterval time.Duration
}

func NewSandboxClaimRuntimeAllocator(k8sClient client.Client, namespace ...string) *SandboxClaimRuntimeAllocator {
	ns := ""
	if len(namespace) > 0 {
		ns = strings.TrimSpace(namespace[0])
	}
	return &SandboxClaimRuntimeAllocator{
		k8sClient:    k8sClient,
		namespace:    ns,
		pollInterval: 500 * time.Millisecond,
	}
}

func (a *SandboxClaimRuntimeAllocator) Start(ctx context.Context) error {
	return nil
}

func (a *SandboxClaimRuntimeAllocator) Stop() {}

func (a *SandboxClaimRuntimeAllocator) Allocate(ctx context.Context, req RuntimeAllocateRequest) (*RuntimeAllocation, error) {
	if req.PoolRef == "" {
		return nil, fmt.Errorf("sandboxclaim allocator requires poolRef")
	}
	if req.Namespace == "" {
		return nil, fmt.Errorf("sandboxclaim allocator requires namespace")
	}
	if req.SessionID == "" {
		return nil, fmt.Errorf("sandboxclaim allocator requires sessionID")
	}

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := a.k8sClient.Get(ctx, types.NamespacedName{Name: req.PoolRef, Namespace: req.Namespace}, pool); err != nil {
		return nil, fmt.Errorf("get sandbox warm pool %s/%s: %w", req.Namespace, req.PoolRef, err)
	}

	claimBase := req.SandboxName
	if claimBase == "" {
		claimBase = req.SessionID
	}
	claimName := runtimeDNSLabel(claimBase)
	now := time.Now().UTC()
	annotations := map[string]string{
		labels.SessionAnnotation:      req.SessionID,
		labels.SandboxLabelKey:        req.SandboxName,
		labels.LastActivityAnnotation: now.Format(time.RFC3339),
	}
	podAnnotations := map[string]string{
		labels.SessionAnnotation:      req.SessionID,
		labels.LastActivityAnnotation: now.Format(time.RFC3339),
	}
	if req.OwnerKeyHash != "" {
		annotations[labels.OwnerKeyHashAnnotation] = req.OwnerKeyHash
		podAnnotations[labels.OwnerKeyHashAnnotation] = req.OwnerKeyHash
	}
	if req.ExperimentID != "" {
		annotations[labels.ExperimentAnnotation] = req.ExperimentID
		podAnnotations[labels.ExperimentAnnotation] = req.ExperimentID
	}
	if req.Managed {
		annotations[labels.ManagedAnnotation] = "true"
		podAnnotations[labels.ManagedAnnotation] = "true"
	}
	annotateLifecycle(annotations, req.Lifecycle)
	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: req.Namespace,
			Labels: map[string]string{
				labels.PoolLabelKey: req.PoolRef,
			},
			Annotations: annotations,
		},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: req.PoolRef},
			Lifecycle:   sandboxClaimLifecycle(now, req.Lifecycle),
			AdditionalPodMetadata: sandboxv1beta1.PodMetadata{
				Annotations: podAnnotations,
			},
		},
	}

	createdClaim := false
	if err := a.k8sClient.Create(ctx, claim); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("create sandbox claim %s/%s: %w", req.Namespace, claimName, err)
		}
	} else {
		createdClaim = true
	}
	cleanupCreatedClaim := func(cause error) (*RuntimeAllocation, error) {
		if createdClaim {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = a.Release(cleanupCtx, RuntimeAllocation{
				Namespace: req.Namespace,
				ClaimName: claimName,
			})
		}
		return nil, cause
	}

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()
	for {
		latest := &extensionsv1beta1.SandboxClaim{}
		if err := a.k8sClient.Get(ctx, types.NamespacedName{Name: claimName, Namespace: req.Namespace}, latest); err != nil {
			return cleanupCreatedClaim(fmt.Errorf("get sandbox claim %s/%s: %w", req.Namespace, claimName, err))
		}
		if got := latest.Annotations[labels.SessionAnnotation]; got != "" && got != req.SessionID {
			return cleanupCreatedClaim(fmt.Errorf("sandbox claim %s/%s is owned by session %q, not %q", req.Namespace, claimName, got, req.SessionID))
		}
		allocation, ready, err := a.allocationFromClaim(ctx, req.PoolRef, latest)
		if err != nil {
			return cleanupCreatedClaim(err)
		}
		if ready {
			return allocation, nil
		}

		select {
		case <-ctx.Done():
			return cleanupCreatedClaim(fmt.Errorf("wait for sandbox claim %s/%s ready: %w", req.Namespace, claimName, ctx.Err()))
		case <-ticker.C:
		}
	}
}

func (a *SandboxClaimRuntimeAllocator) Release(ctx context.Context, allocation RuntimeAllocation) error {
	if allocation.ClaimName == "" {
		return nil
	}
	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      allocation.ClaimName,
			Namespace: allocation.Namespace,
		},
	}
	if err := a.k8sClient.Delete(ctx, claim); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete sandbox claim %s/%s: %w", allocation.Namespace, allocation.ClaimName, err)
	}
	return nil
}

func (a *SandboxClaimRuntimeAllocator) Resolve(ctx context.Context, allocation RuntimeAllocation, sessionID string) (*RuntimeAllocation, error) {
	if allocation.ClaimName == "" || allocation.Namespace == "" {
		return nil, fmt.Errorf("session %s has incomplete sandboxclaim binding", sessionID)
	}

	claim := &extensionsv1beta1.SandboxClaim{}
	if err := a.k8sClient.Get(ctx, types.NamespacedName{Name: allocation.ClaimName, Namespace: allocation.Namespace}, claim); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("session %s sandbox claim %s/%s no longer exists", sessionID, allocation.Namespace, allocation.ClaimName)
		}
		return nil, fmt.Errorf("validate session %s sandbox claim %s/%s: %w", sessionID, allocation.Namespace, allocation.ClaimName, err)
	}
	if claim.DeletionTimestamp != nil {
		return nil, fmt.Errorf("session %s sandbox claim %s/%s is terminating", sessionID, allocation.Namespace, allocation.ClaimName)
	}
	if got := claim.Annotations[labels.SessionAnnotation]; got != "" && got != sessionID {
		return nil, fmt.Errorf("session %s lost sandbox claim ownership for %s/%s (annotation=%q)", sessionID, allocation.Namespace, allocation.ClaimName, got)
	}

	resolved, ready, err := a.allocationFromClaim(ctx, allocation.PoolRef, claim)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, fmt.Errorf("session %s sandbox claim %s/%s is not ready", sessionID, allocation.Namespace, allocation.ClaimName)
	}
	return resolved, nil
}

func (a *SandboxClaimRuntimeAllocator) Touch(ctx context.Context, allocation RuntimeAllocation, sessionID string, at time.Time, lifecycle RuntimeLifecycle) error {
	if allocation.ClaimName == "" || allocation.Namespace == "" {
		return fmt.Errorf("session %s has incomplete sandboxclaim binding", sessionID)
	}

	claim := &extensionsv1beta1.SandboxClaim{}
	if err := a.k8sClient.Get(ctx, types.NamespacedName{Name: allocation.ClaimName, Namespace: allocation.Namespace}, claim); err != nil {
		if apierrors.IsNotFound(err) {
			return err
		}
		return err
	}
	if got := claim.Annotations[labels.SessionAnnotation]; got != "" && got != sessionID {
		return fmt.Errorf("session %s lost sandbox claim ownership for annotation patch on %s/%s (annotation=%q)", sessionID, allocation.Namespace, allocation.ClaimName, got)
	}

	patch := client.MergeFrom(claim.DeepCopy())
	if claim.Annotations == nil {
		claim.Annotations = make(map[string]string)
	}
	claim.Annotations[labels.LastActivityAnnotation] = at.UTC().Format(time.RFC3339)
	annotateLifecycle(claim.Annotations, lifecycle)
	claim.Spec.Lifecycle = sandboxClaimLifecycle(at.UTC(), lifecycle)
	if err := a.k8sClient.Patch(ctx, claim, patch); err != nil {
		if apierrors.IsNotFound(err) {
			return err
		}
		return fmt.Errorf("patch sandbox claim %s/%s last activity: %w", allocation.Namespace, allocation.ClaimName, err)
	}

	if allocation.SandboxName == "" {
		return nil
	}
	sandbox := &sandboxv1beta1.Sandbox{}
	if err := a.k8sClient.Get(ctx, types.NamespacedName{Name: allocation.SandboxName, Namespace: allocation.Namespace}, sandbox); err != nil {
		if apierrors.IsNotFound(err) {
			return err
		}
		return fmt.Errorf("get sandbox %s/%s for last activity patch: %w", allocation.Namespace, allocation.SandboxName, err)
	}
	sandboxPatch := client.MergeFrom(sandbox.DeepCopy())
	if sandbox.Annotations == nil {
		sandbox.Annotations = make(map[string]string)
	}
	sandbox.Annotations[labels.LastActivityAnnotation] = at.UTC().Format(time.RFC3339)
	if err := a.k8sClient.Patch(ctx, sandbox, sandboxPatch); err != nil {
		if apierrors.IsNotFound(err) {
			return err
		}
		return fmt.Errorf("patch sandbox %s/%s last activity: %w", allocation.Namespace, allocation.SandboxName, err)
	}
	return nil
}

func (a *SandboxClaimRuntimeAllocator) DiagnosticStats() map[string]AllocatorPoolStats {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var opts []client.ListOption
	if a.namespace != "" {
		opts = append(opts, client.InNamespace(a.namespace))
	}
	var pools extensionsv1beta1.SandboxWarmPoolList
	if err := a.k8sClient.List(ctx, &pools, opts...); err != nil {
		return map[string]AllocatorPoolStats{}
	}
	var claims extensionsv1beta1.SandboxClaimList
	if err := a.k8sClient.List(ctx, &claims, opts...); err != nil {
		return map[string]AllocatorPoolStats{}
	}

	claimCounts := make(map[types.NamespacedName]int32)
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.DeletionTimestamp != nil || claim.Spec.WarmPoolRef.Name == "" {
			continue
		}
		key := types.NamespacedName{Name: claim.Spec.WarmPoolRef.Name, Namespace: claim.Namespace}
		claimCounts[key]++
	}

	stats := make(map[string]AllocatorPoolStats, len(pools.Items))
	for i := range pools.Items {
		pool := &pools.Items[i]
		key := types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}
		idle := pool.Status.ReadyReplicas - claimCounts[key]
		if idle < 0 {
			idle = 0
		}
		stats[poolMetricLabel(pool.Namespace, pool.Name)] = AllocatorPoolStats{
			IdleCount: int(idle),
		}
	}
	return stats
}

func (a *SandboxClaimRuntimeAllocator) allocationFromClaim(ctx context.Context, poolRef string, claim *extensionsv1beta1.SandboxClaim) (*RuntimeAllocation, bool, error) {
	ready := hasReadyCondition(claim.Status.Conditions)
	sandboxName := claim.Status.SandboxStatus.Name
	podIP := firstString(claim.Status.SandboxStatus.PodIPs)
	podName := ""

	if sandboxName != "" {
		sandbox := &sandboxv1beta1.Sandbox{}
		if err := a.k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: claim.Namespace}, sandbox); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, false, fmt.Errorf("get sandbox %s/%s: %w", claim.Namespace, sandboxName, err)
			}
		} else {
			if sandbox.Annotations != nil {
				podName = sandbox.Annotations[sandboxv1beta1.SandboxPodNameAnnotation]
			}
			if podIP == "" {
				podIP = firstString(sandbox.Status.PodIPs)
			}
			if !ready {
				ready = hasReadyCondition(sandbox.Status.Conditions)
			}
		}
	}
	if podName == "" {
		podName = sandboxName
	}
	if poolRef == "" {
		poolRef = claim.Spec.WarmPoolRef.Name
	}
	if ready && (sandboxName == "" || podIP == "") {
		return nil, false, fmt.Errorf("sandbox claim %s/%s is ready without sandbox name or pod IP", claim.Namespace, claim.Name)
	}

	allocation := &RuntimeAllocation{
		Backend:     runtimeBackendSandboxClaim,
		PoolRef:     poolRef,
		Namespace:   claim.Namespace,
		PodName:     podName,
		PodIP:       podIP,
		ClaimName:   claim.Name,
		SandboxName: sandboxName,
	}
	return allocation, ready && sandboxName != "" && podIP != "", nil
}

func hasReadyCondition(conditions []metav1.Condition) bool {
	for _, condition := range conditions {
		if condition.Type == string(sandboxv1beta1.SandboxConditionReady) && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func runtimeDNSLabel(value string) string {
	cleaned := strings.ToLower(strings.TrimSpace(value))
	cleaned = dnsLabelCleaner.ReplaceAllString(cleaned, "-")
	cleaned = strings.Trim(cleaned, "-")
	if cleaned == "" {
		cleaned = "sandbox"
	}
	if len(cleaned) <= 63 {
		return cleaned
	}

	sum := sha256.Sum256([]byte(cleaned))
	suffix := hex.EncodeToString(sum[:])[:10]
	return strings.Trim(cleaned[:52], "-") + "-" + suffix
}

func annotateLifecycle(annotations map[string]string, lifecycle RuntimeLifecycle) {
	if annotations == nil {
		return
	}
	if lifecycle.IdleTimeout > 0 {
		annotations[labels.IdleTimeoutAnnotation] = durationSecondsString(lifecycle.IdleTimeout)
	}
	if lifecycle.MaxLifetime > 0 {
		annotations[labels.MaxLifetimeAnnotation] = durationSecondsString(lifecycle.MaxLifetime)
	}
	if lifecycle.FinishedTTL > 0 {
		annotations[labels.FinishedTTLAnnotation] = durationSecondsString(lifecycle.FinishedTTL)
	}
}

func sandboxClaimLifecycle(now time.Time, lifecycle RuntimeLifecycle) *extensionsv1beta1.Lifecycle {
	shutdownAt := runtimeShutdownTime(now, lifecycle)
	var ttl *int32
	if lifecycle.FinishedTTL > 0 {
		seconds := int32(lifecycle.FinishedTTL.Seconds())
		if seconds < 0 {
			seconds = 0
		}
		ttl = &seconds
	}
	if shutdownAt == nil && ttl == nil {
		return nil
	}
	lc := &extensionsv1beta1.Lifecycle{
		TTLSecondsAfterFinished: ttl,
		ShutdownPolicy:          extensionsv1beta1.ShutdownPolicyDeleteForeground,
	}
	if shutdownAt != nil {
		lc.ShutdownTime = &metav1.Time{Time: shutdownAt.UTC()}
	}
	return lc
}

func runtimeShutdownTime(now time.Time, lifecycle RuntimeLifecycle) *time.Time {
	var deadline *time.Time
	createdAt := lifecycle.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	lastActivityAt := lifecycle.LastActivityAt
	if lastActivityAt.IsZero() {
		lastActivityAt = now
	}
	if lifecycle.IdleTimeout > 0 {
		t := lastActivityAt.Add(lifecycle.IdleTimeout)
		deadline = &t
	}
	if lifecycle.MaxLifetime > 0 {
		t := createdAt.Add(lifecycle.MaxLifetime)
		if deadline == nil || t.Before(*deadline) {
			deadline = &t
		}
	}
	return deadline
}

func durationSecondsString(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", int64(d.Round(time.Second)/time.Second))
}
