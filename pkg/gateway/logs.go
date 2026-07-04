package gateway

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// StreamSessionLogs streams log entries from a session's sidecar to a channel.
func (g *Gateway) StreamSessionLogs(ctx context.Context, sessionID string, follow bool, tailLines int32) (<-chan interfaces.LogEntry, error) {
	_, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	source, err := g.sidecarClient.StreamLogs(ctx, podIP, follow, tailLines)
	if err != nil {
		releaseSession()
		return nil, err
	}
	out := make(chan interfaces.LogEntry, 128)
	go func() {
		defer releaseSession()
		defer close(out)
		for entry := range source {
			select {
			case out <- entry:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// StreamPoolLogs fans out log streaming to all pods in a pool and merges output.
func (g *Gateway) StreamPoolLogs(ctx context.Context, poolName, namespace string, follow bool, tailLines int32) (<-chan PoolLogEntry, error) {
	namespace, err := g.resolveNamespace(namespace)
	if err != nil {
		return nil, err
	}

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolName, Namespace: namespace}, pool); err != nil {
		return nil, fmt.Errorf("get pool: %w", err)
	}

	var podList corev1.PodList
	if pool.Status.Selector == "" {
		return nil, fmt.Errorf("pool %s/%s has no pod selector yet", namespace, poolName)
	}
	selector, err := k8slabels.Parse(pool.Status.Selector)
	if err != nil {
		return nil, fmt.Errorf("parse pool selector: %w", err)
	}
	opts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: selector},
	}
	if err := g.k8sClient.List(ctx, &podList, opts...); err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	merged := make(chan PoolLogEntry, 256)
	var wg sync.WaitGroup

	for _, pod := range podList.Items {
		if pod.Status.PodIP == "" {
			continue
		}
		podName := pod.Name
		podIP := pod.Status.PodIP

		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, err := g.sidecarClient.StreamLogs(ctx, podIP, follow, tailLines)
			if err != nil {
				merged <- PoolLogEntry{PodName: podName, Entry: interfaces.LogEntry{
					Level: "error", Message: fmt.Sprintf("connect: %v", err), Source: "gateway",
				}}
				return
			}
			for entry := range ch {
				select {
				case merged <- PoolLogEntry{PodName: podName, Entry: entry}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(merged)
	}()

	return merged, nil
}

// PoolLogEntry wraps a LogEntry with the source pod name.
type PoolLogEntry struct {
	PodName string              `json:"podName"`
	Entry   interfaces.LogEntry `json:"entry"`
}
