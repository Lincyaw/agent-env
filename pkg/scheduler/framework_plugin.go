//go:build scheduler_plugin

package scheduler

import (
	"context"
	"math"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kube-scheduler/framework"
)

var _ framework.ScorePlugin = (*FrameworkImageLocalityPlugin)(nil)
var _ framework.PreScorePlugin = (*FrameworkImageLocalityPlugin)(nil)

const imageLocalityStateKey framework.StateKey = "AgentEnvImageLocalityState"

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

func (p *FrameworkImageLocalityPlugin) PreScore(_ context.Context, state framework.CycleState, pod *corev1.Pod, nodes []framework.NodeInfo) *framework.Status {
	if !imageLocalityRequested(pod) {
		return framework.NewStatus(framework.Skip)
	}
	requestedImages := requestedImagesForPod(pod)
	if len(requestedImages) == 0 {
		return framework.NewStatus(framework.Skip)
	}

	cycleState := imageLocalityCycleState{
		requestedImages: requestedImages,
		minImageCount:   math.MaxInt,
		maxImageCount:   0,
	}
	for _, nodeInfo := range nodes {
		imageCount := nodeImageCount(nodeInfo)
		if imageCount < cycleState.minImageCount {
			cycleState.minImageCount = imageCount
		}
		if imageCount > cycleState.maxImageCount {
			cycleState.maxImageCount = imageCount
		}
		if nodeHasAnyRequestedImage(nodeInfo, requestedImages) {
			cycleState.anyRequestedImageCached = true
		}
	}
	if cycleState.minImageCount == math.MaxInt {
		cycleState.minImageCount = 0
	}
	state.Write(imageLocalityStateKey, &cycleState)
	return nil
}

func (p *FrameworkImageLocalityPlugin) Score(_ context.Context, state framework.CycleState, pod *corev1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
	node := nodeInfo.Node()
	if node == nil {
		return 0, framework.NewStatus(framework.Error, "node info has no node")
	}
	if !imageLocalityRequested(pod) {
		return 0, nil
	}

	cycleState := imageLocalityCycleState{
		requestedImages:         requestedImagesForPod(pod),
		anyRequestedImageCached: true,
		minImageCount:           nodeImageCount(nodeInfo),
		maxImageCount:           nodeImageCount(nodeInfo),
	}
	if data, err := state.Read(imageLocalityStateKey); err == nil {
		if stored, ok := data.(*imageLocalityCycleState); ok {
			cycleState = *stored
		}
	}
	return composedNodeScore(pod, nodeInfo, cycleState, p.scorer.options), nil
}

func (p *FrameworkImageLocalityPlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

type imageLocalityCycleState struct {
	requestedImages         []string
	anyRequestedImageCached bool
	minImageCount           int
	maxImageCount           int
}

func (s *imageLocalityCycleState) Clone() framework.StateData {
	clone := *s
	if len(s.requestedImages) > 0 {
		clone.requestedImages = append([]string(nil), s.requestedImages...)
	}
	return &clone
}
