package gateway

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/labels"
)

// RecoverSessions rebuilds the active in-memory session cache after a gateway
// restart. Kubernetes SandboxClaims are the source of truth for active sessions;
// durable store records are loaded only for those live runtimes.
func (g *Gateway) RecoverSessions(ctx context.Context) (int, error) {
	if g.k8sClient != nil && g.runtimeAllocator != nil {
		return g.recoverSessionsFromRuntimeBindings(ctx)
	}
	return g.recoverSessionsFromDurableStore(ctx)
}

func (g *Gateway) recoverSessionsFromDurableStore(ctx context.Context) (int, error) {
	recoverable, ok := g.store.(RecoverableSessionStore)
	if !ok {
		return 0, nil
	}
	recovered, err := recoverable.RecoverActiveSessions(ctx)
	if err != nil {
		return 0, err
	}

	active := 0
	for sessionID, s := range recovered {
		if err := ctx.Err(); err != nil {
			return active, err
		}
		if s == nil {
			continue
		}

		s.mu.RLock()
		closed := s.closed
		allocation := s.runtimeAllocation()
		s.mu.RUnlock()
		if closed {
			g.markSessionDeleted(s, "recovery_closed")
			g.store.Delete(sessionID)
			continue
		}

		if g.runtimeAllocator != nil {
			resolved, err := g.runtimeAllocator.Resolve(ctx, allocation, sessionID)
			if err != nil {
				log.Printf("Warning: dropping recovered session %s: %v", sessionID, err)
				if releaseErr := g.runtimeAllocator.Release(ctx, allocation); releaseErr != nil && !errors.IsNotFound(releaseErr) {
					log.Printf("Warning: failed to release runtime for unrecoverable session %s: %v", sessionID, releaseErr)
				}
				g.markSessionDeleted(s, "recovery_runtime_lost")
				g.store.Delete(sessionID)
				if g.metrics != nil {
					g.metrics.IncrementSessionDeletion("recovery_runtime_lost")
				}
				continue
			}
			s.mu.Lock()
			s.Runtime = *resolved
			if resolved.Namespace != "" {
				s.Info.Namespace = resolved.Namespace
			}
			if resolved.PoolRef != "" {
				s.Info.PoolRef = resolved.PoolRef
			}
			if resolved.PodName != "" {
				s.Info.PodName = resolved.PodName
			}
			if resolved.PodIP != "" {
				s.Info.PodIP = resolved.PodIP
			}
			if resolved.SandboxName != "" {
				s.Info.SandboxName = resolved.SandboxName
			}
			s.mu.Unlock()
		}

		g.store.Set(sessionID, s)
		active++
	}

	if counter, ok := g.store.(SessionCountSetter); ok {
		counter.SetCount(int64(active))
	}
	if g.metrics != nil {
		if _, ok := g.store.(SessionCountSetter); ok {
			g.metrics.SetActiveSessions(int64(active))
		} else {
			g.metrics.SetActiveSessions(g.store.Count())
		}
	}
	return active, nil
}

func (g *Gateway) recoverSessionsFromRuntimeBindings(ctx context.Context) (int, error) {
	if g.k8sClient == nil || g.runtimeAllocator == nil {
		return 0, nil
	}
	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(g.runtimeNamespace())); err != nil {
		return 0, fmt.Errorf("list sandbox claims for recovery: %w", err)
	}

	active := 0
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.DeletionTimestamp != nil {
			continue
		}
		sessionID := strings.TrimSpace(claim.Annotations[labels.SessionAnnotation])
		if sessionID == "" {
			continue
		}
		poolRef := claim.Spec.WarmPoolRef.Name
		allocation := RuntimeAllocation{
			Backend:     runtimeBackendSandboxClaim,
			PoolRef:     poolRef,
			Namespace:   claim.Namespace,
			ClaimName:   claim.Name,
			SandboxName: claim.Status.SandboxStatus.Name,
			PodIP:       firstString(claim.Status.SandboxStatus.PodIPs),
		}
		resolved, err := g.runtimeAllocator.Resolve(ctx, allocation, sessionID)
		if err != nil {
			log.Printf("Warning: cannot recover session %s from claim %s/%s: %v", sessionID, claim.Namespace, claim.Name, err)
			continue
		}

		s, ok, err := g.recoverSessionFromLiveClaim(ctx, sessionID, *resolved, claim)
		if err != nil {
			return active, err
		}
		if !ok {
			continue
		}

		g.store.Set(sessionID, s)
		active++
	}
	if counter, ok := g.store.(SessionCountSetter); ok {
		counter.SetCount(int64(active))
	} else if active > 0 {
		g.store.IncrCount(int64(active))
	}
	if g.metrics != nil {
		g.metrics.SetActiveSessions(g.store.Count())
	}
	return active, nil
}

func (g *Gateway) recoverSessionFromLiveClaim(ctx context.Context, sessionID string, resolved RuntimeAllocation, claim *extensionsv1beta1.SandboxClaim) (*session, bool, error) {
	var s *session
	if targeted, ok := g.store.(targetedRecoverableSessionStore); ok {
		record, err := targeted.RecoverSession(ctx, sessionID)
		if err != nil {
			return nil, false, fmt.Errorf("recover session %s from durable store: %w", sessionID, err)
		}
		if record.deleted {
			log.Printf("Warning: skipping live claim %s/%s for deleted session %s", claim.Namespace, claim.Name, sessionID)
			if releaseErr := g.runtimeAllocator.Release(ctx, resolved); releaseErr != nil && !errors.IsNotFound(releaseErr) {
				log.Printf("Warning: failed to release live claim for deleted session %s: %v", sessionID, releaseErr)
			}
			return nil, false, nil
		}
		s = record.session
	} else if existing, ok := g.store.Get(sessionID); ok {
		s = existing
	}

	if s == nil {
		s = g.newRecoveredSessionFromClaim(ctx, sessionID, resolved, claim)
	}
	if recoveredSessionClosed(s) {
		g.markSessionDeleted(s, "recovery_closed")
		g.store.Delete(sessionID)
		return nil, false, nil
	}

	g.applyRecoveredRuntime(ctx, s, sessionID, resolved, claim)
	return s, true, nil
}

func recoveredSessionClosed(s *session) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

func (g *Gateway) newRecoveredSessionFromClaim(ctx context.Context, sessionID string, resolved RuntimeAllocation, claim *extensionsv1beta1.SandboxClaim) *session {
	info := g.recoveredSessionInfo(ctx, sessionID, resolved, claim)
	lastTask := recoveredLastActivity(claim, info.CreatedAt)
	managed := strings.EqualFold(claim.Annotations[labels.ManagedAnnotation], "true")
	recoveredMode := claim.Annotations[labels.ModeAnnotation]
	idleTimeout := g.gwConfig.IdleTimeout
	if recoveredMode == SessionModeDevbox {
		idleTimeout = g.gwConfig.DevboxIdleTimeout
	}
	info.Mode = recoveredMode
	return &session{
		Info:         info,
		Runtime:      resolved,
		History:      NewStepHistory(),
		managed:      managed,
		experimentID: claim.Annotations[labels.ExperimentAnnotation],
		mode:         recoveredMode,
		ownerKeyHash: claim.Annotations[labels.OwnerKeyHashAnnotation],
		lastTaskTime: lastTask,
		createdAt:    info.CreatedAt,
		idleTimeout:  idleTimeout,
		operations:   make(map[string]*executeOperation),
	}
}

func (g *Gateway) applyRecoveredRuntime(ctx context.Context, s *session, sessionID string, resolved RuntimeAllocation, claim *extensionsv1beta1.SandboxClaim) {
	info := g.recoveredSessionInfo(ctx, sessionID, resolved, claim)
	lastTask := recoveredLastActivity(claim, info.CreatedAt)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.Runtime = resolved
	s.Info.ID = sessionID
	s.Info.Namespace = resolved.Namespace
	s.Info.PoolRef = resolved.PoolRef
	s.Info.PodName = resolved.PodName
	s.Info.PodIP = resolved.PodIP
	s.Info.SandboxName = resolved.SandboxName
	s.Info.Status = "active"
	if s.Info.CreatedAt.IsZero() {
		s.Info.CreatedAt = info.CreatedAt
	}
	if info.Image != "" {
		s.Info.Image = info.Image
	}
	if info.Profile != "" {
		s.Info.Profile = info.Profile
	}
	if s.History == nil {
		s.History = NewStepHistory()
	}
	if s.operations == nil {
		s.operations = make(map[string]*executeOperation)
	}
	if s.createdAt.IsZero() {
		s.createdAt = s.Info.CreatedAt
	}
	if s.lastTaskTime.IsZero() {
		s.lastTaskTime = lastTask
	}
	if s.idleTimeout == 0 {
		s.idleTimeout = g.gwConfig.IdleTimeout
	}
}

func recoveredLastActivity(claim *extensionsv1beta1.SandboxClaim, fallback time.Time) time.Time {
	if raw := strings.TrimSpace(claim.Annotations[labels.LastActivityAnnotation]); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			return parsed
		}
	}
	return fallback
}

func (g *Gateway) recoveredSessionInfo(ctx context.Context, sessionID string, allocation RuntimeAllocation, claim *extensionsv1beta1.SandboxClaim) SessionInfo {
	createdAt := claim.CreationTimestamp.Time
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	info := SessionInfo{
		ID:          sessionID,
		SandboxName: allocation.SandboxName,
		Namespace:   allocation.Namespace,
		PoolRef:     allocation.PoolRef,
		PodIP:       allocation.PodIP,
		PodName:     allocation.PodName,
		CreatedAt:   createdAt,
		Status:      "active",
	}
	if allocation.PoolRef == "" || g.k8sClient == nil {
		return info
	}
	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: allocation.PoolRef, Namespace: allocation.Namespace}, pool); err != nil {
		return info
	}
	poolInfo := g.poolInfoFromSandboxWarmPool(ctx, pool)
	info.Image = poolInfo.Image
	info.Profile = poolInfo.Profile
	return info
}
