package scheduler

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/Lincyaw/agent-env/pkg/scheduling"
)

func TestScorePodOnNodePrefersCachedExecutorImage(t *testing.T) {
	pod := &corev1.Pod{}
	pod.Annotations = map[string]string{scheduling.ExecutorImageAnnotation: "python:3.12"}
	pod.Spec.InitContainers = []corev1.Container{{Name: "copy-executor-agent", Image: "agent-env/executor:latest"}}
	pod.Spec.Containers = []corev1.Container{
		{Name: "executor", Image: "python:3.12"},
		{Name: "sidecar", Image: "agent-env/sidecar:latest"},
	}

	nodeWithExecutor := nodeWithImages("node-a", "python:3.12")
	nodeWithoutExecutor := nodeWithImages("node-b", "agent-env/sidecar:latest", "agent-env/executor:latest")

	executorScore := ScorePodOnNode(pod, nodeWithExecutor, ScoreOptions{})
	otherScore := ScorePodOnNode(pod, nodeWithoutExecutor, ScoreOptions{})
	if executorScore <= otherScore {
		t.Fatalf("executorScore = %d, otherScore = %d, want executorScore higher", executorScore, otherScore)
	}
	if executorScore != 80 {
		t.Fatalf("executorScore = %d, want default executor weight 80", executorScore)
	}
}

func TestScorePodOnNodeFallsBackToNonSidecarContainer(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "executor", Image: "ubuntu:24.04"},
				{Name: "sidecar", Image: "agent-env/sidecar:latest"},
			},
		},
	}
	node := nodeWithImages("node-a", "ubuntu:24.04")

	score := ScorePodOnNode(pod, node, ScoreOptions{})
	if score != 80 {
		t.Fatalf("score = %d, want 80", score)
	}
}

func TestImageLocalityPluginOnlyScoresOptedInPods(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "executor", Image: "python:3.12"}},
		},
	}
	node := nodeWithImages("node-a", "python:3.12")
	plugin := NewImageLocalityPlugin(ScoreOptions{})

	if score := plugin.Score(pod, node); score != 0 {
		t.Fatalf("score without opt-in = %d, want 0", score)
	}

	pod.Annotations = map[string]string{scheduling.ImageLocalityAnnotation: scheduling.ImageLocalityEnabledValue}
	if score := plugin.Score(pod, node); score != 80 {
		t.Fatalf("score with opt-in = %d, want 80", score)
	}
}

func nodeWithImages(name string, images ...string) *corev1.Node {
	node := &corev1.Node{}
	node.Name = name
	for _, image := range images {
		node.Status.Images = append(node.Status.Images, corev1.ContainerImage{
			Names: []string{image},
		})
	}
	return node
}
