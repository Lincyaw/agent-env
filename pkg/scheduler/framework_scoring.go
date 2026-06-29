//go:build scheduler_plugin

package scheduler

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/kube-scheduler/framework"
)

func composedNodeScore(pod *corev1.Pod, nodeInfo framework.NodeInfo, state imageLocalityCycleState, opts ScoreOptions) int64 {
	node := nodeInfo.Node()
	if node == nil {
		return 0
	}

	imageScore := ScorePodOnNode(pod, node, opts)
	cacheSpreadScore := inverseRangeScore(int64(nodeImageCount(nodeInfo)), int64(state.minImageCount), int64(state.maxImageCount))
	loadScore := nodeLoadBalanceScore(nodeInfo)

	imageWeight, spreadWeight, loadWeight := composedWeights(opts, state.anyRequestedImageCached)
	totalWeight := imageWeight + spreadWeight + loadWeight
	if totalWeight <= 0 {
		return 0
	}

	score := imageScore*imageWeight + cacheSpreadScore*spreadWeight + loadScore*loadWeight
	return clampScore(score / totalWeight)
}

func nodeHasAnyRequestedImage(nodeInfo framework.NodeInfo, images []string) bool {
	if len(images) == 0 {
		return false
	}
	states := nodeInfo.GetImageStates()
	if len(states) > 0 {
		for _, image := range images {
			if _, ok := states[image]; ok {
				return true
			}
		}
	}
	node := nodeInfo.Node()
	if node == nil {
		return false
	}
	cached := nodeImageSet(node)
	for _, image := range images {
		if cached[image] {
			return true
		}
	}
	return false
}

func nodeImageCount(nodeInfo framework.NodeInfo) int {
	if states := nodeInfo.GetImageStates(); len(states) > 0 {
		return len(states)
	}
	node := nodeInfo.Node()
	if node == nil {
		return 0
	}
	return len(nodeImageSet(node))
}

func nodeLoadBalanceScore(nodeInfo framework.NodeInfo) int64 {
	allocatable := nodeInfo.GetAllocatable()
	requested := nodeInfo.GetNonZeroRequested()
	if allocatable == nil || requested == nil {
		return maxNodeScore / 2
	}

	cpuScore := freeResourceScore(requested.GetMilliCPU(), allocatable.GetMilliCPU())
	memScore := freeResourceScore(requested.GetMemory(), allocatable.GetMemory())
	podScore := freeResourceScore(int64(len(nodeInfo.GetPods())), int64(allocatable.GetAllowedPodNumber()))
	return clampScore((cpuScore*40 + memScore*40 + podScore*20) / 100)
}
