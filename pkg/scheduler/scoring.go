package scheduler

import (
	"math"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/Lincyaw/agent-env/pkg/scheduling"
)

const maxNodeScore int64 = 100

// ScoreOptions controls how much node-image-cache affinity contributes to a
// scheduler score. Kubernetes scheduler framework Score plugins use 0..100.
type ScoreOptions struct {
	ExecutorImageWeight int64
	OtherImagesWeight   int64
	// Score composition weights used by the kube-scheduler framework adapter.
	// Image locality dominates when a requested image is cached somewhere. When
	// every candidate is cold, the adapter shifts weight to cold-start spread and
	// current node load.
	ImageLocalityWeight int64
	ColdStartWeight     int64
	NodeLoadWeight      int64
}

// ScorePodOnNode scores a pod for image locality against one node's current
// image cache. The function is intentionally framework-free so it can be tested
// here and reused by a kube-scheduler framework adapter.
func ScorePodOnNode(pod *corev1.Pod, node *corev1.Node, opts ScoreOptions) int64 {
	if pod == nil || node == nil {
		return 0
	}

	executorWeight, otherWeight := normalizedScoreWeights(opts)
	cached := nodeImageSet(node)
	if len(cached) == 0 {
		return 0
	}

	executorImage := executorImageForPod(pod)
	var score int64
	if executorImage != "" && cached[executorImage] {
		score += executorWeight
	}

	otherImages := podImagesExcept(pod, executorImage)
	if len(otherImages) > 0 && otherWeight > 0 {
		var hits int64
		for image := range otherImages {
			if cached[image] {
				hits++
			}
		}
		score += otherWeight * hits / int64(len(otherImages))
	}

	if score > maxNodeScore {
		return maxNodeScore
	}
	if score < 0 {
		return 0
	}
	return score
}

func normalizedScoreWeights(opts ScoreOptions) (int64, int64) {
	executorWeight := opts.ExecutorImageWeight
	otherWeight := opts.OtherImagesWeight
	if executorWeight == 0 && otherWeight == 0 {
		return 80, 20
	}
	if executorWeight < 0 {
		executorWeight = 0
	}
	if otherWeight < 0 {
		otherWeight = 0
	}
	total := executorWeight + otherWeight
	if total <= maxNodeScore {
		return executorWeight, otherWeight
	}
	return executorWeight * maxNodeScore / total, otherWeight * maxNodeScore / total
}

func nodeImageSet(node *corev1.Node) map[string]bool {
	images := make(map[string]bool)
	for _, image := range node.Status.Images {
		for _, name := range image.Names {
			if normalized := strings.TrimSpace(name); normalized != "" {
				images[normalized] = true
			}
		}
	}
	return images
}

func executorImageForPod(pod *corev1.Pod) string {
	if image := strings.TrimSpace(pod.Annotations[scheduling.ExecutorImageAnnotation]); image != "" {
		return image
	}
	for _, container := range pod.Spec.Containers {
		if image := strings.TrimSpace(container.Image); image != "" {
			return image
		}
	}
	return ""
}

func podImagesExcept(pod *corev1.Pod, excluded string) map[string]bool {
	images := make(map[string]bool)
	add := func(image string) {
		image = strings.TrimSpace(image)
		if image == "" || image == excluded {
			return
		}
		images[image] = true
	}
	for _, container := range pod.Spec.InitContainers {
		add(container.Image)
	}
	for _, container := range pod.Spec.Containers {
		add(container.Image)
	}
	return images
}

func requestedImagesForPod(pod *corev1.Pod) []string {
	if pod == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var images []string
	add := func(image string) {
		image = strings.TrimSpace(image)
		if image == "" {
			return
		}
		if _, ok := seen[image]; ok {
			return
		}
		seen[image] = struct{}{}
		images = append(images, image)
	}
	add(executorImageForPod(pod))
	for image := range podImagesExcept(pod, "") {
		add(image)
	}
	return images
}

func composedWeights(opts ScoreOptions, anyRequestedImageCached bool) (int64, int64, int64) {
	imageWeight := opts.ImageLocalityWeight
	spreadWeight := opts.ColdStartWeight
	loadWeight := opts.NodeLoadWeight
	if imageWeight == 0 && spreadWeight == 0 && loadWeight == 0 {
		if anyRequestedImageCached {
			return 70, 10, 20
		}
		return 0, 40, 60
	}
	return nonNegative(imageWeight), nonNegative(spreadWeight), nonNegative(loadWeight)
}

func freeResourceScore(used, capacity int64) int64 {
	if capacity <= 0 {
		return maxNodeScore / 2
	}
	free := capacity - used
	if free <= 0 {
		return 0
	}
	if free >= capacity {
		return maxNodeScore
	}
	return clampScore(free * maxNodeScore / capacity)
}

func inverseRangeScore(value, minValue, maxValue int64) int64 {
	if maxValue <= minValue {
		return maxNodeScore / 2
	}
	if value <= minValue {
		return maxNodeScore
	}
	if value >= maxValue {
		return 0
	}
	return clampScore((maxValue - value) * maxNodeScore / (maxValue - minValue))
}

func clampScore(score int64) int64 {
	return int64(math.Max(0, math.Min(float64(maxNodeScore), float64(score))))
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
