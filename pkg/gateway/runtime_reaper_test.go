package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/Lincyaw/agent-env/pkg/labels"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRuntimeReaperDeletesOrphanIdleClaim(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	namespace := "default"
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	shutdownAt := metav1.NewTime(now.Add(-10 * time.Minute))

	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "orphan-claim",
			Namespace:         namespace,
			CreationTimestamp: metav1.NewTime(now.Add(-30 * time.Minute)),
			Annotations: map[string]string{
				labels.SessionAnnotation:      "gw-orphan",
				labels.LastActivityAnnotation: now.Add(-30 * time.Minute).Format(time.RFC3339),
				labels.IdleTimeoutAnnotation:  "600",
			},
		},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "pool"},
			Lifecycle: &extensionsv1beta1.Lifecycle{
				ShutdownTime:   &shutdownAt,
				ShutdownPolicy: extensionsv1beta1.ShutdownPolicyDeleteForeground,
			},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(claim).Build()
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, namespace), nil, nil, nil, GatewayConfig{Namespace: namespace, IdleTimeout: 10 * time.Minute}, NewMemoryStore())

	if err := gw.reapRuntimeClaims(context.Background(), now); err != nil {
		t.Fatalf("reapRuntimeClaims returned error: %v", err)
	}
	err := k8sClient.Get(context.Background(), types.NamespacedName{Name: claim.Name, Namespace: namespace}, &extensionsv1beta1.SandboxClaim{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("claim get error = %v, want NotFound", err)
	}
}

func TestRuntimeReaperDoesNotUseStaleK8sDeadlineForActiveSession(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	namespace := "default"
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	shutdownAt := metav1.NewTime(now.Add(-10 * time.Minute))
	sessionID := "gw-active"

	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sessionID,
			Namespace: namespace,
			Annotations: map[string]string{
				labels.SessionAnnotation: sessionID,
			},
		},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "pool"},
			Lifecycle: &extensionsv1beta1.Lifecycle{
				ShutdownTime:   &shutdownAt,
				ShutdownPolicy: extensionsv1beta1.ShutdownPolicyDeleteForeground,
			},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(claim).Build()
	store := NewMemoryStore()
	store.Set(sessionID, &session{
		Info:         SessionInfo{ID: sessionID, Namespace: namespace, PoolRef: "pool"},
		Runtime:      RuntimeAllocation{Namespace: namespace, ClaimName: sessionID, PoolRef: "pool"},
		History:      NewStepHistory(),
		lastTaskTime: now,
		createdAt:    now.Add(-time.Minute),
		idleTimeout:  10 * time.Minute,
	})
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, namespace), nil, nil, nil, GatewayConfig{Namespace: namespace, IdleTimeout: 10 * time.Minute}, store)

	if err := gw.reapRuntimeClaims(context.Background(), now); err != nil {
		t.Fatalf("reapRuntimeClaims returned error: %v", err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: claim.Name, Namespace: namespace}, &extensionsv1beta1.SandboxClaim{}); err != nil {
		t.Fatalf("claim was deleted despite active session: %v", err)
	}
}

func TestRuntimeReaperDeletesCrashLoopingActiveRuntime(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	namespace := "default"
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	sessionID := "gw-crash"
	podName := "pod-crash"

	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sessionID,
			Namespace: namespace,
			Annotations: map[string]string{
				labels.SessionAnnotation: sessionID,
			},
		},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "pool"},
		},
		Status: extensionsv1beta1.SandboxClaimStatus{
			Conditions: []metav1.Condition{{
				Type:               string(sandboxv1beta1.SandboxConditionReady),
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.NewTime(now.Add(-10 * time.Minute)),
			}},
			SandboxStatus: extensionsv1beta1.SandboxStatus{Name: sessionID},
		},
	}
	sandbox := &sandboxv1beta1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sessionID,
			Namespace: namespace,
			Annotations: map[string]string{
				sandboxv1beta1.SandboxPodNameAnnotation: podName,
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: namespace},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "executor",
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
				},
			}},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(claim, sandbox, pod).Build()
	store := NewMemoryStore()
	store.Set(sessionID, &session{
		Info:         SessionInfo{ID: sessionID, Namespace: namespace, PoolRef: "pool"},
		Runtime:      RuntimeAllocation{Namespace: namespace, ClaimName: sessionID, PoolRef: "pool", SandboxName: sessionID, PodName: podName},
		History:      NewStepHistory(),
		lastTaskTime: now,
		createdAt:    now.Add(-time.Minute),
		idleTimeout:  10 * time.Minute,
	})
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, namespace), nil, nil, nil, GatewayConfig{Namespace: namespace, IdleTimeout: 10 * time.Minute}, store)

	if err := gw.reapRuntimeClaims(context.Background(), now); err != nil {
		t.Fatalf("reapRuntimeClaims returned error: %v", err)
	}
	if _, ok := store.Get(sessionID); ok {
		t.Fatal("session still active after crash-loop runtime reap")
	}
	err := k8sClient.Get(context.Background(), types.NamespacedName{Name: claim.Name, Namespace: namespace}, &extensionsv1beta1.SandboxClaim{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("claim get error = %v, want NotFound", err)
	}
}

func TestRuntimeReaperDeletesStaleClaimForActiveSession(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	namespace := "default"
	sessionID := "gw-restore"

	oldClaim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "old-claim",
			Namespace: namespace,
			Annotations: map[string]string{
				labels.SessionAnnotation: sessionID,
			},
		},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "pool"},
		},
	}
	currentClaim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "current-claim",
			Namespace: namespace,
			Annotations: map[string]string{
				labels.SessionAnnotation: sessionID,
			},
		},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "pool"},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oldClaim, currentClaim).Build()
	store := NewMemoryStore()
	store.Set(sessionID, &session{
		Info:    SessionInfo{ID: sessionID, Namespace: namespace, PoolRef: "pool"},
		Runtime: RuntimeAllocation{Namespace: namespace, ClaimName: currentClaim.Name, PoolRef: "pool"},
		History: NewStepHistory(),
	})
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, namespace), nil, nil, nil, GatewayConfig{Namespace: namespace}, store)

	if err := gw.reapRuntimeClaims(context.Background(), time.Now()); err != nil {
		t.Fatalf("reapRuntimeClaims returned error: %v", err)
	}
	err := k8sClient.Get(context.Background(), types.NamespacedName{Name: oldClaim.Name, Namespace: namespace}, &extensionsv1beta1.SandboxClaim{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("old claim get error = %v, want NotFound", err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: currentClaim.Name, Namespace: namespace}, &extensionsv1beta1.SandboxClaim{}); err != nil {
		t.Fatalf("current claim was deleted: %v", err)
	}
	if _, ok := store.Get(sessionID); !ok {
		t.Fatal("active session was deleted while reaping stale claim")
	}
}
