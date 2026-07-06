package gateway

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Lincyaw/agent-env/pkg/labels"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGatewayRecoverSessionsValidatesRuntimeBindings(t *testing.T) {
	store := newRecoverableMemoryStore(map[string]*session{
		"sess-ok": {
			Info: SessionInfo{
				ID:          "sess-ok",
				SandboxName: "old-sandbox",
				Namespace:   "default",
				PoolRef:     "code",
				PodName:     "old-pod",
				PodIP:       "10.0.0.1",
			},
			Runtime: RuntimeAllocation{
				Backend:     runtimeBackendSandboxClaim,
				PoolRef:     "code",
				Namespace:   "default",
				ClaimName:   "claim-ok",
				SandboxName: "old-sandbox",
				PodName:     "old-pod",
				PodIP:       "10.0.0.1",
			},
			History:      NewStepHistory(),
			lastTaskTime: time.Now(),
			createdAt:    time.Now(),
		},
		"sess-stale": {
			Info: SessionInfo{
				ID:        "sess-stale",
				Namespace: "default",
				PoolRef:   "code",
				PodName:   "stale-pod",
				PodIP:     "10.0.0.9",
			},
			Runtime: RuntimeAllocation{
				Backend:   runtimeBackendSandboxClaim,
				PoolRef:   "code",
				Namespace: "default",
				ClaimName: "missing-claim",
				PodName:   "stale-pod",
				PodIP:     "10.0.0.9",
			},
			History:      NewStepHistory(),
			lastTaskTime: time.Now(),
			createdAt:    time.Now(),
		},
	})
	allocator := &recoveryRuntimeAllocator{
		resolved: map[string]RuntimeAllocation{
			"sess-ok": {
				Backend:     runtimeBackendSandboxClaim,
				PoolRef:     "code",
				Namespace:   "default",
				ClaimName:   "claim-ok",
				SandboxName: "sandbox-ok",
				PodName:     "pod-ok",
				PodIP:       "10.0.0.2",
			},
		},
		errs: map[string]error{
			"sess-stale": fmt.Errorf("claim missing"),
		},
	}
	gw := New(nil, allocator, nil, nil, nil, GatewayConfig{}, store)

	recovered, err := gw.RecoverSessions(context.Background())
	if err != nil {
		t.Fatalf("RecoverSessions returned error: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	if store.Count() != 1 {
		t.Fatalf("store count = %d, want 1", store.Count())
	}
	if _, ok := store.Get("sess-stale"); ok {
		t.Fatal("stale session is still active after recovery")
	}
	s, ok := store.Get("sess-ok")
	if !ok {
		t.Fatal("valid session was not recovered")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Info.PodIP != "10.0.0.2" || s.Info.PodName != "pod-ok" || s.Info.SandboxName != "sandbox-ok" {
		t.Fatalf("session info = %#v, want resolved pod/sandbox binding", s.Info)
	}
	if s.Runtime.ClaimName != "claim-ok" || s.Runtime.SandboxName != "sandbox-ok" {
		t.Fatalf("runtime = %#v, want resolved allocation", s.Runtime)
	}
}

func TestMemoryStoreSessionCountDoesNotGoNegative(t *testing.T) {
	store := NewMemoryStore()
	if got := store.IncrCount(-1); got != 0 {
		t.Fatalf("IncrCount(-1) = %d, want 0", got)
	}
	if got := store.IncrCount(2); got != 2 {
		t.Fatalf("IncrCount(2) = %d, want 2", got)
	}
	if got := store.IncrCount(-1); got != 1 {
		t.Fatalf("IncrCount(-1) after 2 = %d, want 1", got)
	}
	if got := store.SetCount(-3); got != 0 {
		t.Fatalf("SetCount(-3) = %d, want 0", got)
	}
}

func TestGatewayRecoverSessionsFromRuntimeBindingsForMemoryStore(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	namespace := "default"
	sessionID := "gw-recover"
	claimName := "gw-recover"
	sandboxName := "sandbox-recover"
	podName := "pod-recover"
	podIP := "10.0.0.8"
	ownerHash := "owner-hash"
	lastActivity := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	replicas := int32(1)

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&extensionsv1beta1.SandboxTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "tmpl", Namespace: namespace},
		},
		&extensionsv1beta1.SandboxWarmPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "code",
				Namespace: namespace,
				Annotations: map[string]string{
					poolProfileAnnotation: "default",
				},
			},
			Spec: extensionsv1beta1.SandboxWarmPoolSpec{
				Replicas:    &replicas,
				TemplateRef: extensionsv1beta1.SandboxTemplateRef{Name: "tmpl"},
			},
			Status: extensionsv1beta1.SandboxWarmPoolStatus{ReadyReplicas: 1},
		},
		&extensionsv1beta1.SandboxClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      claimName,
				Namespace: namespace,
				Annotations: map[string]string{
					labels.SessionAnnotation:      sessionID,
					labels.LastActivityAnnotation: lastActivity.Format(time.RFC3339),
					labels.OwnerKeyHashAnnotation: ownerHash,
					labels.ManagedAnnotation:      "true",
					labels.ExperimentAnnotation:   "exp-1",
				},
			},
			Spec: extensionsv1beta1.SandboxClaimSpec{
				WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "code"},
			},
			Status: extensionsv1beta1.SandboxClaimStatus{
				Conditions: []metav1.Condition{{
					Type:   string(sandboxv1beta1.SandboxConditionReady),
					Status: metav1.ConditionTrue,
				}},
				SandboxStatus: extensionsv1beta1.SandboxStatus{
					Name:   sandboxName,
					PodIPs: []string{podIP},
				},
			},
		},
		&sandboxv1beta1.Sandbox{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sandboxName,
				Namespace: namespace,
				Annotations: map[string]string{
					sandboxv1beta1.SandboxPodNameAnnotation: podName,
				},
			},
			Status: sandboxv1beta1.SandboxStatus{
				PodIPs: []string{podIP},
				Conditions: []metav1.Condition{{
					Type:   string(sandboxv1beta1.SandboxConditionReady),
					Status: metav1.ConditionTrue,
				}},
			},
		},
	).Build()

	allocator := NewSandboxClaimRuntimeAllocator(k8sClient, namespace)
	store := NewMemoryStore()
	gw := New(k8sClient, allocator, nil, nil, nil, GatewayConfig{Namespace: namespace, IdleTimeout: time.Minute}, store)

	recovered, err := gw.RecoverSessions(context.Background())
	if err != nil {
		t.Fatalf("RecoverSessions returned error: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	s, ok := store.Get(sessionID)
	if !ok {
		t.Fatal("session was not recovered into memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ownerKeyHash != ownerHash || !s.managed || s.experimentID != "exp-1" {
		t.Fatalf("recovered metadata owner=%q managed=%v experiment=%q", s.ownerKeyHash, s.managed, s.experimentID)
	}
	if s.Info.PodIP != podIP || s.Info.PodName != podName || s.Info.Status != "active" {
		t.Fatalf("recovered session info = %#v", s.Info)
	}
	if !s.lastTaskTime.Equal(lastActivity) {
		t.Fatalf("lastTaskTime = %v, want %v", s.lastTaskTime, lastActivity)
	}
}

func TestGatewayRecoverSessionsWithDurableStoreUsesLiveClaimsAsSource(t *testing.T) {
	scheme := newGatewayTestScheme(t)
	namespace := "default"
	liveSessionID := "sess-live"
	staleSessionID := "sess-stale"
	claimName := "claim-live"
	sandboxName := "sandbox-live"
	podName := "pod-live"
	podIP := "10.0.0.8"
	lastActivity := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	replicas := int32(1)

	store := newRecoverableMemoryStore(map[string]*session{
		liveSessionID: {
			Info: SessionInfo{
				ID:          liveSessionID,
				SandboxName: "old-sandbox",
				Namespace:   namespace,
				PoolRef:     "code",
				PodName:     "old-pod",
				PodIP:       "10.0.0.1",
			},
			Runtime: RuntimeAllocation{
				Backend:     runtimeBackendSandboxClaim,
				PoolRef:     "code",
				Namespace:   namespace,
				ClaimName:   claimName,
				SandboxName: "old-sandbox",
				PodName:     "old-pod",
				PodIP:       "10.0.0.1",
			},
			History:      NewStepHistory(),
			lastTaskTime: lastActivity,
			createdAt:    lastActivity.Add(-time.Minute),
		},
		staleSessionID: {
			Info: SessionInfo{
				ID:        staleSessionID,
				Namespace: namespace,
				PoolRef:   "code",
				PodName:   "stale-pod",
				PodIP:     "10.0.0.9",
			},
			Runtime: RuntimeAllocation{
				Backend:   runtimeBackendSandboxClaim,
				PoolRef:   "code",
				Namespace: namespace,
				ClaimName: "missing-claim",
				PodName:   "stale-pod",
				PodIP:     "10.0.0.9",
			},
			History:      NewStepHistory(),
			lastTaskTime: lastActivity,
			createdAt:    lastActivity.Add(-time.Minute),
		},
	})
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&extensionsv1beta1.SandboxTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "tmpl", Namespace: namespace},
		},
		&extensionsv1beta1.SandboxWarmPool{
			ObjectMeta: metav1.ObjectMeta{Name: "code", Namespace: namespace},
			Spec: extensionsv1beta1.SandboxWarmPoolSpec{
				Replicas:    &replicas,
				TemplateRef: extensionsv1beta1.SandboxTemplateRef{Name: "tmpl"},
			},
			Status: extensionsv1beta1.SandboxWarmPoolStatus{ReadyReplicas: 1},
		},
		&extensionsv1beta1.SandboxClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      claimName,
				Namespace: namespace,
				Annotations: map[string]string{
					labels.SessionAnnotation:      liveSessionID,
					labels.LastActivityAnnotation: lastActivity.Format(time.RFC3339),
				},
			},
			Spec: extensionsv1beta1.SandboxClaimSpec{
				WarmPoolRef: extensionsv1beta1.SandboxWarmPoolRef{Name: "code"},
			},
			Status: extensionsv1beta1.SandboxClaimStatus{
				Conditions: []metav1.Condition{{
					Type:   string(sandboxv1beta1.SandboxConditionReady),
					Status: metav1.ConditionTrue,
				}},
				SandboxStatus: extensionsv1beta1.SandboxStatus{
					Name:   sandboxName,
					PodIPs: []string{podIP},
				},
			},
		},
		&sandboxv1beta1.Sandbox{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sandboxName,
				Namespace: namespace,
				Annotations: map[string]string{
					sandboxv1beta1.SandboxPodNameAnnotation: podName,
				},
			},
			Status: sandboxv1beta1.SandboxStatus{
				PodIPs: []string{podIP},
				Conditions: []metav1.Condition{{
					Type:   string(sandboxv1beta1.SandboxConditionReady),
					Status: metav1.ConditionTrue,
				}},
			},
		},
	).Build()
	allocator := NewSandboxClaimRuntimeAllocator(k8sClient, namespace)
	gw := New(k8sClient, allocator, nil, nil, nil, GatewayConfig{Namespace: namespace, IdleTimeout: time.Minute}, store)

	recovered, err := gw.RecoverSessions(context.Background())
	if err != nil {
		t.Fatalf("RecoverSessions returned error: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	if store.recoverActiveCalls != 0 {
		t.Fatalf("full durable recovery calls = %d, want 0", store.recoverActiveCalls)
	}
	if len(store.recoverSessionCalls) != 1 || store.recoverSessionCalls[0] != liveSessionID {
		t.Fatalf("targeted recovery calls = %#v, want [%q]", store.recoverSessionCalls, liveSessionID)
	}
	if store.Count() != 1 {
		t.Fatalf("store count = %d, want 1", store.Count())
	}
	s, ok := store.Get(liveSessionID)
	if !ok {
		t.Fatal("live session was not recovered")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Info.PodIP != podIP || s.Info.PodName != podName || s.Info.SandboxName != sandboxName {
		t.Fatalf("session info = %#v, want resolved runtime binding", s.Info)
	}
}

type recoverableMemoryStore struct {
	mu                  sync.Mutex
	sessions            map[string]*session
	count               int64
	recoverActiveCalls  int
	recoverSessionCalls []string
}

func newRecoverableMemoryStore(sessions map[string]*session) *recoverableMemoryStore {
	cp := make(map[string]*session, len(sessions))
	for id, s := range sessions {
		cp[id] = s
	}
	return &recoverableMemoryStore{
		sessions: cp,
		count:    int64(len(cp)),
	}
}

func (s *recoverableMemoryStore) Get(sessionID string) (*session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	return session, ok
}

func (s *recoverableMemoryStore) Set(sessionID string, session *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[sessionID]; !exists {
		s.count++
	}
	s.sessions[sessionID] = session
}

func (s *recoverableMemoryStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[sessionID]; exists {
		delete(s.sessions, sessionID)
		s.count--
	}
}

func (s *recoverableMemoryStore) Range(fn func(sessionID string, s *session) bool) {
	s.mu.Lock()
	items := make(map[string]*session, len(s.sessions))
	for id, session := range s.sessions {
		items[id] = session
	}
	s.mu.Unlock()

	for id, session := range items {
		if !fn(id, session) {
			return
		}
	}
}

func (s *recoverableMemoryStore) Count() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func (s *recoverableMemoryStore) IncrCount(delta int64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count += delta
	return s.count
}

func (s *recoverableMemoryStore) SetCount(count int64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count = count
	return count
}

func (s *recoverableMemoryStore) Sync(_ string) {}

func (s *recoverableMemoryStore) SyncHistory(_ string) {}

func (s *recoverableMemoryStore) GetHistorical(_ string) (*session, bool) { return nil, false }

func (s *recoverableMemoryStore) FindByExperiment(_ string) []string { return nil }

func (s *recoverableMemoryStore) Close() error {
	return nil
}

func (s *recoverableMemoryStore) RecoverActiveSessions(ctx context.Context) (map[string]*session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoverActiveCalls++
	recovered := make(map[string]*session, len(s.sessions))
	for id, session := range s.sessions {
		recovered[id] = session
	}
	return recovered, nil
}

func (s *recoverableMemoryStore) RecoverSession(ctx context.Context, sessionID string) (sessionRecoveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return sessionRecoveryRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoverSessionCalls = append(s.recoverSessionCalls, sessionID)
	session, ok := s.sessions[sessionID]
	if !ok {
		return sessionRecoveryRecord{}, nil
	}
	session.mu.RLock()
	deleted := session.closed
	session.mu.RUnlock()
	if deleted {
		return sessionRecoveryRecord{found: true, deleted: true}, nil
	}
	return sessionRecoveryRecord{session: session, found: true}, nil
}

type recoveryRuntimeAllocator struct {
	resolved map[string]RuntimeAllocation
	errs     map[string]error
}

func (a *recoveryRuntimeAllocator) Start(ctx context.Context) error { return nil }
func (a *recoveryRuntimeAllocator) Stop()                           {}

func (a *recoveryRuntimeAllocator) Allocate(ctx context.Context, req RuntimeAllocateRequest) (*RuntimeAllocation, error) {
	return nil, fmt.Errorf("unexpected Allocate in recovery test")
}

func (a *recoveryRuntimeAllocator) Release(ctx context.Context, allocation RuntimeAllocation) error {
	return nil
}

func (a *recoveryRuntimeAllocator) Resolve(ctx context.Context, allocation RuntimeAllocation, sessionID string) (*RuntimeAllocation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := a.errs[sessionID]; err != nil {
		return nil, err
	}
	allocation, ok := a.resolved[sessionID]
	if !ok {
		return nil, fmt.Errorf("no resolved allocation for %s", sessionID)
	}
	return &allocation, nil
}

func (a *recoveryRuntimeAllocator) Touch(ctx context.Context, allocation RuntimeAllocation, sessionID string, at time.Time, lifecycle RuntimeLifecycle) error {
	return nil
}

func (a *recoveryRuntimeAllocator) DiagnosticStats() map[string]AllocatorPoolStats {
	return nil
}
