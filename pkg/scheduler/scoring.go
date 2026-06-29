package scheduler

import (
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
		if container.Name == "sidecar" {
			continue
		}
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
