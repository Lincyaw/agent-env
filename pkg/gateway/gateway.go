package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/audit"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/Lincyaw/agent-env/pkg/labels"
	"github.com/Lincyaw/agent-env/pkg/scheduling"
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

// GatewayConfig holds Gateway-level configuration.
type GatewayConfig struct {
	IdleTimeout                     time.Duration
	MaxLifetime                     time.Duration
	DevboxIdleTimeout               time.Duration
	DevboxMaxLifetime               time.Duration
	SweepInterval                   time.Duration
	Namespace                       string
	SidecarImage                    string
	SidecarHTTPPort                 int
	SidecarGRPCPort                 int
	WorkspaceDir                    string
	ExecutorAgentImage              string
	ImagePullPolicy                 string
	GRPCAuthToken                   string
	GRPCAuthSecretName              string
	PodHTTPProxy                    string
	PodNoProxy                      string
	AdmissionQueueTimeout           time.Duration
	AdmissionQueuePollInterval      time.Duration
	PoolAutoscalerEnabled           bool
	PoolAutoscalerInterval          time.Duration
	PoolAutoscalerBuffer            int32
	PoolAutoscalerMinReplicas       int32
	PoolAutoscalerMaxReplicas       int32
	SchedulerName                   string
	ImageLocalityEnabled            bool
	DefaultSandboxRequestCPU        string
	DefaultSandboxRequestMemory     string
	DefaultSandboxLimitCPU          string
	DefaultSandboxLimitMemory       string
	SandboxNetworkPolicyManagement  string
	SandboxRuntimeClassName         string
	SandboxSeccompProfileType       string
	SandboxSeccompLocalhostProfile  string
	SandboxAllowPrivilegeEscalation bool
	K8sRESTConfig                   *rest.Config
}

const (
	defaultSandboxRequestCPU    = "500m"
	defaultSandboxRequestMemory = "512Mi"
	defaultSandboxLimitCPU      = "8"
	defaultSandboxLimitMemory   = "16Gi"
)

// session holds internal session state.
type session struct {
	mu                  sync.RWMutex
	Info                SessionInfo
	Runtime             RuntimeAllocation
	History             *StepHistory
	managed             bool
	experimentID        string
	mode                string
	ownerKeyHash        string
	closed              bool
	deletionReason      string
	deletedAt           *time.Time
	lastTaskTime        time.Time
	lastAnnotationPatch time.Time
	idleTimeout         time.Duration
	maxLifetime         time.Duration
	createdAt           time.Time
	activeExecs         int32 // atomic; >0 means execution in progress, sweep must not kill
	operations          map[string]*executeOperation
	privateContainers   map[string]PrivateContainerSpec
}

func (s *session) runtimeAllocation() RuntimeAllocation {
	allocation := s.Runtime
	if allocation.Namespace == "" {
		allocation.Namespace = s.Info.Namespace
	}
	if allocation.PoolRef == "" {
		allocation.PoolRef = s.Info.PoolRef
	}
	if allocation.PodName == "" {
		allocation.PodName = s.Info.PodName
	}
	if allocation.PodIP == "" {
		allocation.PodIP = s.Info.PodIP
	}
	if allocation.SandboxName == "" {
		allocation.SandboxName = s.Info.SandboxName
	}
	if allocation.Backend == "" {
		allocation.Backend = runtimeBackendSandboxClaim
	}
	return allocation
}

// Gateway manages sessions and forwards execution to sidecars.
type Gateway struct {
	k8sClient           client.Client
	k8sRESTConfig       *rest.Config
	runtimeAllocator    RuntimeAllocator
	poolSelector        PoolSelector
	admissionController AdmissionController
	sidecarClient       interfaces.SidecarClient
	metrics             interfaces.MetricsCollector
	trajectoryWriter    *audit.TrajectoryWriter
	store               SessionStore
	gwConfig            GatewayConfig
	sweepStopCh         chan struct{}
	sweepWg             sync.WaitGroup
	autoscaleStopCh     chan struct{}
	autoscaleStopOnce   sync.Once
	autoscaleWg         sync.WaitGroup
	admissionQueueMu    sync.Mutex
	admissionQueueDepth map[types.NamespacedName]int32
	trajMu              sync.RWMutex
	trajCh              chan audit.TrajectoryEntry
	trajWg              sync.WaitGroup
}

// New creates a new gateway. metrics and trajectoryWriter may be nil.
// If store is nil, a default MemoryStore is used.
func New(k8sClient client.Client, runtimeAllocator RuntimeAllocator, sidecarClient interfaces.SidecarClient, metrics interfaces.MetricsCollector, trajectoryWriter *audit.TrajectoryWriter, gwConfig GatewayConfig, store SessionStore) *Gateway {
	if store == nil {
		store = NewMemoryStore()
	}
	gw := &Gateway{
		k8sClient:           k8sClient,
		k8sRESTConfig:       copyRESTConfig(gwConfig.K8sRESTConfig),
		runtimeAllocator:    runtimeAllocator,
		poolSelector:        DefaultPoolSelector{},
		admissionController: NewDefaultAdmissionController(),
		sidecarClient:       sidecarClient,
		metrics:             metrics,
		trajectoryWriter:    trajectoryWriter,
		store:               store,
		gwConfig:            gwConfig,
		sweepStopCh:         make(chan struct{}),
		autoscaleStopCh:     make(chan struct{}),
		admissionQueueDepth: make(map[types.NamespacedName]int32),
	}
	return gw
}

func copyRESTConfig(cfg *rest.Config) *rest.Config {
	if cfg == nil {
		return nil
	}
	return rest.CopyConfig(cfg)
}

func (g *Gateway) runtimeNamespace() string {
	ns := strings.TrimSpace(g.gwConfig.Namespace)
	if ns == "" {
		return "default"
	}
	return ns
}

func (g *Gateway) resolveNamespace(requested string) (string, error) {
	allowed := g.runtimeNamespace()
	ns := strings.TrimSpace(requested)
	if ns == "" {
		return allowed, nil
	}
	if ns != allowed {
		return "", fmt.Errorf("%w: namespace %q is not allowed; gateway is scoped to namespace %q", ErrNamespaceNotAllowed, ns, allowed)
	}
	return ns, nil
}

// SetTrajectoryWriter installs a ClickHouse writer after gateway startup and
// starts the trajectory worker. If the worker is already running, the new
// writer is closed and ignored.
func (g *Gateway) SetTrajectoryWriter(writer *audit.TrajectoryWriter) {
	if writer == nil {
		return
	}
	g.trajMu.Lock()
	if g.trajCh != nil || g.trajectoryWriter != nil {
		g.trajMu.Unlock()
		writer.Close()
		return
	}
	g.trajectoryWriter = writer
	g.trajMu.Unlock()
	g.StartTrajectoryWorker()
}

// StartTrajectoryWorker starts a single background goroutine to drain the
// trajectory write channel. Must be called after New() if trajectoryWriter is set.
func (g *Gateway) StartTrajectoryWorker() {
	g.trajMu.Lock()
	if g.trajectoryWriter == nil {
		g.trajMu.Unlock()
		return
	}
	if g.trajCh != nil {
		g.trajMu.Unlock()
		return
	}
	writer := g.trajectoryWriter
	ch := make(chan audit.TrajectoryEntry, 4096)
	g.trajCh = ch
	g.trajWg.Add(1)
	g.trajMu.Unlock()

	go func() {
		defer g.trajWg.Done()
		for entry := range ch {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := writer.WriteEntry(ctx, entry); err != nil {
				log.Printf("Warning: failed to write trajectory entry: %v", err)
			}
			cancel()
		}
	}()
}

// StopTrajectoryWorker closes the trajectory channel and waits for the worker to drain.
func (g *Gateway) StopTrajectoryWorker() {
	g.trajMu.Lock()
	ch := g.trajCh
	writer := g.trajectoryWriter
	g.trajCh = nil
	g.trajectoryWriter = nil
	g.trajMu.Unlock()

	if ch != nil {
		close(ch)
	}
	g.trajWg.Wait()
	if writer != nil {
		writer.Close()
	}
}

func (g *Gateway) enqueueTrajectory(entry audit.TrajectoryEntry, sessionID string, step int) {
	g.trajMu.RLock()
	defer g.trajMu.RUnlock()
	if g.trajCh == nil {
		return
	}
	select {
	case g.trajCh <- entry:
	default:
		log.Printf("Warning: trajectory channel full, dropping entry for session %s step %d", sessionID, step)
	}
}

// CreateSession allocates a sandbox runtime from the pool and registers a session.
func (g *Gateway) CreateSession(ctx context.Context, req CreateSessionRequest) (*SessionInfo, error) {
	ctx, span := otel.Tracer("gateway").Start(ctx, "Gateway.CreateSession",
		traceStartAttrs("image", req.Image, "profile", req.Profile, "namespace", req.Namespace),
	)
	defer span.End()

	ns, err := g.resolveNamespace(req.Namespace)
	if err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	if strings.TrimSpace(req.Image) == "" && strings.TrimSpace(req.Profile) == "" && strings.TrimSpace(req.PoolName) == "" {
		err := fmt.Errorf("image or profile is required")
		recordSpanErr(span, err)
		return nil, err
	}
	if len(req.PrivateContainers) > 0 && strings.TrimSpace(req.Image) == "" && strings.TrimSpace(req.PoolName) == "" {
		err := fmt.Errorf("privateContainers require image-backed pool creation or an explicit poolName")
		recordSpanErr(span, err)
		return nil, err
	}
	if err := validatePrivateContainers(req.PrivateContainers); err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	if !validSessionMode(req.Mode) {
		err := fmt.Errorf("invalid session mode: %q (valid: \"\", \"devbox\")", req.Mode)
		recordSpanErr(span, err)
		return nil, err
	}
	claimEnv, err := parseConfigEnvVars(req.ConfigEnv)
	if err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	var autoCreatedPool string
	req, autoCreatedPool, err = g.ensureImageBackedSessionPool(ctx, req, ns)
	if err != nil {
		recordSpanErr(span, err)
		return nil, fmt.Errorf("ensure session pool: %w", err)
	}
	cleanupAutoCreatedPool := func() {
		if autoCreatedPool == "" {
			return
		}
		if stopped, cleanupErr := g.stopManagedPoolIfUnused(ctx, autoCreatedPool, ns); cleanupErr != nil {
			log.Printf("Warning: failed to cleanup unused managed pool %s/%s after session create failure: %v", ns, autoCreatedPool, cleanupErr)
		} else if stopped {
			log.Printf("Stopped unused managed pool %s/%s after session create failure", ns, autoCreatedPool)
		}
	}

	intent := g.resourceIntentFromCreateSession(ctx, req, ns)
	selection, admission, err := g.planSessionAllocation(ctx, intent)
	if err != nil {
		cleanupAutoCreatedPool()
		recordSpanErr(span, err)
		return nil, fmt.Errorf("plan session allocation: %w", err)
	}
	poolRef := selection.PoolName
	ns = selection.Namespace

	if len(claimEnv) > 0 {
		if err := g.ensureClaimEnvInjectionPolicy(ctx, poolRef, ns); err != nil {
			cleanupAutoCreatedPool()
			recordSpanErr(span, err)
			return nil, err
		}
	}

	sessionID := sessionName(req.Image, randomSuffix(8))
	sandboxName := sessionID
	ownerHash, _ := KeyHashFromContext(ctx)
	createdAt := time.Now()
	idleTimeout := g.resolveIdleTimeout(req)
	maxLifetime := g.resolveMaxLifetime(req)
	lifecycle := g.runtimeLifecycle(createdAt, createdAt, idleTimeout, maxLifetime)
	span.SetAttributes(
		attribute.String("session.id", sessionID),
		attribute.String("pool.selected", poolRef),
		attribute.String("pool.selection_reason", selection.Reason),
		attribute.String("admission.reason", admission.Reason),
		attribute.Int("admission.warm_available", int(admission.WarmAvailable)),
	)

	// Allocate a runtime from the selected pool (5-minute timeout).
	allocCtx, allocCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer allocCancel()

	allocStart := time.Now()
	allocation, err := g.runtimeAllocator.Allocate(allocCtx, RuntimeAllocateRequest{
		PoolRef:      poolRef,
		Namespace:    ns,
		SessionID:    sessionID,
		SandboxName:  sandboxName,
		OwnerKeyHash: ownerHash,
		Managed:      req.Managed,
		ExperimentID: req.ExperimentID,
		Mode:         req.Mode,
		Lifecycle:    lifecycle,
		Env:          claimEnv,
	})
	if err != nil {
		recordSpanErr(span, err)
		if g.metrics != nil {
			result := "error"
			if allocCtx.Err() == context.DeadlineExceeded {
				result = "timeout"
			}
			g.metrics.IncrementPodAllocationResult(poolRef, result)
		}
		diag := g.diagnosePoolHealth(ctx, poolRef, ns)
		cleanupAutoCreatedPool()
		return nil, fmt.Errorf("allocate runtime from pool %s: %w (%s)", poolRef, err, diag)
	}
	span.SetAttributes(
		attribute.String("runtime.backend", allocation.Backend),
		attribute.String("pod.name", allocation.PodName),
		attribute.String("pod.ip", allocation.PodIP),
	)

	info := SessionInfo{
		ID:          sessionID,
		SandboxName: sandboxName,
		Namespace:   ns,
		Image:       selection.Pool.Image,
		PoolRef:     poolRef,
		Profile:     selection.Pool.Profile,
		Mode:        req.Mode,
		PodIP:       allocation.PodIP,
		PodName:     allocation.PodName,
		CreatedAt:   createdAt,
		Status:      "active",
	}

	g.store.Set(sessionID, &session{
		Info:                info,
		Runtime:             *allocation,
		History:             NewStepHistory(),
		managed:             req.Managed,
		experimentID:        req.ExperimentID,
		mode:                req.Mode,
		ownerKeyHash:        ownerHash,
		lastTaskTime:        createdAt,
		lastAnnotationPatch: createdAt,
		createdAt:           createdAt,
		idleTimeout:         idleTimeout,
		maxLifetime:         maxLifetime,
		operations:          make(map[string]*executeOperation),
		privateContainers:   privateContainerMap(req.PrivateContainers),
	})

	activeSessions := g.store.IncrCount(1)
	if g.metrics != nil {
		g.metrics.SetActiveSessions(activeSessions)
		allocationDuration := time.Since(allocStart)
		g.metrics.RecordSessionAllocationDuration(poolRef, allocationDuration)
		g.metrics.RecordSandboxReadyDuration(poolRef, allocationDuration)
		g.metrics.IncrementPodAllocationResult(poolRef, "success")
	}

	return &info, nil
}

// GetSession returns session info.
func (g *Gateway) GetSession(sessionID string) (*SessionInfo, error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	s.mu.RLock()
	info := s.Info
	if info.Status == "" {
		info.Status = "active"
	}
	s.mu.RUnlock()
	return &info, nil
}

func (g *Gateway) GetHistoricalSession(sessionID string) (*session, bool) {
	historical, ok := g.store.(interface {
		GetHistorical(string) (*session, bool)
	})
	if !ok {
		return nil, false
	}
	return historical.GetHistorical(sessionID)
}

// DeleteSession releases the sandbox runtime and removes the session.
func (g *Gateway) DeleteSession(ctx context.Context, sessionID string) error {
	return g.deleteSession(ctx, sessionID, "deleted")
}

func (g *Gateway) deleteSession(ctx context.Context, sessionID string, reason string) error {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}
	s.closed = true
	if reason == "" {
		reason = "deleted"
	}
	now := time.Now()
	s.deletionReason = reason
	s.deletedAt = &now
	s.Info.Status = "deleted"
	s.Info.DeletionReason = reason
	s.Info.DeletedAt = &now
	allocation := s.runtimeAllocation()
	podName := allocation.PodName
	podIP := allocation.PodIP
	s.mu.Unlock()

	// Release the runtime by deleting the SandboxClaim. agent-sandbox handles
	// the underlying Sandbox/Pod lifecycle.
	if g.runtimeAllocator != nil {
		if err := g.runtimeAllocator.Release(ctx, allocation); err != nil && !errors.IsNotFound(err) {
			log.Printf("Warning: failed to release runtime %s for session %s: %v", podName, sessionID, err)
		}
	}

	// Close the gRPC connection to the deleted pod
	if podIP != "" && g.sidecarClient != nil {
		if err := g.sidecarClient.CloseConnection(podIP); err != nil {
			log.Printf("Warning: failed to close sidecar connection for pod %s: %v", podName, err)
		}
	}

	g.store.Delete(sessionID)
	activeSessions := g.store.IncrCount(-1)

	if g.metrics != nil {
		g.metrics.SetActiveSessions(activeSessions)
		g.metrics.IncrementSessionDeletion(reason)
	}

	g.cleanupManagedPoolAfterSessionDelete(ctx, allocation)

	return nil
}

func (g *Gateway) cleanupManagedPoolAfterSessionDelete(ctx context.Context, allocation RuntimeAllocation) {
	if g.k8sClient == nil || allocation.PoolRef == "" {
		return
	}
	namespace := allocation.Namespace
	if namespace == "" {
		namespace = g.runtimeNamespace()
	}
	if stopped, err := g.stopManagedPoolIfUnused(ctx, allocation.PoolRef, namespace); err != nil {
		log.Printf("Warning: failed to cleanup unused managed pool %s/%s after session delete: %v", namespace, allocation.PoolRef, err)
	} else if stopped {
		log.Printf("Stopped unused managed pool %s/%s after session delete", namespace, allocation.PoolRef)
	}
}

// RecoverSessions rebuilds the active in-memory session cache after a gateway
// restart. Kubernetes SandboxClaims are the source of truth for active sessions;
// durable store records are loaded only for those live runtimes. Historical
// records remain available through GetHistoricalSession/replay without being
// promoted back to active sessions.
func (g *Gateway) RecoverSessions(ctx context.Context) (int, error) {
	if g.k8sClient != nil && g.runtimeAllocator != nil {
		return g.recoverSessionsFromRuntimeBindings(ctx)
	}
	return g.recoverSessionsFromDurableStore(ctx)
}

func (g *Gateway) recoverSessionsFromDurableStore(ctx context.Context) (int, error) {
	recoverable, ok := g.store.(RecoverableSessionStore)
	if !ok {
		return 0, nil
	}
	recovered, err := recoverable.RecoverActiveSessions(ctx)
	if err != nil {
		return 0, err
	}

	active := 0
	for sessionID, s := range recovered {
		if err := ctx.Err(); err != nil {
			return active, err
		}
		if s == nil {
			continue
		}

		s.mu.RLock()
		closed := s.closed
		allocation := s.runtimeAllocation()
		s.mu.RUnlock()
		if closed {
			g.markSessionDeleted(s, "recovery_closed")
			g.store.Delete(sessionID)
			continue
		}

		if g.runtimeAllocator != nil {
			resolved, err := g.runtimeAllocator.Resolve(ctx, allocation, sessionID)
			if err != nil {
				log.Printf("Warning: dropping recovered session %s: %v", sessionID, err)
				if releaseErr := g.runtimeAllocator.Release(ctx, allocation); releaseErr != nil && !errors.IsNotFound(releaseErr) {
					log.Printf("Warning: failed to release runtime for unrecoverable session %s: %v", sessionID, releaseErr)
				}
				g.markSessionDeleted(s, "recovery_runtime_lost")
				g.store.Delete(sessionID)
				if g.metrics != nil {
					g.metrics.IncrementSessionDeletion("recovery_runtime_lost")
				}
				continue
			}
			s.mu.Lock()
			s.Runtime = *resolved
			if resolved.Namespace != "" {
				s.Info.Namespace = resolved.Namespace
			}
			if resolved.PoolRef != "" {
				s.Info.PoolRef = resolved.PoolRef
			}
			if resolved.PodName != "" {
				s.Info.PodName = resolved.PodName
			}
			if resolved.PodIP != "" {
				s.Info.PodIP = resolved.PodIP
			}
			if resolved.SandboxName != "" {
				s.Info.SandboxName = resolved.SandboxName
			}
			s.mu.Unlock()
		}

		g.store.Set(sessionID, s)
		active++
	}

	if counter, ok := g.store.(SessionCountSetter); ok {
		counter.SetCount(int64(active))
	}
	if g.metrics != nil {
		if _, ok := g.store.(SessionCountSetter); ok {
			g.metrics.SetActiveSessions(int64(active))
		} else {
			g.metrics.SetActiveSessions(g.store.Count())
		}
	}
	return active, nil
}

func (g *Gateway) recoverSessionsFromRuntimeBindings(ctx context.Context) (int, error) {
	if g.k8sClient == nil || g.runtimeAllocator == nil {
		return 0, nil
	}
	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(g.runtimeNamespace())); err != nil {
		return 0, fmt.Errorf("list sandbox claims for recovery: %w", err)
	}

	active := 0
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.DeletionTimestamp != nil {
			continue
		}
		sessionID := strings.TrimSpace(claim.Annotations[labels.SessionAnnotation])
		if sessionID == "" {
			continue
		}
		poolRef := claim.Spec.WarmPoolRef.Name
		allocation := RuntimeAllocation{
			Backend:     runtimeBackendSandboxClaim,
			PoolRef:     poolRef,
			Namespace:   claim.Namespace,
			ClaimName:   claim.Name,
			SandboxName: claim.Status.SandboxStatus.Name,
			PodIP:       firstString(claim.Status.SandboxStatus.PodIPs),
		}
		resolved, err := g.runtimeAllocator.Resolve(ctx, allocation, sessionID)
		if err != nil {
			log.Printf("Warning: cannot recover session %s from claim %s/%s: %v", sessionID, claim.Namespace, claim.Name, err)
			continue
		}

		s, ok, err := g.recoverSessionFromLiveClaim(ctx, sessionID, *resolved, claim)
		if err != nil {
			return active, err
		}
		if !ok {
			continue
		}

		g.store.Set(sessionID, s)
		active++
	}
	if counter, ok := g.store.(SessionCountSetter); ok {
		counter.SetCount(int64(active))
	} else if active > 0 {
		g.store.IncrCount(int64(active))
	}
	if g.metrics != nil {
		g.metrics.SetActiveSessions(g.store.Count())
	}
	return active, nil
}

func (g *Gateway) recoverSessionFromLiveClaim(ctx context.Context, sessionID string, resolved RuntimeAllocation, claim *extensionsv1beta1.SandboxClaim) (*session, bool, error) {
	var s *session
	if targeted, ok := g.store.(targetedRecoverableSessionStore); ok {
		record, err := targeted.RecoverSession(ctx, sessionID)
		if err != nil {
			return nil, false, fmt.Errorf("recover session %s from durable store: %w", sessionID, err)
		}
		if record.deleted {
			log.Printf("Warning: skipping live claim %s/%s for deleted session %s", claim.Namespace, claim.Name, sessionID)
			if releaseErr := g.runtimeAllocator.Release(ctx, resolved); releaseErr != nil && !errors.IsNotFound(releaseErr) {
				log.Printf("Warning: failed to release live claim for deleted session %s: %v", sessionID, releaseErr)
			}
			return nil, false, nil
		}
		s = record.session
	} else if existing, ok := g.store.Get(sessionID); ok {
		s = existing
	}

	if s == nil {
		s = g.newRecoveredSessionFromClaim(ctx, sessionID, resolved, claim)
	}
	if recoveredSessionClosed(s) {
		g.markSessionDeleted(s, "recovery_closed")
		g.store.Delete(sessionID)
		return nil, false, nil
	}

	g.applyRecoveredRuntime(ctx, s, sessionID, resolved, claim)
	return s, true, nil
}

func recoveredSessionClosed(s *session) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

func (g *Gateway) newRecoveredSessionFromClaim(ctx context.Context, sessionID string, resolved RuntimeAllocation, claim *extensionsv1beta1.SandboxClaim) *session {
	info := g.recoveredSessionInfo(ctx, sessionID, resolved, claim)
	lastTask := recoveredLastActivity(claim, info.CreatedAt)
	managed := strings.EqualFold(claim.Annotations[labels.ManagedAnnotation], "true")
	recoveredMode := claim.Annotations[labels.ModeAnnotation]
	idleTimeout := g.gwConfig.IdleTimeout
	maxLifetime := g.gwConfig.MaxLifetime
	if recoveredMode == SessionModeDevbox {
		idleTimeout = g.gwConfig.DevboxIdleTimeout
		maxLifetime = g.gwConfig.DevboxMaxLifetime
	}
	info.Mode = recoveredMode
	return &session{
		Info:         info,
		Runtime:      resolved,
		History:      NewStepHistory(),
		managed:      managed,
		experimentID: claim.Annotations[labels.ExperimentAnnotation],
		mode:         recoveredMode,
		ownerKeyHash: claim.Annotations[labels.OwnerKeyHashAnnotation],
		lastTaskTime: lastTask,
		createdAt:    info.CreatedAt,
		idleTimeout:  idleTimeout,
		maxLifetime:  maxLifetime,
		operations:   make(map[string]*executeOperation),
	}
}

func (g *Gateway) applyRecoveredRuntime(ctx context.Context, s *session, sessionID string, resolved RuntimeAllocation, claim *extensionsv1beta1.SandboxClaim) {
	info := g.recoveredSessionInfo(ctx, sessionID, resolved, claim)
	lastTask := recoveredLastActivity(claim, info.CreatedAt)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.Runtime = resolved
	s.Info.ID = sessionID
	s.Info.Namespace = resolved.Namespace
	s.Info.PoolRef = resolved.PoolRef
	s.Info.PodName = resolved.PodName
	s.Info.PodIP = resolved.PodIP
	s.Info.SandboxName = resolved.SandboxName
	s.Info.Status = "active"
	if s.Info.CreatedAt.IsZero() {
		s.Info.CreatedAt = info.CreatedAt
	}
	if info.Image != "" {
		s.Info.Image = info.Image
	}
	if info.Profile != "" {
		s.Info.Profile = info.Profile
	}
	if s.History == nil {
		s.History = NewStepHistory()
	}
	if s.operations == nil {
		s.operations = make(map[string]*executeOperation)
	}
	if s.createdAt.IsZero() {
		s.createdAt = s.Info.CreatedAt
	}
	if s.lastTaskTime.IsZero() {
		s.lastTaskTime = lastTask
	}
	if s.idleTimeout == 0 {
		s.idleTimeout = g.gwConfig.IdleTimeout
	}
	if s.maxLifetime == 0 {
		s.maxLifetime = g.gwConfig.MaxLifetime
	}
}

func recoveredLastActivity(claim *extensionsv1beta1.SandboxClaim, fallback time.Time) time.Time {
	if raw := strings.TrimSpace(claim.Annotations[labels.LastActivityAnnotation]); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			return parsed
		}
	}
	return fallback
}

func (g *Gateway) recoveredSessionInfo(ctx context.Context, sessionID string, allocation RuntimeAllocation, claim *extensionsv1beta1.SandboxClaim) SessionInfo {
	createdAt := claim.CreationTimestamp.Time
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	info := SessionInfo{
		ID:          sessionID,
		SandboxName: allocation.SandboxName,
		Namespace:   allocation.Namespace,
		PoolRef:     allocation.PoolRef,
		PodIP:       allocation.PodIP,
		PodName:     allocation.PodName,
		CreatedAt:   createdAt,
		Status:      "active",
	}
	if allocation.PoolRef == "" || g.k8sClient == nil {
		return info
	}
	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: allocation.PoolRef, Namespace: allocation.Namespace}, pool); err != nil {
		return info
	}
	poolInfo := g.poolInfoFromSandboxWarmPool(ctx, pool)
	info.Image = poolInfo.Image
	info.Profile = poolInfo.Profile
	return info
}

// resolveSessionPodIP validates the session's SandboxClaim binding before
// returning the current pod IP. Pod IPs are reusable; the Claim/Sandbox identity
// is the control-plane binding.
func (g *Gateway) resolveSessionPodIP(ctx context.Context, sessionID string) (*session, string, error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, "", fmt.Errorf("session %s not found", sessionID)
	}

	s.mu.RLock()
	closed := s.closed
	info := s.Info
	s.mu.RUnlock()
	if closed {
		return nil, "", fmt.Errorf("session %s not found", sessionID)
	}
	if g.runtimeAllocator == nil {
		return nil, "", fmt.Errorf("runtime allocator not configured")
	}
	resolved, err := g.runtimeAllocator.Resolve(ctx, s.runtimeAllocation(), sessionID)
	if err != nil {
		g.dropSession(sessionID, s)
		return nil, "", err
	}

	if resolved.PodIP != info.PodIP {
		if info.PodIP != "" {
			if err := g.sidecarClient.CloseConnection(info.PodIP); err != nil {
				log.Printf("Warning: failed to close stale sidecar connection for pod %s: %v", info.PodName, err)
			}
		}
		s.mu.Lock()
		s.Info.PodIP = resolved.PodIP
		s.Info.PodName = resolved.PodName
		s.Runtime = *resolved
		s.mu.Unlock()
		if rs, ok := g.store.(*RedisStore); ok {
			rs.Sync(sessionID)
		}
	}

	return s, resolved.PodIP, nil
}

func (g *Gateway) acquireSessionPodIP(ctx context.Context, sessionID string) (*session, string, func(), error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, "", func() {}, fmt.Errorf("session %s not found", sessionID)
	}
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return nil, "", func() {}, fmt.Errorf("session %s not found", sessionID)
	}

	atomic.AddInt32(&s.activeExecs, 1)
	stopHeartbeat := func() {}
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			stopHeartbeat()
			g.touchLastTaskTime(sessionID)
			atomic.AddInt32(&s.activeExecs, -1)
		})
	}

	validated, podIP, err := g.resolveSessionPodIP(ctx, sessionID)
	if err != nil {
		release()
		return nil, "", func() {}, err
	}
	g.touchLastTaskTime(sessionID)
	stopHeartbeat = g.startSessionHeartbeat(sessionID, validated)
	return validated, podIP, release, nil
}

func (g *Gateway) dropSession(sessionID string, s *session) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	now := time.Now()
	s.deletionReason = "runtime_lost"
	s.deletedAt = &now
	s.Info.Status = "deleted"
	s.Info.DeletionReason = s.deletionReason
	s.Info.DeletedAt = &now
	info := s.Info
	allocation := s.runtimeAllocation()
	s.mu.Unlock()

	if info.PodIP != "" {
		if g.sidecarClient != nil {
			if err := g.sidecarClient.CloseConnection(info.PodIP); err != nil {
				log.Printf("Warning: failed to close sidecar connection for dropped session %s: %v", sessionID, err)
			}
		}
	}
	if g.runtimeAllocator != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := g.runtimeAllocator.Release(ctx, allocation); err != nil && !errors.IsNotFound(err) {
			log.Printf("Warning: failed to release runtime for dropped session %s: %v", sessionID, err)
		}
		cancel()
	}
	g.store.Delete(sessionID)
	activeSessions := g.store.IncrCount(-1)
	if g.metrics != nil {
		g.metrics.SetActiveSessions(activeSessions)
		g.metrics.IncrementSessionDeletion("runtime_lost")
	}

	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cleanupCancel()
	g.cleanupManagedPoolAfterSessionDelete(cleanupCtx, allocation)
}

func (g *Gateway) markSessionDeleted(s *session, reason string) {
	if s == nil {
		return
	}
	if reason == "" {
		reason = "deleted"
	}
	now := time.Now()
	s.mu.Lock()
	s.closed = true
	s.deletionReason = reason
	s.deletedAt = &now
	s.Info.Status = "deleted"
	s.Info.DeletionReason = reason
	s.Info.DeletedAt = &now
	s.mu.Unlock()
}

// ExecuteSteps executes steps directly via sidecar gRPC.
func (g *Gateway) ExecuteSteps(ctx context.Context, sessionID string, req ExecuteRequest) (*ExecuteResponse, error) {
	return g.executeStepsWithOperation(ctx, sessionID, req)
}

func (g *Gateway) executeStepsNow(ctx context.Context, sessionID string, req ExecuteRequest) (*ExecuteResponse, error) {
	ctx, span := otel.Tracer("gateway").Start(ctx, "Gateway.ExecuteSteps",
		traceStartAttrs("session.id", sessionID),
	)
	span.SetAttributes(attribute.Int("steps.count", len(req.Steps)))
	defer span.End()

	s, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	defer releaseSession()

	span.SetAttributes(attribute.String("pod.ip", podIP))

	resp := &ExecuteResponse{
		SessionID:   sessionID,
		OperationID: req.OperationID,
	}

	totalStart := time.Now()

	for _, step := range req.Steps {
		start := time.Now()
		inputJSON, _ := json.Marshal(step)

		var result StepResult
		result.Name = step.Name
		result.Input = inputJSON
		result.Timestamp = start

		execReq := &sidecar.ExecRequest{
			Command:        step.Command,
			Env:            step.Env,
			WorkingDir:     step.WorkDir,
			TimeoutSeconds: resolveStepTimeoutSeconds(step),
		}
		grpcStart := time.Now()
		execResp, err := g.sidecarClient.Execute(ctx, podIP, execReq)
		if g.metrics != nil {
			g.metrics.RecordSidecarCallDuration("Execute", time.Since(grpcStart))
		}
		if err != nil {
			result.Output.Stderr = err.Error()
			result.Output.ExitCode = 1
		} else {
			result.Output.Stdout = execResp.GetStdout()
			result.Output.Stderr = execResp.GetStderr()
			result.Output.ExitCode = execResp.GetExitCode()
		}

		result.DurationMs = time.Since(start).Milliseconds()

		// Record step metrics
		if g.metrics != nil {
			stepType := step.Name
			if stepType == "" {
				stepType = "unnamed"
			}
			g.metrics.RecordGatewayStepDuration(stepType, time.Since(start))
			outcome := "success"
			if result.Output.ExitCode != 0 {
				outcome = "error"
			}
			g.metrics.IncrementGatewayStepResult(stepType, outcome)
		}

		// Record in history and get the atomically assigned index
		stepRecord := StepRecord{
			Name:       result.Name,
			Input:      result.Input,
			Output:     result.Output,
			DurationMs: result.DurationMs,
			Timestamp:  result.Timestamp,
		}
		globalIdx := s.History.Add(stepRecord)

		result.Index = globalIdx
		result.SnapshotID = fmt.Sprintf("%d", globalIdx)

		resp.Results = append(resp.Results, result)

		obsJSON, _ := json.Marshal(result.Output)
		g.enqueueTrajectory(audit.TrajectoryEntry{
			SessionID:   sessionID,
			Step:        globalIdx,
			Name:        result.Name,
			Action:      result.Input,
			Observation: obsJSON,
			SnapshotID:  result.SnapshotID,
			DurationMs:  result.DurationMs,
			Timestamp:   result.Timestamp,
		}, sessionID, globalIdx)
	}

	resp.TotalDurationMs = time.Since(totalStart).Milliseconds()

	// Update in-memory lastTaskTime for session idle tracking
	g.touchLastTaskTime(sessionID)

	if rs, ok := g.store.(*RedisStore); ok {
		rs.Sync(sessionID)
	}

	return resp, nil
}

// sseOutputEvent is the partial output streamed as an SSE "output" event.
type sseOutputEvent struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// ExecuteStepsSSE streams step execution as SSE events to the HTTP response writer.
// Each gRPC chunk produces an "output" event; each completed step produces a "result" event.
// History, metrics, and trajectory recording are identical to ExecuteSteps.
func (g *Gateway) ExecuteStepsSSE(w http.ResponseWriter, ctx context.Context, sessionID string, req ExecuteRequest) {
	ctx, span := otel.Tracer("gateway").Start(ctx, "Gateway.ExecuteStepsSSE",
		traceStartAttrs("session.id", sessionID),
	)
	span.SetAttributes(attribute.Int("steps.count", len(req.Steps)))
	defer span.End()

	s, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		recordSpanErr(span, err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}
	defer releaseSession()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	span.SetAttributes(attribute.String("pod.ip", podIP))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	totalStart := time.Now()

	for _, step := range req.Steps {
		start := time.Now()
		inputJSON, _ := json.Marshal(step)

		var result StepResult
		result.Name = step.Name
		result.Input = inputJSON
		result.Timestamp = start

		execReq := &sidecar.ExecRequest{
			Command:        step.Command,
			Env:            step.Env,
			WorkingDir:     step.WorkDir,
			TimeoutSeconds: resolveStepTimeoutSeconds(step),
		}

		grpcStart := time.Now()
		streamCh, err := g.sidecarClient.ExecuteStream(ctx, podIP, execReq)
		if g.metrics != nil {
			g.metrics.RecordSidecarCallDuration("ExecuteStream", time.Since(grpcStart))
		}

		if err != nil {
			result.Output.Stderr = err.Error()
			result.Output.ExitCode = 1
		} else {
			var stdout, stderr strings.Builder
			for chunk := range streamCh {
				stdoutChunk := chunk.GetStdout()
				stderrChunk := chunk.GetStderr()

				if stdoutChunk != "" {
					stdout.WriteString(stdoutChunk)
				}
				if stderrChunk != "" {
					stderr.WriteString(stderrChunk)
				}

				// Stream partial output as SSE event
				if stdoutChunk != "" || stderrChunk != "" {
					outEvt := sseOutputEvent{Stdout: stdoutChunk, Stderr: stderrChunk}
					data, _ := json.Marshal(outEvt)
					fmt.Fprintf(w, "event: output\ndata: %s\n\n", data)
					flusher.Flush()
				}

				if chunk.IsDone() {
					result.Output.ExitCode = chunk.GetExitCode()
				}
			}
			result.Output.Stdout = stdout.String()
			result.Output.Stderr = stderr.String()
		}

		result.DurationMs = time.Since(start).Milliseconds()

		// Record step metrics (same as ExecuteSteps)
		if g.metrics != nil {
			stepType := step.Name
			if stepType == "" {
				stepType = "unnamed"
			}
			g.metrics.RecordGatewayStepDuration(stepType, time.Since(start))
			outcome := "success"
			if result.Output.ExitCode != 0 {
				outcome = "error"
			}
			g.metrics.IncrementGatewayStepResult(stepType, outcome)
		}

		// Record in history (same as ExecuteSteps)
		stepRecord := StepRecord{
			Name:       result.Name,
			Input:      result.Input,
			Output:     result.Output,
			DurationMs: result.DurationMs,
			Timestamp:  result.Timestamp,
		}
		globalIdx := s.History.Add(stepRecord)

		result.Index = globalIdx
		result.SnapshotID = fmt.Sprintf("%d", globalIdx)

		obsJSON, _ := json.Marshal(result.Output)
		g.enqueueTrajectory(audit.TrajectoryEntry{
			SessionID:   sessionID,
			Step:        globalIdx,
			Name:        result.Name,
			Action:      result.Input,
			Observation: obsJSON,
			SnapshotID:  result.SnapshotID,
			DurationMs:  result.DurationMs,
			Timestamp:   result.Timestamp,
		}, sessionID, globalIdx)

		// Stream the completed step result as SSE event
		resultData, _ := json.Marshal(result)
		fmt.Fprintf(w, "event: result\ndata: %s\n\n", resultData)
		flusher.Flush()
	}

	_ = time.Since(totalStart) // totalDurationMs available if needed

	// Update in-memory lastTaskTime for session idle tracking (same as ExecuteSteps)
	g.touchLastTaskTime(sessionID)

	if rs, ok := g.store.(*RedisStore); ok {
		rs.Sync(sessionID)
	}
}

// Restore restores a session to a previous snapshot by allocating a new pod and replaying steps.
// ReplayFrom replays steps from a source session into the target session's sandbox.
// This is the cross-session counterpart of Restore: instead of restoring a session
// to its own earlier state, it replays another session's history into a fresh sandbox.
func (g *Gateway) ReplayFrom(ctx context.Context, targetSessionID string, req ReplayRequest) (*ReplayResponse, error) {
	sourceSession, ok := g.store.Get(req.SourceSessionID)
	if !ok {
		if historical, hasHistory := g.store.(interface {
			GetHistorical(string) (*session, bool)
		}); hasHistory {
			sourceSession, ok = historical.GetHistorical(req.SourceSessionID)
		}
		if !ok {
			return nil, fmt.Errorf("source session %s not found", req.SourceSessionID)
		}
	}
	// Get source history
	var records []StepRecord
	if req.UpToStep != nil {
		records = sourceSession.History.GetUpTo(*req.UpToStep)
	} else {
		records = sourceSession.History.GetAll()
	}

	_, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, targetSessionID)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	replayed := 0
	errors := 0

	for _, record := range records {
		if record.Name == uploadFileStepName {
			continue
		}

		var step StepRequest
		if err := json.Unmarshal(record.Input, &step); err != nil {
			errors++
			continue
		}
		execReq := &sidecar.ExecRequest{
			Command:        step.Command,
			Env:            step.Env,
			WorkingDir:     step.WorkDir,
			TimeoutSeconds: resolveStepTimeoutSeconds(step),
		}
		if _, err := g.sidecarClient.Execute(ctx, podIP, execReq); err != nil {
			errors++
			continue
		}
		replayed++
	}

	g.touchLastTaskTime(targetSessionID)
	return &ReplayResponse{StepsReplayed: replayed, Errors: errors}, nil
}

func (g *Gateway) Restore(ctx context.Context, sessionID string, snapshotID string) (retErr error) {
	restoreStart := time.Now()
	defer func() {
		if g.metrics != nil {
			g.metrics.RecordRestoreDuration(time.Since(restoreStart))
			result := "success"
			if retErr != nil {
				result = "error"
			}
			g.metrics.IncrementRestoreResult(result)
		}
	}()

	targetIdx, err := strconv.Atoi(snapshotID)
	if err != nil {
		return fmt.Errorf("invalid snapshot_id %q: must be a step index", snapshotID)
	}

	s, ok := g.store.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	atomic.AddInt32(&s.activeExecs, 1)
	defer atomic.AddInt32(&s.activeExecs, -1)

	// Get history records up to the target index
	records := s.History.GetUpTo(targetIdx)
	if len(records) == 0 && targetIdx >= 0 {
		// targetIdx 0 might have 1 record; if none, the index is out of range
		if targetIdx > 0 {
			return fmt.Errorf("no history records up to index %d", targetIdx)
		}
	}

	// Read current session info under read lock
	s.mu.RLock()
	oldAllocation := s.runtimeAllocation()
	lifecycle := g.sessionRuntimeLifecycleLocked(s, time.Now())
	s.mu.RUnlock()

	newSandboxName := fmt.Sprintf("%s-r%d", sessionID, time.Now().UnixMilli())

	// Allocate a new runtime from the pool and replay into it.
	allocCtx, allocCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer allocCancel()

	newAllocation, err := g.runtimeAllocator.Allocate(allocCtx, RuntimeAllocateRequest{
		PoolRef:     oldAllocation.PoolRef,
		Namespace:   oldAllocation.Namespace,
		SessionID:   sessionID,
		SandboxName: newSandboxName,
		Lifecycle:   lifecycle,
	})
	if err != nil {
		return fmt.Errorf("allocate new runtime for restore: %w", err)
	}

	// Replay each step from history on the new pod
	for _, record := range records {
		if record.Name == uploadFileStepName {
			continue
		}

		var step StepRequest
		if err := json.Unmarshal(record.Input, &step); err != nil {
			log.Printf("Warning: failed to unmarshal step %d for replay: %v", record.Index, err)
			continue
		}

		execReq := &sidecar.ExecRequest{
			Command:        step.Command,
			Env:            step.Env,
			WorkingDir:     step.WorkDir,
			TimeoutSeconds: resolveStepTimeoutSeconds(step),
		}
		if _, err := g.sidecarClient.Execute(ctx, newAllocation.PodIP, execReq); err != nil {
			if err := g.releaseRestoreAllocation(*newAllocation); err != nil {
				log.Printf("Warning: failed to release runtime %s after restore failure: %v", newAllocation.PodName, err)
			}
			return fmt.Errorf("replay step %d failed: %w", record.Index, err)
		}
	}

	// Update session to point at the new pod under write lock
	s.mu.Lock()
	s.Info.PodIP = newAllocation.PodIP
	s.Info.PodName = newAllocation.PodName
	s.Info.SandboxName = newSandboxName
	s.Runtime = *newAllocation
	s.mu.Unlock()

	// Truncate history to records 0..targetIdx
	s.History.TruncateTo(targetIdx)
	g.touchLastTaskTime(sessionID)
	if rs, ok := g.store.(*RedisStore); ok {
		rs.Sync(sessionID)
	}

	// Release old runtime directly (async, best-effort) and close its gRPC connection.
	go func() {
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer bgCancel()
		if err := g.runtimeAllocator.Release(bgCtx, oldAllocation); err != nil {
			log.Printf("Warning: failed to release old runtime %s: %v", oldAllocation.PodName, err)
		}
		if oldAllocation.PodIP != "" && g.sidecarClient != nil {
			if err := g.sidecarClient.CloseConnection(oldAllocation.PodIP); err != nil {
				log.Printf("Warning: failed to close sidecar connection for old runtime %s: %v", oldAllocation.PodName, err)
			}
		}
	}()

	return nil
}

func (g *Gateway) releaseRestoreAllocation(allocation RuntimeAllocation) error {
	bgCtx, bgCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer bgCancel()
	return g.runtimeAllocator.Release(bgCtx, allocation)
}

// GetHistory returns the execution history for a session.
func (g *Gateway) GetHistory(sessionID string) ([]StepRecord, error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return s.History.GetAll(), nil
}

// ExportTrajectory exports the trajectory as JSONL.
func (g *Gateway) ExportTrajectory(sessionID string) ([]byte, error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return s.History.ExportTrajectory(sessionID)
}

// CreatePool creates an agent-sandbox SandboxTemplate and SandboxWarmPool.
func (g *Gateway) CreatePool(ctx context.Context, req CreatePoolRequest) error {
	ns, err := g.resolveNamespace(req.Namespace)
	if err != nil {
		return err
	}

	replicas := req.Replicas
	if replicas < 0 {
		replicas = 0
	}

	resources := req.Resources
	if resources == nil {
		defaultResources, err := g.defaultSandboxResources()
		if err != nil {
			return err
		}
		resources = &defaultResources
	}

	// Set default workspace dir if not specified
	workspaceDir := req.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = "/workspace"
	}

	if hasJSONPayload(req.ConfigEnv) {
		return fmt.Errorf("pool configEnv is not supported by SandboxWarmPool-backed pools; pass configEnv when creating a session")
	}
	if hasJSONPayload(req.Tools) {
		return fmt.Errorf("tools are not supported by SandboxWarmPool-backed pools yet")
	}
	if err := validatePrivateContainers(req.PrivateContainers); err != nil {
		return err
	}

	templateName := sandboxTemplateName(req.Name)
	existingPool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: ns}, existingPool); err == nil {
		return fmt.Errorf("create sandbox warm pool: %w", errors.NewAlreadyExists(extensionsv1beta1.Resource("sandboxwarmpools"), req.Name))
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("get sandbox warm pool before create: %w", err)
	}
	if err := g.ensureSandboxRuntimeSecret(ctx, ns); err != nil {
		return err
	}

	templateMeta := metav1.ObjectMeta{
		Name:      templateName,
		Namespace: ns,
	}
	poolMeta := metav1.ObjectMeta{
		Name:      req.Name,
		Namespace: ns,
	}
	templateAnnotations := map[string]string{}
	poolAnnotations := map[string]string{}
	podAnnotations := map[string]string{}
	if profile := strings.TrimSpace(req.Profile); profile != "" {
		templateAnnotations[poolProfileAnnotation] = profile
		poolAnnotations[poolProfileAnnotation] = profile
	}
	if req.Managed {
		templateAnnotations[labels.ManagedPoolAnnotation] = "true"
		poolAnnotations[labels.ManagedPoolAnnotation] = "true"
	}
	poolAnnotations[labels.PoolStateAnnotation] = labels.PoolStateRunning
	imageLocalityEnabled := g.gwConfig.ImageLocalityEnabled || hasJSONPayload(req.ImageLocality)
	if imageLocalityEnabled {
		templateAnnotations[scheduling.ImageLocalityAnnotation] = scheduling.ImageLocalityEnabledValue
		poolAnnotations[scheduling.ImageLocalityAnnotation] = scheduling.ImageLocalityEnabledValue
		podAnnotations[scheduling.ImageLocalityAnnotation] = scheduling.ImageLocalityEnabledValue
	}
	if req.Image != "" && (imageLocalityEnabled || strings.TrimSpace(g.gwConfig.SchedulerName) != "") {
		templateAnnotations[scheduling.ExecutorImageAnnotation] = req.Image
		podAnnotations[scheduling.ExecutorImageAnnotation] = req.Image
	}
	if len(templateAnnotations) > 0 {
		templateMeta.Annotations = templateAnnotations
	}
	if len(poolAnnotations) > 0 {
		poolMeta.Annotations = poolAnnotations
	}
	podMetadata := sandboxv1beta1.PodMetadata{}
	if len(podAnnotations) > 0 {
		podMetadata.Annotations = podAnnotations
	}
	template := &extensionsv1beta1.SandboxTemplate{
		ObjectMeta: templateMeta,
		Spec: extensionsv1beta1.SandboxTemplateSpec{
			NetworkPolicyManagement: g.sandboxNetworkPolicyManagement(),
			EnvVarsInjectionPolicy:  extensionsv1beta1.EnvVarsInjectionPolicyOverrides,
			Service:                 boolPtr(false),
			PodTemplate: sandboxv1beta1.PodTemplate{
				ObjectMeta: podMetadata,
				Spec:       g.sandboxPodSpec(req.Image, workspaceDir, *resources, req.PrivateContainers),
			},
		},
	}
	createdTemplate := false
	if err := g.k8sClient.Create(ctx, template); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("create sandbox template: %w", err)
		}
	} else {
		createdTemplate = true
	}

	pool := &extensionsv1beta1.SandboxWarmPool{
		ObjectMeta: poolMeta,
		Spec: extensionsv1beta1.SandboxWarmPoolSpec{
			Replicas:    int32Ptr(replicas),
			TemplateRef: extensionsv1beta1.SandboxTemplateRef{Name: templateName},
			UpdateStrategy: &extensionsv1beta1.SandboxWarmPoolUpdateStrategy{
				Type: extensionsv1beta1.RecreateSandboxWarmPoolUpdateStrategyType,
			},
		},
	}
	if err := g.k8sClient.Create(ctx, pool); err != nil {
		if createdTemplate {
			if cleanupErr := g.k8sClient.Delete(ctx, template); cleanupErr != nil && !errors.IsNotFound(cleanupErr) {
				log.Printf("Warning: failed to cleanup sandbox template %s/%s after pool create failure: %v", ns, templateName, cleanupErr)
			}
		}
		return fmt.Errorf("create sandbox warm pool: %w", err)
	}
	return nil
}

func (g *Gateway) ensureClaimEnvInjectionPolicy(ctx context.Context, poolName, namespace string) error {
	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolName, Namespace: namespace}, pool); err != nil {
		return fmt.Errorf("get sandbox warm pool %s/%s for configEnv: %w", namespace, poolName, err)
	}
	templateName := pool.Spec.TemplateRef.Name
	if templateName == "" {
		return fmt.Errorf("sandbox warm pool %s/%s has no templateRef for configEnv", namespace, poolName)
	}
	template := &extensionsv1beta1.SandboxTemplate{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: templateName, Namespace: namespace}, template); err != nil {
		return fmt.Errorf("get sandbox template %s/%s for configEnv: %w", namespace, templateName, err)
	}
	if template.Spec.EnvVarsInjectionPolicy == extensionsv1beta1.EnvVarsInjectionPolicyOverrides {
		return nil
	}
	patch := client.MergeFrom(template.DeepCopy())
	template.Spec.EnvVarsInjectionPolicy = extensionsv1beta1.EnvVarsInjectionPolicyOverrides
	if err := g.k8sClient.Patch(ctx, template, patch); err != nil {
		return fmt.Errorf("patch sandbox template %s/%s env injection policy: %w", namespace, templateName, err)
	}
	return nil
}

func (g *Gateway) defaultSandboxResources() (corev1.ResourceRequirements, error) {
	requestCPU, err := parseDefaultSandboxQuantity("sandbox default request cpu", g.gwConfig.DefaultSandboxRequestCPU, defaultSandboxRequestCPU)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	requestMemory, err := parseDefaultSandboxQuantity("sandbox default request memory", g.gwConfig.DefaultSandboxRequestMemory, defaultSandboxRequestMemory)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	limitCPU, err := parseDefaultSandboxQuantity("sandbox default limit cpu", g.gwConfig.DefaultSandboxLimitCPU, defaultSandboxLimitCPU)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	limitMemory, err := parseDefaultSandboxQuantity("sandbox default limit memory", g.gwConfig.DefaultSandboxLimitMemory, defaultSandboxLimitMemory)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    requestCPU,
			corev1.ResourceMemory: requestMemory,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    limitCPU,
			corev1.ResourceMemory: limitMemory,
		},
	}, nil
}

func parseDefaultSandboxQuantity(name, configured, fallback string) (resource.Quantity, error) {
	value := strings.TrimSpace(configured)
	if value == "" {
		value = fallback
	}
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("%s must be a valid Kubernetes quantity: %q", name, configured)
	}
	if q.Sign() <= 0 {
		return resource.Quantity{}, fmt.Errorf("%s must be positive: %q", name, value)
	}
	return q, nil
}

// GetPool returns SandboxWarmPool info.
func (g *Gateway) GetPool(ctx context.Context, name, namespace string) (*PoolInfo, error) {
	namespace, err := g.resolveNamespace(namespace)
	if err != nil {
		return nil, err
	}

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pool); err != nil {
		return nil, err
	}

	info := g.poolInfoFromSandboxWarmPool(ctx, pool)
	return &info, nil
}

// ScalePool updates the replica count of a SandboxWarmPool.
func (g *Gateway) ScalePool(ctx context.Context, name string, req ScalePoolRequest) (*PoolInfo, error) {
	ns, err := g.resolveNamespace(req.Namespace)
	if err != nil {
		return nil, err
	}

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, pool); err != nil {
		return nil, fmt.Errorf("get pool: %w", err)
	}

	if req.Resources != nil {
		return nil, fmt.Errorf("updating resources requires updating the SandboxTemplate and is not supported by ScalePool")
	}
	pool.Spec.Replicas = int32Ptr(req.Replicas)
	if pool.Annotations == nil {
		pool.Annotations = make(map[string]string)
	}
	if req.Replicas > 0 {
		pool.Annotations[labels.PoolStateAnnotation] = labels.PoolStateRunning
		delete(pool.Annotations, scheduling.PoolAutoscaleAnnotation)
	} else {
		pool.Annotations[labels.PoolStateAnnotation] = labels.PoolStateStopped
		pool.Annotations[scheduling.PoolAutoscaleAnnotation] = "false"
	}

	if err := g.k8sClient.Update(ctx, pool); err != nil {
		return nil, fmt.Errorf("update pool: %w", err)
	}

	return g.GetPool(ctx, name, ns)
}

// DeletePool drains a pool without deleting its SandboxWarmPool or template.
// It deletes active sessions and SandboxClaims bound to the pool, then sets
// SandboxWarmPool.spec.replicas to 0 so idle warm capacity is torn down by
// agent-sandbox.
func (g *Gateway) DeletePool(ctx context.Context, name, namespace string) error {
	namespace, err := g.resolveNamespace(namespace)
	if err != nil {
		return err
	}

	if err := g.markPoolDraining(ctx, name, namespace); err != nil {
		return err
	}
	if err := g.deleteSessionsForPool(ctx, name, namespace); err != nil {
		return err
	}
	if err := g.deleteClaimsForPool(ctx, name, namespace); err != nil {
		return err
	}
	return g.markPoolStopped(ctx, name, namespace)
}

// DestroyPool drains a pool, then deletes the SandboxWarmPool and the template
// it references when that template is owned by the ARL-created pool contract.
func (g *Gateway) DestroyPool(ctx context.Context, name, namespace string) error {
	namespace, err := g.resolveNamespace(namespace)
	if err != nil {
		return err
	}

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pool); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get pool for destroy: %w", err)
	}
	templateName := pool.Spec.TemplateRef.Name

	if err := g.DeletePool(ctx, name, namespace); err != nil {
		return err
	}

	if err := g.k8sClient.Delete(ctx, pool); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete sandbox warm pool %s/%s: %w", namespace, name, err)
	}
	if templateName == "" {
		return nil
	}
	if err := g.deletePoolTemplateIfOwned(ctx, templateName, name, namespace); err != nil {
		return err
	}
	return nil
}

func (g *Gateway) deleteClaimsForPool(ctx context.Context, poolName, namespace string) error {
	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list sandbox claims for pool delete: %w", err)
	}
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.Spec.WarmPoolRef.Name != poolName {
			continue
		}
		if err := g.k8sClient.Delete(ctx, claim, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete sandbox claim %s/%s for pool %s: %w", namespace, claim.Name, poolName, err)
		}
	}
	return nil
}

func (g *Gateway) markPoolDraining(ctx context.Context, name, namespace string) error {
	return g.patchPoolLifecycle(ctx, name, namespace, 0, labels.PoolStateDraining, true)
}

func (g *Gateway) markPoolStopped(ctx context.Context, name, namespace string) error {
	return g.patchPoolLifecycle(ctx, name, namespace, 0, labels.PoolStateStopped, true)
}

func (g *Gateway) patchPoolLifecycle(ctx context.Context, name, namespace string, replicas int32, state string, disableAutoscale bool) error {
	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pool); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get pool %s/%s: %w", namespace, name, err)
	}
	before := pool.DeepCopy()
	if pool.Annotations == nil {
		pool.Annotations = make(map[string]string)
	}
	pool.Spec.Replicas = int32Ptr(replicas)
	pool.Annotations[labels.PoolStateAnnotation] = state
	if disableAutoscale {
		pool.Annotations[scheduling.PoolAutoscaleAnnotation] = "false"
	}
	if err := g.k8sClient.Patch(ctx, pool, client.MergeFrom(before)); err != nil {
		return fmt.Errorf("patch pool %s/%s lifecycle: %w", namespace, name, err)
	}
	return nil
}

func (g *Gateway) deletePoolTemplateIfOwned(ctx context.Context, templateName, poolName, namespace string) error {
	template := &extensionsv1beta1.SandboxTemplate{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: templateName, Namespace: namespace}, template); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get sandbox template %s/%s for pool destroy: %w", namespace, templateName, err)
	}
	managed := strings.EqualFold(strings.TrimSpace(template.Annotations[labels.ManagedPoolAnnotation]), "true")
	defaultName := templateName == sandboxTemplateName(poolName)
	if !managed && !defaultName {
		return nil
	}
	if err := g.k8sClient.Delete(ctx, template); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete sandbox template %s/%s: %w", namespace, templateName, err)
	}
	return nil
}

func (g *Gateway) deleteSessionsForPool(ctx context.Context, poolName, namespace string) error {
	if g.store == nil {
		return nil
	}

	var sessionIDs []string
	g.store.Range(func(sessionID string, s *session) bool {
		s.mu.RLock()
		closed := s.closed
		allocation := s.runtimeAllocation()
		s.mu.RUnlock()

		if closed || allocation.PoolRef != poolName {
			return true
		}
		if allocation.Namespace != "" && allocation.Namespace != namespace {
			return true
		}
		sessionIDs = append(sessionIDs, sessionID)
		return true
	})

	for _, sessionID := range sessionIDs {
		if err := g.deleteSession(ctx, sessionID, "pool_deleted"); err != nil {
			if isSessionNotFoundError(err, sessionID) {
				continue
			}
			return fmt.Errorf("delete session %s for pool %s: %w", sessionID, poolName, err)
		}
	}
	return nil
}

func isSessionNotFoundError(err error, sessionID string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "session "+sessionID+" not found")
}

// checkPoolHealth returns an error if the SandboxWarmPool cannot be found.
func (g *Gateway) checkPoolHealth(ctx context.Context, poolRef, namespace string) error {
	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolRef, Namespace: namespace}, pool); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("pool %q not found in namespace %q", poolRef, namespace)
		}
		return fmt.Errorf("get pool: %w", err)
	}
	return nil
}

// diagnosePoolHealth returns a diagnostic string about pool health (used in timeout errors).
func (g *Gateway) diagnosePoolHealth(ctx context.Context, poolRef, namespace string) string {
	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolRef, Namespace: namespace}, pool); err != nil {
		return fmt.Sprintf("unable to check pool health: %v", err)
	}

	return fmt.Sprintf("pool=%s desired=%d replicas=%d ready=%d template=%s",
		poolRef, desiredSandboxWarmPoolReplicas(pool), pool.Status.Replicas, pool.Status.ReadyReplicas, pool.Spec.TemplateRef.Name)
}

const (
	defaultRuntimeFinishedTTL = 5 * time.Minute
	runtimePatchMaxInterval   = 5 * time.Minute
	runtimePatchMinInterval   = 5 * time.Second
)

func (g *Gateway) runtimeLifecycle(createdAt, lastActivityAt time.Time, idleTimeout, maxLifetime time.Duration) RuntimeLifecycle {
	return RuntimeLifecycle{
		CreatedAt:      createdAt,
		LastActivityAt: lastActivityAt,
		IdleTimeout:    idleTimeout,
		MaxLifetime:    maxLifetime,
		FinishedTTL:    defaultRuntimeFinishedTTL,
	}
}

func (g *Gateway) sessionRuntimeLifecycleLocked(s *session, at time.Time) RuntimeLifecycle {
	createdAt := s.createdAt
	if createdAt.IsZero() {
		createdAt = s.Info.CreatedAt
	}
	if createdAt.IsZero() {
		createdAt = at
	}
	return g.runtimeLifecycle(createdAt, at, s.idleTimeout, s.maxLifetime)
}

func runtimePatchInterval(idleTimeout time.Duration) time.Duration {
	if idleTimeout <= 0 {
		return runtimePatchMaxInterval
	}
	interval := idleTimeout / 2
	if interval < runtimePatchMinInterval {
		return runtimePatchMinInterval
	}
	if interval > runtimePatchMaxInterval {
		return runtimePatchMaxInterval
	}
	return interval
}

func (g *Gateway) startSessionHeartbeat(sessionID string, s *session) func() {
	s.mu.RLock()
	idleTimeout := s.idleTimeout
	s.mu.RUnlock()
	if idleTimeout <= 0 {
		return func() {}
	}
	interval := runtimePatchInterval(idleTimeout)
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				g.touchLastTaskTime(sessionID)
			}
		}
	}()
	return func() {
		once.Do(func() { close(stop) })
	}
}

// touchLastTaskTime updates the in-memory lastTaskTime for session idle tracking
// and asynchronously patches runtime last-activity annotations (throttled to at most once per 30s).
func (g *Gateway) touchLastTaskTime(sessionID string) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return
	}
	now := time.Now()

	s.mu.Lock()
	s.lastTaskTime = now
	lifecycle := g.sessionRuntimeLifecycleLocked(s, now)
	shouldPatch := now.Sub(s.lastAnnotationPatch) >= runtimePatchInterval(s.idleTimeout)
	if shouldPatch {
		s.lastAnnotationPatch = now
	}
	allocation := s.runtimeAllocation()
	s.mu.Unlock()

	if rs, ok := g.store.(*RedisStore); ok {
		rs.Sync(sessionID)
	}

	if shouldPatch && g.runtimeAllocator != nil {
		go func() {
			bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer bgCancel()

			if err := g.runtimeAllocator.Touch(bgCtx, allocation, sessionID, now, lifecycle); err != nil {
				log.Printf("Warning: failed to patch last-activity for runtime %s: %v", allocation.PodName, err)
				if errors.IsNotFound(err) {
					if current, ok := g.store.Get(sessionID); ok {
						g.dropSession(sessionID, current)
					}
				}
			}
		}()
	}
}

// resolveIdleTimeout returns the idle timeout for a session request,
// falling back to devbox or gateway-wide defaults based on mode.
func (g *Gateway) resolveIdleTimeout(req CreateSessionRequest) time.Duration {
	if req.IdleTimeoutSeconds > 0 {
		return time.Duration(req.IdleTimeoutSeconds) * time.Second
	}
	if req.Mode == SessionModeDevbox {
		return g.gwConfig.DevboxIdleTimeout
	}
	return g.gwConfig.IdleTimeout
}

// resolveMaxLifetime returns the max lifetime for a session request,
// falling back to devbox or gateway-wide defaults based on mode.
func (g *Gateway) resolveMaxLifetime(req CreateSessionRequest) time.Duration {
	if req.MaxLifetimeSeconds > 0 {
		return time.Duration(req.MaxLifetimeSeconds) * time.Second
	}
	if req.Mode == SessionModeDevbox {
		return g.gwConfig.DevboxMaxLifetime
	}
	return g.gwConfig.MaxLifetime
}

func resolveStepTimeoutSeconds(step StepRequest) int32 {
	if step.TimeoutSeconds > 0 {
		return step.TimeoutSeconds
	}
	if step.Timeout > 0 {
		return step.Timeout
	}
	return 0
}

// StartSessionSweep starts the background session reaper goroutine.
func (g *Gateway) StartSessionSweep() {
	g.sweepWg.Add(1)
	go g.sessionSweepLoop()
}

// StopSessionSweep signals the session sweep goroutine to exit and waits.
func (g *Gateway) StopSessionSweep() {
	close(g.sweepStopCh)
	g.sweepWg.Wait()
}

// CleanupStaleConnections removes gRPC connections in Shutdown or TransientFailure state.
func (g *Gateway) CleanupStaleConnections() int {
	if g.sidecarClient == nil {
		return 0
	}
	return g.sidecarClient.CleanupStale()
}

func (g *Gateway) sessionSweepLoop() {
	defer g.sweepWg.Done()
	ticker := time.NewTicker(g.gwConfig.SweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.sweepStopCh:
			return
		case <-ticker.C:
			g.sweepSessions()
			g.sweepRuntimeClaims()
		}
	}
}

func (g *Gateway) sweepSessions() {
	now := time.Now()
	g.store.Range(func(sessionID string, s *session) bool {
		if atomic.LoadInt32(&s.activeExecs) > 0 {
			return true
		}

		s.mu.RLock()
		lastTask := s.lastTaskTime
		created := s.createdAt
		idleTimeout := s.idleTimeout
		maxLifetime := s.maxLifetime
		s.mu.RUnlock()

		// Check max lifetime (0 means no limit)
		if maxLifetime > 0 && now.Sub(created) > maxLifetime {
			log.Printf("Session %s exceeded max lifetime (%v), deleting", sessionID, maxLifetime)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := g.deleteSession(ctx, sessionID, "max_lifetime"); err != nil {
				log.Printf("Warning: failed to delete expired session %s: %v", sessionID, err)
			}
			cancel()
			return true
		}

		// Check idle timeout (0 means no limit)
		if idleTimeout > 0 && now.Sub(lastTask) > idleTimeout {
			log.Printf("Session %s idle for %v (timeout=%v), deleting", sessionID, now.Sub(lastTask), idleTimeout)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := g.deleteSession(ctx, sessionID, "idle_timeout"); err != nil {
				log.Printf("Warning: failed to delete idle session %s: %v", sessionID, err)
			}
			cancel()
		}

		return true
	})
}

const (
	runtimeOrphanGrace   = 5 * time.Minute
	runtimeNotReadyGrace = 5 * time.Minute
)

func (g *Gateway) sweepRuntimeClaims() {
	if g.k8sClient == nil || g.runtimeAllocator == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := g.reapRuntimeClaims(ctx, time.Now()); err != nil {
		log.Printf("Warning: runtime claim reaper failed: %v", err)
	}
}

func (g *Gateway) reapRuntimeClaims(ctx context.Context, now time.Time) error {
	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(g.runtimeNamespace())); err != nil {
		return fmt.Errorf("list sandbox claims for runtime reaper: %w", err)
	}
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.DeletionTimestamp != nil {
			continue
		}
		sessionID := strings.TrimSpace(claim.Annotations[labels.SessionAnnotation])
		if sessionID == "" {
			continue
		}
		if err := g.reapRuntimeClaim(ctx, claim, sessionID, now); err != nil {
			log.Printf("Warning: failed to reap runtime claim %s/%s for session %s: %v", claim.Namespace, claim.Name, sessionID, err)
		}
	}
	return nil
}

func (g *Gateway) reapRuntimeClaim(ctx context.Context, claim *extensionsv1beta1.SandboxClaim, sessionID string, now time.Time) error {
	s, hasSession := g.store.Get(sessionID)
	terminalExpired := claimTerminalExpired(claim, now)
	idleExpired := false
	if !hasSession {
		idleExpired = claimIdleExpired(claim, now, g.gwConfig.IdleTimeout)
	}
	unhealthy, err := g.claimRuntimeUnhealthy(ctx, claim, now)
	if err != nil {
		return err
	}
	if hasSession {
		if atomic.LoadInt32(&s.activeExecs) > 0 {
			return nil
		}
		s.mu.RLock()
		currentAllocation := s.runtimeAllocation()
		s.mu.RUnlock()
		if currentAllocation.ClaimName != "" &&
			(currentAllocation.ClaimName != claim.Name || currentAllocation.Namespace != claim.Namespace) {
			return g.runtimeAllocator.Release(ctx, RuntimeAllocation{
				Backend:     runtimeBackendSandboxClaim,
				PoolRef:     claim.Spec.WarmPoolRef.Name,
				Namespace:   claim.Namespace,
				ClaimName:   claim.Name,
				SandboxName: claim.Status.SandboxStatus.Name,
				PodIP:       firstString(claim.Status.SandboxStatus.PodIPs),
			})
		}
		if !terminalExpired && !unhealthy {
			return nil
		}
		reason := "runtime_finished"
		if unhealthy {
			reason = "runtime_unhealthy"
		}
		return g.deleteSession(ctx, sessionID, reason)
	}

	if !terminalExpired && !idleExpired && !unhealthy {
		return nil
	}

	return g.runtimeAllocator.Release(ctx, RuntimeAllocation{
		Backend:     runtimeBackendSandboxClaim,
		PoolRef:     claim.Spec.WarmPoolRef.Name,
		Namespace:   claim.Namespace,
		ClaimName:   claim.Name,
		SandboxName: claim.Status.SandboxStatus.Name,
		PodIP:       firstString(claim.Status.SandboxStatus.PodIPs),
	})
}

func claimIdleExpired(claim *extensionsv1beta1.SandboxClaim, now time.Time, fallback time.Duration) bool {
	if claim.Spec.Lifecycle != nil && claim.Spec.Lifecycle.ShutdownTime != nil {
		return now.After(claim.Spec.Lifecycle.ShutdownTime.Time.Add(runtimeOrphanGrace))
	}
	idleTimeout, ok := durationAnnotation(claim.Annotations, labels.IdleTimeoutAnnotation)
	if !ok {
		idleTimeout = fallback
	}
	if idleTimeout <= 0 {
		return false
	}
	lastActivity := claim.CreationTimestamp.Time
	if raw := strings.TrimSpace(claim.Annotations[labels.LastActivityAnnotation]); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			lastActivity = parsed
		}
	}
	if lastActivity.IsZero() {
		lastActivity = now
	}
	return now.After(lastActivity.Add(idleTimeout).Add(runtimeOrphanGrace))
}

func claimTerminalExpired(claim *extensionsv1beta1.SandboxClaim, now time.Time) bool {
	finished := meta.FindStatusCondition(claim.Status.Conditions, string(sandboxv1beta1.SandboxConditionFinished))
	if finished == nil || finished.Status != metav1.ConditionTrue {
		return false
	}
	ttl, ok := durationAnnotation(claim.Annotations, labels.FinishedTTLAnnotation)
	if !ok {
		ttl = defaultRuntimeFinishedTTL
	}
	transition := finished.LastTransitionTime.Time
	if transition.IsZero() {
		transition = claim.CreationTimestamp.Time
	}
	if transition.IsZero() {
		transition = now
	}
	return now.After(transition.Add(ttl).Add(runtimeOrphanGrace))
}

func (g *Gateway) claimRuntimeUnhealthy(ctx context.Context, claim *extensionsv1beta1.SandboxClaim, now time.Time) (bool, error) {
	ready := meta.FindStatusCondition(claim.Status.Conditions, string(sandboxv1beta1.SandboxConditionReady))
	if ready == nil || ready.Status != metav1.ConditionFalse {
		return false, nil
	}
	transition := ready.LastTransitionTime.Time
	if transition.IsZero() {
		transition = claim.CreationTimestamp.Time
	}
	if transition.IsZero() || now.Sub(transition) < runtimeNotReadyGrace {
		return false, nil
	}
	pod, err := g.podForClaim(ctx, claim)
	if err != nil || pod == nil {
		return false, err
	}
	return podHasUnrecoverableStatus(pod), nil
}

func (g *Gateway) podForClaim(ctx context.Context, claim *extensionsv1beta1.SandboxClaim) (*corev1.Pod, error) {
	sandboxName := claim.Status.SandboxStatus.Name
	if sandboxName == "" {
		return nil, nil
	}
	sandbox := &sandboxv1beta1.Sandbox{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: claim.Namespace}, sandbox); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get sandbox %s/%s for runtime reaper: %w", claim.Namespace, sandboxName, err)
	}
	podName := sandboxName
	if sandbox.Annotations != nil && sandbox.Annotations[sandboxv1beta1.SandboxPodNameAnnotation] != "" {
		podName = sandbox.Annotations[sandboxv1beta1.SandboxPodNameAnnotation]
	}
	pod := &corev1.Pod{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: claim.Namespace}, pod); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get pod %s/%s for runtime reaper: %w", claim.Namespace, podName, err)
	}
	return pod, nil
}

func podHasUnrecoverableStatus(pod *corev1.Pod) bool {
	if pod.Status.Phase == corev1.PodFailed {
		return true
	}
	for _, status := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
		if status.State.Waiting != nil && unrecoverableWaitingReason(status.State.Waiting.Reason) {
			return true
		}
		if status.LastTerminationState.Terminated != nil && unrecoverableTerminatedReason(status.LastTerminationState.Terminated.Reason) {
			return true
		}
	}
	return false
}

func unrecoverableWaitingReason(reason string) bool {
	switch reason {
	case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "InvalidImageName",
		"CreateContainerConfigError", "CreateContainerError", "RunContainerError":
		return true
	default:
		return false
	}
}

func unrecoverableTerminatedReason(reason string) bool {
	switch reason {
	case "OOMKilled", "Error", "ContainerCannotRun":
		return true
	default:
		return false
	}
}

func durationAnnotation(annotations map[string]string, key string) (time.Duration, bool) {
	raw := strings.TrimSpace(annotations[key])
	if raw == "" {
		return 0, false
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

// randomSuffix returns a hex string of n random bytes (2n hex chars).
func randomSuffix(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// validLabelValue validates a Kubernetes label value (max 63 chars, alphanumeric/dash/underscore/dot).
var validLabelValue = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]{0,61}[a-zA-Z0-9])?$`)

// CreateManagedSession creates a session with automatic pool management.
func (g *Gateway) CreateManagedSession(ctx context.Context, req CreateManagedSessionRequest) (*ManagedSessionInfo, error) {
	ctx, span := otel.Tracer("gateway").Start(ctx, "Gateway.CreateManagedSession",
		traceStartAttrs(
			"experiment.id", req.ExperimentID,
			"image", req.Image,
			"profile", req.Profile,
			"namespace", req.Namespace,
		),
	)
	defer span.End()

	if !validLabelValue.MatchString(req.ExperimentID) {
		err := fmt.Errorf("experimentId must be a valid Kubernetes label value (max 63 chars, alphanumeric/dash/underscore/dot, must start and end with alphanumeric)")
		recordSpanErr(span, err)
		return nil, err
	}

	ns, err := g.resolveNamespace(req.Namespace)
	if err != nil {
		recordSpanErr(span, err)
		return nil, err
	}

	if hasJSONPayload(req.Tools) {
		err := fmt.Errorf("managed session tools are not supported by sandbox-backed pools yet")
		recordSpanErr(span, err)
		return nil, err
	}
	if err := validatePrivateContainers(req.PrivateContainers); err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	if _, err := parseConfigEnvVars(req.ConfigEnv); err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	image := normalizeImage(req.Image)
	profile := normalizeProfile(req.Profile)
	poolName, err := managedPoolName(image, ns, profile, req.PrivateContainers)
	if err != nil {
		recordSpanErr(span, err)
		return nil, fmt.Errorf("derive managed pool name: %w", err)
	}
	createdPool := false
	if err := g.CreatePool(ctx, CreatePoolRequest{
		Name:              poolName,
		Image:             image,
		Profile:           profile,
		Replicas:          1,
		Namespace:         ns,
		Resources:         req.Resources,
		WorkspaceDir:      req.WorkspaceDir,
		PrivateContainers: req.PrivateContainers,
		Managed:           true,
	}); err != nil && !errors.IsAlreadyExists(err) {
		recordSpanErr(span, err)
		return nil, fmt.Errorf("ensure managed sandbox pool: %w", err)
	} else if err == nil {
		createdPool = true
	}
	span.SetAttributes(attribute.String("pool.name", poolName))

	info, err := g.CreateSession(ctx, CreateSessionRequest{
		Image:              image,
		Profile:            profile,
		PoolName:           poolName,
		Namespace:          ns,
		Mode:               req.Mode,
		IdleTimeoutSeconds: req.IdleTimeoutSeconds,
		MaxLifetimeSeconds: req.MaxLifetimeSeconds,
		ConfigEnv:          req.ConfigEnv,
		Managed:            true,
		ExperimentID:       req.ExperimentID,
		PrivateContainers:  req.PrivateContainers,
	})
	if err != nil {
		if createdPool {
			if stopped, cleanupErr := g.stopManagedPoolIfUnused(ctx, poolName, ns); cleanupErr != nil {
				log.Printf("Warning: failed to cleanup unused managed pool %s/%s after managed session create failure: %v", ns, poolName, cleanupErr)
			} else if stopped {
				log.Printf("Stopped unused managed pool %s/%s after managed session create failure", ns, poolName)
			}
		}
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Persist experiment index to Redis (if using RedisStore).
	if rs, ok := g.store.(*RedisStore); ok {
		rs.Sync(info.ID)
	}

	return &ManagedSessionInfo{
		SessionInfo:  *info,
		ExperimentID: req.ExperimentID,
		Managed:      true,
	}, nil
}

// ListExperimentSessions returns all sessions for an experiment,
// including soft-deleted sessions whose history is still in Redis.
func (g *Gateway) ListExperimentSessions(experimentID string) []ManagedSessionInfo {
	results := make([]ManagedSessionInfo, 0)
	seen := make(map[string]bool)
	g.store.Range(func(id string, s *session) bool {
		s.mu.RLock()
		if s.managed && s.experimentID == experimentID {
			results = append(results, ManagedSessionInfo{
				SessionInfo:  s.Info,
				ExperimentID: s.experimentID,
				Managed:      true,
			})
			seen[id] = true
		}
		s.mu.RUnlock()
		return true
	})

	if rs, ok := g.store.(*RedisStore); ok {
		for _, id := range rs.FindByExperiment(experimentID) {
			if seen[id] {
				continue
			}
			if s, ok := rs.GetHistorical(id); ok {
				s.mu.RLock()
				results = append(results, ManagedSessionInfo{
					SessionInfo:  s.Info,
					ExperimentID: s.experimentID,
					Managed:      true,
				})
				s.mu.RUnlock()
			}
		}
	}

	return results
}

// ListSessions returns all active sessions.
func (g *Gateway) ListSessions() []SessionListItem {
	var items []SessionListItem
	g.store.Range(func(_ string, s *session) bool {
		s.mu.RLock()
		item := SessionListItem{
			SessionInfo:  s.Info,
			Managed:      s.managed,
			ExperimentID: s.experimentID,
		}
		s.mu.RUnlock()
		items = append(items, item)
		return true
	})
	return items
}

// ListPools returns SandboxWarmPool CRDs in the gateway namespace.
func (g *Gateway) ListPools(ctx context.Context, namespace string) ([]PoolInfo, error) {
	namespace, err := g.resolveNamespace(namespace)
	if err != nil {
		return nil, err
	}

	var poolList extensionsv1beta1.SandboxWarmPoolList
	opts := []client.ListOption{client.InNamespace(namespace)}
	if err := g.k8sClient.List(ctx, &poolList, opts...); err != nil {
		return nil, err
	}

	pools := make([]PoolInfo, 0, len(poolList.Items))
	for i := range poolList.Items {
		pools = append(pools, g.poolInfoFromSandboxWarmPool(ctx, &poolList.Items[i]))
	}
	return pools, nil
}

// ListExperiments returns aggregate experiment summaries.
func (g *Gateway) ListExperiments() []ExperimentSummary {
	expMap := make(map[string]*ExperimentSummary)
	g.store.Range(func(_ string, s *session) bool {
		s.mu.RLock()
		if s.managed && s.experimentID != "" {
			if exp, ok := expMap[s.experimentID]; ok {
				exp.SessionCount++
			} else {
				expMap[s.experimentID] = &ExperimentSummary{
					ExperimentID: s.experimentID,
					SessionCount: 1,
					Image:        s.Info.Image,
					Profile:      s.Info.Profile,
					Namespace:    s.Info.Namespace,
				}
			}
		}
		s.mu.RUnlock()
		return true
	})

	results := make([]ExperimentSummary, 0, len(expMap))
	for _, v := range expMap {
		results = append(results, *v)
	}
	return results
}

// StreamSessionLogs streams log entries from a session's sidecar to a channel.
func (g *Gateway) StreamSessionLogs(ctx context.Context, sessionID string, follow bool, tailLines int32) (<-chan interfaces.LogEntry, error) {
	_, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	source, err := g.sidecarClient.StreamLogs(ctx, podIP, follow, tailLines)
	if err != nil {
		releaseSession()
		return nil, err
	}
	out := make(chan interfaces.LogEntry, 128)
	go func() {
		defer releaseSession()
		defer close(out)
		for entry := range source {
			select {
			case out <- entry:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// StreamPoolLogs fans out log streaming to all pods in a pool and merges output.
func (g *Gateway) StreamPoolLogs(ctx context.Context, poolName, namespace string, follow bool, tailLines int32) (<-chan PoolLogEntry, error) {
	namespace, err := g.resolveNamespace(namespace)
	if err != nil {
		return nil, err
	}

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolName, Namespace: namespace}, pool); err != nil {
		return nil, fmt.Errorf("get pool: %w", err)
	}

	// List all pods belonging to this pool
	var podList corev1.PodList
	if pool.Status.Selector == "" {
		return nil, fmt.Errorf("pool %s/%s has no pod selector yet", namespace, poolName)
	}
	selector, err := k8slabels.Parse(pool.Status.Selector)
	if err != nil {
		return nil, fmt.Errorf("parse pool selector: %w", err)
	}
	opts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: selector},
	}
	if err := g.k8sClient.List(ctx, &podList, opts...); err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	merged := make(chan PoolLogEntry, 256)
	var wg sync.WaitGroup

	for _, pod := range podList.Items {
		if pod.Status.PodIP == "" {
			continue
		}
		podName := pod.Name
		podIP := pod.Status.PodIP

		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, err := g.sidecarClient.StreamLogs(ctx, podIP, follow, tailLines)
			if err != nil {
				merged <- PoolLogEntry{PodName: podName, Entry: interfaces.LogEntry{
					Level: "error", Message: fmt.Sprintf("connect: %v", err), Source: "gateway",
				}}
				return
			}
			for entry := range ch {
				select {
				case merged <- PoolLogEntry{PodName: podName, Entry: entry}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(merged)
	}()

	return merged, nil
}

// PoolLogEntry wraps a LogEntry with the source pod name.
type PoolLogEntry struct {
	PodName string              `json:"podName"`
	Entry   interfaces.LogEntry `json:"entry"`
}

func (g *Gateway) poolInfoFromSandboxWarmPool(ctx context.Context, pool *extensionsv1beta1.SandboxWarmPool) PoolInfo {
	info := PoolInfo{
		Name:          pool.Name,
		Namespace:     pool.Namespace,
		Profile:       firstNonEmpty(profileFromObjectMeta(pool.ObjectMeta), defaultPoolProfile),
		Replicas:      desiredSandboxWarmPoolReplicas(pool),
		ReadyReplicas: pool.Status.ReadyReplicas,
		State:         firstNonEmpty(pool.Annotations[labels.PoolStateAnnotation], labels.PoolStateRunning),
		CreatedAt:     pool.CreationTimestamp.Time,
	}
	template := &extensionsv1beta1.SandboxTemplate{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: pool.Spec.TemplateRef.Name, Namespace: pool.Namespace}, template); err == nil {
		info.Image = primarySandboxTemplateImage(template)
		info.Profile = firstNonEmpty(profileFromObjectMeta(pool.ObjectMeta), profileFromObjectMeta(template.ObjectMeta), defaultPoolProfile)
	}
	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(pool.Namespace)); err == nil {
		for i := range claims.Items {
			if claims.Items[i].Spec.WarmPoolRef.Name == pool.Name && claims.Items[i].DeletionTimestamp == nil {
				info.AllocatedReplicas++
			}
		}
	}
	return info
}

// DeleteExperiment deletes all sessions for an experiment.
func (g *Gateway) DeleteExperiment(ctx context.Context, experimentID string) (int, error) {
	sessions := g.ListExperimentSessions(experimentID)
	pools := make(map[types.NamespacedName]struct{})
	for _, s := range sessions {
		if s.PoolRef == "" {
			continue
		}
		namespace := s.Namespace
		if namespace == "" {
			namespace = g.runtimeNamespace()
		}
		pools[types.NamespacedName{Name: s.PoolRef, Namespace: namespace}] = struct{}{}
	}

	deleted := 0
	var lastErr error
	for _, s := range sessions {
		if s.Status == "deleted" || s.DeletedAt != nil {
			continue
		}
		if err := g.DeleteSession(ctx, s.ID); err != nil {
			lastErr = err
			log.Printf("Warning: failed to delete session %s in experiment %s: %v", s.ID, experimentID, err)
		} else {
			deleted++
		}
	}

	for pool := range pools {
		if stoppedPool, err := g.stopManagedPoolIfUnused(ctx, pool.Name, pool.Namespace); err != nil {
			lastErr = err
			log.Printf("Warning: failed to stop unused managed pool %s/%s after deleting experiment %s: %v", pool.Namespace, pool.Name, experimentID, err)
		} else if stoppedPool {
			log.Printf("Stopped unused managed pool %s/%s after deleting experiment %s", pool.Namespace, pool.Name, experimentID)
		}
	}

	return deleted, lastErr
}

func (g *Gateway) stopManagedPoolIfUnused(ctx context.Context, poolName, namespace string) (bool, error) {
	if g.k8sClient == nil || strings.TrimSpace(poolName) == "" {
		return false, nil
	}
	namespace, err := g.resolveNamespace(namespace)
	if err != nil {
		return false, err
	}

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolName, Namespace: namespace}, pool); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get managed pool %s/%s: %w", namespace, poolName, err)
	}
	if !isManagedPool(pool) {
		return false, nil
	}

	inUse, err := g.poolHasActiveBindings(ctx, poolName, namespace)
	if err != nil {
		return false, err
	}
	if inUse {
		return false, nil
	}
	if poolLifecycleStopped(pool) {
		return false, nil
	}

	if err := g.markPoolStopped(ctx, poolName, namespace); err != nil {
		return false, err
	}
	return true, nil
}

func poolLifecycleStopped(pool *extensionsv1beta1.SandboxWarmPool) bool {
	if pool == nil {
		return false
	}
	state := strings.ToLower(strings.TrimSpace(pool.Annotations[labels.PoolStateAnnotation]))
	autoscale := strings.ToLower(strings.TrimSpace(pool.Annotations[scheduling.PoolAutoscaleAnnotation]))
	return desiredSandboxWarmPoolReplicas(pool) == 0 && state == labels.PoolStateStopped && autoscale == "false"
}

func isManagedPool(pool *extensionsv1beta1.SandboxWarmPool) bool {
	if pool == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(pool.Annotations[labels.ManagedPoolAnnotation]), "true")
}

func (g *Gateway) poolHasActiveBindings(ctx context.Context, poolName, namespace string) (bool, error) {
	if g.store != nil {
		inUse := false
		g.store.Range(func(_ string, s *session) bool {
			s.mu.RLock()
			closed := s.closed
			allocation := s.runtimeAllocation()
			s.mu.RUnlock()
			if closed || allocation.PoolRef != poolName {
				return true
			}
			if allocation.Namespace != "" && allocation.Namespace != namespace {
				return true
			}
			inUse = true
			return false
		})
		if inUse {
			return true, nil
		}
	}

	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(namespace)); err != nil {
		return false, fmt.Errorf("list sandbox claims for managed pool cleanup: %w", err)
	}
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.DeletionTimestamp != nil || claim.Spec.WarmPoolRef.Name != poolName {
			continue
		}
		return true, nil
	}
	return false, nil
}
