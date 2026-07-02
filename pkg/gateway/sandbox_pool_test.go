package gateway

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Lincyaw/agent-env/pkg/labels"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/Lincyaw/agent-env/pkg/scheduling"

	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
)

func TestCheckPoolHealthUsesSandboxWarmPool(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	pool := &extensionsv1beta1.SandboxWarmPool{}
	pool.Name = "pool"
	pool.Namespace = "default"
	pool.Spec.TemplateRef.Name = "pool-template"
	replicas := int32(1)
	pool.Spec.Replicas = &replicas
	pool.Status.Replicas = 1
	pool.Status.ReadyReplicas = 1

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	gw := &Gateway{k8sClient: k8sClient}

	if err := gw.checkPoolHealth(context.Background(), "pool", "default"); err != nil {
		t.Fatalf("checkPoolHealth returned error: %v", err)
	}
}

func TestCheckPoolHealthFailsWhenSandboxWarmPoolMissing(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	gw := &Gateway{k8sClient: k8sClient}

	err := gw.checkPoolHealth(context.Background(), "missing", "default")
	if err == nil {
		t.Fatal("checkPoolHealth succeeded for missing pool")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("checkPoolHealth error = %q, want not found", err)
	}
}

func TestDiagnosePoolHealthUsesSandboxWarmPoolStatus(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	pool := &extensionsv1beta1.SandboxWarmPool{}
	pool.Name = "pool"
	pool.Namespace = "default"
	pool.Spec.TemplateRef.Name = "pool-template"
	replicas := int32(3)
	pool.Spec.Replicas = &replicas
	pool.Status.Replicas = 3
	pool.Status.ReadyReplicas = 2

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	gw := &Gateway{k8sClient: k8sClient}

	got := gw.diagnosePoolHealth(context.Background(), "pool", "default")
	for _, want := range []string{"pool=pool", "desired=3", "replicas=3", "ready=2", "template=pool-template"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnosePoolHealth = %q, want substring %q", got, want)
		}
	}
}

func TestCreatePoolCreatesSandboxWarmPoolAndExecutableTemplate(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	gw := &Gateway{
		k8sClient: k8sClient,
		gwConfig: GatewayConfig{
			SidecarImage:                    "arl-sidecar:orbstack",
			ExecutorAgentImage:              "arl-executor-agent:orbstack",
			ImagePullPolicy:                 string(corev1.PullIfNotPresent),
			SidecarHTTPPort:                 8080,
			SidecarGRPCPort:                 9090,
			WorkspaceDir:                    "/workspace",
			GRPCAuthToken:                   "test-token",
			SandboxNetworkPolicyManagement:  string(extensionsv1beta1.NetworkPolicyManagementManaged),
			SandboxRuntimeClassName:         "kata",
			SandboxSeccompProfileType:       string(corev1.SeccompProfileTypeLocalhost),
			SandboxSeccompLocalhostProfile:  "profiles/agent-env.json",
			SandboxAllowPrivilegeEscalation: false,
		},
	}

	err := gw.CreatePool(context.Background(), CreatePoolRequest{
		Name:      "pool",
		Namespace: "default",
		Image:     "busybox:1.36.1",
		Replicas:  2,
	})
	if err != nil {
		t.Fatalf("CreatePool returned error: %v", err)
	}

	template := &extensionsv1beta1.SandboxTemplate{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "pool-template", Namespace: "default"}, template); err != nil {
		t.Fatalf("get sandbox template: %v", err)
	}
	podSpec := template.Spec.PodTemplate.Spec
	if len(podSpec.InitContainers) < 2 {
		t.Fatalf("initContainers length = %d, want at least 2", len(podSpec.InitContainers))
	}
	if !hasContainer(podSpec.InitContainers, "copy-executor-agent") {
		t.Fatal("template missing copy-executor-agent init container")
	}
	if !hasContainer(podSpec.Containers, "executor") {
		t.Fatal("template missing executor container")
	}
	if !hasContainer(podSpec.Containers, "sidecar") {
		t.Fatal("template missing sidecar container")
	}
	sidecar := findContainer(podSpec.Containers, "sidecar")
	if got := sidecar.Command; len(got) != 1 || got[0] != "/sidecar" {
		t.Fatalf("sidecar command = %#v, want /sidecar", got)
	}
	if hasVolumeMountName(sidecar.VolumeMounts, "workspace") {
		t.Fatalf("sidecar workspace mounts = %#v, want no workspace mount", sidecar.VolumeMounts)
	}
	if !hasVolumeMount(sidecar.VolumeMounts, "arl-socket", "/var/run/arl") {
		t.Fatalf("sidecar mounts = %#v, want arl-socket mounted at /var/run/arl", sidecar.VolumeMounts)
	}
	if sidecar.StartupProbe == nil || sidecar.StartupProbe.HTTPGet == nil || sidecar.StartupProbe.HTTPGet.Path != "/healthz" {
		t.Fatalf("sidecar startup probe = %#v, want HTTP /healthz", sidecar.StartupProbe)
	}
	if sidecar.ReadinessProbe == nil || sidecar.ReadinessProbe.HTTPGet == nil || sidecar.ReadinessProbe.HTTPGet.Path != "/readyz" {
		t.Fatalf("sidecar readiness probe = %#v, want HTTP /readyz", sidecar.ReadinessProbe)
	}
	if template.Spec.NetworkPolicyManagement != extensionsv1beta1.NetworkPolicyManagementManaged {
		t.Fatalf("NetworkPolicyManagement = %q, want Managed", template.Spec.NetworkPolicyManagement)
	}
	if podSpec.RuntimeClassName == nil || *podSpec.RuntimeClassName != "kata" {
		t.Fatalf("RuntimeClassName = %v, want kata", podSpec.RuntimeClassName)
	}
	if podSpec.SecurityContext == nil || podSpec.SecurityContext.SeccompProfile == nil {
		t.Fatal("pod seccomp profile missing")
	}
	if podSpec.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeLocalhost {
		t.Fatalf("SeccompProfile.Type = %q, want Localhost", podSpec.SecurityContext.SeccompProfile.Type)
	}
	if podSpec.SecurityContext.SeccompProfile.LocalhostProfile == nil || *podSpec.SecurityContext.SeccompProfile.LocalhostProfile != "profiles/agent-env.json" {
		t.Fatalf("SeccompProfile.LocalhostProfile = %v, want profiles/agent-env.json", podSpec.SecurityContext.SeccompProfile.LocalhostProfile)
	}
	for _, container := range append(podSpec.InitContainers, podSpec.Containers...) {
		if container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil {
			t.Fatalf("container %s missing allowPrivilegeEscalation", container.Name)
		}
		if *container.SecurityContext.AllowPrivilegeEscalation {
			t.Fatalf("container %s allowPrivilegeEscalation = true, want false", container.Name)
		}
	}

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "pool", Namespace: "default"}, pool); err != nil {
		t.Fatalf("get sandbox warm pool: %v", err)
	}
	if pool.Spec.TemplateRef.Name != "pool-template" {
		t.Fatalf("TemplateRef.Name = %q, want pool-template", pool.Spec.TemplateRef.Name)
	}
	if pool.Spec.Replicas == nil || *pool.Spec.Replicas != 2 {
		t.Fatalf("Replicas = %v, want 2", pool.Spec.Replicas)
	}

	secret := &corev1.Secret{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: defaultGRPCAuthSecretName, Namespace: "default"}, secret); err != nil {
		t.Fatalf("get gRPC token secret: %v", err)
	}
	if string(secret.Data["token"]) != "test-token" {
		t.Fatalf("secret token = %q, want test-token", string(secret.Data["token"]))
	}
}

func TestCreatePoolAppliesSchedulerNameAndImageLocalityHints(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	gw := &Gateway{
		k8sClient: k8sClient,
		gwConfig: GatewayConfig{
			SchedulerName:        "agent-env-image-locality",
			ImageLocalityEnabled: true,
			GRPCAuthToken:        "test-token",
		},
	}

	if err := gw.CreatePool(context.Background(), CreatePoolRequest{
		Name:      "pool",
		Namespace: "default",
		Image:     "python:3.12",
		Replicas:  1,
		Profile:   "code",
	}); err != nil {
		t.Fatalf("CreatePool returned error: %v", err)
	}

	template := &extensionsv1beta1.SandboxTemplate{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "pool-template", Namespace: "default"}, template); err != nil {
		t.Fatalf("get sandbox template: %v", err)
	}
	if got := template.Spec.PodTemplate.Spec.SchedulerName; got != "agent-env-image-locality" {
		t.Fatalf("SchedulerName = %q, want agent-env-image-locality", got)
	}
	if got := template.Annotations[scheduling.ImageLocalityAnnotation]; got != scheduling.ImageLocalityEnabledValue {
		t.Fatalf("template image locality annotation = %q, want enabled", got)
	}
	if got := template.Spec.PodTemplate.ObjectMeta.Annotations[scheduling.ExecutorImageAnnotation]; got != "python:3.12" {
		t.Fatalf("pod executor image annotation = %q, want python:3.12", got)
	}
}

func TestCreatePoolCleansTemplateWhenWarmPoolCreateFails(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	k8sClient := interceptor.NewClient(baseClient, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*extensionsv1beta1.SandboxWarmPool); ok {
				return fmt.Errorf("injected warm pool create failure")
			}
			return c.Create(ctx, obj, opts...)
		},
	})
	gw := &Gateway{k8sClient: k8sClient, gwConfig: GatewayConfig{GRPCAuthToken: "test-token"}}

	err := gw.CreatePool(context.Background(), CreatePoolRequest{
		Name:      "pool",
		Namespace: "default",
		Image:     "python:3.12",
		Replicas:  1,
	})
	if err == nil {
		t.Fatal("CreatePool succeeded, want injected error")
	}

	template := &extensionsv1beta1.SandboxTemplate{}
	err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "pool-template", Namespace: "default"}, template)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("template get error = %v, want not found after rollback", err)
	}
}

func TestCreatePoolDoesNotCreateTemplateWhenWarmPoolAlreadyExists(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	existingPool := &extensionsv1beta1.SandboxWarmPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "default"},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingPool).Build()
	gw := &Gateway{k8sClient: k8sClient, gwConfig: GatewayConfig{GRPCAuthToken: "test-token"}}

	err := gw.CreatePool(context.Background(), CreatePoolRequest{
		Name:      "pool",
		Namespace: "default",
		Image:     "python:3.12",
		Replicas:  1,
	})
	if !apierrors.IsAlreadyExists(err) {
		t.Fatalf("CreatePool error = %v, want AlreadyExists", err)
	}
	err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "pool-template", Namespace: "default"}, &extensionsv1beta1.SandboxTemplate{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("template get error = %v, want not found", err)
	}
	err = k8sClient.Get(context.Background(), types.NamespacedName{Name: defaultGRPCAuthSecretName, Namespace: "default"}, &corev1.Secret{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("secret get error = %v, want not found", err)
	}
}

func TestCreateManagedSessionCleansNewPoolOnSessionCreateFailure(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	gw := New(k8sClient, failingRuntimeAllocator{}, nil, nil, nil, GatewayConfig{Namespace: "default", GRPCAuthToken: "test-token"}, NewMemoryStore())

	_, err := gw.CreateManagedSession(context.Background(), CreateManagedSessionRequest{
		Image:        "python:3.12",
		Profile:      "code",
		ExperimentID: "exp-cleanup",
	})
	if err == nil {
		t.Fatal("CreateManagedSession succeeded, want allocation error")
	}

	poolName, err := managedPoolName("python:3.12", "default", "code", nil, nil)
	if err != nil {
		t.Fatalf("managedPoolName returned error: %v", err)
	}
	for _, obj := range []struct {
		name string
		obj  client.Object
	}{
		{name: poolName, obj: &extensionsv1beta1.SandboxWarmPool{}},
		{name: sandboxTemplateName(poolName), obj: &extensionsv1beta1.SandboxTemplate{}},
	} {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: obj.name, Namespace: "default"}, obj.obj)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("get %s error = %v, want not found after cleanup", obj.name, err)
		}
	}
}

func TestDeleteSessionCleansUnusedManagedPool(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	poolName := "managed-pool"
	pool := managedPoolObject(poolName, "default")
	template := managedTemplateObject(sandboxTemplateName(poolName), "default")
	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-session", Namespace: "default"},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: poolName},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template, claim).Build()
	store := NewMemoryStore()
	putIntegrationSession(store, "default", poolName, "claim-session", "session-clean", "exp-clean")
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, "default"), nil, nil, nil, GatewayConfig{Namespace: "default"}, store)

	if err := gw.DeleteSession(context.Background(), "session-clean"); err != nil {
		t.Fatalf("DeleteSession returned error: %v", err)
	}
	for _, obj := range []struct {
		name string
		obj  client.Object
	}{
		{name: "claim-session", obj: &extensionsv1beta1.SandboxClaim{}},
		{name: poolName, obj: &extensionsv1beta1.SandboxWarmPool{}},
		{name: sandboxTemplateName(poolName), obj: &extensionsv1beta1.SandboxTemplate{}},
	} {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: obj.name, Namespace: "default"}, obj.obj)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("get %s error = %v, want not found", obj.name, err)
		}
	}
}

func TestDeleteSessionContinuesWhenManagedPoolCleanupCheckFails(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	poolName := "managed-pool"
	pool := managedPoolObject(poolName, "default")
	template := managedTemplateObject(sandboxTemplateName(poolName), "default")
	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-session", Namespace: "default"},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: poolName},
		},
	}
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template, claim).Build()
	k8sClient := interceptor.NewClient(baseClient, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*extensionsv1beta1.SandboxClaimList); ok {
				return fmt.Errorf("injected claim list failure")
			}
			return c.List(ctx, list, opts...)
		},
	})
	store := NewMemoryStore()
	putIntegrationSession(store, "default", poolName, "claim-session", "session-clean", "exp-clean")
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, "default"), nil, nil, nil, GatewayConfig{Namespace: "default"}, store)

	if err := gw.DeleteSession(context.Background(), "session-clean"); err != nil {
		t.Fatalf("DeleteSession returned error: %v", err)
	}
	if _, ok := store.Get("session-clean"); ok {
		t.Fatal("session is still active after DeleteSession")
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: poolName, Namespace: "default"}, &extensionsv1beta1.SandboxWarmPool{}); err != nil {
		t.Fatalf("managed pool was deleted despite cleanup check failure: %v", err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: sandboxTemplateName(poolName), Namespace: "default"}, &extensionsv1beta1.SandboxTemplate{}); err != nil {
		t.Fatalf("managed template was deleted despite cleanup check failure: %v", err)
	}
}

func TestDeletePoolDeletesBoundSandboxClaims(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	pool := &extensionsv1beta1.SandboxWarmPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "default"},
	}
	template := &extensionsv1beta1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-template", Namespace: "default"},
	}
	boundClaim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-bound", Namespace: "default"},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "pool"},
		},
	}
	otherClaim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-other", Namespace: "default"},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "other"},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template, boundClaim, otherClaim).Build()
	gw := &Gateway{k8sClient: k8sClient}

	if err := gw.DeletePool(context.Background(), "pool", "default"); err != nil {
		t.Fatalf("DeletePool returned error: %v", err)
	}

	for _, obj := range []struct {
		name string
		obj  client.Object
	}{
		{name: "pool", obj: &extensionsv1beta1.SandboxWarmPool{}},
		{name: "pool-template", obj: &extensionsv1beta1.SandboxTemplate{}},
		{name: "claim-bound", obj: &extensionsv1beta1.SandboxClaim{}},
	} {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: obj.name, Namespace: "default"}, obj.obj)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("get %s error = %v, want not found", obj.name, err)
		}
	}

	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "claim-other", Namespace: "default"}, &extensionsv1beta1.SandboxClaim{}); err != nil {
		t.Fatalf("get unrelated claim: %v", err)
	}
}

func TestDeletePoolDeletesActiveSessionsForPool(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	pool := &extensionsv1beta1.SandboxWarmPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "default"},
	}
	template := &extensionsv1beta1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-template", Namespace: "default"},
	}
	boundClaim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-bound", Namespace: "default"},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "pool"},
		},
	}
	otherClaim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-other", Namespace: "default"},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "other"},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template, boundClaim, otherClaim).Build()
	store := NewMemoryStore()
	now := time.Now()
	store.Set("session-bound", &session{
		Info: SessionInfo{
			ID:        "session-bound",
			Namespace: "default",
			PoolRef:   "pool",
			Status:    "active",
			CreatedAt: now,
		},
		Runtime: RuntimeAllocation{
			Namespace: "default",
			PoolRef:   "pool",
			ClaimName: "claim-bound",
		},
		History:      NewStepHistory(),
		lastTaskTime: now,
		createdAt:    now,
	})
	store.Set("session-other", &session{
		Info: SessionInfo{
			ID:        "session-other",
			Namespace: "default",
			PoolRef:   "other",
			Status:    "active",
			CreatedAt: now,
		},
		Runtime: RuntimeAllocation{
			Namespace: "default",
			PoolRef:   "other",
			ClaimName: "claim-other",
		},
		History:      NewStepHistory(),
		lastTaskTime: now,
		createdAt:    now,
	})
	store.IncrCount(2)
	gw := New(k8sClient, &recordingRuntimeAllocator{}, nil, nil, nil, GatewayConfig{Namespace: "default"}, store)

	if err := gw.DeletePool(context.Background(), "pool", "default"); err != nil {
		t.Fatalf("DeletePool returned error: %v", err)
	}

	if _, ok := store.Get("session-bound"); ok {
		t.Fatal("bound session is still active after pool delete")
	}
	historical, ok := store.GetHistorical("session-bound")
	if !ok {
		t.Fatal("bound session tombstone not found")
	}
	historical.mu.RLock()
	status := historical.Info.Status
	reason := historical.Info.DeletionReason
	historical.mu.RUnlock()
	if status != "deleted" || reason != "pool_deleted" {
		t.Fatalf("bound session status/reason = %q/%q, want deleted/pool_deleted", status, reason)
	}
	if _, ok := store.Get("session-other"); !ok {
		t.Fatal("unrelated session was deleted")
	}
	if count := store.Count(); count != 1 {
		t.Fatalf("store count = %d, want 1", count)
	}

	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "claim-bound", Namespace: "default"}, &extensionsv1beta1.SandboxClaim{}); !apierrors.IsNotFound(err) {
		t.Fatalf("get bound claim error = %v, want not found", err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "claim-other", Namespace: "default"}, &extensionsv1beta1.SandboxClaim{}); err != nil {
		t.Fatalf("get unrelated claim: %v", err)
	}
}

func TestDeletePoolFallsBackToDirectClaimDeleteWhenRuntimeReleaseFails(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	pool := &extensionsv1beta1.SandboxWarmPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "default"},
	}
	template := &extensionsv1beta1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-template", Namespace: "default"},
	}
	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-bound", Namespace: "default"},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "pool"},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template, claim).Build()
	store := NewMemoryStore()
	now := time.Now()
	store.Set("session-bound", &session{
		Info: SessionInfo{
			ID:        "session-bound",
			Namespace: "default",
			PoolRef:   "pool",
			Status:    "active",
			CreatedAt: now,
		},
		Runtime: RuntimeAllocation{
			Namespace: "default",
			PoolRef:   "pool",
			ClaimName: "claim-bound",
		},
		History:      NewStepHistory(),
		lastTaskTime: now,
		createdAt:    now,
	})
	store.IncrCount(1)
	gw := New(k8sClient, releaseFailingRuntimeAllocator{}, nil, nil, nil, GatewayConfig{Namespace: "default"}, store)

	if err := gw.DeletePool(context.Background(), "pool", "default"); err != nil {
		t.Fatalf("DeletePool returned error: %v", err)
	}
	if _, ok := store.Get("session-bound"); ok {
		t.Fatal("session is still active after pool delete")
	}
	for _, obj := range []struct {
		name string
		obj  client.Object
	}{
		{name: "claim-bound", obj: &extensionsv1beta1.SandboxClaim{}},
		{name: "pool", obj: &extensionsv1beta1.SandboxWarmPool{}},
		{name: "pool-template", obj: &extensionsv1beta1.SandboxTemplate{}},
	} {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: obj.name, Namespace: "default"}, obj.obj)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("get %s error = %v, want not found", obj.name, err)
		}
	}
}

func TestDeleteExperimentCleansUnusedManagedPool(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	poolName := "managed-pool"
	pool := managedPoolObject(poolName, "default")
	template := managedTemplateObject(sandboxTemplateName(poolName), "default")
	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "claim-exp",
			Namespace: "default",
			Annotations: map[string]string{
				labels.SessionAnnotation: "session-exp",
			},
		},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: poolName},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template, claim).Build()
	store := NewMemoryStore()
	now := time.Now()
	store.Set("session-exp", &session{
		Info: SessionInfo{
			ID:        "session-exp",
			Namespace: "default",
			PoolRef:   poolName,
			Status:    "active",
			CreatedAt: now,
		},
		Runtime: RuntimeAllocation{
			Namespace: "default",
			PoolRef:   poolName,
			ClaimName: "claim-exp",
		},
		History:      NewStepHistory(),
		managed:      true,
		experimentID: "exp-1",
		lastTaskTime: now,
		createdAt:    now,
	})
	store.IncrCount(1)
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, "default"), nil, nil, nil, GatewayConfig{Namespace: "default"}, store)

	deleted, err := gw.DeleteExperiment(context.Background(), "exp-1")
	if err != nil {
		t.Fatalf("DeleteExperiment returned error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	for _, obj := range []struct {
		name string
		obj  client.Object
	}{
		{name: poolName, obj: &extensionsv1beta1.SandboxWarmPool{}},
		{name: sandboxTemplateName(poolName), obj: &extensionsv1beta1.SandboxTemplate{}},
		{name: "claim-exp", obj: &extensionsv1beta1.SandboxClaim{}},
	} {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: obj.name, Namespace: "default"}, obj.obj)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("get %s error = %v, want not found", obj.name, err)
		}
	}
}

func TestDeleteExperimentKeepsManagedPoolStillInUse(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	poolName := "managed-pool"
	pool := managedPoolObject(poolName, "default")
	template := managedTemplateObject(sandboxTemplateName(poolName), "default")
	claimExp1 := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "claim-exp-1",
			Namespace: "default",
			Annotations: map[string]string{
				labels.SessionAnnotation: "session-exp-1",
			},
		},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: poolName},
		},
	}
	claimExp2 := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "claim-exp-2",
			Namespace: "default",
			Annotations: map[string]string{
				labels.SessionAnnotation: "session-exp-2",
			},
		},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: poolName},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template, claimExp1, claimExp2).Build()
	store := NewMemoryStore()
	now := time.Now()
	for _, spec := range []struct {
		sessionID    string
		claimName    string
		experimentID string
	}{
		{sessionID: "session-exp-1", claimName: "claim-exp-1", experimentID: "exp-1"},
		{sessionID: "session-exp-2", claimName: "claim-exp-2", experimentID: "exp-2"},
	} {
		store.Set(spec.sessionID, &session{
			Info: SessionInfo{
				ID:        spec.sessionID,
				Namespace: "default",
				PoolRef:   poolName,
				Status:    "active",
				CreatedAt: now,
			},
			Runtime: RuntimeAllocation{
				Namespace: "default",
				PoolRef:   poolName,
				ClaimName: spec.claimName,
			},
			History:      NewStepHistory(),
			managed:      true,
			experimentID: spec.experimentID,
			lastTaskTime: now,
			createdAt:    now,
		})
	}
	store.IncrCount(2)
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, "default"), nil, nil, nil, GatewayConfig{Namespace: "default"}, store)

	deleted, err := gw.DeleteExperiment(context.Background(), "exp-1")
	if err != nil {
		t.Fatalf("DeleteExperiment returned error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: poolName, Namespace: "default"}, &extensionsv1beta1.SandboxWarmPool{}); err != nil {
		t.Fatalf("managed pool was deleted while still in use: %v", err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: sandboxTemplateName(poolName), Namespace: "default"}, &extensionsv1beta1.SandboxTemplate{}); err != nil {
		t.Fatalf("managed template was deleted while still in use: %v", err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "claim-exp-1", Namespace: "default"}, &extensionsv1beta1.SandboxClaim{}); !apierrors.IsNotFound(err) {
		t.Fatalf("deleted experiment claim error = %v, want not found", err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "claim-exp-2", Namespace: "default"}, &extensionsv1beta1.SandboxClaim{}); err != nil {
		t.Fatalf("other experiment claim was deleted: %v", err)
	}
	if _, ok := store.Get("session-exp-2"); !ok {
		t.Fatal("other experiment session was deleted")
	}
}

func TestDeleteManagedPoolIfUnusedReturnsErrorWhenClaimListFails(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	poolName := "managed-pool"
	pool := managedPoolObject(poolName, "default")
	template := managedTemplateObject(sandboxTemplateName(poolName), "default")
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template).Build()
	k8sClient := interceptor.NewClient(baseClient, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*extensionsv1beta1.SandboxClaimList); ok {
				return fmt.Errorf("injected claim list failure")
			}
			return c.List(ctx, list, opts...)
		},
	})
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, "default"), nil, nil, nil, GatewayConfig{Namespace: "default"}, NewMemoryStore())

	deleted, err := gw.deleteManagedPoolIfUnused(context.Background(), poolName, "default")
	if err == nil {
		t.Fatal("deleteManagedPoolIfUnused succeeded, want list error")
	}
	if deleted {
		t.Fatal("deleteManagedPoolIfUnused deleted pool despite list error")
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: poolName, Namespace: "default"}, &extensionsv1beta1.SandboxWarmPool{}); err != nil {
		t.Fatalf("managed pool was deleted despite list failure: %v", err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: sandboxTemplateName(poolName), Namespace: "default"}, &extensionsv1beta1.SandboxTemplate{}); err != nil {
		t.Fatalf("managed template was deleted despite list failure: %v", err)
	}
}

func TestDeleteManagedPoolIfUnusedReturnsErrorWhenPoolDeleteFails(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	poolName := "managed-pool"
	pool := managedPoolObject(poolName, "default")
	template := managedTemplateObject(sandboxTemplateName(poolName), "default")
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, template).Build()
	k8sClient := interceptor.NewClient(baseClient, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, ok := obj.(*extensionsv1beta1.SandboxWarmPool); ok {
				return fmt.Errorf("injected warm pool delete failure")
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, "default"), nil, nil, nil, GatewayConfig{Namespace: "default"}, NewMemoryStore())

	deleted, err := gw.deleteManagedPoolIfUnused(context.Background(), poolName, "default")
	if err == nil {
		t.Fatal("deleteManagedPoolIfUnused succeeded, want pool delete error")
	}
	if deleted {
		t.Fatal("deleteManagedPoolIfUnused reported deletion despite pool delete failure")
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: poolName, Namespace: "default"}, &extensionsv1beta1.SandboxWarmPool{}); err != nil {
		t.Fatalf("managed pool disappeared despite delete failure: %v", err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: sandboxTemplateName(poolName), Namespace: "default"}, &extensionsv1beta1.SandboxTemplate{}); err != nil {
		t.Fatalf("managed template disappeared despite pool delete failure: %v", err)
	}
}

func TestDeleteManagedPoolIfUnusedCanRetryTemplateCleanupAfterPoolGone(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	poolName := "managed-pool"
	template := managedTemplateObject(sandboxTemplateName(poolName), "default")
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(template).Build()
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, "default"), nil, nil, nil, GatewayConfig{Namespace: "default"}, NewMemoryStore())

	deleted, err := gw.deleteManagedPoolIfUnused(context.Background(), poolName, "default")
	if err != nil {
		t.Fatalf("deleteManagedPoolIfUnused returned error: %v", err)
	}
	if !deleted {
		t.Fatal("deleteManagedPoolIfUnused did not delete managed template after pool was already gone")
	}
	err = k8sClient.Get(context.Background(), types.NamespacedName{Name: sandboxTemplateName(poolName), Namespace: "default"}, &extensionsv1beta1.SandboxTemplate{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("template get error = %v, want not found", err)
	}
}

func TestDeleteManagedPoolIfUnusedDoesNotDeleteUnmarkedTemplateAfterPoolGone(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	poolName := "managed-pool"
	template := &extensionsv1beta1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: sandboxTemplateName(poolName), Namespace: "default"},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(template).Build()
	gw := New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, "default"), nil, nil, nil, GatewayConfig{Namespace: "default"}, NewMemoryStore())

	deleted, err := gw.deleteManagedPoolIfUnused(context.Background(), poolName, "default")
	if err != nil {
		t.Fatalf("deleteManagedPoolIfUnused returned error: %v", err)
	}
	if deleted {
		t.Fatal("deleteManagedPoolIfUnused deleted unmarked template after pool was gone")
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: sandboxTemplateName(poolName), Namespace: "default"}, &extensionsv1beta1.SandboxTemplate{}); err != nil {
		t.Fatalf("unmarked template was deleted: %v", err)
	}
}

func hasContainer(containers []corev1.Container, name string) bool {
	return findContainer(containers, name).Name != ""
}

func findContainer(containers []corev1.Container, name string) corev1.Container {
	for _, container := range containers {
		if container.Name == name {
			return container
		}
	}
	return corev1.Container{}
}

func hasVolumeMount(mounts []corev1.VolumeMount, name, mountPath string) bool {
	for _, mount := range mounts {
		if mount.Name == name && mount.MountPath == mountPath {
			return true
		}
	}
	return false
}

func hasVolumeMountName(mounts []corev1.VolumeMount, name string) bool {
	for _, mount := range mounts {
		if mount.Name == name {
			return true
		}
	}
	return false
}

func managedPoolObject(name, namespace string) *extensionsv1beta1.SandboxWarmPool {
	replicas := int32(1)
	return &extensionsv1beta1.SandboxWarmPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				labels.ManagedPoolAnnotation: "true",
			},
		},
		Spec: extensionsv1beta1.SandboxWarmPoolSpec{
			Replicas:    &replicas,
			TemplateRef: extensionsv1beta1.SandboxTemplateRef{Name: sandboxTemplateName(name)},
		},
	}
}

func managedTemplateObject(name, namespace string) *extensionsv1beta1.SandboxTemplate {
	return &extensionsv1beta1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				labels.ManagedPoolAnnotation: "true",
			},
		},
	}
}

type failingRuntimeAllocator struct{}

func (f failingRuntimeAllocator) Start(ctx context.Context) error { return nil }
func (f failingRuntimeAllocator) Stop()                           {}

func (f failingRuntimeAllocator) Allocate(ctx context.Context, req RuntimeAllocateRequest) (*RuntimeAllocation, error) {
	return nil, fmt.Errorf("injected allocation failure")
}

func (f failingRuntimeAllocator) Release(ctx context.Context, allocation RuntimeAllocation) error {
	return nil
}

func (f failingRuntimeAllocator) Resolve(ctx context.Context, allocation RuntimeAllocation, sessionID string) (*RuntimeAllocation, error) {
	return &allocation, nil
}

func (f failingRuntimeAllocator) Touch(ctx context.Context, allocation RuntimeAllocation, sessionID string, at time.Time, lifecycle RuntimeLifecycle) error {
	return nil
}

func (f failingRuntimeAllocator) DiagnosticStats() map[string]AllocatorPoolStats {
	return nil
}

type releaseFailingRuntimeAllocator struct{}

func (r releaseFailingRuntimeAllocator) Start(ctx context.Context) error { return nil }
func (r releaseFailingRuntimeAllocator) Stop()                           {}

func (r releaseFailingRuntimeAllocator) Allocate(ctx context.Context, req RuntimeAllocateRequest) (*RuntimeAllocation, error) {
	return nil, fmt.Errorf("unexpected Allocate")
}

func (r releaseFailingRuntimeAllocator) Release(ctx context.Context, allocation RuntimeAllocation) error {
	return fmt.Errorf("injected release failure")
}

func (r releaseFailingRuntimeAllocator) Resolve(ctx context.Context, allocation RuntimeAllocation, sessionID string) (*RuntimeAllocation, error) {
	return &allocation, nil
}

func (r releaseFailingRuntimeAllocator) Touch(ctx context.Context, allocation RuntimeAllocation, sessionID string, at time.Time, lifecycle RuntimeLifecycle) error {
	return nil
}

func (r releaseFailingRuntimeAllocator) DiagnosticStats() map[string]AllocatorPoolStats {
	return nil
}

func newGatewayTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := sandboxv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add sandbox scheme: %v", err)
	}
	if err := extensionsv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add sandbox extension scheme: %v", err)
	}
	return scheme
}
