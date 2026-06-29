package gateway

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
