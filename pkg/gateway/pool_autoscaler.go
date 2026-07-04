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
		activeClaims := claimCounts[key]
		queuedRequests := queuedCounts[key]
		target := g.poolAutoscaleTarget(queuedRequests)
		current := desiredSandboxWarmPoolReplicas(pool)
		g.publishPoolMetrics(pool, activeClaims, queuedRequests, current)
		if target == current {
			continue
		}

		before := pool.DeepCopy()
		pool.Spec.Replicas = int32Ptr(target)
		if err := g.k8sClient.Patch(ctx, pool, client.MergeFrom(before)); err != nil {
			return fmt.Errorf("scale sandbox warm pool %s/%s from %d to %d: %w", pool.Namespace, pool.Name, current, target, err)
		}
		g.publishPoolMetrics(pool, activeClaims, queuedRequests, target)
	}
	return nil
}

func (g *Gateway) publishCurrentPoolMetrics(ctx context.Context) error {
	if g.metrics == nil || g.k8sClient == nil {
		return nil
	}
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
		key := types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}
		g.publishPoolMetrics(pool, claimCounts[key], queuedCounts[key], desiredSandboxWarmPoolReplicas(pool))
	}
	return nil
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

func (g *Gateway) publishPoolMetrics(pool *v1beta1.SandboxWarmPool, allocated, queued, desired int32) {
	if g.metrics == nil || pool == nil {
		return
	}
	label := poolMetricLabel(pool.Namespace, pool.Name)
	g.metrics.SetPoolDesiredReplicas(label, int(desired))
	g.metrics.SetPoolReadyReplicas(label, int(pool.Status.ReadyReplicas))
	g.metrics.SetPoolAllocatedReplicas(label, int(allocated))
	g.metrics.SetAdmissionQueueDepth(label, int(queued))
	saturation := 0.0
	if desired > 0 {
		saturation = float64(allocated) / float64(desired)
	}
	g.metrics.SetPoolSaturation(label, saturation)
}

func poolMetricLabel(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}
