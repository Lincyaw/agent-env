package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
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
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

// session holds internal session state.
type session struct {
	Info    SessionInfo
	History *StepHistory
}

// Gateway manages sessions and forwards execution to sidecars.
type Gateway struct {
	k8sClient        client.Client
	sidecarClient    interfaces.SidecarClient
	trajectoryWriter *audit.TrajectoryWriter
	sessions         sync.Map // sessionID → *session
}

// New creates a new gateway. trajectoryWriter may be nil to disable trajectory recording.
func New(k8sClient client.Client, sidecarClient interfaces.SidecarClient, trajectoryWriter *audit.TrajectoryWriter) *Gateway {
	return &Gateway{
		k8sClient:        k8sClient,
		sidecarClient:    sidecarClient,
		trajectoryWriter: trajectoryWriter,
	}
}

// CreateSession creates a Sandbox CRD, waits for Ready, and registers a session.
func (g *Gateway) CreateSession(ctx context.Context, req CreateSessionRequest) (*SessionInfo, error) {
	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	// Pre-flight: check pool health before creating sandbox
	if err := g.checkPoolHealth(ctx, req.PoolRef, ns); err != nil {
		return nil, fmt.Errorf("pool not ready: %w", err)
	}

	sessionID := fmt.Sprintf("gw-%d", time.Now().UnixMilli())
	sandboxName := sessionID

	sandbox := &arlv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sandboxName,
			Namespace: ns,
		},
		Spec: arlv1alpha1.SandboxSpec{
			PoolRef:   req.PoolRef,
			KeepAlive: req.KeepAlive,
		},
	}

	if err := g.k8sClient.Create(ctx, sandbox); err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}

	// Poll until sandbox is Ready (with timeout)
	pollCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	var podIP, podName string
	poolCheckTicker := 0
	for {
		select {
		case <-pollCtx.Done():
			// On timeout, check pool health for diagnostic info
			diag := g.diagnosePoolHealth(ctx, req.PoolRef, ns)
			return nil, fmt.Errorf("timeout waiting for sandbox %s to be ready: %s", sandboxName, diag)
		case <-time.After(500 * time.Millisecond):
		}

		sb := &arlv1alpha1.Sandbox{}
		if err := g.k8sClient.Get(pollCtx, types.NamespacedName{Name: sandboxName, Namespace: ns}, sb); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("get sandbox: %w", err)
		}

		if sb.Status.Phase == arlv1alpha1.SandboxPhaseReady {
			podIP = sb.Status.PodIP
			podName = sb.Status.PodName
			break
		}
		if sb.Status.Phase == arlv1alpha1.SandboxPhaseFailed {
			// Include sandbox conditions in error
			failMsg := g.extractSandboxFailure(sb)
			return nil, fmt.Errorf("sandbox %s failed: %s", sandboxName, failMsg)
		}

		// Periodically check pool health during wait (every 5 iterations = ~2.5s)
		poolCheckTicker++
		if poolCheckTicker%5 == 0 {
			if err := g.checkPoolHealth(ctx, req.PoolRef, ns); err != nil {
				// Clean up the pending sandbox
				_ = g.k8sClient.Delete(ctx, sandbox)
				return nil, fmt.Errorf("pool became unhealthy while waiting: %w", err)
			}
		}
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
		Info:    info,
		History: NewStepHistory(),
	})

	return &info, nil
}

// GetSession returns session info.
func (g *Gateway) GetSession(sessionID string) (*SessionInfo, error) {
	val, ok := g.sessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	s := val.(*session)
	return &s.Info, nil
}

// DeleteSession deletes the sandbox and removes the session.
func (g *Gateway) DeleteSession(ctx context.Context, sessionID string) error {
	val, ok := g.sessions.Load(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	s := val.(*session)

	sandbox := &arlv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.Info.SandboxName,
			Namespace: s.Info.Namespace,
		},
	}

	if err := g.k8sClient.Delete(ctx, sandbox); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete sandbox: %w", err)
	}

	g.sessions.Delete(sessionID)
	return nil
}

// ExecuteSteps executes steps directly via sidecar gRPC.
func (g *Gateway) ExecuteSteps(ctx context.Context, sessionID string, req ExecuteRequest) (*ExecuteResponse, error) {
	val, ok := g.sessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	s := val.(*session)
	podIP := s.Info.PodIP

	resp := &ExecuteResponse{
		SessionID: sessionID,
	}

	totalStart := time.Now()

	for _, step := range req.Steps {
		start := time.Now()
		inputJSON, _ := json.Marshal(step)

		globalIdx := s.History.Len()

		var result StepResult
		result.Index = globalIdx
		result.Name = step.Name
		result.Input = inputJSON
		result.Timestamp = start

		execReq := &sidecar.ExecRequest{
			Command:    step.Command,
			Env:        step.Env,
			WorkingDir: step.WorkDir,
		}
		execResp, err := g.sidecarClient.Execute(ctx, podIP, execReq)
		if err != nil {
			result.Output.Stderr = err.Error()
			result.Output.ExitCode = 1
		} else {
			result.Output.Stdout = execResp.GetStdout()
			result.Output.Stderr = execResp.GetStderr()
			result.Output.ExitCode = execResp.GetExitCode()
		}

		result.DurationMs = time.Since(start).Milliseconds()
		result.SnapshotID = fmt.Sprintf("%d", globalIdx)

		resp.Results = append(resp.Results, result)

		// Record in history
		stepRecord := StepRecord{
			Name:       result.Name,
			Input:      result.Input,
			Output:     result.Output,
			SnapshotID: result.SnapshotID,
			DurationMs: result.DurationMs,
			Timestamp:  result.Timestamp,
		}
		s.History.Add(stepRecord)

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

	// Update Sandbox status.lastTaskTime so the idle-timeout controller
	// measures idle duration from the last execution, not from creation time.
	go g.touchSandboxLastTaskTime(s.Info.SandboxName, s.Info.Namespace)

	return resp, nil
}

// Restore restores a session to a previous snapshot by creating a new sandbox and replaying steps.
func (g *Gateway) Restore(ctx context.Context, sessionID string, snapshotID string) error {
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

	// Save old sandbox info for cleanup
	oldSandboxName := s.Info.SandboxName
	oldNamespace := s.Info.Namespace

	// Create a new Sandbox CRD
	newSandboxName := fmt.Sprintf("%s-r%d", sessionID, time.Now().UnixMilli())
	sandbox := &arlv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      newSandboxName,
			Namespace: s.Info.Namespace,
		},
		Spec: arlv1alpha1.SandboxSpec{
			PoolRef:   s.Info.PoolRef,
			KeepAlive: true,
		},
	}

	if err := g.k8sClient.Create(ctx, sandbox); err != nil {
		return fmt.Errorf("create new sandbox for restore: %w", err)
	}

	// Poll until sandbox is Ready
	pollCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	var newPodIP, newPodName string
	for {
		select {
		case <-pollCtx.Done():
			return fmt.Errorf("timeout waiting for restore sandbox %s to be ready", newSandboxName)
		case <-time.After(500 * time.Millisecond):
		}

		sb := &arlv1alpha1.Sandbox{}
		if err := g.k8sClient.Get(pollCtx, types.NamespacedName{Name: newSandboxName, Namespace: s.Info.Namespace}, sb); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("get restore sandbox: %w", err)
		}

		if sb.Status.Phase == arlv1alpha1.SandboxPhaseReady {
			newPodIP = sb.Status.PodIP
			newPodName = sb.Status.PodName
			break
		}
		if sb.Status.Phase == arlv1alpha1.SandboxPhaseFailed {
			return fmt.Errorf("restore sandbox %s failed", newSandboxName)
		}
	}

	// Replay each step from history on the new sandbox
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
			return fmt.Errorf("replay step %d failed: %w", record.Index, err)
		}
	}

	// Update session to point at the new sandbox
	s.Info.PodIP = newPodIP
	s.Info.PodName = newPodName
	s.Info.SandboxName = newSandboxName

	// Truncate history to records 0..targetIdx
	s.History.TruncateTo(targetIdx)

	// Delete old sandbox (async, best-effort)
	go func() {
		oldSandbox := &arlv1alpha1.Sandbox{
			ObjectMeta: metav1.ObjectMeta{
				Name:      oldSandboxName,
				Namespace: oldNamespace,
			},
		}
		if err := g.k8sClient.Delete(context.Background(), oldSandbox); err != nil {
			log.Printf("Warning: failed to delete old sandbox %s: %v", oldSandboxName, err)
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
							Name:      "executor",
							Image:     req.Image,
							Command:   []string{"sh", "-c", "sleep infinity"},
							Resources: *resources,
							VolumeMounts: []corev1.VolumeMount{
								{Name: "workspace", MountPath: workspaceDir},
							},
						},
					},
				},
			},
			Tools: req.Tools,
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
				return fmt.Errorf("pool %q has failing pods and no ready replicas: %s", poolRef, cond.Message)
			}
			// If some replicas are ready despite failures, log warning but allow
			log.Printf("Warning: pool %q has failing pods but %d ready replicas: %s",
				poolRef, pool.Status.ReadyReplicas, cond.Message)
		}
	}

	return nil
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

// touchSandboxLastTaskTime patches the Sandbox status.lastTaskTime to now.
// Runs asynchronously so it doesn't block the execute response.
func (g *Gateway) touchSandboxLastTaskTime(sandboxName, namespace string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sb := &arlv1alpha1.Sandbox{}
	key := types.NamespacedName{Name: sandboxName, Namespace: namespace}
	if err := g.k8sClient.Get(ctx, key, sb); err != nil {
		log.Printf("Warning: failed to get sandbox %s for lastTaskTime update: %v", sandboxName, err)
		return
	}

	now := metav1.Now()
	sb.Status.LastTaskTime = &now
	if err := g.k8sClient.Status().Update(ctx, sb); err != nil {
		log.Printf("Warning: failed to update sandbox %s lastTaskTime: %v", sandboxName, err)
	}
}

// extractSandboxFailure returns a failure message from sandbox conditions.
func (g *Gateway) extractSandboxFailure(sb *arlv1alpha1.Sandbox) string {
	for _, cond := range sb.Status.Conditions {
		if cond.Status == "False" && cond.Message != "" {
			return fmt.Sprintf("%s: %s", cond.Reason, cond.Message)
		}
	}
	return "unknown reason"
}
