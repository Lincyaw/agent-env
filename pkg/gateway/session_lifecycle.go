package gateway

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateSession allocates a sandbox runtime from the pool and registers a session.
func (g *Gateway) CreateSession(ctx context.Context, req CreateSessionRequest) (*SessionInfo, error) {
	ctx, span := otel.Tracer("gateway").Start(ctx, "Gateway.CreateSession",
		traceStartAttrs("image", req.Image, "profile", req.Profile, "namespace", req.Namespace),
	)
	defer span.End()

	ns, err := g.resolveNamespace(req.Namespace)
	if err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	if strings.TrimSpace(req.Image) == "" && strings.TrimSpace(req.Profile) == "" && strings.TrimSpace(req.PoolName) == "" {
		err := fmt.Errorf("image or profile is required")
		recordSpanErr(span, err)
		return nil, err
	}
	if len(req.PrivateContainers) > 0 && strings.TrimSpace(req.Image) == "" && strings.TrimSpace(req.PoolName) == "" {
		err := fmt.Errorf("privateContainers require image-backed pool creation or an explicit poolName")
		recordSpanErr(span, err)
		return nil, err
	}
	if err := validatePrivateContainers(req.PrivateContainers); err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	if !validSessionMode(req.Mode) {
		err := fmt.Errorf("invalid session mode: %q (valid: \"\", \"devbox\")", req.Mode)
		recordSpanErr(span, err)
		return nil, err
	}
	claimEnv, err := parseConfigEnvVars(req.ConfigEnv)
	if err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	if req.Mode == SessionModeDevbox && req.Devbox != nil {
		claimEnv = injectDevboxEnv(claimEnv, req.Devbox)
	}
	var autoCreatedPool string
	req, autoCreatedPool, err = g.ensureImageBackedSessionPool(ctx, req, ns)
	if err != nil {
		recordSpanErr(span, err)
		return nil, fmt.Errorf("ensure session pool: %w", err)
	}
	cleanupAutoCreatedPool := func() {
		if autoCreatedPool == "" {
			return
		}
		if stopped, cleanupErr := g.stopManagedPoolIfUnused(ctx, autoCreatedPool, ns); cleanupErr != nil {
			log.Printf("Warning: failed to cleanup unused managed pool %s/%s after session create failure: %v", ns, autoCreatedPool, cleanupErr)
		} else if stopped {
			log.Printf("Stopped unused managed pool %s/%s after session create failure", ns, autoCreatedPool)
		}
	}

	intent := g.resourceIntentFromCreateSession(ctx, req, ns)
	selection, admission, err := g.planSessionAllocation(ctx, intent)
	if err != nil {
		cleanupAutoCreatedPool()
		recordSpanErr(span, err)
		return nil, fmt.Errorf("plan session allocation: %w", err)
	}
	poolRef := selection.PoolName
	ns = selection.Namespace

	if len(claimEnv) > 0 {
		if err := g.ensureClaimEnvInjectionPolicy(ctx, poolRef, ns); err != nil {
			cleanupAutoCreatedPool()
			recordSpanErr(span, err)
			return nil, err
		}
	}

	sessionID := sessionName(req.Image, randomSuffix(8))
	sandboxName := sessionID
	ownerHash, _ := KeyHashFromContext(ctx)
	createdAt := time.Now()
	idleTimeout := g.resolveIdleTimeout(req)
	maxLifetime := g.resolveMaxLifetime(req)
	lifecycle := g.runtimeLifecycle(createdAt, createdAt, idleTimeout, maxLifetime)
	span.SetAttributes(
		attribute.String("session.id", sessionID),
		attribute.String("pool.selected", poolRef),
		attribute.String("pool.selection_reason", selection.Reason),
		attribute.String("admission.reason", admission.Reason),
		attribute.Int("admission.warm_available", int(admission.WarmAvailable)),
	)

	allocCtx, allocCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer allocCancel()

	allocStart := time.Now()
	allocation, err := g.runtimeAllocator.Allocate(allocCtx, RuntimeAllocateRequest{
		PoolRef:              poolRef,
		Namespace:            ns,
		SessionID:            sessionID,
		SandboxName:          sandboxName,
		OwnerKeyHash:         ownerHash,
		Managed:              req.Managed,
		ExperimentID:         req.ExperimentID,
		Mode:                 req.Mode,
		Lifecycle:            lifecycle,
		Env:                  claimEnv,
		VolumeClaimTemplates: g.devboxVolumeClaimTemplates(req),
	})
	if err != nil {
		recordSpanErr(span, err)
		if g.metrics != nil {
			result := "error"
			if allocCtx.Err() == context.DeadlineExceeded {
				result = "timeout"
			}
			g.metrics.IncrementPodAllocationResult(poolRef, result)
		}
		diag := g.diagnosePoolHealth(ctx, poolRef, ns)
		cleanupAutoCreatedPool()
		return nil, fmt.Errorf("allocate runtime from pool %s: %w (%s)", poolRef, err, diag)
	}
	span.SetAttributes(
		attribute.String("runtime.backend", allocation.Backend),
		attribute.String("pod.name", allocation.PodName),
		attribute.String("pod.ip", allocation.PodIP),
	)

	info := SessionInfo{
		ID:          sessionID,
		SandboxName: sandboxName,
		Namespace:   ns,
		Image:       selection.Pool.Image,
		PoolRef:     poolRef,
		Profile:     selection.Pool.Profile,
		Mode:        req.Mode,
		PodIP:       allocation.PodIP,
		PodName:     allocation.PodName,
		CreatedAt:   createdAt,
		Status:      "active",
	}
	if req.Mode == SessionModeDevbox {
		info.ConnectionInfo = buildConnectionInfo(sessionID, allocation.PodIP, req.Devbox)
	}

	g.store.Set(sessionID, &session{
		Info:                info,
		Runtime:             *allocation,
		History:             NewStepHistory(),
		managed:             req.Managed,
		experimentID:        req.ExperimentID,
		mode:                req.Mode,
		ownerKeyHash:        ownerHash,
		lastTaskTime:        createdAt,
		lastAnnotationPatch: createdAt,
		createdAt:           createdAt,
		idleTimeout:         idleTimeout,
		maxLifetime:         maxLifetime,
		operations:          make(map[string]*executeOperation),
		privateContainers:   privateContainerMap(req.PrivateContainers),
	})

	activeSessions := g.store.IncrCount(1)
	if g.metrics != nil {
		g.metrics.SetActiveSessions(activeSessions)
		allocationDuration := time.Since(allocStart)
		g.metrics.RecordSessionAllocationDuration(poolRef, allocationDuration)
		g.metrics.RecordSandboxReadyDuration(poolRef, allocationDuration)
		g.metrics.IncrementPodAllocationResult(poolRef, "success")
	}

	return &info, nil
}

// GetSession returns session info.
func (g *Gateway) GetSession(sessionID string) (*SessionInfo, error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	s.mu.RLock()
	info := s.Info
	if info.Status == "" {
		info.Status = "active"
	}
	s.mu.RUnlock()
	return &info, nil
}

func (g *Gateway) GetHistoricalSession(sessionID string) (*session, bool) {
	return g.store.GetHistorical(sessionID)
}

// SuspendSession suspends a devbox session (keeps PVC, terminates pod).
func (g *Gateway) SuspendSession(ctx context.Context, sessionID string) error {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	s.mu.RLock()
	mode := s.mode
	sandboxName := s.Runtime.SandboxName
	ns := s.Info.Namespace
	s.mu.RUnlock()

	if mode != SessionModeDevbox {
		return fmt.Errorf("only devbox sessions can be suspended")
	}
	if sandboxName == "" {
		return fmt.Errorf("session %s has no sandbox binding", sessionID)
	}

	sandbox := &sandboxv1beta1.Sandbox{}
	key := types.NamespacedName{Name: sandboxName, Namespace: ns}
	if err := g.k8sClient.Get(ctx, key, sandbox); err != nil {
		return fmt.Errorf("get sandbox %s: %w", sandboxName, err)
	}
	if sandbox.Spec.OperatingMode == sandboxv1beta1.SandboxOperatingModeSuspended {
		return nil
	}
	patch := client.MergeFrom(sandbox.DeepCopy())
	sandbox.Spec.OperatingMode = sandboxv1beta1.SandboxOperatingModeSuspended
	if err := g.k8sClient.Patch(ctx, sandbox, patch); err != nil {
		return fmt.Errorf("suspend sandbox %s: %w", sandboxName, err)
	}

	s.mu.Lock()
	s.Info.Status = "suspended"
	s.mu.Unlock()
	return nil
}

// ResumeSession resumes a suspended devbox session.
func (g *Gateway) ResumeSession(ctx context.Context, sessionID string) error {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	s.mu.RLock()
	mode := s.mode
	sandboxName := s.Runtime.SandboxName
	ns := s.Info.Namespace
	s.mu.RUnlock()

	if mode != SessionModeDevbox {
		return fmt.Errorf("only devbox sessions can be resumed")
	}
	if sandboxName == "" {
		return fmt.Errorf("session %s has no sandbox binding", sessionID)
	}

	sandbox := &sandboxv1beta1.Sandbox{}
	key := types.NamespacedName{Name: sandboxName, Namespace: ns}
	if err := g.k8sClient.Get(ctx, key, sandbox); err != nil {
		return fmt.Errorf("get sandbox %s: %w", sandboxName, err)
	}
	if sandbox.Spec.OperatingMode == sandboxv1beta1.SandboxOperatingModeRunning {
		return nil
	}
	patch := client.MergeFrom(sandbox.DeepCopy())
	sandbox.Spec.OperatingMode = sandboxv1beta1.SandboxOperatingModeRunning
	if err := g.k8sClient.Patch(ctx, sandbox, patch); err != nil {
		return fmt.Errorf("resume sandbox %s: %w", sandboxName, err)
	}

	s.mu.Lock()
	s.Info.Status = "active"
	s.mu.Unlock()
	return nil
}

// DeleteSession releases the sandbox runtime and removes the session.
func (g *Gateway) DeleteSession(ctx context.Context, sessionID string) error {
	return g.deleteSession(ctx, sessionID, "deleted")
}

func (g *Gateway) deleteSession(ctx context.Context, sessionID string, reason string) error {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}
	s.closed = true
	if reason == "" {
		reason = "deleted"
	}
	now := time.Now()
	s.deletionReason = reason
	s.deletedAt = &now
	s.Info.Status = "deleted"
	s.Info.DeletionReason = reason
	s.Info.DeletedAt = &now
	allocation := s.runtimeAllocation()
	podName := allocation.PodName
	podIP := allocation.PodIP
	s.mu.Unlock()

	if g.runtimeAllocator != nil {
		if err := g.runtimeAllocator.Release(ctx, allocation); err != nil && !errors.IsNotFound(err) {
			log.Printf("Warning: failed to release runtime %s for session %s: %v", podName, sessionID, err)
		}
	}

	if podIP != "" && g.sidecarClient != nil {
		if err := g.sidecarClient.CloseConnection(podIP); err != nil {
			log.Printf("Warning: failed to close sidecar connection for pod %s: %v", podName, err)
		}
	}

	g.store.Delete(sessionID)
	activeSessions := g.store.IncrCount(-1)

	if g.metrics != nil {
		g.metrics.SetActiveSessions(activeSessions)
		g.metrics.IncrementSessionDeletion(reason)
	}

	g.cleanupManagedPoolAfterSessionDelete(ctx, allocation)

	return nil
}

func (g *Gateway) cleanupManagedPoolAfterSessionDelete(ctx context.Context, allocation RuntimeAllocation) {
	if g.k8sClient == nil || allocation.PoolRef == "" {
		return
	}
	namespace := allocation.Namespace
	if namespace == "" {
		namespace = g.runtimeNamespace()
	}
	if stopped, err := g.stopManagedPoolIfUnused(ctx, allocation.PoolRef, namespace); err != nil {
		log.Printf("Warning: failed to cleanup unused managed pool %s/%s after session delete: %v", namespace, allocation.PoolRef, err)
	} else if stopped {
		log.Printf("Stopped unused managed pool %s/%s after session delete", namespace, allocation.PoolRef)
	}
}

func (g *Gateway) dropSession(sessionID string, s *session) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	now := time.Now()
	s.deletionReason = "runtime_lost"
	s.deletedAt = &now
	s.Info.Status = "deleted"
	s.Info.DeletionReason = s.deletionReason
	s.Info.DeletedAt = &now
	info := s.Info
	allocation := s.runtimeAllocation()
	s.mu.Unlock()

	if info.PodIP != "" {
		if g.sidecarClient != nil {
			if err := g.sidecarClient.CloseConnection(info.PodIP); err != nil {
				log.Printf("Warning: failed to close sidecar connection for dropped session %s: %v", sessionID, err)
			}
		}
	}
	if g.runtimeAllocator != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := g.runtimeAllocator.Release(ctx, allocation); err != nil && !errors.IsNotFound(err) {
			log.Printf("Warning: failed to release runtime for dropped session %s: %v", sessionID, err)
		}
		cancel()
	}
	g.store.Delete(sessionID)
	activeSessions := g.store.IncrCount(-1)
	if g.metrics != nil {
		g.metrics.SetActiveSessions(activeSessions)
		g.metrics.IncrementSessionDeletion("runtime_lost")
	}

	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cleanupCancel()
	g.cleanupManagedPoolAfterSessionDelete(cleanupCtx, allocation)
}

func (g *Gateway) markSessionDeleted(s *session, reason string) {
	if s == nil {
		return
	}
	if reason == "" {
		reason = "deleted"
	}
	now := time.Now()
	s.mu.Lock()
	s.closed = true
	s.deletionReason = reason
	s.deletedAt = &now
	s.Info.Status = "deleted"
	s.Info.DeletionReason = reason
	s.Info.DeletedAt = &now
	s.mu.Unlock()
}
