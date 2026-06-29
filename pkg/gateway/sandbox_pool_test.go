package gateway

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
			SidecarImage:       "arl-sidecar:orbstack",
			ExecutorAgentImage: "arl-executor-agent:orbstack",
			ImagePullPolicy:    string(corev1.PullIfNotPresent),
			SidecarHTTPPort:    8080,
			SidecarGRPCPort:    9090,
			WorkspaceDir:       "/workspace",
			GRPCAuthToken:      "test-token",
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
	if !hasVolumeMount(sidecar.VolumeMounts, "workspace", "/workspace") {
		t.Fatalf("sidecar workspace mounts = %#v, want workspace mounted at /workspace", sidecar.VolumeMounts)
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
