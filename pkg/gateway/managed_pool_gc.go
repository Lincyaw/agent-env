package gateway

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/labels"
)

const defaultManagedPoolGCInterval = 10 * time.Minute

type managedPoolGCCandidate struct {
	pool     extensionsv1beta1.SandboxWarmPool
	lastUsed time.Time
}

// StartManagedPoolGC starts low-frequency LRU cleanup for stopped managed pools.
func (g *Gateway) StartManagedPoolGC() {
	if !g.gwConfig.ManagedPoolGCEnabled || g.k8sClient == nil {
		return
	}
	g.managedPoolGCWg.Add(1)
	go g.managedPoolGCLoop()
}

// StopManagedPoolGC signals the managed pool GC goroutine to exit and waits.
func (g *Gateway) StopManagedPoolGC() {
	if g.managedPoolGCStopCh == nil {
		return
	}
	g.managedPoolGCStopOnce.Do(func() {
		close(g.managedPoolGCStopCh)
	})
	g.managedPoolGCWg.Wait()
}

func (g *Gateway) managedPoolGCLoop() {
	defer g.managedPoolGCWg.Done()

	if deleted, err := g.reconcileManagedPoolGC(context.Background()); err != nil {
		log.Printf("managed pool GC reconcile failed: %v", err)
	} else if deleted > 0 {
		log.Printf("managed pool GC deleted %d stopped managed pool(s)", deleted)
	}

	interval := g.gwConfig.ManagedPoolGCInterval
	if interval <= 0 {
		interval = defaultManagedPoolGCInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-g.managedPoolGCStopCh:
			return
		case <-ticker.C:
			if deleted, err := g.reconcileManagedPoolGC(context.Background()); err != nil {
				log.Printf("managed pool GC reconcile failed: %v", err)
			} else if deleted > 0 {
				log.Printf("managed pool GC deleted %d stopped managed pool(s)", deleted)
			}
		}
	}
}

func (g *Gateway) reconcileManagedPoolGC(ctx context.Context) (int, error) {
	if !g.gwConfig.ManagedPoolGCEnabled || g.k8sClient == nil {
		return 0, nil
	}
	namespace := g.runtimeNamespace()
	references, err := g.managedPoolReferences(ctx, namespace)
	if err != nil {
		return 0, err
	}

	g.stopIdleRunningManagedPools(ctx, namespace, references)

	var pools extensionsv1beta1.SandboxWarmPoolList
	if err := g.k8sClient.List(ctx, &pools,
		client.InNamespace(namespace),
		client.MatchingLabels{
			labels.ManagedPoolLabelKey: "true",
			labels.PoolStateLabelKey:   labels.PoolStateStopped,
		},
	); err != nil {
		return 0, fmt.Errorf("list stopped managed pools for GC: %w", err)
	}

	now := time.Now()
	candidates := make([]managedPoolGCCandidate, 0, len(pools.Items))
	for i := range pools.Items {
		pool := &pools.Items[i]
		key := types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}
		if _, inUse := references[key]; inUse {
			continue
		}
		if !isManagedPool(pool) || !poolLifecycleStopped(pool) {
			continue
		}
		candidates = append(candidates, managedPoolGCCandidate{
			pool:     *pool.DeepCopy(),
			lastUsed: managedPoolLastUsedAt(pool, now),
		})
	}

	maxStopped := g.gwConfig.ManagedPoolGCMaxStopped
	if len(candidates) <= maxStopped {
		return 0, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].lastUsed.Equal(candidates[j].lastUsed) {
			if candidates[i].pool.CreationTimestamp.Equal(&candidates[j].pool.CreationTimestamp) {
				return candidates[i].pool.Name < candidates[j].pool.Name
			}
			return candidates[i].pool.CreationTimestamp.Before(&candidates[j].pool.CreationTimestamp)
		}
		return candidates[i].lastUsed.Before(candidates[j].lastUsed)
	})

	excess := len(candidates) - maxStopped
	minIdleAge := g.gwConfig.ManagedPoolGCMinIdleAge
	deleted := 0
	for _, candidate := range candidates {
		if deleted >= excess {
			break
		}
		if minIdleAge > 0 && now.Sub(candidate.lastUsed) < minIdleAge {
			continue
		}
		removed, err := g.deleteManagedPoolGCCandidate(ctx, candidate.pool.Namespace, candidate.pool.Name)
		if err != nil {
			return deleted, err
		}
		if removed {
			deleted++
		}
	}
	return deleted, nil
}

// stopIdleRunningManagedPools is the safety net for pools left warming after
// wait-style create failures: a running managed pool with no bindings, no
// queued admission waiters, and no activity for MinIdleAge is marked stopped
// so it flows into the stopped-pool LRU above.
func (g *Gateway) stopIdleRunningManagedPools(ctx context.Context, namespace string, references map[types.NamespacedName]struct{}) {
	minIdle := g.gwConfig.ManagedPoolGCMinIdleAge
	if minIdle <= 0 {
		return
	}
	var pools extensionsv1beta1.SandboxWarmPoolList
	if err := g.k8sClient.List(ctx, &pools,
		client.InNamespace(namespace),
		client.MatchingLabels{labels.ManagedPoolLabelKey: "true"},
	); err != nil {
		log.Printf("managed pool GC: list managed pools for idle check failed: %v", err)
		return
	}
	queued := g.admissionQueueSnapshot()
	now := time.Now()
	for i := range pools.Items {
		pool := &pools.Items[i]
		if pool.DeletionTimestamp != nil || !isManagedPool(pool) || poolLifecycleStopped(pool) {
			continue
		}
		key := types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}
		if _, inUse := references[key]; inUse {
			continue
		}
		if queued[key] > 0 {
			continue
		}
		if now.Sub(managedPoolLastUsedAt(pool, now)) < minIdle {
			continue
		}
		if stopped, err := g.stopManagedPoolIfUnused(ctx, pool.Name, pool.Namespace); err != nil {
			log.Printf("managed pool GC: stop idle running pool %s/%s failed: %v", pool.Namespace, pool.Name, err)
		} else if stopped {
			log.Printf("managed pool GC stopped idle running pool %s/%s (idle > %s)", pool.Namespace, pool.Name, minIdle)
		}
	}
}

func (g *Gateway) managedPoolReferences(ctx context.Context, namespace string) (map[types.NamespacedName]struct{}, error) {
	references := make(map[types.NamespacedName]struct{})
	if g.store != nil {
		g.store.Range(func(_ string, s *session) bool {
			s.mu.RLock()
			closed := s.closed
			allocation := s.runtimeAllocation()
			s.mu.RUnlock()
			if closed || strings.TrimSpace(allocation.PoolRef) == "" {
				return true
			}
			refNamespace := allocation.Namespace
			if refNamespace == "" {
				refNamespace = namespace
			}
			if refNamespace != namespace {
				return true
			}
			references[types.NamespacedName{Name: allocation.PoolRef, Namespace: refNamespace}] = struct{}{}
			return true
		})
	}

	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("list sandbox claims for managed pool GC: %w", err)
	}
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.DeletionTimestamp != nil || strings.TrimSpace(claim.Spec.WarmPoolRef.Name) == "" {
			continue
		}
		references[types.NamespacedName{Name: claim.Spec.WarmPoolRef.Name, Namespace: claim.Namespace}] = struct{}{}
	}
	return references, nil
}

func managedPoolLastUsedAt(pool *extensionsv1beta1.SandboxWarmPool, now time.Time) time.Time {
	if pool == nil {
		return now
	}
	if value := strings.TrimSpace(pool.Annotations[labels.PoolLastUsedAnnotation]); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed
		}
	}
	if !pool.CreationTimestamp.IsZero() {
		return pool.CreationTimestamp.Time
	}
	return now
}

func (g *Gateway) deleteManagedPoolGCCandidate(ctx context.Context, namespace, poolName string) (bool, error) {
	g.poolStopMu.Lock()
	defer g.poolStopMu.Unlock()

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolName, Namespace: namespace}, pool); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get managed pool %s/%s for GC: %w", namespace, poolName, err)
	}
	if !isManagedPool(pool) || !poolLifecycleStopped(pool) {
		return false, nil
	}
	inUse, err := g.poolHasActiveBindings(ctx, poolName, namespace)
	if err != nil {
		return false, err
	}
	if inUse {
		return false, nil
	}

	templateName := pool.Spec.TemplateRef.Name
	// Pool GC removes only gateway-owned control-plane objects. Image cache
	// cleanup is a separate concern and must not be coupled to pool eviction.
	if err := g.k8sClient.Delete(ctx, pool, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !errors.IsNotFound(err) {
		return false, fmt.Errorf("delete stopped managed pool %s/%s: %w", namespace, poolName, err)
	}
	if g.poolIndex != nil {
		g.poolIndex.deletePool(pool)
	}
	if templateName == "" {
		return true, nil
	}
	if err := g.deletePoolTemplateIfOwned(ctx, templateName, poolName, namespace); err != nil {
		return true, err
	}
	return true, nil
}
