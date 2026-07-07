package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

// ReplayFrom replays steps from a source session into the target session's sandbox.
func (g *Gateway) ReplayFrom(ctx context.Context, targetSessionID string, req ReplayRequest) (*ReplayResponse, error) {
	sourceSession, ok := g.store.Get(req.SourceSessionID)
	if !ok {
		sourceSession, ok = g.store.GetHistorical(req.SourceSessionID)
		if !ok {
			return nil, fmt.Errorf("source session %s not found", req.SourceSessionID)
		}
	}
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

	s, ok := g.store.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	atomic.AddInt32(&s.activeExecs, 1)
	defer atomic.AddInt32(&s.activeExecs, -1)

	records := s.History.GetUpTo(targetIdx)
	if len(records) == 0 && targetIdx >= 0 {
		if targetIdx > 0 {
			return fmt.Errorf("no history records up to index %d", targetIdx)
		}
	}

	s.mu.RLock()
	oldAllocation := s.runtimeAllocation()
	lifecycle := g.sessionRuntimeLifecycleLocked(s, time.Now())
	s.mu.RUnlock()

	newSandboxName := fmt.Sprintf("%s-r%d", sessionID, time.Now().UnixMilli())

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
		diag := g.diagnosePoolHealth(ctx, oldAllocation.PoolRef, oldAllocation.Namespace)
		return fmt.Errorf("allocate new runtime for restore: %w (%s)", err, diag)
	}

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

	s.mu.Lock()
	s.Info.PodIP = newAllocation.PodIP
	s.Info.PodName = newAllocation.PodName
	s.Info.SandboxName = newSandboxName
	s.Runtime = *newAllocation
	s.mu.Unlock()

	s.History.TruncateTo(targetIdx)
	g.touchLastTaskTime(sessionID)
	g.store.SyncHistory(sessionID)

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
