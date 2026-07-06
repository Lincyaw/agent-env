package gateway

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/Lincyaw/agent-env/pkg/labels"
)

func TestSyncedPoolIndexAvoidsK8sListsOnHotPaths(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	objects := largePoolFixtureObjects(1000, 20, 50)
	var listCalls atomic.Int64
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	k8sClient := interceptor.NewClient(baseClient, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			listCalls.Add(1)
			return c.List(ctx, list, opts...)
		},
	})
	gw := New(k8sClient, &recordingRuntimeAllocator{}, nil, nil, nil, GatewayConfig{}, NewMemoryStore())
	if err := gw.refreshPoolIndexFromReader(context.Background(), k8sClient); err != nil {
		t.Fatalf("refreshPoolIndexFromReader returned error: %v", err)
	}

	listCalls.Store(0)
	pools, err := gw.ListPools(context.Background(), "default")
	if err != nil {
		t.Fatalf("ListPools returned error: %v", err)
	}
	if len(pools) != 20 {
		t.Fatalf("ListPools length = %d, want 20 active pools", len(pools))
	}

	selection, decision, err := gw.tryPlanSessionAllocation(context.Background(), ResourceIntent{
		Scope:   RequestScope{Namespace: "default"},
		Profile: "code",
		Image:   "python:3.12",
	})
	if err != nil {
		t.Fatalf("tryPlanSessionAllocation returned error: %v", err)
	}
	if selection.PoolName == "" || !decision.Admitted {
		t.Fatalf("selection=%#v decision=%#v, want admitted pool", selection, decision)
	}
	if got := listCalls.Load(); got != 0 {
		t.Fatalf("hot paths issued %d Kubernetes list call(s), want 0", got)
	}
}

func TestPoolMetadataLabelsAreLifecycleOnly(t *testing.T) {
	pool := testSandboxWarmPool("pool-1", "default", "template-1", 1, 1, "code")
	reconcilePoolObjectLabels(pool)
	if pool.Labels[labels.PoolProfileLabelKey] != "code" {
		t.Fatalf("profile label = %q, want code", pool.Labels[labels.PoolProfileLabelKey])
	}

	applyPoolStateMetadata(&pool.ObjectMeta, labels.PoolStateDraining)
	if pool.Labels[labels.PoolStateLabelKey] != labels.PoolStateDraining {
		t.Fatalf("state label = %q, want draining", pool.Labels[labels.PoolStateLabelKey])
	}
}

func BenchmarkListPoolsSlowK8sNoIndex(b *testing.B) {
	gw := benchmarkGatewayWithLargePools(b, 1000, 20, 50, 2*time.Millisecond)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := gw.ListPools(context.Background(), "default"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListPoolsSyncedIndex(b *testing.B) {
	gw := benchmarkGatewayWithLargePools(b, 1000, 20, 50, 2*time.Millisecond)
	if err := gw.refreshPoolIndexFromReader(context.Background(), gw.k8sClient); err != nil {
		b.Fatalf("refreshPoolIndexFromReader returned error: %v", err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		pools, err := gw.ListPools(context.Background(), "default")
		if err != nil {
			b.Fatal(err)
		}
		if len(pools) != 20 {
			b.Fatalf("ListPools length = %d, want 20", len(pools))
		}
	}
}

func BenchmarkPlanSessionAllocationSlowK8sNoIndex(b *testing.B) {
	gw := benchmarkGatewayWithLargePools(b, 1000, 20, 50, 2*time.Millisecond)
	intent := ResourceIntent{
		Scope:   RequestScope{Namespace: "default"},
		Profile: "code",
		Image:   "python:3.12",
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := gw.tryPlanSessionAllocation(context.Background(), intent); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPlanSessionAllocationSyncedIndex(b *testing.B) {
	gw := benchmarkGatewayWithLargePools(b, 1000, 20, 50, 2*time.Millisecond)
	if err := gw.refreshPoolIndexFromReader(context.Background(), gw.k8sClient); err != nil {
		b.Fatalf("refreshPoolIndexFromReader returned error: %v", err)
	}
	intent := ResourceIntent{
		Scope:   RequestScope{Namespace: "default"},
		Profile: "code",
		Image:   "python:3.12",
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := gw.tryPlanSessionAllocation(context.Background(), intent); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkGatewayWithLargePools(b *testing.B, stoppedPools, activePools, claims int, listDelay time.Duration) *Gateway {
	b.Helper()
	scheme := newGatewayTestScheme(b)
	objects := largePoolFixtureObjects(stoppedPools, activePools, claims)
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	k8sClient := interceptor.NewClient(baseClient, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if listDelay > 0 {
				timer := time.NewTimer(listDelay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
			return c.List(ctx, list, opts...)
		},
	})
	return New(k8sClient, &recordingRuntimeAllocator{}, nil, nil, nil, GatewayConfig{}, NewMemoryStore())
}

func largePoolFixtureObjects(stoppedPools, activePools, claims int) []client.Object {
	objects := make([]client.Object, 0, stoppedPools+activePools+stoppedPools+activePools+claims)
	now := time.Now().Add(-time.Hour)
	for i := range stoppedPools {
		name := fmt.Sprintf("stopped-%04d", i)
		templateName := sandboxTemplateName(name)
		pool := testSandboxWarmPool(name, "default", templateName, 0, 0, "code")
		pool.CreationTimestamp = metav1.Time{Time: now.Add(time.Duration(i) * time.Second)}
		pool.Annotations[labels.PoolStateAnnotation] = labels.PoolStateStopped
		template := testSandboxTemplate(templateName, "default", "python:3.12", "code")
		objects = append(objects, pool, template)
	}
	for i := range activePools {
		name := fmt.Sprintf("active-%04d", i)
		templateName := sandboxTemplateName(name)
		pool := testSandboxWarmPool(name, "default", templateName, 2, 2, "code")
		pool.CreationTimestamp = metav1.Time{Time: now.Add(time.Duration(stoppedPools+i) * time.Second)}
		pool.Annotations[labels.PoolStateAnnotation] = labels.PoolStateRunning
		template := testSandboxTemplate(templateName, "default", "python:3.12", "code")
		objects = append(objects, pool, template)
	}
	for i := range claims {
		poolName := fmt.Sprintf("active-%04d", i%activePools)
		claim := &extensionsv1beta1.SandboxClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("claim-%04d", i),
				Namespace: "default",
				Labels: map[string]string{
					labels.PoolLabelKey: poolName,
				},
			},
			Spec: extensionsv1beta1.SandboxClaimSpec{
				WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: poolName},
			},
		}
		objects = append(objects, claim)
	}
	return objects
}

func TestSnapshotPoolsForIntentFallsBackWhenIndexMissesPinnedPool(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	pool := testSandboxWarmPool("code", "default", "code-template", 1, 1, "code")
	template := testSandboxTemplate("code-template", "default", "python:3.12", "code")
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template).Build()
	gw := New(k8sClient, &recordingRuntimeAllocator{}, nil, nil, nil, GatewayConfig{}, NewMemoryStore())
	gw.poolIndex.setSynced(true)

	snapshots, err := gw.snapshotPoolsForIntent(context.Background(), ResourceIntent{
		Scope:          RequestScope{Namespace: "default"},
		PinnedPoolName: "code",
	})
	if err != nil {
		t.Fatalf("snapshotPoolsForIntent returned error: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Name != "code" {
		t.Fatalf("snapshots = %#v, want pinned code pool", snapshots)
	}
}

func TestSyncedPoolIndexSurfacesSelectorMissWithoutK8sList(t *testing.T) {
	gw := New(nil, &recordingRuntimeAllocator{}, nil, nil, nil, GatewayConfig{}, NewMemoryStore())
	gw.poolIndex.setSynced(true)

	_, _, err := gw.tryPlanSessionAllocation(context.Background(), ResourceIntent{
		Scope:   RequestScope{Namespace: "default"},
		Profile: "missing",
	})
	if err == nil || !strings.Contains(err.Error(), "no pool") {
		t.Fatalf("tryPlanSessionAllocation error = %v, want selector miss", err)
	}
}
