package scheduler

import (
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/Lincyaw/agent-env/pkg/scheduling"
)

const ImageLocalityPluginName = "AgentEnvImageLocality"

// ImageLocalityPlugin is the framework-independent scoring adapter. A real
// kube-scheduler framework plugin can delegate Name and Score to this type.
type ImageLocalityPlugin struct {
	options ScoreOptions
}

func NewImageLocalityPlugin(options ScoreOptions) *ImageLocalityPlugin {
	return &ImageLocalityPlugin{options: options}
}

func (p *ImageLocalityPlugin) Name() string {
	return ImageLocalityPluginName
}

func (p *ImageLocalityPlugin) Score(pod *corev1.Pod, node *corev1.Node) int64 {
	if !imageLocalityRequested(pod) {
		return 0
	}
	return ScorePodOnNode(pod, node, p.options)
}

func imageLocalityRequested(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	value := strings.ToLower(strings.TrimSpace(pod.Annotations[scheduling.ImageLocalityAnnotation]))
	return value == scheduling.ImageLocalityEnabledValue || value == "true" || value == "yes"
}
