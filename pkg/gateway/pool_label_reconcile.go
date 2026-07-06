package gateway

import (
	"context"
	"fmt"
	"strings"

	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/labels"
)

// ReconcilePoolMetadataLabels backfills label copies of low-frequency pool
// metadata annotations. It intentionally does not infer missing state for
// legacy pools; incorrect stopped/running labels would be worse than a missing
// label during migration.
func (g *Gateway) ReconcilePoolMetadataLabels(ctx context.Context) (int, error) {
	namespace := g.runtimeNamespace()
	var poolList extensionsv1beta1.SandboxWarmPoolList
	if err := g.k8sClient.List(ctx, &poolList, client.InNamespace(namespace)); err != nil {
		return 0, fmt.Errorf("list sandbox warm pools for metadata label reconciliation: %w", err)
	}

	patched := 0
	for i := range poolList.Items {
		pool := &poolList.Items[i]
		before := pool.DeepCopy()
		reconcilePoolObjectLabels(pool)
		if stringMapEqual(before.Labels, pool.Labels) {
			continue
		}
		if err := g.k8sClient.Patch(ctx, pool, client.MergeFrom(before)); err != nil {
			return patched, fmt.Errorf("patch sandbox warm pool %s/%s metadata labels: %w", pool.Namespace, pool.Name, err)
		}
		patched++
	}
	return patched, nil
}

func reconcilePoolObjectLabels(pool *extensionsv1beta1.SandboxWarmPool) {
	if pool == nil {
		return
	}
	if profile := strings.TrimSpace(pool.Annotations[labels.PoolProfileAnnotation]); profile != "" {
		setLabelIfValid(&pool.ObjectMeta, labels.PoolProfileLabelKey, profile)
	}
	if managed := strings.TrimSpace(pool.Annotations[labels.ManagedPoolAnnotation]); strings.EqualFold(managed, "true") {
		setLabelIfValid(&pool.ObjectMeta, labels.ManagedPoolLabelKey, "true")
	}
	if state := strings.TrimSpace(pool.Annotations[labels.PoolStateAnnotation]); state != "" {
		setLabelIfValid(&pool.ObjectMeta, labels.PoolStateLabelKey, state)
	}
}

func stringMapEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		if right[key] != leftValue {
			return false
		}
	}
	return true
}
