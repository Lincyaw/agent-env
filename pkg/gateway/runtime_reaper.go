package gateway

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/labels"
)

const (
	runtimeOrphanGrace   = 5 * time.Minute
	runtimeNotReadyGrace = 5 * time.Minute
)

// StartSessionSweep starts the background session reaper goroutine.
func (g *Gateway) StartSessionSweep() {
	g.sweepWg.Add(1)
	go g.sessionSweepLoop()
}

// StopSessionSweep signals the session sweep goroutine to exit and waits.
func (g *Gateway) StopSessionSweep() {
	close(g.sweepStopCh)
	g.sweepWg.Wait()
}

// CleanupStaleConnections removes gRPC connections in Shutdown or TransientFailure state.
func (g *Gateway) CleanupStaleConnections() int {
	if g.sidecarClient == nil {
		return 0
	}
	return g.sidecarClient.CleanupStale()
}

func (g *Gateway) sessionSweepLoop() {
	defer g.sweepWg.Done()
	ticker := time.NewTicker(g.gwConfig.SweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.sweepStopCh:
			return
		case <-ticker.C:
			g.sweepSessions()
			g.sweepRuntimeClaims()
		}
	}
}

func (g *Gateway) sweepSessions() {
	now := time.Now()
	g.store.Range(func(sessionID string, s *session) bool {
		if atomic.LoadInt32(&s.activeExecs) > 0 {
			return true
		}

		s.mu.RLock()
		lastTask := s.lastTaskTime
		idleTimeout := s.idleTimeout
		s.mu.RUnlock()

		if idleTimeout > 0 && now.Sub(lastTask) > idleTimeout {
			log.Printf("Session %s idle for %v (timeout=%v), deleting", sessionID, now.Sub(lastTask), idleTimeout)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := g.deleteSession(ctx, sessionID, "idle_timeout"); err != nil {
				log.Printf("Warning: failed to delete idle session %s: %v", sessionID, err)
			}
			cancel()
		}

		return true
	})
}

func (g *Gateway) sweepRuntimeClaims() {
	if g.k8sClient == nil || g.runtimeAllocator == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := g.reapRuntimeClaims(ctx, time.Now()); err != nil {
		log.Printf("Warning: runtime claim reaper failed: %v", err)
	}
}

func (g *Gateway) reapRuntimeClaims(ctx context.Context, now time.Time) error {
	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(g.runtimeNamespace())); err != nil {
		return fmt.Errorf("list sandbox claims for runtime reaper: %w", err)
	}
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.DeletionTimestamp != nil {
			continue
		}
		sessionID := strings.TrimSpace(claim.Annotations[labels.SessionAnnotation])
		if sessionID == "" {
			continue
		}
		if err := g.reapRuntimeClaim(ctx, claim, sessionID, now); err != nil {
			log.Printf("Warning: failed to reap runtime claim %s/%s for session %s: %v", claim.Namespace, claim.Name, sessionID, err)
		}
	}
	return nil
}

func (g *Gateway) reapRuntimeClaim(ctx context.Context, claim *extensionsv1beta1.SandboxClaim, sessionID string, now time.Time) error {
	s, hasSession := g.store.Get(sessionID)
	if !hasSession {
		// A cache miss is not proof the session is gone: cache-backed stores
		// (RedisStore) only serve local entries from Get. Confirm against the
		// durable record before treating the claim as orphaned — releasing a
		// live session's claim destroys its runtime.
		if targeted, ok := g.store.(targetedRecoverableSessionStore); ok {
			record, err := targeted.RecoverSession(ctx, sessionID)
			if err != nil {
				return fmt.Errorf("verify session %s against durable store: %w", sessionID, err)
			}
			if record.found && !record.deleted && record.session != nil {
				log.Printf("Runtime reaper: re-hydrated session %s from durable store for claim %s/%s; skipping orphan release",
					sessionID, claim.Namespace, claim.Name)
				record.session.mu.Lock()
				record.session.Runtime.ClaimName = claim.Name
				record.session.Runtime.Namespace = claim.Namespace
				record.session.Runtime.PoolRef = claim.Spec.WarmPoolRef.Name
				record.session.Runtime.SandboxName = claim.Status.SandboxStatus.Name
				record.session.Runtime.Backend = runtimeBackendSandboxClaim
				if podIP := firstString(claim.Status.SandboxStatus.PodIPs); podIP != "" {
					record.session.Runtime.PodIP = podIP
					record.session.Info.PodIP = podIP
				}
				record.session.mu.Unlock()
				g.store.Set(sessionID, record.session)
				g.store.IncrCount(1)
				s, hasSession = record.session, true
			}
		}
	}
	terminalExpired := claimTerminalExpired(claim, now)
	idleExpired := false
	if !hasSession {
		idleExpired = claimIdleExpired(claim, now, g.gwConfig.IdleTimeout)
	}
	unhealthy, err := g.claimRuntimeUnhealthy(ctx, claim, now)
	if err != nil {
		return err
	}
	if hasSession {
		if atomic.LoadInt32(&s.activeExecs) > 0 {
			return nil
		}
		s.mu.RLock()
		currentAllocation := s.runtimeAllocation()
		s.mu.RUnlock()
		if currentAllocation.ClaimName != "" &&
			(currentAllocation.ClaimName != claim.Name || currentAllocation.Namespace != claim.Namespace) {
			log.Printf("Runtime reaper: releasing stale claim %s/%s for session %s (current claim is %s/%s)",
				claim.Namespace, claim.Name, sessionID, currentAllocation.Namespace, currentAllocation.ClaimName)
			return g.runtimeAllocator.Release(ctx, RuntimeAllocation{
				Backend:     runtimeBackendSandboxClaim,
				PoolRef:     claim.Spec.WarmPoolRef.Name,
				Namespace:   claim.Namespace,
				ClaimName:   claim.Name,
				SandboxName: claim.Status.SandboxStatus.Name,
				PodIP:       firstString(claim.Status.SandboxStatus.PodIPs),
			})
		}
		if !terminalExpired && !unhealthy {
			return nil
		}
		reason := "runtime_finished"
		if unhealthy {
			reason = "runtime_unhealthy"
		}
		return g.deleteSession(ctx, sessionID, reason)
	}

	if !terminalExpired && !idleExpired && !unhealthy {
		return nil
	}

	log.Printf("Runtime reaper: releasing orphaned claim %s/%s (session=%s, terminalExpired=%t, idleExpired=%t, unhealthy=%t)",
		claim.Namespace, claim.Name, sessionID, terminalExpired, idleExpired, unhealthy)
	return g.runtimeAllocator.Release(ctx, RuntimeAllocation{
		Backend:     runtimeBackendSandboxClaim,
		PoolRef:     claim.Spec.WarmPoolRef.Name,
		Namespace:   claim.Namespace,
		ClaimName:   claim.Name,
		SandboxName: claim.Status.SandboxStatus.Name,
		PodIP:       firstString(claim.Status.SandboxStatus.PodIPs),
	})
}

func claimIdleExpired(claim *extensionsv1beta1.SandboxClaim, now time.Time, fallback time.Duration) bool {
	if claim.Spec.Lifecycle != nil && claim.Spec.Lifecycle.ShutdownTime != nil {
		return now.After(claim.Spec.Lifecycle.ShutdownTime.Time.Add(runtimeOrphanGrace))
	}
	idleTimeout, ok := durationAnnotation(claim.Annotations, labels.IdleTimeoutAnnotation)
	if !ok {
		idleTimeout = fallback
	}
	if idleTimeout <= 0 {
		return false
	}
	lastActivity := claim.CreationTimestamp.Time
	if raw := strings.TrimSpace(claim.Annotations[labels.LastActivityAnnotation]); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			lastActivity = parsed
		}
	}
	if lastActivity.IsZero() {
		lastActivity = now
	}
	return now.After(lastActivity.Add(idleTimeout).Add(runtimeOrphanGrace))
}

func claimTerminalExpired(claim *extensionsv1beta1.SandboxClaim, now time.Time) bool {
	finished := meta.FindStatusCondition(claim.Status.Conditions, string(sandboxv1beta1.SandboxConditionFinished))
	if finished == nil || finished.Status != metav1.ConditionTrue {
		return false
	}
	ttl, ok := durationAnnotation(claim.Annotations, labels.FinishedTTLAnnotation)
	if !ok {
		ttl = defaultRuntimeFinishedTTL
	}
	transition := finished.LastTransitionTime.Time
	if transition.IsZero() {
		transition = claim.CreationTimestamp.Time
	}
	if transition.IsZero() {
		transition = now
	}
	return now.After(transition.Add(ttl).Add(runtimeOrphanGrace))
}

func (g *Gateway) claimRuntimeUnhealthy(ctx context.Context, claim *extensionsv1beta1.SandboxClaim, now time.Time) (bool, error) {
	ready := meta.FindStatusCondition(claim.Status.Conditions, string(sandboxv1beta1.SandboxConditionReady))
	if ready == nil || ready.Status != metav1.ConditionFalse {
		return false, nil
	}
	transition := ready.LastTransitionTime.Time
	if transition.IsZero() {
		transition = claim.CreationTimestamp.Time
	}
	if transition.IsZero() || now.Sub(transition) < runtimeNotReadyGrace {
		return false, nil
	}
	pod, err := g.podForClaim(ctx, claim)
	if err != nil || pod == nil {
		return false, err
	}
	return podHasUnrecoverableStatus(pod), nil
}

func (g *Gateway) podForClaim(ctx context.Context, claim *extensionsv1beta1.SandboxClaim) (*corev1.Pod, error) {
	sandboxName := claim.Status.SandboxStatus.Name
	if sandboxName == "" {
		return nil, nil
	}
	sandbox := &sandboxv1beta1.Sandbox{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: claim.Namespace}, sandbox); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get sandbox %s/%s for runtime reaper: %w", claim.Namespace, sandboxName, err)
	}
	podName := sandboxName
	if sandbox.Annotations != nil && sandbox.Annotations[sandboxv1beta1.SandboxPodNameAnnotation] != "" {
		podName = sandbox.Annotations[sandboxv1beta1.SandboxPodNameAnnotation]
	}
	pod := &corev1.Pod{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: claim.Namespace}, pod); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get pod %s/%s for runtime reaper: %w", claim.Namespace, podName, err)
	}
	return pod, nil
}

func podHasUnrecoverableStatus(pod *corev1.Pod) bool {
	if pod.Status.Phase == corev1.PodFailed {
		return true
	}
	for _, status := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
		if status.State.Waiting != nil && unrecoverableWaitingReason(status.State.Waiting.Reason) {
			return true
		}
		if status.LastTerminationState.Terminated != nil && unrecoverableTerminatedReason(status.LastTerminationState.Terminated.Reason) {
			return true
		}
	}
	return false
}

func unrecoverableWaitingReason(reason string) bool {
	switch reason {
	case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "InvalidImageName",
		"CreateContainerConfigError", "CreateContainerError", "RunContainerError":
		return true
	default:
		return false
	}
}

func unrecoverableTerminatedReason(reason string) bool {
	switch reason {
	case "OOMKilled", "Error", "ContainerCannotRun":
		return true
	default:
		return false
	}
}

func durationAnnotation(annotations map[string]string, key string) (time.Duration, bool) {
	raw := strings.TrimSpace(annotations[key])
	if raw == "" {
		return 0, false
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}
