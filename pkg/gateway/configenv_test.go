package gateway

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	configenvutil "github.com/Lincyaw/agent-env/pkg/configenv"
	"github.com/Lincyaw/agent-env/pkg/labels"
)

func TestClaimPodRejectsStaleConfigHash(t *testing.T) {
	scheme := newGatewayTestScheme(t)

	pool := &arlv1alpha1.WarmPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "default"},
		Spec: arlv1alpha1.WarmPoolSpec{
			ConfigEnv: &arlv1alpha1.ConfigEnvSpec{
				Vars: map[string]string{"name": "demo"},
				EnvVars: []corev1.EnvVar{{
					Name:  "APP_NAME",
					Value: "{{ .Vars.name }}",
				}},
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "default",
			Labels: map[string]string{
				labels.PoolLabelKey:   "pool",
				labels.StatusLabelKey: labels.StatusIdle,
			},
			Annotations: map[string]string{
				configenvutil.HashAnnotation: "stale",
			},
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, pod).Build()
	pa := &PodAllocator{k8sClient: k8sClient}

	claimed, err := pa.claimPod(context.Background(), pod.DeepCopy())
	if err != nil {
		t.Fatalf("claimPod returned error: %v", err)
	}
	if claimed {
		t.Fatal("claimPod claimed a pod with stale config hash")
	}

	got := &corev1.Pod{}
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(pod), got); err != nil {
		t.Fatalf("get pod after claim attempt: %v", err)
	}
	if got.Labels[labels.StatusLabelKey] != labels.StatusIdle {
		t.Fatalf("pod status label = %q, want %q", got.Labels[labels.StatusLabelKey], labels.StatusIdle)
	}
}

func TestClaimPodClaimsMatchingConfigHash(t *testing.T) {
	scheme := newGatewayTestScheme(t)

	pool := &arlv1alpha1.WarmPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "default"},
		Spec: arlv1alpha1.WarmPoolSpec{
			ConfigEnv: &arlv1alpha1.ConfigEnvSpec{
				Vars: map[string]string{"name": "demo"},
				EnvVars: []corev1.EnvVar{{
					Name:  "APP_NAME",
					Value: "{{ .Vars.name }}",
				}},
			},
		},
	}
	hash, err := configenvutil.DesiredHashForPool(pool)
	if err != nil {
		t.Fatalf("DesiredHashForPool returned error: %v", err)
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "default",
			Labels: map[string]string{
				labels.PoolLabelKey:   "pool",
				labels.StatusLabelKey: labels.StatusIdle,
			},
			Annotations: map[string]string{
				configenvutil.HashAnnotation: hash,
			},
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, pod).Build()
	pa := &PodAllocator{k8sClient: k8sClient}

	claimed, err := pa.claimPod(context.Background(), pod.DeepCopy())
	if err != nil {
		t.Fatalf("claimPod returned error: %v", err)
	}
	if !claimed {
		t.Fatal("claimPod did not claim a pod with matching config hash")
	}

	got := &corev1.Pod{}
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(pod), got); err != nil {
		t.Fatalf("get pod after claim attempt: %v", err)
	}
	if got.Labels[labels.StatusLabelKey] != labels.StatusAllocated {
		t.Fatalf("pod status label = %q, want %q", got.Labels[labels.StatusLabelKey], labels.StatusAllocated)
	}
	if got.Annotations[labels.LastActivityAnnotation] == "" {
		t.Fatal("claimPod did not set last-activity annotation")
	}
}

func TestCheckPoolHealthFailsOnConfigEnvCondition(t *testing.T) {
	scheme := newGatewayTestScheme(t)

	pool := &arlv1alpha1.WarmPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "default"},
		Status: arlv1alpha1.WarmPoolStatus{
			Conditions: []metav1.Condition{{
				Type:    configenvutil.ReadyConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  "ConfigEnvFailed",
				Message: "missing template var",
			}},
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	gw := &Gateway{k8sClient: k8sClient}

	err := gw.checkPoolHealth(context.Background(), "pool", "default")
	if err == nil {
		t.Fatal("checkPoolHealth succeeded for pool with failed configEnv condition")
	}
	if !strings.Contains(err.Error(), "invalid configEnv") {
		t.Fatalf("checkPoolHealth error = %q, want invalid configEnv message", err)
	}
}

func newGatewayTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}
	if err := arlv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add arlv1alpha1 scheme: %v", err)
	}
	return scheme
}
