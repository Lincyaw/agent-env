package scheduler

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestImageSchedulerRecordsImageCacheAndSelectsCachedNodes(t *testing.T) {
	scheduler := NewImageScheduler(nil)
	scheduler.upsertNode(schedulableNode("node-a", "python:3.12"))
	scheduler.upsertNode(schedulableNode("node-b", "ubuntu:24.04"))

	cached := scheduler.CachedNodesForImage("python:3.12")
	if len(cached) != 1 || cached[0] != "node-a" {
		t.Fatalf("CachedNodesForImage = %#v, want [node-a]", cached)
	}

	selected := scheduler.SelectNodes("python:3.12", 2)
	if len(selected) != 1 || selected[0] != "node-a" {
		t.Fatalf("SelectNodes = %#v, want only cached node-a", selected)
	}
}

func TestImageSchedulerFallsBackToAllSchedulableNodes(t *testing.T) {
	scheduler := NewImageScheduler(nil)
	scheduler.upsertNode(schedulableNode("node-a", "python:3.12"))
	scheduler.upsertNode(schedulableNode("node-b", "ubuntu:24.04"))

	selected := scheduler.SelectNodes("golang:1.26", 2)
	if len(selected) != 2 {
		t.Fatalf("SelectNodes length = %d, want 2 fallback nodes", len(selected))
	}
}

func schedulableNode(name string, images ...string) *corev1.Node {
	node := &corev1.Node{}
	node.Name = name
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:   corev1.NodeReady,
		Status: corev1.ConditionTrue,
	}}
	for _, image := range images {
		node.Status.Images = append(node.Status.Images, corev1.ContainerImage{Names: []string{image}})
	}
	return node
}
