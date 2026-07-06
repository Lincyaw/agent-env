package gateway

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/labels"
	"github.com/Lincyaw/agent-env/pkg/scheduling"
)

const defaultPoolAutoscalerInterval = 30 * time.Second

// StartPoolAutoscaler starts the warm-pool sizing control loop when enabled.
func (g *Gateway) StartPoolAutoscaler() {
	if !g.gwConfig.PoolAutoscalerEnabled {
		return
	}
	g.autoscaleWg.Add(1)
	go g.poolAutoscaleLoop()
}

// StopPoolAutoscaler signals the autoscaler goroutine to exit and waits.
func (g *Gateway) StopPoolAutoscaler() {
	if g.autoscaleStopCh == nil {
		return
	}
	g.autoscaleStopOnce.Do(func() {
		close(g.autoscaleStopCh)
	})
	g.autoscaleWg.Wait()
}

func (g *Gateway) poolAutoscaleLoop() {
	defer g.autoscaleWg.Done()

	if err := g.reconcilePoolAutoscaling(context.Background()); err != nil {
		log.Printf("pool autoscaler reconcile failed: %v", err)
	}

	interval := g.gwConfig.PoolAutoscalerInterval
	if interval <= 0 {
		interval = defaultPoolAutoscalerInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-g.autoscaleStopCh:
			return
		case <-ticker.C:
			if err := g.reconcilePoolAutoscaling(context.Background()); err != nil {
				log.Printf("pool autoscaler reconcile failed: %v", err)
			}
		}
	}
}

func (g *Gateway) reconcilePoolAutoscaling(ctx context.Context) error {
	namespace := g.runtimeNamespace()
	var pools v1beta1.SandboxWarmPoolList
	if err := g.k8sClient.List(ctx, &pools, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list sandbox warm pools: %w", err)
	}

	claimCounts, err := g.activeClaimCountsByPool(ctx)
	if err != nil {
		return err
	}
	queuedCounts := g.admissionQueueSnapshot()

	for i := range pools.Items {
		pool := &pools.Items[i]
		if poolAutoscalingDisabled(pool) {
			continue
		}

		key := types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}
		queuedRequests := queuedCounts[key]
		target := g.poolAutoscaleTarget(queuedRequests)
		current := desiredSandboxWarmPoolReplicas(pool)
		if target == current {
			continue
		}

		before := pool.DeepCopy()
		pool.Spec.Replicas = int32Ptr(target)
		if err := g.k8sClient.Patch(ctx, pool, client.MergeFrom(before)); err != nil {
			return fmt.Errorf("scale sandbox warm pool %s/%s from %d to %d: %w", pool.Namespace, pool.Name, current, target, err)
		}
	}
	g.publishWarmPoolAggregateMetrics(pools.Items, claimCounts, queuedCounts)
	return nil
}

func (g *Gateway) publishCurrentPoolMetrics(ctx context.Context) error {
	if g.metrics == nil {
		return nil
	}
	namespace := g.runtimeNamespace()
	if readModel, ok := g.syncedPoolReadModel(); ok {
		queuedCounts := g.admissionQueueSnapshot()
		g.publishPoolAggregateMetrics(readModel.ListPools(PoolListOptions{Namespace: namespace}), queuedCounts)
		return nil
	}
	if g.k8sClient == nil {
		return nil
	}
	var pools v1beta1.SandboxWarmPoolList
	if err := g.k8sClient.List(ctx, &pools, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list sandbox warm pools: %w", err)
	}
	claimCounts, err := g.activeClaimCountsByPool(ctx)
	if err != nil {
		return err
	}
	queuedCounts := g.admissionQueueSnapshot()
	g.publishWarmPoolAggregateMetrics(pools.Items, claimCounts, queuedCounts)
	return nil
}

func (g *Gateway) allocatorDiagnosticStats() map[string]AllocatorPoolStats {
	if readModel, ok := g.syncedPoolReadModel(); ok {
		pools := readModel.ListPools(PoolListOptions{Namespace: g.runtimeNamespace()})
		stats := make(map[string]AllocatorPoolStats, len(pools))
		for _, pool := range pools {
			ready := pool.ReadyReplicas
			if ready < 0 {
				ready = 0
			}
			stats[poolMetricLabel(pool.Namespace, pool.Name)] = AllocatorPoolStats{
				IdleCount: int(ready),
			}
		}
		return stats
	}
	if g.runtimeAllocator == nil {
		return nil
	}
	return g.runtimeAllocator.DiagnosticStats()
}

func (g *Gateway) activeClaimCountsByPool(ctx context.Context) (map[types.NamespacedName]int32, error) {
	namespace := g.runtimeNamespace()
	var claims v1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("list sandbox claims: %w", err)
	}

	counts := make(map[types.NamespacedName]int32)
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.DeletionTimestamp != nil || claim.Spec.WarmPoolRef.Name == "" {
			continue
		}
		key := types.NamespacedName{Name: claim.Spec.WarmPoolRef.Name, Namespace: claim.Namespace}
		counts[key]++
	}
	return counts, nil
}

func (g *Gateway) poolAutoscaleTarget(queuedRequests int32) int32 {
	buffer := g.gwConfig.PoolAutoscalerBuffer
	if buffer < 0 {
		buffer = 0
	}
	minReplicas := g.gwConfig.PoolAutoscalerMinReplicas
	if minReplicas < 0 {
		minReplicas = 0
	}
	maxReplicas := g.gwConfig.PoolAutoscalerMaxReplicas
	if maxReplicas < 0 {
		maxReplicas = 0
	}

	if queuedRequests < 0 {
		queuedRequests = 0
	}

	target := queuedRequests + buffer
	if target < minReplicas {
		target = minReplicas
	}
	if maxReplicas > 0 && target > maxReplicas {
		target = maxReplicas
	}
	return target
}

func poolAutoscalingDisabled(pool *v1beta1.SandboxWarmPool) bool {
	state := strings.ToLower(strings.TrimSpace(pool.Annotations[labels.PoolStateAnnotation]))
	if state == labels.PoolStateStopped || state == labels.PoolStateDraining {
		return true
	}
	value := strings.ToLower(strings.TrimSpace(pool.Annotations[scheduling.PoolAutoscaleAnnotation]))
	return value == "false" || value == "disabled" || value == "off"
}

type poolMetricAggregate struct {
	desired   int
	ready     int
	allocated int
	queued    int
}

func (g *Gateway) publishWarmPoolAggregateMetrics(
	pools []v1beta1.SandboxWarmPool,
	claimCounts map[types.NamespacedName]int32,
	queuedCounts map[types.NamespacedName]int32,
) {
	if g.metrics == nil {
		return
	}
	infos := make([]PoolInfo, 0, len(pools))
	for i := range pools {
		pool := &pools[i]
		key := types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}
		infos = append(infos, PoolInfo{
			Name:              pool.Name,
			Namespace:         pool.Namespace,
			Profile:           firstNonEmpty(profileFromObjectMeta(pool.ObjectMeta), defaultPoolProfile),
			Replicas:          desiredSandboxWarmPoolReplicas(pool),
			ReadyReplicas:     pool.Status.ReadyReplicas,
			AllocatedReplicas: claimCounts[key],
			State:             firstNonEmpty(pool.Annotations[labels.PoolStateAnnotation], labels.PoolStateRunning),
		})
	}
	g.publishPoolAggregateMetrics(infos, queuedCounts)
}

func (g *Gateway) publishPoolAggregateMetrics(pools []PoolInfo, queuedCounts map[types.NamespacedName]int32) {
	if g.metrics == nil {
		return
	}
	g.metrics.ResetPoolAggregateMetrics()
	aggregates := make(map[string]*poolMetricAggregate)
	for _, pool := range pools {
		profile := firstNonEmpty(pool.Profile, defaultPoolProfile)
		state := strings.ToLower(firstNonEmpty(pool.State, labels.PoolStateRunning))
		key := profile + "\x00" + state
		aggregate := aggregates[key]
		if aggregate == nil {
			aggregate = &poolMetricAggregate{}
			aggregates[key] = aggregate
		}
		aggregate.desired += int(pool.Replicas)
		aggregate.ready += int(pool.ReadyReplicas)
		aggregate.allocated += int(pool.AllocatedReplicas)
		aggregate.queued += int(queuedCounts[types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}])
	}
	for key, aggregate := range aggregates {
		parts := strings.SplitN(key, "\x00", 2)
		profile := parts[0]
		state := labels.PoolStateRunning
		if len(parts) == 2 {
			state = parts[1]
		}
		saturation := 0.0
		if aggregate.desired > 0 {
			saturation = float64(aggregate.allocated) / float64(aggregate.desired)
		}
		g.metrics.SetPoolAggregateMetrics(
			profile,
			state,
			aggregate.desired,
			aggregate.ready,
			aggregate.allocated,
			aggregate.queued,
			saturation,
		)
	}
}

func poolMetricLabel(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}
