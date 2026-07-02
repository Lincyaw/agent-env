package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/Lincyaw/agent-env/pkg/labels"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
)

func TestSandboxClaimRuntimeAllocatorAllocate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := sandboxv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add sandbox scheme: %v", err)
	}
	if err := extensionsv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add sandbox extension scheme: %v", err)
	}

	namespace := "arl"
	poolName := "small"
	sessionID := "gw-test-session"
	sandboxName := "gw-test-sandbox"
	podName := "sandbox-pod-1"
	podIP := "10.0.0.7"

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&extensionsv1beta1.SandboxWarmPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      poolName,
				Namespace: namespace,
			},
			Status: extensionsv1beta1.SandboxWarmPoolStatus{
				ReadyReplicas: 1,
			},
		}).
		Build()

	allocator := NewSandboxClaimRuntimeAllocator(k8sClient)
	allocator.pollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		claimKey := types.NamespacedName{Name: sandboxName, Namespace: namespace}
		for {
			claim := &extensionsv1beta1.SandboxClaim{}
			if err := k8sClient.Get(ctx, claimKey, claim); err == nil {
				sandbox := &sandboxv1beta1.Sandbox{
					ObjectMeta: metav1.ObjectMeta{
						Name:      sandboxName,
						Namespace: namespace,
						Annotations: map[string]string{
							sandboxv1beta1.SandboxPodNameAnnotation: podName,
						},
					},
					Status: sandboxv1beta1.SandboxStatus{
						PodIPs: []string{podIP},
						Conditions: []metav1.Condition{{
							Type:   string(sandboxv1beta1.SandboxConditionReady),
							Status: metav1.ConditionTrue,
							Reason: sandboxv1beta1.SandboxReasonDependenciesReady,
						}},
					},
				}
				if err := k8sClient.Create(ctx, sandbox); err != nil {
					t.Errorf("create sandbox: %v", err)
					return
				}

				claim.Status.Conditions = []metav1.Condition{{
					Type:   string(sandboxv1beta1.SandboxConditionReady),
					Status: metav1.ConditionTrue,
					Reason: sandboxv1beta1.SandboxReasonDependenciesReady,
				}}
				claim.Status.SandboxStatus = extensionsv1beta1.SandboxStatus{
					Name:   sandboxName,
					PodIPs: []string{podIP},
				}
				if err := k8sClient.Update(ctx, claim); err != nil {
					t.Errorf("update claim status: %v", err)
				}
				return
			}

			select {
			case <-ctx.Done():
				t.Errorf("wait for claim create: %v", ctx.Err())
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()

	allocation, err := allocator.Allocate(ctx, RuntimeAllocateRequest{
		PoolRef:     poolName,
		Namespace:   namespace,
		SessionID:   sessionID,
		SandboxName: sandboxName,
		Lifecycle: RuntimeLifecycle{
			CreatedAt:      time.Now(),
			LastActivityAt: time.Now(),
			IdleTimeout:    10 * time.Minute,
			MaxLifetime:    time.Hour,
			FinishedTTL:    5 * time.Minute,
		},
	})
	if err != nil {
		t.Fatalf("Allocate returned error: %v", err)
	}
	<-done

	if allocation.Backend != runtimeBackendSandboxClaim {
		t.Fatalf("Backend = %q, want %q", allocation.Backend, runtimeBackendSandboxClaim)
	}
	if allocation.ClaimName != sandboxName {
		t.Fatalf("ClaimName = %q, want %q", allocation.ClaimName, sandboxName)
	}
	if allocation.SandboxName != sandboxName {
		t.Fatalf("SandboxName = %q, want %q", allocation.SandboxName, sandboxName)
	}
	if allocation.PodName != podName {
		t.Fatalf("PodName = %q, want %q", allocation.PodName, podName)
	}
	if allocation.PodIP != podIP {
		t.Fatalf("PodIP = %q, want %q", allocation.PodIP, podIP)
	}

	claim := &extensionsv1beta1.SandboxClaim{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: sandboxName, Namespace: namespace}, claim); err != nil {
		t.Fatalf("get created claim: %v", err)
	}
	if got := claim.Annotations[labels.SessionAnnotation]; got != sessionID {
		t.Fatalf("claim session annotation = %q, want %q", got, sessionID)
	}
	if got := claim.Spec.AdditionalPodMetadata.Annotations[labels.SessionAnnotation]; got != sessionID {
		t.Fatalf("pod metadata session annotation = %q, want %q", got, sessionID)
	}
	if len(claim.Spec.AdditionalPodMetadata.Labels) != 0 {
		t.Fatalf("pod metadata labels = %#v, want none", claim.Spec.AdditionalPodMetadata.Labels)
	}
	if claim.Spec.Lifecycle == nil || claim.Spec.Lifecycle.ShutdownTime == nil {
		t.Fatalf("claim lifecycle = %#v, want shutdownTime", claim.Spec.Lifecycle)
	}
	if claim.Spec.Lifecycle.ShutdownPolicy != extensionsv1beta1.ShutdownPolicyDeleteForeground {
		t.Fatalf("shutdown policy = %q, want DeleteForeground", claim.Spec.Lifecycle.ShutdownPolicy)
	}
	if claim.Spec.Lifecycle.TTLSecondsAfterFinished == nil || *claim.Spec.Lifecycle.TTLSecondsAfterFinished != 300 {
		t.Fatalf("finished TTL = %#v, want 300", claim.Spec.Lifecycle.TTLSecondsAfterFinished)
	}
	if got := claim.Annotations[labels.IdleTimeoutAnnotation]; got != "600" {
		t.Fatalf("idle timeout annotation = %q, want 600", got)
	}
}

func TestSandboxClaimRuntimeAllocatorCleansUpCreatedClaimOnTimeout(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := sandboxv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add sandbox scheme: %v", err)
	}
	if err := extensionsv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add sandbox extension scheme: %v", err)
	}

	namespace := "arl"
	poolName := "small"
	claimName := "gw-timeout"
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&extensionsv1beta1.SandboxWarmPool{
			ObjectMeta: metav1.ObjectMeta{Name: poolName, Namespace: namespace},
		}).
		Build()
	allocator := NewSandboxClaimRuntimeAllocator(k8sClient)
	allocator.pollInterval = time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := allocator.Allocate(ctx, RuntimeAllocateRequest{
		PoolRef:     poolName,
		Namespace:   namespace,
		SessionID:   claimName,
		SandboxName: claimName,
	})
	if err == nil {
		t.Fatal("Allocate returned nil error, want timeout")
	}

	claim := &extensionsv1beta1.SandboxClaim{}
	err = k8sClient.Get(context.Background(), types.NamespacedName{Name: claimName, Namespace: namespace}, claim)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("created claim still exists or unexpected error: %v", err)
	}
}

func TestSandboxClaimRuntimeAllocatorDiagnosticStats(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := sandboxv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add sandbox scheme: %v", err)
	}
	if err := extensionsv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add sandbox extension scheme: %v", err)
	}

	replicas := int32(3)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&extensionsv1beta1.SandboxWarmPool{
				ObjectMeta: metav1.ObjectMeta{Name: "code", Namespace: "default"},
				Spec: extensionsv1beta1.SandboxWarmPoolSpec{
					Replicas: &replicas,
				},
				Status: extensionsv1beta1.SandboxWarmPoolStatus{
					ReadyReplicas: 3,
				},
			},
			&extensionsv1beta1.SandboxClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "claim-1", Namespace: "default"},
				Spec: extensionsv1beta1.SandboxClaimSpec{
					WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "code"},
				},
			},
		).
		Build()

	stats := NewSandboxClaimRuntimeAllocator(k8sClient).DiagnosticStats()
	got := stats["default/code"]
	if got.IdleCount != 2 {
		t.Fatalf("IdleCount = %d, want 2", got.IdleCount)
	}
}

func TestSandboxClaimRuntimeAllocatorTouchReturnsNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := sandboxv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add sandbox scheme: %v", err)
	}
	if err := extensionsv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add sandbox extension scheme: %v", err)
	}

	allocator := NewSandboxClaimRuntimeAllocator(fake.NewClientBuilder().WithScheme(scheme).Build())
	err := allocator.Touch(context.Background(), RuntimeAllocation{
		Namespace: "default",
		ClaimName: "missing-claim",
	}, "gw-missing", time.Now(), RuntimeLifecycle{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("Touch error = %v, want NotFound", err)
	}
}
