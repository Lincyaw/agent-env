package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/Lincyaw/agent-env/pkg/labels"
)

func TestDefaultPoolSelectorUsesPinnedPool(t *testing.T) {
	selector := DefaultPoolSelector{}
	pools := []PoolSnapshot{
		{Name: "small", Namespace: "default", Profile: "small", ReadyReplicas: 5},
		{Name: "large", Namespace: "default", Profile: "large", ReadyReplicas: 1},
	}

	selection, err := selector.SelectPool(context.Background(), ResourceIntent{
		Scope:          RequestScope{Namespace: "default"},
		PinnedPoolName: "large",
	}, pools)
	if err != nil {
		t.Fatalf("SelectPool returned error: %v", err)
	}
	if selection.PoolName != "large" {
		t.Fatalf("PoolName = %q, want large", selection.PoolName)
	}
	if selection.Reason != "pinned_pool" {
		t.Fatalf("Reason = %q, want pinned_pool", selection.Reason)
	}
}

func TestDefaultPoolSelectorChoosesProfileWithMostWarmCapacity(t *testing.T) {
	selector := DefaultPoolSelector{}
	pools := []PoolSnapshot{
		{Name: "code-a", Namespace: "default", Profile: "code", ReadyReplicas: 2, AllocatedReplicas: 2},
		{Name: "code-b", Namespace: "default", Profile: "code", ReadyReplicas: 3, AllocatedReplicas: 1},
		{Name: "gpu", Namespace: "default", Profile: "gpu", ReadyReplicas: 10},
	}

	selection, err := selector.SelectPool(context.Background(), ResourceIntent{
		Scope:   RequestScope{Namespace: "default"},
		Profile: "code",
	}, pools)
	if err != nil {
		t.Fatalf("SelectPool returned error: %v", err)
	}
	if selection.PoolName != "code-b" {
		t.Fatalf("PoolName = %q, want code-b", selection.PoolName)
	}
	if selection.Pool.WarmAvailable() != 3 {
		t.Fatalf("WarmAvailable = %d, want 3", selection.Pool.WarmAvailable())
	}
}

func TestDefaultAdmissionControllerRequiresWarmCapacityByDefault(t *testing.T) {
	admission := NewDefaultAdmissionController()

	warm, err := admission.Admit(context.Background(), ResourceIntent{}, PoolSelection{
		Pool: PoolSnapshot{ReadyReplicas: 2, AllocatedReplicas: 1},
	})
	if err != nil {
		t.Fatalf("warm Admit returned error: %v", err)
	}
	if !warm.Admitted {
		t.Fatalf("warm decision = %#v, want admitted warm path", warm)
	}

	_, err = admission.Admit(context.Background(), ResourceIntent{}, PoolSelection{
		Pool: PoolSnapshot{ReadyReplicas: 0},
	})
	if !errors.Is(err, ErrPoolAtCapacity) {
		t.Fatalf("empty-capacity Admit error = %v, want ErrPoolAtCapacity", err)
	}
}

func TestSnapshotPoolsIncludesProfileImageAndClaims(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	pool := testSandboxWarmPool("code", "default", "code-template", 3, 2, "code")
	template := testSandboxTemplate("code-template", "default", "python:3.12", "code")
	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-1", Namespace: "default"},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "code"},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template, claim).Build()
	gw := &Gateway{k8sClient: k8sClient}

	snapshots, err := gw.snapshotPools(context.Background(), "default")
	if err != nil {
		t.Fatalf("snapshotPools returned error: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("snapshots length = %d, want 1", len(snapshots))
	}
	got := snapshots[0]
	if got.Profile != "code" {
		t.Fatalf("Profile = %q, want code", got.Profile)
	}
	if got.Image != "python:3.12" {
		t.Fatalf("Image = %q, want python:3.12", got.Image)
	}
	if got.AllocatedReplicas != 1 {
		t.Fatalf("AllocatedReplicas = %d, want 1", got.AllocatedReplicas)
	}
	if got.WarmAvailable() != 2 {
		t.Fatalf("WarmAvailable = %d, want 2", got.WarmAvailable())
	}
}

func TestPinnedPoolSnapshotAvoidsTemplateList(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	pool := testSandboxWarmPool("code", "default", "code-template", 1, 1, "code")
	template := testSandboxTemplate("code-template", "default", "python:3.12", "code")
	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-1", Namespace: "default"},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "code"},
		},
	}
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template, claim).Build()
	k8sClient := interceptor.NewClient(baseClient, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*extensionsv1beta1.SandboxTemplateList); ok {
				return errors.New("unexpected full template list")
			}
			return c.List(ctx, list, opts...)
		},
	})
	gw := &Gateway{k8sClient: k8sClient}

	selection, decision, err := gw.tryPlanSessionAllocation(context.Background(), ResourceIntent{
		Scope:          RequestScope{Namespace: "default"},
		Profile:        "code",
		Image:          "python:3.12",
		PinnedPoolName: "code",
	})
	if err != nil {
		t.Fatalf("tryPlanSessionAllocation returned error: %v", err)
	}
	if selection.PoolName != "code" {
		t.Fatalf("PoolName = %q, want code", selection.PoolName)
	}
	if !decision.Admitted {
		t.Fatalf("decision = %#v, want admitted", decision)
	}
}

func TestListPoolsOmitsStoppedPoolsByDefault(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	active := testSandboxWarmPool("active", "default", "active-template", 1, 1, "code")
	stopped := testSandboxWarmPool("stopped", "default", "stopped-template", 0, 0, "code")
	stopped.Annotations[labels.PoolStateAnnotation] = labels.PoolStateStopped
	activeTemplate := testSandboxTemplate("active-template", "default", "python:3.12", "code")
	stoppedTemplate := testSandboxTemplate("stopped-template", "default", "python:3.12", "code")
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(active, stopped, activeTemplate, stoppedTemplate).Build()
	gw := &Gateway{k8sClient: k8sClient}

	pools, err := gw.ListPools(context.Background(), "default")
	if err != nil {
		t.Fatalf("ListPools returned error: %v", err)
	}
	if len(pools) != 1 || pools[0].Name != "active" {
		t.Fatalf("ListPools = %#v, want only active pool", pools)
	}

	allPools, err := gw.ListPoolsWithOptions(context.Background(), PoolListOptions{Namespace: "default", IncludeStopped: true})
	if err != nil {
		t.Fatalf("ListPoolsWithOptions returned error: %v", err)
	}
	if len(allPools) != 2 {
		t.Fatalf("all pools length = %d, want 2", len(allPools))
	}
}

func TestPlanSessionAllocationQueuesThenRejectsWhenWarmCapacityDoesNotFree(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	pool := testSandboxWarmPool("code", "default", "code-template", 1, 0, "code")
	template := testSandboxTemplate("code-template", "default", "python:3.12", "code")
	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-1", Namespace: "default"},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "code"},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template, claim).Build()
	gw := New(k8sClient, &recordingRuntimeAllocator{}, nil, nil, nil, GatewayConfig{
		AdmissionQueueTimeout:      time.Millisecond,
		AdmissionQueuePollInterval: time.Millisecond,
	}, NewMemoryStore())

	_, _, err := gw.planSessionAllocation(context.Background(), ResourceIntent{
		Scope:   RequestScope{Namespace: "default"},
		Profile: "code",
	})
	if !errors.Is(err, ErrPoolAtCapacity) {
		t.Fatalf("planSessionAllocation error = %v, want ErrPoolAtCapacity", err)
	}
	if !strings.Contains(err.Error(), "queued for") || !strings.Contains(err.Error(), "waiting for warm capacity") {
		t.Fatalf("planSessionAllocation error = %v, want explicit queue timeout", err)
	}
}

func TestCreateSessionUsesProfilePolicySelection(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	smallPool := testSandboxWarmPool("small", "default", "small-template", 1, 1, "small")
	smallTemplate := testSandboxTemplate("small-template", "default", "busybox:1.36", "small")
	codePool := testSandboxWarmPool("code", "default", "code-template", 3, 2, "code")
	codeTemplate := testSandboxTemplate("code-template", "default", "python:3.12", "code")
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(smallPool, smallTemplate, codePool, codeTemplate).Build()
	allocator := &recordingRuntimeAllocator{
		allocation: RuntimeAllocation{
			Backend:     runtimeBackendSandboxClaim,
			Namespace:   "default",
			PodName:     "pod-1",
			PodIP:       "10.0.0.1",
			ClaimName:   "claim-1",
			SandboxName: "sandbox-1",
		},
	}
	gw := New(k8sClient, allocator, nil, nil, nil, GatewayConfig{}, NewMemoryStore())

	info, err := gw.CreateSession(context.Background(), CreateSessionRequest{
		Namespace: "default",
		Profile:   "code",
	})
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if info.PoolRef != "code" {
		t.Fatalf("PoolRef = %q, want code", info.PoolRef)
	}
	if allocator.lastRequest.PoolRef != "code" {
		t.Fatalf("allocator PoolRef = %q, want code", allocator.lastRequest.PoolRef)
	}
}

func TestEnsureImageBackedSessionPoolCreatesProfiledPool(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	gw := New(k8sClient, &recordingRuntimeAllocator{}, nil, nil, nil, GatewayConfig{GRPCAuthToken: "token"}, NewMemoryStore())

	req, createdPool, err := gw.ensureImageBackedSessionPool(context.Background(), CreateSessionRequest{
		Namespace: "default",
		Image:     "docker.io/library/python:3.12",
		Profile:   "code",
	}, "default")
	if err != nil {
		t.Fatalf("ensureImageBackedSessionPool returned error: %v", err)
	}

	wantPool, err := managedPoolName("python:3.12", "default", "code", nil)
	if err != nil {
		t.Fatalf("managedPoolName returned error: %v", err)
	}
	if req.PoolName != wantPool {
		t.Fatalf("PoolName = %q, want %q", req.PoolName, wantPool)
	}
	if createdPool != wantPool {
		t.Fatalf("createdPool = %q, want %q", createdPool, wantPool)
	}
	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Name: wantPool, Namespace: "default"}, pool); err != nil {
		t.Fatalf("created pool get error: %v", err)
	}
	if pool.Annotations[poolProfileAnnotation] != "code" {
		t.Fatalf("pool profile annotation = %q, want code", pool.Annotations[poolProfileAnnotation])
	}
}

func TestEnsureImageBackedSessionPoolAvoidsFullPoolSnapshot(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	poolName, err := managedPoolName("python:3.12", "default", "code", nil)
	if err != nil {
		t.Fatalf("managedPoolName returned error: %v", err)
	}
	pool := testSandboxWarmPool(poolName, "default", sandboxTemplateName(poolName), 1, 1, "code")
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	k8sClient := interceptor.NewClient(baseClient, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			switch list.(type) {
			case *extensionsv1beta1.SandboxWarmPoolList, *extensionsv1beta1.SandboxTemplateList:
				return errors.New("unexpected full pool snapshot list")
			default:
				return c.List(ctx, list, opts...)
			}
		},
	})
	gw := New(k8sClient, &recordingRuntimeAllocator{}, nil, nil, nil, GatewayConfig{}, NewMemoryStore())

	req, createdPool, err := gw.ensureImageBackedSessionPool(context.Background(), CreateSessionRequest{
		Namespace: "default",
		Image:     "python:3.12",
		Profile:   "code",
	}, "default")
	if err != nil {
		t.Fatalf("ensureImageBackedSessionPool returned error: %v", err)
	}
	if req.PoolName != poolName {
		t.Fatalf("PoolName = %q, want %q", req.PoolName, poolName)
	}
	if createdPool != "" {
		t.Fatalf("createdPool = %q, want empty for existing pool", createdPool)
	}
}

func TestCreateSessionDefaultsToGatewayNamespace(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	pool := testSandboxWarmPool("code", "arl1", "code-template", 1, 1, "code")
	template := testSandboxTemplate("code-template", "arl1", "python:3.12", "code")
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template).Build()
	allocator := &recordingRuntimeAllocator{
		allocation: RuntimeAllocation{
			Backend:   runtimeBackendSandboxClaim,
			PodName:   "pod-1",
			PodIP:     "10.0.0.1",
			ClaimName: "claim-1",
		},
	}
	gw := New(k8sClient, allocator, nil, nil, nil, GatewayConfig{Namespace: "arl1"}, NewMemoryStore())

	info, err := gw.CreateSession(context.Background(), CreateSessionRequest{Profile: "code"})
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if info.Namespace != "arl1" {
		t.Fatalf("Namespace = %q, want arl1", info.Namespace)
	}
	if allocator.lastRequest.Namespace != "arl1" {
		t.Fatalf("allocator Namespace = %q, want arl1", allocator.lastRequest.Namespace)
	}
}

func TestCreateSessionRejectsCrossNamespaceRequest(t *testing.T) {
	gw := New(nil, &recordingRuntimeAllocator{}, nil, nil, nil, GatewayConfig{Namespace: "arl1"}, NewMemoryStore())

	_, err := gw.CreateSession(context.Background(), CreateSessionRequest{
		Namespace: "default",
		Profile:   "code",
	})
	if err == nil || !strings.Contains(err.Error(), `namespace "default" is not allowed`) {
		t.Fatalf("CreateSession error = %v, want namespace rejection", err)
	}
}

func TestSessionJSONDoesNotExposePoolRef(t *testing.T) {
	payload, err := json.Marshal(SessionInfo{
		ID:        "gw-1",
		Namespace: "default",
		Image:     "python:3.12",
		Profile:   "code",
		PoolRef:   "internal-pool",
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if strings.Contains(string(payload), "poolRef") {
		t.Fatalf("SessionInfo JSON = %s, want no poolRef", payload)
	}
}

func TestCreateSessionRequestDoesNotDecodePoolRef(t *testing.T) {
	var req CreateSessionRequest
	if err := json.Unmarshal([]byte(`{"poolRef":"old-pool","image":"python:3.12","profile":"code"}`), &req); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if req.PoolName != "" {
		t.Fatalf("PoolName = %q, want empty", req.PoolName)
	}
	if req.Image != "python:3.12" || req.Profile != "code" {
		t.Fatalf("decoded request = %#v, want image/profile preserved", req)
	}
}

func testSandboxWarmPool(name, namespace, template string, replicas, ready int32, profile string) *extensionsv1beta1.SandboxWarmPool {
	return &extensionsv1beta1.SandboxWarmPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{poolProfileAnnotation: profile},
		},
		Spec: extensionsv1beta1.SandboxWarmPoolSpec{
			Replicas:    &replicas,
			TemplateRef: extensionsv1beta1.SandboxTemplateRef{Name: template},
		},
		Status: extensionsv1beta1.SandboxWarmPoolStatus{
			Replicas:      replicas,
			ReadyReplicas: ready,
		},
	}
}

func testSandboxTemplate(name, namespace, image, profile string) *extensionsv1beta1.SandboxTemplate {
	return &extensionsv1beta1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{poolProfileAnnotation: profile},
		},
		Spec: extensionsv1beta1.SandboxTemplateSpec{
			PodTemplate: sandboxv1beta1.PodTemplate{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "executor",
						Image: image,
					}},
				},
			},
		},
	}
}

type recordingRuntimeAllocator struct {
	allocation  RuntimeAllocation
	lastRequest RuntimeAllocateRequest
}

func (a *recordingRuntimeAllocator) Start(ctx context.Context) error { return nil }

func (a *recordingRuntimeAllocator) Stop() {}

func (a *recordingRuntimeAllocator) Allocate(ctx context.Context, req RuntimeAllocateRequest) (*RuntimeAllocation, error) {
	a.lastRequest = req
	allocation := a.allocation
	allocation.PoolRef = req.PoolRef
	allocation.Namespace = req.Namespace
	allocation.SandboxName = req.SandboxName
	return &allocation, nil
}

func (a *recordingRuntimeAllocator) Release(ctx context.Context, allocation RuntimeAllocation) error {
	return nil
}

func (a *recordingRuntimeAllocator) Resolve(ctx context.Context, allocation RuntimeAllocation, sessionID string) (*RuntimeAllocation, error) {
	resolved := a.allocation
	return &resolved, nil
}

func (a *recordingRuntimeAllocator) Touch(ctx context.Context, allocation RuntimeAllocation, sessionID string, at time.Time, lifecycle RuntimeLifecycle) error {
	return nil
}

func (a *recordingRuntimeAllocator) DiagnosticStats() map[string]AllocatorPoolStats {
	return nil
}
