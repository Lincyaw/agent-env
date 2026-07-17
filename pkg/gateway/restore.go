package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Lincyaw/agent-env/pkg/audit"
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

// ReplayFrom replays steps from a source session, optionally as an async operation.
func (g *Gateway) ReplayFrom(ctx context.Context, targetSessionID string, req ReplayRequest) (*ReplayResponse, error) {
	if req.OperationID == "" {
		return g.replayNow(ctx, targetSessionID, req)
	}
	return g.replayWithOperation(ctx, targetSessionID, req)
}

func (g *Gateway) replayWithOperation(ctx context.Context, targetSessionID string, req ReplayRequest) (*ReplayResponse, error) {
	hash := replayRequestHash(req)
	op, _, err := g.getOrStartOperation(targetSessionID, req.OperationID, hash, func(bgCtx context.Context) (any, error) {
		return g.replayNow(bgCtx, targetSessionID, req)
	})
	if err != nil {
		return nil, err
	}

	select {
	case <-op.done:
	case <-ctx.Done():
		return nil, &OperationPending{OperationID: req.OperationID, SessionID: targetSessionID}
	}

	if op.err != nil {
		return nil, op.err
	}
	resp, _ := op.result.(*ReplayResponse)
	return resp, nil
}

func replayRequestHash(req ReplayRequest) string {
	cp := req
	cp.OperationID = ""
	raw, _ := json.Marshal(cp)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// replayNow replays steps synchronously.
func (g *Gateway) replayNow(ctx context.Context, targetSessionID string, req ReplayRequest) (*ReplayResponse, error) {
	records, err := g.replayRecords(ctx, req.SourceSessionID, req.UpToStep)
	if err != nil {
		return nil, err
	}

	log.Printf("Replay %s → %s: %d steps to replay", req.SourceSessionID, targetSessionID, len(records))

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
			log.Printf("Warning: replay exec step %d unmarshal failed: %v", record.Index, err)
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
			log.Printf("Warning: replay exec step %d failed on %s: %v", record.Index, podIP, err)
			errors++
			continue
		}
		replayed++
		if replayed%10 == 0 {
			log.Printf("Replay %s: %d/%d steps done (%d errors)", targetSessionID, replayed, len(records), errors)
		}
	}

	log.Printf("Replay %s → %s complete: %d replayed, %d errors", req.SourceSessionID, targetSessionID, replayed, errors)
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
	if err != nil {
		return fmt.Errorf("write to %s path %s: %w", podIP, upload.Path, err)
	}
	return nil
}

// Restore restores a session to a previous snapshot, optionally as an async operation.
func (g *Gateway) Restore(ctx context.Context, sessionID string, req RestoreRequest) (*RestoreResponse, error) {
	if req.OperationID == "" {
		return g.restoreNow(ctx, sessionID, req.SnapshotID)
	}
	return g.restoreWithOperation(ctx, sessionID, req)
}

func (g *Gateway) restoreWithOperation(ctx context.Context, sessionID string, req RestoreRequest) (*RestoreResponse, error) {
	hash := restoreRequestHash(req)
	op, _, err := g.getOrStartOperation(sessionID, req.OperationID, hash, func(bgCtx context.Context) (any, error) {
		return g.restoreNow(bgCtx, sessionID, req.SnapshotID)
	})
	if err != nil {
		return nil, err
	}

	select {
	case <-op.done:
	case <-ctx.Done():
		return nil, &OperationPending{OperationID: req.OperationID, SessionID: sessionID}
	}

	if op.err != nil {
		return nil, op.err
	}
	resp, _ := op.result.(*RestoreResponse)
	return resp, nil
}

func restoreRequestHash(req RestoreRequest) string {
	cp := req
	cp.OperationID = ""
	raw, _ := json.Marshal(cp)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// restoreNow restores a session synchronously, returning a RestoreResponse.
func (g *Gateway) restoreNow(ctx context.Context, sessionID string, snapshotID string) (resp *RestoreResponse, retErr error) {
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
		return nil, fmt.Errorf("invalid snapshot_id %q: must be a step index", snapshotID)
	}

	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	atomic.AddInt32(&s.activeExecs, 1)
	defer atomic.AddInt32(&s.activeExecs, -1)

	records := s.History.GetUpTo(targetIdx)
	if len(records) == 0 && targetIdx >= 0 {
		if targetIdx > 0 {
			return nil, fmt.Errorf("no history records up to index %d", targetIdx)
		}
	}

	s.mu.RLock()
	oldAllocation := s.runtimeAllocation()
	lifecycle := g.sessionRuntimeLifecycleLocked(s, time.Now())
	s.mu.RUnlock()

	log.Printf("Restore %s to snapshot %s: %d steps to replay", sessionID, snapshotID, len(records))

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
		return nil, fmt.Errorf("allocate new runtime for restore: %w (%s)", err, diag)
	}

	log.Printf("Restore %s: new pod %s (%s) allocated", sessionID, newAllocation.PodName, newAllocation.PodIP)

	stepsReplayed := 0
	for _, record := range records {
		if record.Name == uploadFileStepName {
			if err := g.replayUpload(ctx, newAllocation.PodIP, record); err != nil {
				log.Printf("Warning: restore upload step %d failed: %v", record.Index, err)
				if err := g.releaseRestoreAllocation(*newAllocation); err != nil {
					log.Printf("Warning: failed to release runtime %s after restore failure: %v", newAllocation.PodName, err)
				}
				return nil, fmt.Errorf("replay upload step %d failed: %w", record.Index, err)
			}
			stepsReplayed++
			continue
		}

		var step StepRequest
		if err := json.Unmarshal(record.Input, &step); err != nil {
			log.Printf("Warning: failed to unmarshal step %d for replay: %v", record.Index, err)
			continue
		}

		restoreTimeout := resolveStepTimeoutSeconds(step)
		if restoreTimeout < 600 {
			restoreTimeout = 600
		}
		execReq := &sidecar.ExecRequest{
			Command:        step.Command,
			Env:            step.Env,
			WorkingDir:     step.WorkDir,
			TimeoutSeconds: restoreTimeout,
		}
		if _, err := g.sidecarClient.Execute(ctx, newAllocation.PodIP, execReq); err != nil {
			if err := g.releaseRestoreAllocation(*newAllocation); err != nil {
				log.Printf("Warning: failed to release runtime %s after restore failure: %v", newAllocation.PodName, err)
			}
			return nil, fmt.Errorf("replay step %d failed: %w", record.Index, err)
		}
		stepsReplayed++
		if stepsReplayed%10 == 0 {
			log.Printf("Restore %s: %d/%d steps done", sessionID, stepsReplayed, len(records))
		}
	}

	log.Printf("Restore %s complete: %d steps replayed on %s", sessionID, stepsReplayed, newAllocation.PodName)

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

	return &RestoreResponse{
		SnapshotID:    snapshotID,
		StepsReplayed: stepsReplayed,
	}, nil
}

func (g *Gateway) releaseRestoreAllocation(allocation RuntimeAllocation) error {
	bgCtx, bgCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer bgCancel()
	return g.runtimeAllocator.Release(bgCtx, allocation)
}
