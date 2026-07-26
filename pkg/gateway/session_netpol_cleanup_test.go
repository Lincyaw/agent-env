package gateway

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// netpolScheme is the gateway test scheme plus NetworkPolicy, which the shared
// helper does not register.
func netpolScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := newGatewayTestScheme(t)
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add networking scheme: %v", err)
	}
	return scheme
}

// A session ends either by being deleted or by losing its runtime. Only the
// first path removed the per-session NetworkPolicy, so every "runtime lost"
// left one behind, selecting a claim UID that no longer exists.

func TestDropSessionRemovesThePerSessionNetworkPolicy(t *testing.T) {
	scheme := netpolScheme(t)
	const sessionID = "sess-1"
	npName := sessionNetworkPolicyName(sessionID)

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: npName, Namespace: "arl"},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(np).Build()

	store := NewMemoryStore()
	s := &session{idleTimeout: 0}
	s.Info.Namespace = "arl"
	s.Info.PodName = "pod-1"
	store.Set(sessionID, s)

	gw := &Gateway{
		k8sClient: k8sClient,
		store:     store,
		gwConfig: GatewayConfig{
			Namespace:                      "arl",
			SandboxNetworkPolicyManagement: "Managed",
		},
	}

	gw.dropSession(sessionID, s)

	err := k8sClient.Get(context.Background(),
		types.NamespacedName{Name: npName, Namespace: "arl"}, &networkingv1.NetworkPolicy{})
	if err == nil {
		t.Error("the per-session NetworkPolicy outlived the session it belonged to")
	}
}

func TestDropSessionLeavesPoliciesAloneWhenUnmanaged(t *testing.T) {
	// Unmanaged means the gateway does not own these objects, so it must not
	// delete them either.
	scheme := netpolScheme(t)
	const sessionID = "sess-1"
	npName := sessionNetworkPolicyName(sessionID)

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: npName, Namespace: "arl"},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(np).Build()

	store := NewMemoryStore()
	s := &session{}
	s.Info.Namespace = "arl"
	store.Set(sessionID, s)

	gw := &Gateway{
		k8sClient: k8sClient,
		store:     store,
		gwConfig:  GatewayConfig{Namespace: "arl"},
	}

	gw.dropSession(sessionID, s)

	if err := k8sClient.Get(context.Background(),
		types.NamespacedName{Name: npName, Namespace: "arl"}, &networkingv1.NetworkPolicy{}); err != nil {
		t.Errorf("unmanaged policy should be untouched: %v", err)
	}
}
