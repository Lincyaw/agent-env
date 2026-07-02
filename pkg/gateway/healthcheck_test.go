package gateway

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHealthCheckerCollectsImagePullDuration(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: "pulling", Namespace: "default", UID: types.UID("pulling-1")},
			InvolvedObject: corev1.ObjectReference{
				Kind:      "Pod",
				Namespace: "default",
				Name:      "sandbox-pod",
			},
			Reason:    "Pulling",
			Message:   `Pulling image "python:3.12"`,
			EventTime: metav1.MicroTime{Time: start},
		},
		&corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: "pulled", Namespace: "default", UID: types.UID("pulled-1")},
			InvolvedObject: corev1.ObjectReference{
				Kind:      "Pod",
				Namespace: "default",
				Name:      "sandbox-pod",
			},
			Reason:    "Pulled",
			Message:   `Successfully pulled image "python:3.12" in 5s`,
			EventTime: metav1.MicroTime{Time: start.Add(5 * time.Second)},
		},
		&corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: "other-ns-pulled", Namespace: "other", UID: types.UID("other-ns-pulled-1")},
			InvolvedObject: corev1.ObjectReference{
				Kind:      "Pod",
				Namespace: "other",
				Name:      "sandbox-pod",
			},
			Reason:    "Pulled",
			Message:   `Successfully pulled image "ignored:latest" in 1s`,
			EventTime: metav1.MicroTime{Time: start.Add(time.Second)},
		},
	).Build()
	metrics := &recordingMetricsCollector{}
	hc := NewHealthChecker(&Gateway{k8sClient: k8sClient}, metrics, "")

	if err := hc.collectImagePullMetrics(context.Background()); err != nil {
		t.Fatalf("collectImagePullMetrics returned error: %v", err)
	}
	if len(metrics.imagePullDurations) != 1 {
		t.Fatalf("imagePullDurations length = %d, want 1", len(metrics.imagePullDurations))
	}
	if got := metrics.imagePullDurations["python:3.12"]; got != 5*time.Second {
		t.Fatalf("image pull duration = %v, want 5s", got)
	}

	if err := hc.collectImagePullMetrics(context.Background()); err != nil {
		t.Fatalf("collectImagePullMetrics second call returned error: %v", err)
	}
	if len(metrics.imagePullDurations) != 1 {
		t.Fatalf("imagePullDurations length after duplicate collect = %d, want 1", len(metrics.imagePullDurations))
	}
}

type recordingMetricsCollector struct {
	imagePullDurations map[string]time.Duration
}

func (m *recordingMetricsCollector) RecordSessionAllocationDuration(poolName string, duration time.Duration) {
}
func (m *recordingMetricsCollector) IncrementPodAllocationResult(poolName, result string) {}
func (m *recordingMetricsCollector) RecordSandboxReadyDuration(poolName string, duration time.Duration) {
}
func (m *recordingMetricsCollector) RecordImagePullDuration(image string, duration time.Duration) {
	if m.imagePullDurations == nil {
		m.imagePullDurations = make(map[string]time.Duration)
	}
	m.imagePullDurations[image] = duration
}
func (m *recordingMetricsCollector) SetActiveSessions(count int64)                 {}
func (m *recordingMetricsCollector) IncrementSessionDeletion(reason string)        {}
func (m *recordingMetricsCollector) IncrementExecuteOperationResult(result string) {}
func (m *recordingMetricsCollector) RecordGatewayStepDuration(stepType string, duration time.Duration) {
}
func (m *recordingMetricsCollector) IncrementGatewayStepResult(stepType, result string) {}
func (m *recordingMetricsCollector) RecordSidecarCallDuration(method string, duration time.Duration) {
}
func (m *recordingMetricsCollector) RecordRestoreDuration(duration time.Duration)  {}
func (m *recordingMetricsCollector) IncrementRestoreResult(result string)          {}
func (m *recordingMetricsCollector) SetGatewayGoroutines(count int)                {}
func (m *recordingMetricsCollector) SetGatewaySessionsTotal(count int)             {}
func (m *recordingMetricsCollector) SetIdleQueueDepth(pool string, count int)      {}
func (m *recordingMetricsCollector) SetPendingWaiters(pool string, count int)      {}
func (m *recordingMetricsCollector) SetAdmissionQueueDepth(pool string, count int) {}
func (m *recordingMetricsCollector) SetPoolSaturation(pool string, saturation float64) {
}
func (m *recordingMetricsCollector) SetPoolDesiredReplicas(pool string, count int)   {}
func (m *recordingMetricsCollector) SetPoolReadyReplicas(pool string, count int)     {}
func (m *recordingMetricsCollector) SetPoolAllocatedReplicas(pool string, count int) {}
