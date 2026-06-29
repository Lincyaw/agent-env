package gateway

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Lincyaw/agent-env/pkg/scheduling"
)

func TestReconcilePoolAutoscalingSizesPoolFromActiveClaimsAndBuffer(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	pool := testSandboxWarmPool("code", "default", "code-template", 1, 1, "code")
	claims := []extensionsv1beta1.SandboxClaim{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "claim-1", Namespace: "default"},
			Spec: extensionsv1beta1.SandboxClaimSpec{
				WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "code"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "claim-2", Namespace: "default"},
			Spec: extensionsv1beta1.SandboxClaimSpec{
				WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "code"},
			},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, &claims[0], &claims[1]).Build()
	gw := &Gateway{
		k8sClient: k8sClient,
		gwConfig: GatewayConfig{
			PoolAutoscalerBuffer:      1,
			PoolAutoscalerMinReplicas: 1,
			PoolAutoscalerMaxReplicas: 5,
		},
	}

	if err := gw.reconcilePoolAutoscaling(context.Background()); err != nil {
		t.Fatalf("reconcilePoolAutoscaling returned error: %v", err)
	}

	got := &extensionsv1beta1.SandboxWarmPool{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "code", Namespace: "default"}, got); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 3 {
		t.Fatalf("Replicas = %v, want 3", got.Spec.Replicas)
	}
}

func TestReconcilePoolAutoscalingSkipsDisabledPool(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	pool := testSandboxWarmPool("code", "default", "code-template", 2, 2, "code")
	pool.Annotations[scheduling.PoolAutoscaleAnnotation] = "disabled"
	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-1", Namespace: "default"},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "code"},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, claim).Build()
	gw := &Gateway{
		k8sClient: k8sClient,
		gwConfig: GatewayConfig{
			PoolAutoscalerBuffer:      1,
			PoolAutoscalerMinReplicas: 0,
			PoolAutoscalerMaxReplicas: 10,
		},
	}

	if err := gw.reconcilePoolAutoscaling(context.Background()); err != nil {
		t.Fatalf("reconcilePoolAutoscaling returned error: %v", err)
	}

	got := &extensionsv1beta1.SandboxWarmPool{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "code", Namespace: "default"}, got); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 2 {
		t.Fatalf("Replicas = %v, want unchanged 2", got.Spec.Replicas)
	}
}
