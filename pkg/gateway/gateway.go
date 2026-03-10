package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	"github.com/Lincyaw/agent-env/pkg/audit"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/Lincyaw/agent-env/pkg/labels"
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

// GatewayConfig holds Gateway-level configuration.
type GatewayConfig struct {
	IdleTimeout   time.Duration
	MaxLifetime   time.Duration
	SweepInterval time.Duration
}

// session holds internal session state.
type session struct {
	mu                  sync.RWMutex
	Info                SessionInfo
	History             *StepHistory
	managed             bool
	experimentID        string
	lastTaskTime        time.Time
	lastAnnotationPatch time.Time
	idleTimeout         time.Duration
	maxLifetime         time.Duration
	createdAt           time.Time
}

// Gateway manages sessions and forwards execution to sidecars.
type Gateway struct {
	k8sClient        client.Client
	podAllocator     *PodAllocator
	sidecarClient    interfaces.SidecarClient
	metrics          interfaces.MetricsCollector
	trajectoryWriter *audit.TrajectoryWriter
	sessions         sync.Map // sessionID → *session
	sessionCount     atomic.Int64
	poolManager      *PoolManager
	gwConfig         GatewayConfig
	sweepStopCh      chan struct{}
	sweepWg          sync.WaitGroup
}

// New creates a new gateway. metrics and trajectoryWriter may be nil.
func New(k8sClient client.Client, podAllocator *PodAllocator, sidecarClient interfaces.SidecarClient, metrics interfaces.MetricsCollector, trajectoryWriter *audit.TrajectoryWriter, pmConfig *PoolManagerConfig, gwConfig GatewayConfig) *Gateway {
	gw := &Gateway{
		k8sClient:        k8sClient,
		podAllocator:     podAllocator,
		sidecarClient:    sidecarClient,
		metrics:          metrics,
		trajectoryWriter: trajectoryWriter,
		gwConfig:         gwConfig,
		sweepStopCh:      make(chan struct{}),
	}
	if pmConfig != nil {
		gw.poolManager = NewPoolManager(k8sClient, *pmConfig)
	}
	return gw
}

// StartPoolManager starts the pool manager background goroutine and recovers state.
func (g *Gateway) StartPoolManager(ctx context.Context) error {
	if g.poolManager == nil {
		return nil
	}
	orphans, err := g.poolManager.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover pool manager: %w", err)
	}

	// Recover sessions from allocated pods.
	// For each pod with a recent last-activity annotation AND a session annotation,
	// rebuild the in-memory session so clients can continue using their session IDs.
	// Pods without recent activity or without session annotations are reclaimed to idle.
	if len(orphans) > 0 {
		idleTimeout := g.gwConfig.IdleTimeout
		if idleTimeout <= 0 {
			idleTimeout = 10 * time.Minute // conservative fallback
		}
		now := time.Now()
		recovered := 0
		reclaimed := 0

		for _, pod := range orphans {
			sessionID := pod.Annotations[labels.SessionAnnotation]
			recentlyActive := false

			if ts, ok := pod.Annotations[labels.LastActivityAnnotation]; ok {
				lastActivity, parseErr := time.Parse(time.RFC3339, ts)
				if parseErr == nil && now.Sub(lastActivity) <= idleTimeout {
					recentlyActive = true
				}
			}

			// If recently active AND has session annotation, rebuild the session
			if recentlyActive && sessionID != "" {
				poolRef := pod.Labels[labels.PoolLabelKey]
				sandboxName := pod.Labels[labels.SandboxLabelKey]
				managed := pod.Labels[labelManaged] == "true"
				experimentID := pod.Labels[labelExperiment]
				lastActivity, _ := time.Parse(time.RFC3339, pod.Annotations[labels.LastActivityAnnotation])

				info := SessionInfo{
					ID:          sessionID,
					SandboxName: sandboxName,
					Namespace:   pod.Namespace,
					PoolRef:     poolRef,
					PodIP:       pod.Status.PodIP,
					PodName:     pod.Name,
					CreatedAt:   pod.CreationTimestamp.Time,
				}

				s := &session{
					Info:                info,
					History:             NewStepHistory(),
					managed:             managed,
					experimentID:        experimentID,
					lastTaskTime:        lastActivity,
					lastAnnotationPatch: lastActivity,
					createdAt:           pod.CreationTimestamp.Time,
					idleTimeout:         g.gwConfig.IdleTimeout,
					maxLifetime:         g.gwConfig.MaxLifetime,
				}

				g.sessions.Store(sessionID, s)
				g.sessionCount.Add(1)
				recovered++

				// Restore pool manager session count for managed sessions
				if managed {
					if val, ok := g.poolManager.pools.Load(poolRef); ok {
						state := val.(*poolState)
						state.sessionCount.Add(1)
					}
				}

				log.Printf("Recovered session %s (pod=%s/%s, pool=%s, lastActivity=%s)",
					sessionID, pod.Namespace, pod.Name, poolRef, lastActivity.Format(time.RFC3339))
			} else {
				// Expired or missing session — workspace is tainted, delete the pod.
				// WarmPoolController will create a clean replacement.
				if err := g.k8sClient.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
					log.Printf("Warning: failed to delete orphaned pod %s/%s: %v", pod.Namespace, pod.Name, err)
				} else {
					reclaimed++
				}
			}
		}

		if recovered > 0 {
			log.Printf("Recovered %d sessions from pod annotations", recovered)
		}
		if reclaimed > 0 {
			log.Printf("Deleted %d orphaned allocated pods (workspace tainted)", reclaimed)
		}

		if g.metrics != nil {
			g.metrics.SetActiveSessions(g.sessionCount.Load())
		}
	}

	g.poolManager.Start()
	return nil
}

// StopPoolManager stops the pool manager background goroutine.
func (g *Gateway) StopPoolManager() {
	if g.poolManager != nil {
		g.poolManager.Stop()
	}
}

// CreateSession allocates a pod from the pool via PodAllocator and registers a session.
func (g *Gateway) CreateSession(ctx context.Context, req CreateSessionRequest) (*SessionInfo, error) {
	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	// Pre-flight: check pool health before allocating
	if err := g.checkPoolHealth(ctx, req.PoolRef, ns); err != nil {
		return nil, fmt.Errorf("pool not ready: %w", err)
	}

	sessionID := fmt.Sprintf("gw-%d-%s", time.Now().UnixMilli(), randomSuffix(4))
	sandboxName := sessionID

	// Allocate pod from warm pool via Informer-backed queue (5-minute timeout)
	allocCtx, allocCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer allocCancel()

	allocStart := time.Now()
	pod, err := g.podAllocator.Allocate(allocCtx, req.PoolRef, ns)
	if err != nil {
		diag := g.diagnosePoolHealth(ctx, req.PoolRef, ns)
		return nil, fmt.Errorf("allocate pod from pool %s: %w (%s)", req.PoolRef, err, diag)
	}

	podIP := pod.Status.PodIP
	podName := pod.Name

	// Patch sandbox label and session annotation onto the allocated pod
	patchPod := pod.DeepCopy()
	patch := client.MergeFrom(pod.DeepCopy())
	if patchPod.Labels == nil {
		patchPod.Labels = make(map[string]string)
	}
	patchPod.Labels[labels.SandboxLabelKey] = sandboxName
	if patchPod.Annotations == nil {
		patchPod.Annotations = make(map[string]string)
	}
	patchPod.Annotations[labels.SessionAnnotation] = sessionID
	if patchErr := g.k8sClient.Patch(ctx, patchPod, patch); patchErr != nil {
		log.Printf("Warning: failed to patch sandbox label on pod %s: %v", podName, patchErr)
	}

	info := SessionInfo{
		ID:          sessionID,
		SandboxName: sandboxName,
		Namespace:   ns,
		PoolRef:     req.PoolRef,
		PodIP:       podIP,
		PodName:     podName,
		CreatedAt:   time.Now(),
	}

	g.sessions.Store(sessionID, &session{
		Info:         info,
		History:      NewStepHistory(),
		lastTaskTime: time.Now(),
		createdAt:    time.Now(),
		idleTimeout:  g.resolveIdleTimeout(req),
		maxLifetime:  g.resolveMaxLifetime(req),
	})

	if g.metrics != nil {
		g.metrics.SetActiveSessions(g.sessionCount.Add(1))
		g.metrics.RecordSessionAllocationDuration(req.PoolRef, time.Since(allocStart))
	}

	return &info, nil
}

// GetSession returns session info.
func (g *Gateway) GetSession(sessionID string) (*SessionInfo, error) {
	val, ok := g.sessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	s := val.(*session)
	s.mu.RLock()
	info := s.Info
	s.mu.RUnlock()
	return &info, nil
}

// DeleteSession deletes the pod directly and removes the session.
func (g *Gateway) DeleteSession(ctx context.Context, sessionID string) error {
	val, ok := g.sessions.Load(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	s := val.(*session)

	s.mu.RLock()
	podName := s.Info.PodName
	namespace := s.Info.Namespace
	poolRef := s.Info.PoolRef
	managed := s.managed
	s.mu.RUnlock()

	// Delete pod directly via PodAllocator (WarmPoolController will create replacement)
	if err := g.podAllocator.Release(ctx, podName, namespace); err != nil && !errors.IsNotFound(err) {
		log.Printf("Warning: failed to delete pod %s for session %s: %v", podName, sessionID, err)
	}

	g.sessions.Delete(sessionID)

	if g.metrics != nil {
		g.metrics.SetActiveSessions(g.sessionCount.Add(-1))
	}

	// Release from pool manager if managed
	if managed && g.poolManager != nil {
		g.poolManager.ReleaseSession(poolRef)
	}

	return nil
}

// ExecuteSteps executes steps directly via sidecar gRPC.
func (g *Gateway) ExecuteSteps(ctx context.Context, sessionID string, req ExecuteRequest) (*ExecuteResponse, error) {
	val, ok := g.sessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	s := val.(*session)

	s.mu.RLock()
	podIP := s.Info.PodIP
	s.mu.RUnlock()

	resp := &ExecuteResponse{
		SessionID: sessionID,
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
			Command:    step.Command,
			Env:        step.Env,
			WorkingDir: step.WorkDir,
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

		// Write to ClickHouse trajectory if configured
		if g.trajectoryWriter != nil {
			obsJSON, _ := json.Marshal(result.Output)
			entry := audit.TrajectoryEntry{
				SessionID:   sessionID,
				Step:        globalIdx,
				Name:        result.Name,
				Action:      result.Input,
				Observation: obsJSON,
				SnapshotID:  result.SnapshotID,
				DurationMs:  result.DurationMs,
				Timestamp:   result.Timestamp,
			}
			// Write asynchronously to avoid blocking execution
			go func(e audit.TrajectoryEntry) {
				bgCtx := context.Background()
				if err := g.trajectoryWriter.WriteEntry(bgCtx, e); err != nil {
					log.Printf("Warning: failed to write trajectory entry: %v", err)
				}
			}(entry)
		}
	}

	resp.TotalDurationMs = time.Since(totalStart).Milliseconds()

	// Update in-memory lastTaskTime for session idle tracking
	g.touchLastTaskTime(sessionID)

	return resp, nil
}

// Restore restores a session to a previous snapshot by allocating a new pod and replaying steps.
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

	val, ok := g.sessions.Load(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	s := val.(*session)

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
	oldPodName := s.Info.PodName
	oldNamespace := s.Info.Namespace
	poolRef := s.Info.PoolRef
	s.mu.RUnlock()

	// Allocate a new pod from the pool via PodAllocator
	allocCtx, allocCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer allocCancel()

	newPod, err := g.podAllocator.Allocate(allocCtx, poolRef, oldNamespace)
	if err != nil {
		return fmt.Errorf("allocate new pod for restore: %w", err)
	}

	newPodIP := newPod.Status.PodIP
	newPodName := newPod.Name
	newSandboxName := fmt.Sprintf("%s-r%d", sessionID, time.Now().UnixMilli())

	// Patch sandbox label and session annotation on the new pod
	patchPod := newPod.DeepCopy()
	patch := client.MergeFrom(newPod.DeepCopy())
	if patchPod.Labels == nil {
		patchPod.Labels = make(map[string]string)
	}
	patchPod.Labels[labels.SandboxLabelKey] = newSandboxName
	if patchPod.Annotations == nil {
		patchPod.Annotations = make(map[string]string)
	}
	patchPod.Annotations[labels.SessionAnnotation] = sessionID
	if patchErr := g.k8sClient.Patch(ctx, patchPod, patch); patchErr != nil {
		log.Printf("Warning: failed to patch sandbox label on pod %s: %v", newPodName, patchErr)
	}

	// Replay each step from history on the new pod
	for _, record := range records {
		var step StepRequest
		if err := json.Unmarshal(record.Input, &step); err != nil {
			log.Printf("Warning: failed to unmarshal step %d for replay: %v", record.Index, err)
			continue
		}

		execReq := &sidecar.ExecRequest{
			Command:    step.Command,
			Env:        step.Env,
			WorkingDir: step.WorkDir,
		}
		if _, err := g.sidecarClient.Execute(ctx, newPodIP, execReq); err != nil {
			// Release the newly allocated pod since restore failed
			go func() {
				bgCtx, bgCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer bgCancel()
				if relErr := g.podAllocator.Release(bgCtx, newPodName, oldNamespace); relErr != nil {
					log.Printf("Warning: failed to release pod %s after restore failure: %v", newPodName, relErr)
				}
			}()
			return fmt.Errorf("replay step %d failed: %w", record.Index, err)
		}
	}

	// Update session to point at the new pod under write lock
	s.mu.Lock()
	s.Info.PodIP = newPodIP
	s.Info.PodName = newPodName
	s.Info.SandboxName = newSandboxName
	s.mu.Unlock()

	// Truncate history to records 0..targetIdx
	s.History.TruncateTo(targetIdx)

	// Delete old pod directly (async, best-effort)
	go func() {
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer bgCancel()
		if err := g.podAllocator.Release(bgCtx, oldPodName, oldNamespace); err != nil {
			log.Printf("Warning: failed to delete old pod %s: %v", oldPodName, err)
		}
	}()

	return nil
}

// GetHistory returns the execution history for a session.
func (g *Gateway) GetHistory(sessionID string) ([]StepRecord, error) {
	val, ok := g.sessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	s := val.(*session)
	return s.History.GetAll(), nil
}

// ExportTrajectory exports the trajectory as JSONL.
func (g *Gateway) ExportTrajectory(sessionID string) ([]byte, error) {
	val, ok := g.sessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	s := val.(*session)
	return s.History.ExportTrajectory(sessionID)
}

// CreatePool creates a WarmPool CRD.
func (g *Gateway) CreatePool(ctx context.Context, req CreatePoolRequest) error {
	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	replicas := req.Replicas
	if replicas <= 0 {
		replicas = 2
	}

	// Set default resources if not specified
	resources := req.Resources
	if resources == nil {
		resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		}
	}

	// Set default workspace dir if not specified
	workspaceDir := req.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = "/workspace"
	}

	pool := &arlv1alpha1.WarmPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: ns,
		},
		Spec: arlv1alpha1.WarmPoolSpec{
			Replicas: replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "executor",
							Image:           req.Image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"sh", "-c", "sleep infinity"},
							Resources:       *resources,
							VolumeMounts: []corev1.VolumeMount{
								{Name: "workspace", MountPath: workspaceDir},
							},
						},
					},
				},
			},
			Tools:         req.Tools,
			ImageLocality: req.ImageLocality,
		},
	}

	return g.k8sClient.Create(ctx, pool)
}

// GetPool returns WarmPool info.
func (g *Gateway) GetPool(ctx context.Context, name, namespace string) (*PoolInfo, error) {
	if namespace == "" {
		namespace = "default"
	}

	pool := &arlv1alpha1.WarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pool); err != nil {
		return nil, err
	}

	info := &PoolInfo{
		Name:              pool.Name,
		Namespace:         pool.Namespace,
		Replicas:          pool.Spec.Replicas,
		ReadyReplicas:     pool.Status.ReadyReplicas,
		AllocatedReplicas: pool.Status.AllocatedReplicas,
	}

	for _, c := range pool.Status.Conditions {
		info.Conditions = append(info.Conditions, PoolCondition{
			Type:    c.Type,
			Status:  string(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
		})
	}

	return info, nil
}

// ScalePool updates the replica count of a WarmPool.
func (g *Gateway) ScalePool(ctx context.Context, name string, req ScalePoolRequest) (*PoolInfo, error) {
	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	pool := &arlv1alpha1.WarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, pool); err != nil {
		return nil, fmt.Errorf("get pool: %w", err)
	}

	pool.Spec.Replicas = req.Replicas

	if req.Resources != nil {
		for i := range pool.Spec.Template.Spec.Containers {
			if pool.Spec.Template.Spec.Containers[i].Name == "executor" {
				pool.Spec.Template.Spec.Containers[i].Resources = *req.Resources
				break
			}
		}
	}

	if err := g.k8sClient.Update(ctx, pool); err != nil {
		return nil, fmt.Errorf("update pool: %w", err)
	}

	return g.GetPool(ctx, name, ns)
}

// DeletePool deletes a WarmPool CRD.
func (g *Gateway) DeletePool(ctx context.Context, name, namespace string) error {
	if namespace == "" {
		namespace = "default"
	}

	pool := &arlv1alpha1.WarmPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	return g.k8sClient.Delete(ctx, pool)
}

// checkPoolHealth returns an error if the pool is unhealthy (failing pods, no ready replicas).
func (g *Gateway) checkPoolHealth(ctx context.Context, poolRef, namespace string) error {
	pool := &arlv1alpha1.WarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolRef, Namespace: namespace}, pool); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("pool %q not found in namespace %q", poolRef, namespace)
		}
		return fmt.Errorf("get pool: %w", err)
	}

	// Check for PodsFailing condition
	for _, cond := range pool.Status.Conditions {
		if cond.Type == "PodsFailing" && cond.Status == "True" {
			if pool.Status.ReadyReplicas == 0 {
				if isTransientImagePullRateLimit(cond.Message) {
					log.Printf("Warning: pool %q has transient image pull rate-limit with no ready replicas, continuing: %s",
						poolRef, cond.Message)
					return nil
				}
				return fmt.Errorf("pool %q has failing pods and no ready replicas: %s", poolRef, cond.Message)
			}
			// If some replicas are ready despite failures, log warning but allow
			log.Printf("Warning: pool %q has failing pods but %d ready replicas: %s",
				poolRef, pool.Status.ReadyReplicas, cond.Message)
		}
	}

	return nil
}

func isTransientImagePullRateLimit(message string) bool {
	if message == "" {
		return false
	}

	lower := strings.ToLower(message)
	isPullFailure := strings.Contains(lower, "imagepullbackoff") || strings.Contains(lower, "errimagepull")
	if !isPullFailure {
		return false
	}

	return strings.Contains(lower, "qps exceeded") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "toomanyrequests") ||
		strings.Contains(lower, "429")
}

// diagnosePoolHealth returns a diagnostic string about pool health (used in timeout errors).
func (g *Gateway) diagnosePoolHealth(ctx context.Context, poolRef, namespace string) string {
	pool := &arlv1alpha1.WarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolRef, Namespace: namespace}, pool); err != nil {
		return fmt.Sprintf("unable to check pool health: %v", err)
	}

	diag := fmt.Sprintf("pool=%s replicas=%d ready=%d allocated=%d",
		poolRef, pool.Spec.Replicas, pool.Status.ReadyReplicas, pool.Status.AllocatedReplicas)

	for _, cond := range pool.Status.Conditions {
		if cond.Status == "True" || (cond.Type == "Ready" && cond.Status == "False") {
			diag += fmt.Sprintf(" [%s: %s]", cond.Type, cond.Message)
		}
	}

	return diag
}

// annotationPatchMinInterval is the minimum time between pod annotation patches
// to avoid excessive K8s API calls.
const annotationPatchMinInterval = 30 * time.Second

// touchLastTaskTime updates the in-memory lastTaskTime for session idle tracking
// and asynchronously patches the pod's last-activity annotation (throttled to at most once per 30s).
func (g *Gateway) touchLastTaskTime(sessionID string) {
	val, ok := g.sessions.Load(sessionID)
	if !ok {
		return
	}
	s := val.(*session)
	now := time.Now()

	s.mu.Lock()
	s.lastTaskTime = now
	shouldPatch := now.Sub(s.lastAnnotationPatch) >= annotationPatchMinInterval
	if shouldPatch {
		s.lastAnnotationPatch = now
	}
	podName := s.Info.PodName
	namespace := s.Info.Namespace
	s.mu.Unlock()

	if shouldPatch {
		go func() {
			bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer bgCancel()

			pod := &corev1.Pod{}
			if err := g.k8sClient.Get(bgCtx, types.NamespacedName{Name: podName, Namespace: namespace}, pod); err != nil {
				log.Printf("Warning: failed to get pod %s for annotation patch: %v", podName, err)
				return
			}
			patch := client.MergeFrom(pod.DeepCopy())
			if pod.Annotations == nil {
				pod.Annotations = make(map[string]string)
			}
			pod.Annotations[labels.LastActivityAnnotation] = now.UTC().Format(time.RFC3339)
			if err := g.k8sClient.Patch(bgCtx, pod, patch); err != nil {
				log.Printf("Warning: failed to patch last-activity annotation on pod %s: %v", podName, err)
			}
		}()
	}
}

// resolveIdleTimeout returns the idle timeout for a session request,
// falling back to the gateway-wide default.
func (g *Gateway) resolveIdleTimeout(req CreateSessionRequest) time.Duration {
	if req.IdleTimeoutSeconds > 0 {
		return time.Duration(req.IdleTimeoutSeconds) * time.Second
	}
	return g.gwConfig.IdleTimeout
}

// resolveMaxLifetime returns the max lifetime for a session request,
// falling back to the gateway-wide default.
func (g *Gateway) resolveMaxLifetime(req CreateSessionRequest) time.Duration {
	if req.MaxLifetimeSeconds > 0 {
		return time.Duration(req.MaxLifetimeSeconds) * time.Second
	}
	return g.gwConfig.MaxLifetime
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
		}
	}
}

func (g *Gateway) sweepSessions() {
	now := time.Now()
	g.sessions.Range(func(key, value any) bool {
		sessionID := key.(string)
		s := value.(*session)

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
			if err := g.DeleteSession(ctx, sessionID); err != nil {
				log.Printf("Warning: failed to delete expired session %s: %v", sessionID, err)
			}
			cancel()
			return true
		}

		// Check idle timeout (0 means no limit)
		if idleTimeout > 0 && now.Sub(lastTask) > idleTimeout {
			log.Printf("Session %s idle for %v (timeout=%v), deleting", sessionID, now.Sub(lastTask), idleTimeout)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := g.DeleteSession(ctx, sessionID); err != nil {
				log.Printf("Warning: failed to delete idle session %s: %v", sessionID, err)
			}
			cancel()
		}

		return true
	})
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
	if g.poolManager == nil {
		return nil, fmt.Errorf("pool manager not configured")
	}

	if !validLabelValue.MatchString(req.ExperimentID) {
		return nil, fmt.Errorf("experimentId must be a valid Kubernetes label value (max 63 chars, alphanumeric/dash/underscore/dot, must start and end with alphanumeric)")
	}

	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	poolName, err := g.poolManager.AcquireSession(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("acquire pool: %w", err)
	}

	// Create session using existing logic, with experiment labels pre-applied
	info, err := g.CreateSession(ctx, CreateSessionRequest{
		PoolRef:   poolName,
		Namespace: ns,
		ExtraLabels: map[string]string{
			labelManaged:    "true",
			labelExperiment: req.ExperimentID,
		},
	})
	if err != nil {
		// Release the acquired slot since session creation failed
		g.poolManager.ReleaseSession(poolName)
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Mark the session as managed
	val, ok := g.sessions.Load(info.ID)
	if ok {
		s := val.(*session)
		s.mu.Lock()
		s.managed = true
		s.experimentID = req.ExperimentID
		s.mu.Unlock()
	}

	return &ManagedSessionInfo{
		SessionInfo:  *info,
		ExperimentID: req.ExperimentID,
		Managed:      true,
	}, nil
}

// ListExperimentSessions returns all active sessions for an experiment.
func (g *Gateway) ListExperimentSessions(experimentID string) []ManagedSessionInfo {
	results := make([]ManagedSessionInfo, 0)
	g.sessions.Range(func(_, value any) bool {
		s := value.(*session)
		s.mu.RLock()
		if s.managed && s.experimentID == experimentID {
			results = append(results, ManagedSessionInfo{
				SessionInfo:  s.Info,
				ExperimentID: s.experimentID,
				Managed:      true,
			})
		}
		s.mu.RUnlock()
		return true
	})
	return results
}

// DeleteExperiment deletes all sessions for an experiment.
func (g *Gateway) DeleteExperiment(ctx context.Context, experimentID string) (int, error) {
	sessions := g.ListExperimentSessions(experimentID)
	deleted := 0
	var lastErr error
	for _, s := range sessions {
		if err := g.DeleteSession(ctx, s.ID); err != nil {
			lastErr = err
			log.Printf("Warning: failed to delete session %s in experiment %s: %v", s.ID, experimentID, err)
		} else {
			deleted++
		}
	}

	return deleted, lastErr
}
