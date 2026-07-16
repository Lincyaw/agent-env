package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Lincyaw/agent-env/pkg/audit"
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

// ReplayFrom replays steps from a source session into the target session's sandbox.
func (g *Gateway) ReplayFrom(ctx context.Context, targetSessionID string, req ReplayRequest) (*ReplayResponse, error) {
	records, err := g.replayRecords(ctx, req.SourceSessionID, req.UpToStep)
	if err != nil {
		return nil, err
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
			if err := g.replayUpload(ctx, podIP, record); err != nil {
				log.Printf("Warning: replay upload step %d failed: %v", record.Index, err)
				errors++
			} else {
				replayed++
			}
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

func (g *Gateway) replayRecords(ctx context.Context, sourceSessionID string, upToStep *int) ([]StepRecord, error) {
	sourceSession, ok := g.store.Get(sourceSessionID)
	if !ok {
		sourceSession, ok = g.store.GetHistorical(sourceSessionID)
	}
	if ok {
		if upToStep != nil {
			return sourceSession.History.GetUpTo(*upToStep), nil
		}
		return sourceSession.History.GetAll(), nil
	}
	return g.replayRecordsFromTrajectory(ctx, sourceSessionID, upToStep)
}

func (g *Gateway) replayRecordsFromTrajectory(ctx context.Context, sessionID string, upToStep *int) ([]StepRecord, error) {
	if g.trajectoryWriter == nil {
		return nil, fmt.Errorf("source session %s not found", sessionID)
	}
	var entries []audit.TrajectoryEntry
	var err error
	if upToStep != nil {
		entries, err = g.trajectoryWriter.GetTrajectoryUpTo(ctx, sessionID, *upToStep)
	} else {
		entries, err = g.trajectoryWriter.GetTrajectory(ctx, sessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("source session %s not found in store or trajectory: %w", sessionID, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("source session %s not found", sessionID)
	}
	records := make([]StepRecord, len(entries))
	for i, e := range entries {
		records[i] = StepRecord{
			Index:     e.Step,
			Name:      e.Name,
			Input:     e.Action,
			Timestamp: e.Timestamp,
		}
	}
	return records, nil
}

func (g *Gateway) replayUpload(ctx context.Context, podIP string, record StepRecord) error {
	var upload uploadRecord
	if err := json.Unmarshal(record.Input, &upload); err != nil {
		return fmt.Errorf("unmarshal upload record: %w", err)
	}
	if upload.SHA256 == "" || upload.Path == "" {
		return fmt.Errorf("upload record missing path or sha256")
	}
	if g.trajectoryWriter == nil {
		return fmt.Errorf("no trajectory writer for blob retrieval")
	}
	content, err := g.trajectoryWriter.GetBlob(ctx, upload.SHA256)
	if err != nil {
		return fmt.Errorf("retrieve blob %s: %w", upload.SHA256[:12], err)
	}
	_, err = g.sidecarClient.WriteFile(ctx, podIP, upload.Path, bytes.NewReader(content), upload.SHA256)
	return err
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
			if err := g.replayUpload(ctx, newAllocation.PodIP, record); err != nil {
				log.Printf("Warning: restore upload step %d failed: %v", record.Index, err)
				if err := g.releaseRestoreAllocation(*newAllocation); err != nil {
					log.Printf("Warning: failed to release runtime %s after restore failure: %v", newAllocation.PodName, err)
				}
				return fmt.Errorf("replay upload step %d failed: %w", record.Index, err)
			}
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
