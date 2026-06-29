//go:build scheduler_plugin

package scheduler

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kube-scheduler/framework"
)

var _ framework.ScorePlugin = (*FrameworkImageLocalityPlugin)(nil)

// FrameworkImageLocalityPlugin adapts ImageLocalityPlugin to the Kubernetes
// scheduler framework ScorePlugin interface. It is behind the scheduler_plugin
// build tag so the gateway module does not pull k8s.io/kubernetes into normal
// builds.
type FrameworkImageLocalityPlugin struct {
	scorer *ImageLocalityPlugin
}

func NewFrameworkImageLocalityPlugin(_ context.Context, _ runtime.Object, _ framework.Handle) (framework.Plugin, error) {
	return &FrameworkImageLocalityPlugin{
		scorer: NewImageLocalityPlugin(ScoreOptions{}),
	}, nil
}

func (p *FrameworkImageLocalityPlugin) Name() string {
	return ImageLocalityPluginName
}

func (p *FrameworkImageLocalityPlugin) Score(_ context.Context, _ framework.CycleState, pod *corev1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
	node := nodeInfo.Node()
	if node == nil {
		return 0, framework.NewStatus(framework.Error, "node info has no node")
	}
	return p.scorer.Score(pod, node), nil
}

func (p *FrameworkImageLocalityPlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}
