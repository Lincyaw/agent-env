package gateway

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Lincyaw/agent-env/pkg/labels"
	"github.com/Lincyaw/agent-env/pkg/scheduling"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestK8sLifecycleIntegration(t *testing.T) {
	k8sClient, namespace := setupK8sLifecycleIntegration(t)

	t.Run("DeletePoolDrainsSessionsClaimsAndStopsPool", func(t *testing.T) {
		ctx := context.Background()
		poolName := "pool-drain"
		claimName := "claim-drain"
		sessionID := "session-drain"
		gw := newIntegrationGateway(k8sClient, namespace)

		if err := gw.CreatePool(ctx, CreatePoolRequest{
			Name:      poolName,
			Namespace: namespace,
			Image:     "busybox:1.36",
			Replicas:  0,
		}); err != nil {
			t.Fatalf("CreatePool returned error: %v", err)
		}
		createIntegrationClaim(t, k8sClient, namespace, claimName, poolName, sessionID)
		now := time.Now()
		gw.store.Set(sessionID, &session{
			Info: SessionInfo{
				ID:        sessionID,
				Namespace: namespace,
				PoolRef:   poolName,
				Status:    "active",
				CreatedAt: now,
			},
			Runtime: RuntimeAllocation{
				Namespace: namespace,
				PoolRef:   poolName,
				ClaimName: claimName,
			},
			History:      NewStepHistory(),
			lastTaskTime: now,
			createdAt:    now,
		})
		gw.store.IncrCount(1)

		if err := gw.DeletePool(ctx, poolName, namespace); err != nil {
			t.Fatalf("DeletePool returned error: %v", err)
		}

		assertActiveSessionGone(t, gw.store, sessionID, "pool_deleted")
		assertK8sNotFoundEventually(t, k8sClient, namespace, claimName, &extensionsv1beta1.SandboxClaim{})
		assertK8sPoolStopped(t, k8sClient, namespace, poolName)
		assertK8sExists(t, k8sClient, namespace, sandboxTemplateName(poolName), &extensionsv1beta1.SandboxTemplate{})
	})

	t.Run("DestroyPoolDeletesStoppedPoolAndOwnedTemplate", func(t *testing.T) {
		ctx := context.Background()
		poolName := "pool-destroy"
		gw := newIntegrationGateway(k8sClient, namespace)

		if err := gw.CreatePool(ctx, CreatePoolRequest{
			Name:      poolName,
			Namespace: namespace,
			Image:     "busybox:1.36",
			Replicas:  0,
		}); err != nil {
			t.Fatalf("CreatePool returned error: %v", err)
		}

		if err := gw.DestroyPool(ctx, poolName, namespace); err != nil {
			t.Fatalf("DestroyPool returned error: %v", err)
		}

		assertK8sNotFoundEventually(t, k8sClient, namespace, poolName, &extensionsv1beta1.SandboxWarmPool{})
		assertK8sNotFoundEventually(t, k8sClient, namespace, sandboxTemplateName(poolName), &extensionsv1beta1.SandboxTemplate{})
	})

	t.Run("RuntimeReaperDeletesOrphanIdleClaim", func(t *testing.T) {
		ctx := context.Background()
		poolName := "pool-orphan"
		claimName := "claim-orphan"
		sessionID := "session-orphan"
		gw := newIntegrationGateway(k8sClient, namespace)

		if err := gw.CreatePool(ctx, CreatePoolRequest{
			Name:      poolName,
			Namespace: namespace,
			Image:     "busybox:1.36",
			Replicas:  0,
		}); err != nil {
			t.Fatalf("CreatePool returned error: %v", err)
		}
		shutdownAt := metav1.NewTime(time.Now().Add(-15 * time.Minute))
		createIntegrationClaim(t, k8sClient, namespace, claimName, poolName, sessionID, func(claim *extensionsv1beta1.SandboxClaim) {
			claim.Spec.Lifecycle = &extensionsv1beta1.Lifecycle{
				ShutdownTime:   &shutdownAt,
				ShutdownPolicy: extensionsv1beta1.ShutdownPolicyDeleteForeground,
			}
			claim.Annotations[labels.IdleTimeoutAnnotation] = "60"
		})

		if err := gw.reapRuntimeClaims(ctx, time.Now()); err != nil {
			t.Fatalf("reapRuntimeClaims returned error: %v", err)
		}

		assertK8sNotFoundEventually(t, k8sClient, namespace, claimName, &extensionsv1beta1.SandboxClaim{})
		assertK8sExists(t, k8sClient, namespace, poolName, &extensionsv1beta1.SandboxWarmPool{})
	})

	t.Run("DeleteExperimentStopsUnusedManagedPool", func(t *testing.T) {
		ctx := context.Background()
		poolName := "managed-exp-clean"
		claimName := "claim-exp-clean"
		sessionID := "session-exp-clean"
		gw := newIntegrationGateway(k8sClient, namespace)

		if err := gw.CreatePool(ctx, CreatePoolRequest{
			Name:      poolName,
			Namespace: namespace,
			Image:     "busybox:1.36",
			Replicas:  0,
			Managed:   true,
		}); err != nil {
			t.Fatalf("CreatePool returned error: %v", err)
		}
		createIntegrationClaim(t, k8sClient, namespace, claimName, poolName, sessionID)
		putIntegrationSession(gw.store, namespace, poolName, claimName, sessionID, "exp-clean")

		deleted, err := gw.DeleteExperiment(ctx, "exp-clean")
		if err != nil {
			t.Fatalf("DeleteExperiment returned error: %v", err)
		}
		if deleted != 1 {
			t.Fatalf("deleted = %d, want 1", deleted)
		}

		assertK8sNotFoundEventually(t, k8sClient, namespace, claimName, &extensionsv1beta1.SandboxClaim{})
		assertK8sPoolStopped(t, k8sClient, namespace, poolName)
		assertK8sExists(t, k8sClient, namespace, sandboxTemplateName(poolName), &extensionsv1beta1.SandboxTemplate{})
	})

	t.Run("DeleteExperimentKeepsManagedPoolStillInUse", func(t *testing.T) {
		ctx := context.Background()
		poolName := "managed-exp-shared"
		gw := newIntegrationGateway(k8sClient, namespace)

		if err := gw.CreatePool(ctx, CreatePoolRequest{
			Name:      poolName,
			Namespace: namespace,
			Image:     "busybox:1.36",
			Replicas:  0,
			Managed:   true,
		}); err != nil {
			t.Fatalf("CreatePool returned error: %v", err)
		}
		createIntegrationClaim(t, k8sClient, namespace, "claim-shared-1", poolName, "session-shared-1")
		createIntegrationClaim(t, k8sClient, namespace, "claim-shared-2", poolName, "session-shared-2")
		putIntegrationSession(gw.store, namespace, poolName, "claim-shared-1", "session-shared-1", "exp-one")
		putIntegrationSession(gw.store, namespace, poolName, "claim-shared-2", "session-shared-2", "exp-two")

		deleted, err := gw.DeleteExperiment(ctx, "exp-one")
		if err != nil {
			t.Fatalf("DeleteExperiment returned error: %v", err)
		}
		if deleted != 1 {
			t.Fatalf("deleted = %d, want 1", deleted)
		}

		assertK8sNotFoundEventually(t, k8sClient, namespace, "claim-shared-1", &extensionsv1beta1.SandboxClaim{})
		assertK8sExists(t, k8sClient, namespace, "claim-shared-2", &extensionsv1beta1.SandboxClaim{})
		assertK8sExists(t, k8sClient, namespace, poolName, &extensionsv1beta1.SandboxWarmPool{})
		assertK8sExists(t, k8sClient, namespace, sandboxTemplateName(poolName), &extensionsv1beta1.SandboxTemplate{})
	})
}

func setupK8sLifecycleIntegration(t *testing.T) (client.Client, string) {
	t.Helper()
	if os.Getenv("ARL_K8S_INTEGRATION") != "1" {
		t.Skip("set ARL_K8S_INTEGRATION=1 to run real Kubernetes lifecycle tests")
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	if contextName := strings.TrimSpace(os.Getenv("ARL_K8S_CONTEXT")); contextName != "" {
		overrides.CurrentContext = contextName
	}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		t.Fatalf("load kubeconfig: %v", err)
	}
	contextName := overrides.CurrentContext
	if contextName == "" {
		contextName = rawConfig.CurrentContext
	}
	if !strings.Contains(strings.ToLower(contextName), "orbstack") && os.Getenv("ARL_K8S_ALLOW_NON_ORBSTACK") != "1" {
		t.Skipf("refusing destructive integration test on context %q; set ARL_K8S_CONTEXT=orbstack or ARL_K8S_ALLOW_NON_ORBSTACK=1", contextName)
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		t.Fatalf("build Kubernetes rest config: %v", err)
	}
	scheme := newGatewayTestScheme(t)
	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("create Kubernetes client: %v", err)
	}

	namespace := fmt.Sprintf("arl-lifecycle-it-%d", time.Now().UnixNano())
	if err := k8sClient.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}); err != nil {
		t.Fatalf("create integration namespace %s: %v", namespace, err)
	}
	if os.Getenv("ARL_K8S_KEEP_NAMESPACE") != "1" {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
		})
	}
	t.Logf("running Kubernetes lifecycle integration tests in context=%s namespace=%s", contextName, namespace)
	return k8sClient, namespace
}

func newIntegrationGateway(k8sClient client.Client, namespace string) *Gateway {
	store := NewMemoryStore()
	return New(k8sClient, NewSandboxClaimRuntimeAllocator(k8sClient, namespace), nil, nil, nil, GatewayConfig{
		Namespace:     namespace,
		GRPCAuthToken: "integration-token",
	}, store)
}

func createIntegrationClaim(t *testing.T, k8sClient client.Client, namespace, claimName, poolName, sessionID string, mutate ...func(*extensionsv1beta1.SandboxClaim)) {
	t.Helper()
	claim := &extensionsv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: namespace,
			Annotations: map[string]string{
				labels.SessionAnnotation:      sessionID,
				labels.LastActivityAnnotation: time.Now().UTC().Format(time.RFC3339),
			},
		},
		Spec: extensionsv1beta1.SandboxClaimSpec{
			WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: poolName},
		},
	}
	for _, fn := range mutate {
		fn(claim)
	}
	if err := k8sClient.Create(context.Background(), claim); err != nil {
		t.Fatalf("create sandbox claim %s/%s: %v", namespace, claimName, err)
	}
}

func putIntegrationSession(store SessionStore, namespace, poolName, claimName, sessionID, experimentID string) {
	now := time.Now()
	store.Set(sessionID, &session{
		Info: SessionInfo{
			ID:        sessionID,
			Namespace: namespace,
			PoolRef:   poolName,
			Status:    "active",
			CreatedAt: now,
		},
		Runtime: RuntimeAllocation{
			Namespace: namespace,
			PoolRef:   poolName,
			ClaimName: claimName,
		},
		History:      NewStepHistory(),
		managed:      true,
		experimentID: experimentID,
		lastTaskTime: now,
		createdAt:    now,
	})
	store.IncrCount(1)
}

func assertActiveSessionGone(t *testing.T, store SessionStore, sessionID, reason string) {
	t.Helper()
	if _, ok := store.Get(sessionID); ok {
		t.Fatalf("session %s is still active", sessionID)
	}
	historical, ok := store.(interface {
		GetHistorical(string) (*session, bool)
	})
	if !ok {
		return
	}
	s, ok := historical.GetHistorical(sessionID)
	if !ok {
		t.Fatalf("session %s tombstone not found", sessionID)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Info.Status != "deleted" || s.Info.DeletionReason != reason {
		t.Fatalf("session %s status/reason = %q/%q, want deleted/%s", sessionID, s.Info.Status, s.Info.DeletionReason, reason)
	}
}

func assertK8sNotFoundEventually(t *testing.T, k8sClient client.Client, namespace, name string, obj client.Object) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	key := types.NamespacedName{Name: name, Namespace: namespace}
	err := wait.PollUntilContextTimeout(ctx, 250*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		err := k8sClient.Get(ctx, key, obj)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("object %T %s/%s still exists or could not be read: %v", obj, namespace, name, err)
	}
}

func assertK8sExists(t *testing.T, k8sClient client.Client, namespace, name string, obj client.Object) {
	t.Helper()
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, obj); err != nil {
		t.Fatalf("expected %T %s/%s to exist: %v", obj, namespace, name, err)
	}
}

func assertK8sPoolStopped(t *testing.T, k8sClient client.Client, namespace, name string) {
	t.Helper()
	pool := &extensionsv1beta1.SandboxWarmPool{}
	assertK8sExists(t, k8sClient, namespace, name, pool)
	if got := desiredSandboxWarmPoolReplicas(pool); got != 0 {
		t.Fatalf("pool %s/%s replicas = %d, want 0", namespace, name, got)
	}
	if got := pool.Annotations[labels.PoolStateAnnotation]; got != labels.PoolStateStopped {
		t.Fatalf("pool %s/%s state = %q, want %q", namespace, name, got, labels.PoolStateStopped)
	}
	if got := pool.Annotations[scheduling.PoolAutoscaleAnnotation]; got != "false" {
		t.Fatalf("pool %s/%s autoscale annotation = %q, want false", namespace, name, got)
	}
}
