package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/audit"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// GatewayConfig holds Gateway-level configuration.
type GatewayConfig struct {
	IdleTimeout                     time.Duration
	MaxLifetime                     time.Duration
	DevboxIdleTimeout               time.Duration
	DevboxMaxLifetime               time.Duration
	DevboxStorageClassName          string
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
	activeExecs         int32
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
	poolStopMu          sync.Mutex
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

func (g *Gateway) resolveIdleTimeout(req CreateSessionRequest) time.Duration {
	if req.IdleTimeoutSeconds > 0 {
		return time.Duration(req.IdleTimeoutSeconds) * time.Second
	}
	if req.Mode == SessionModeDevbox {
		return g.gwConfig.DevboxIdleTimeout
	}
	return g.gwConfig.IdleTimeout
}

func (g *Gateway) resolveMaxLifetime(req CreateSessionRequest) time.Duration {
	if req.MaxLifetimeSeconds > 0 {
		return time.Duration(req.MaxLifetimeSeconds) * time.Second
	}
	if req.Mode == SessionModeDevbox {
		return g.gwConfig.DevboxMaxLifetime
	}
	return g.gwConfig.MaxLifetime
}

func randomSuffix(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var validLabelValue = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]{0,61}[a-zA-Z0-9])?$`)
