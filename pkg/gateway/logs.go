package gateway

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	sandboxcontrollers "sigs.k8s.io/agent-sandbox/controllers"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// maxPoolLogPods bounds the fan-in when streaming a whole pool, so a large
// pool cannot open an unbounded number of API server connections.
const maxPoolLogPods = 10

// StreamSessionLogs streams the executor container logs of the pod backing a
// session. Command stdout/stderr is not part of this stream — that is captured
// by the executor agent and recorded in the trajectory. What surfaces here is
// the executor agent's own logging plus anything the user image writes to the
// container's stdout.
func (g *Gateway) StreamSessionLogs(ctx context.Context, sessionID string, follow bool, tail int32) (<-chan interfaces.LogEntry, error) {
	if g.k8sClientset == nil {
		return nil, fmt.Errorf("log streaming is unavailable: gateway has no Kubernetes REST client")
	}

	s, ok := g.store.Get(sessionID)
	if !ok {
		s, ok = g.store.GetHistorical(sessionID)
	}
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	podName := s.Info.PodName
	if podName == "" {
		return nil, fmt.Errorf("session %s has no pod bound yet", sessionID)
	}
	namespace := s.Info.Namespace
	if namespace == "" {
		namespace = g.runtimeNamespace()
	}

	out := make(chan interfaces.LogEntry, 64)
	go func() {
		defer close(out)
		g.pipePodLogs(ctx, podName, namespace, follow, tail, func(entry interfaces.LogEntry) bool {
			select {
			case out <- entry:
				return true
			case <-ctx.Done():
				return false
			}
		})
	}()
	return out, nil
}

// StreamPoolLogs fans in the executor logs of every warm pod in a pool. Each
// entry carries its source pod name so a multi-replica pool stays readable.
func (g *Gateway) StreamPoolLogs(ctx context.Context, poolRef, namespace string, follow bool, tail int32) (<-chan PoolLogEntry, error) {
	if g.k8sClientset == nil || g.k8sClient == nil {
		return nil, fmt.Errorf("log streaming is unavailable: gateway has no Kubernetes REST client")
	}

	ns, err := g.resolveNamespace(namespace)
	if err != nil {
		return nil, err
	}

	var pods corev1.PodList
	if err := g.k8sClient.List(ctx, &pods,
		client.InNamespace(ns),
		client.MatchingLabels{sandboxv1beta1.SandboxWarmPoolLabel: sandboxcontrollers.NameHash(poolRef)},
	); err != nil {
		return nil, fmt.Errorf("list pods for pool %s: %w", poolRef, err)
	}

	names := make([]string, 0, len(pods.Items))
	for i := range pods.Items {
		if pods.Items[i].DeletionTimestamp == nil {
			names = append(names, pods.Items[i].Name)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no running pods found for pool %s", poolRef)
	}
	truncated := 0
	if len(names) > maxPoolLogPods {
		truncated = len(names) - maxPoolLogPods
		names = names[:maxPoolLogPods]
	}

	out := make(chan PoolLogEntry, 128)
	var wg sync.WaitGroup
	for _, podName := range names {
		wg.Add(1)
		go func(podName string) {
			defer wg.Done()
			g.pipePodLogs(ctx, podName, ns, follow, tail, func(entry interfaces.LogEntry) bool {
				select {
				case out <- PoolLogEntry{PodName: podName, Entry: entry}:
					return true
				case <-ctx.Done():
					return false
				}
			})
		}(podName)
	}
	if truncated > 0 {
		out <- PoolLogEntry{
			PodName: poolRef,
			Entry: interfaces.LogEntry{
				Level:   "warn",
				Message: fmt.Sprintf("pool has more pods than the streaming limit; %d pod(s) omitted", truncated),
				Source:  "gateway",
			},
		}
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out, nil
}

// pipePodLogs reads the executor container log stream of one pod and hands each
// line to emit. It returns when the stream ends, the context is cancelled, or
// emit reports that the consumer is gone.
func (g *Gateway) pipePodLogs(
	ctx context.Context,
	podName, namespace string,
	follow bool,
	tail int32,
	emit func(interfaces.LogEntry) bool,
) {
	opts := &corev1.PodLogOptions{
		Container:  executorContainerName,
		Follow:     follow,
		Timestamps: true,
	}
	if tail > 0 {
		lines := int64(tail)
		opts.TailLines = &lines
	}

	stream, err := g.k8sClientset.CoreV1().Pods(namespace).GetLogs(podName, opts).Stream(ctx)
	if err != nil {
		emit(interfaces.LogEntry{
			Level:   "error",
			Message: fmt.Sprintf("open log stream for pod %s: %v", podName, err),
			Source:  "gateway",
		})
		return
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		if !emit(parseLogLine(scanner.Text())) {
			return
		}
	}
	if err := scanner.Err(); err != nil && !isStreamClosed(err) {
		emit(interfaces.LogEntry{
			Level:   "error",
			Message: fmt.Sprintf("read log stream for pod %s: %v", podName, err),
			Source:  "gateway",
		})
	}
}

// parseLogLine splits the RFC3339 timestamp prefix that the API server prepends
// when Timestamps is set, and infers a level from the remaining text so clients
// can colour or filter without re-parsing.
func parseLogLine(line string) interfaces.LogEntry {
	entry := interfaces.LogEntry{Source: executorContainerName, Message: line}
	if ts, rest, ok := strings.Cut(line, " "); ok && strings.Contains(ts, "T") {
		entry.Timestamp = ts
		entry.Message = rest
	}
	entry.Level = inferLogLevel(entry.Message)
	return entry
}

func inferLogLevel(message string) string {
	upper := strings.ToUpper(message)
	switch {
	case strings.Contains(upper, "ERROR"):
		return "error"
	case strings.Contains(upper, "WARN"):
		return "warn"
	case strings.Contains(upper, "DEBUG"):
		return "debug"
	case strings.Contains(upper, "TRACE"):
		return "trace"
	default:
		return "info"
	}
}

func isStreamClosed(err error) bool {
	return err == io.EOF || strings.Contains(err.Error(), "context canceled") ||
		strings.Contains(err.Error(), "use of closed network connection")
}

// PoolLogEntry wraps a LogEntry with the source pod name.
type PoolLogEntry struct {
	PodName string              `json:"podName"`
	Entry   interfaces.LogEntry `json:"entry"`
}
