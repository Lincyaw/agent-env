package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/Lincyaw/agent-env/pkg/labels"
	"github.com/Lincyaw/agent-env/pkg/scheduling"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestManagedPoolGCDeletesOldestStoppedPoolsByLRU(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	namespace := "default"
	now := time.Now()
	poolNames := []string{"pool-oldest", "pool-older", "pool-newer", "pool-newest"}
	lastUsed := []time.Time{
		now.Add(-4 * time.Hour),
		now.Add(-3 * time.Hour),
		now.Add(-2 * time.Hour),
		now.Add(-1 * time.Hour),
	}
	objects := make([]client.Object, 0, len(poolNames)*2)
	for i, name := range poolNames {
		objects = append(objects,
			stoppedManagedPoolForGC(name, namespace, lastUsed[i]),
			managedTemplateObject(sandboxTemplateName(name), namespace),
		)
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, namespace), nil, nil, nil, GatewayConfig{
		Namespace:               namespace,
		ManagedPoolGCEnabled:    true,
		ManagedPoolGCMaxStopped: 2,
		ManagedPoolGCMinIdleAge: 30 * time.Minute,
	}, NewMemoryStore())

	deleted, err := gw.reconcileManagedPoolGC(context.Background())
	if err != nil {
		t.Fatalf("reconcileManagedPoolGC returned error: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}

	for _, name := range []string{"pool-oldest", "pool-older"} {
		assertGCDeletedPoolAndTemplate(t, k8sClient, namespace, name)
	}
	for _, name := range []string{"pool-newer", "pool-newest"} {
		assertGCKeptPoolAndTemplate(t, k8sClient, namespace, name)
	}
}

func TestManagedPoolGCSkipsReferencedPools(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	namespace := "default"
	old := time.Now().Add(-4 * time.Hour)
	freePool := stoppedManagedPoolForGC("pool-free", namespace, old)
	claimPool := stoppedManagedPoolForGC("pool-claim", namespace, old)
	sessionPool := stoppedManagedPoolForGC("pool-session", namespace, old)
	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-live", Namespace: namespace},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: claimPool.Name},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		freePool, managedTemplateObject(sandboxTemplateName(freePool.Name), namespace),
		claimPool, managedTemplateObject(sandboxTemplateName(claimPool.Name), namespace),
		sessionPool, managedTemplateObject(sandboxTemplateName(sessionPool.Name), namespace),
		claim,
	).Build()
	store := NewMemoryStore()
	store.Set("session-live", &session{
		Info: SessionInfo{
			ID:        "session-live",
			Namespace: namespace,
			PoolRef:   sessionPool.Name,
			Status:    "active",
			CreatedAt: time.Now(),
		},
		Runtime: RuntimeAllocation{
			Namespace: namespace,
			PoolRef:   sessionPool.Name,
		},
		History:      NewStepHistory(),
		lastTaskTime: time.Now(),
		createdAt:    time.Now(),
	})
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, namespace), nil, nil, nil, GatewayConfig{
		Namespace:               namespace,
		ManagedPoolGCEnabled:    true,
		ManagedPoolGCMaxStopped: 0,
		ManagedPoolGCMinIdleAge: time.Minute,
	}, store)

	deleted, err := gw.reconcileManagedPoolGC(context.Background())
	if err != nil {
		t.Fatalf("reconcileManagedPoolGC returned error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	assertGCDeletedPoolAndTemplate(t, k8sClient, namespace, freePool.Name)
	assertGCKeptPoolAndTemplate(t, k8sClient, namespace, claimPool.Name)
	assertGCKeptPoolAndTemplate(t, k8sClient, namespace, sessionPool.Name)
}

func TestManagedPoolGCRespectsMinIdleAgeAndManagedMarker(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	namespace := "default"
	recentPool := stoppedManagedPoolForGC("pool-recent", namespace, time.Now().Add(-10*time.Minute))
	unmanagedPool := stoppedManagedPoolForGC("pool-unmanaged", namespace, time.Now().Add(-4*time.Hour))
	delete(unmanagedPool.Annotations, labels.ManagedPoolAnnotation)
	delete(unmanagedPool.Labels, labels.ManagedPoolLabelKey)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		recentPool, managedTemplateObject(sandboxTemplateName(recentPool.Name), namespace),
		unmanagedPool, managedTemplateObject(sandboxTemplateName(unmanagedPool.Name), namespace),
	).Build()
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, namespace), nil, nil, nil, GatewayConfig{
		Namespace:               namespace,
		ManagedPoolGCEnabled:    true,
		ManagedPoolGCMaxStopped: 0,
		ManagedPoolGCMinIdleAge: time.Hour,
	}, NewMemoryStore())

	deleted, err := gw.reconcileManagedPoolGC(context.Background())
	if err != nil {
		t.Fatalf("reconcileManagedPoolGC returned error: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
	}
	assertGCKeptPoolAndTemplate(t, k8sClient, namespace, recentPool.Name)
	assertGCKeptPoolAndTemplate(t, k8sClient, namespace, unmanagedPool.Name)
}

func stoppedManagedPoolForGC(name, namespace string, lastUsed time.Time) *extensionsv1beta1.SandboxWarmPool {
	pool := managedPoolObject(name, namespace)
	replicas := int32(0)
	pool.Spec.Replicas = &replicas
	if pool.Annotations == nil {
		pool.Annotations = make(map[string]string)
	}
	pool.Annotations[labels.PoolStateAnnotation] = labels.PoolStateStopped
	pool.Annotations[labels.PoolLastUsedAnnotation] = lastUsed.UTC().Format(time.RFC3339)
	pool.Annotations[scheduling.PoolAutoscaleAnnotation] = "false"
	if pool.Labels == nil {
		pool.Labels = make(map[string]string)
	}
	pool.Labels[labels.ManagedPoolLabelKey] = "true"
	pool.Labels[labels.PoolStateLabelKey] = labels.PoolStateStopped
	pool.CreationTimestamp = metav1.NewTime(lastUsed.Add(-time.Minute))
	return pool
}

func assertGCDeletedPoolAndTemplate(t *testing.T, k8sClient client.Client, namespace, poolName string) {
	t.Helper()
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: poolName, Namespace: namespace}, &extensionsv1beta1.SandboxWarmPool{}); !apierrors.IsNotFound(err) {
		t.Fatalf("get deleted pool %s/%s error = %v, want not found", namespace, poolName, err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: sandboxTemplateName(poolName), Namespace: namespace}, &extensionsv1beta1.SandboxTemplate{}); !apierrors.IsNotFound(err) {
		t.Fatalf("get deleted template for pool %s/%s error = %v, want not found", namespace, poolName, err)
	}
}

func assertGCKeptPoolAndTemplate(t *testing.T, k8sClient client.Client, namespace, poolName string) {
	t.Helper()
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: poolName, Namespace: namespace}, &extensionsv1beta1.SandboxWarmPool{}); err != nil {
		t.Fatalf("expected pool %s/%s to remain: %v", namespace, poolName, err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: sandboxTemplateName(poolName), Namespace: namespace}, &extensionsv1beta1.SandboxTemplate{}); err != nil {
		t.Fatalf("expected template for pool %s/%s to remain: %v", namespace, poolName, err)
	}
}
