package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Lincyaw/agent-env/pkg/audit"
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

func resolveStepTimeoutSeconds(step StepRequest) int32 {
	if step.TimeoutSeconds > 0 {
		return step.TimeoutSeconds
	}
	if step.Timeout > 0 {
		return step.Timeout
	}
	return 0
}

const runtimeReadyPollInterval = 2 * time.Second

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

	resolved, err := g.resolveWithRetry(ctx, s, sessionID)
	if err != nil {
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
		g.store.Sync(sessionID)
	}

	return s, resolved.PodIP, nil
}

// resolveWithRetry calls Resolve and polls when the runtime is temporarily
// not ready (e.g. sandbox still binding after a gateway restart). The caller's
// context controls the deadline so the HTTP request timeout is respected.
func (g *Gateway) resolveWithRetry(ctx context.Context, s *session, sessionID string) (*RuntimeAllocation, error) {
	for {
		resolved, err := g.runtimeAllocator.Resolve(ctx, s.runtimeAllocation(), sessionID)
		if err == nil {
			return resolved, nil
		}
		var notReady *RuntimeNotReadyError
		if !errors.As(err, &notReady) {
			g.dropSession(sessionID, s)
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("session %s runtime did not become ready: %w", sessionID, ctx.Err())
		case <-time.After(runtimeReadyPollInterval):
		}
	}
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

// recordStepResult handles the common post-execution bookkeeping for a completed step:
// metrics, history recording, and trajectory enqueueing.
func (g *Gateway) recordStepResult(s *session, sessionID string, result *StepResult, start time.Time) {
	storedOutput, outputBytes, outputTruncated := g.retainedStepOutput(result.Output)
	g.recordRetainedStepResult(s, sessionID, result, start, storedOutput, outputBytes, outputTruncated)
}

func (g *Gateway) recordRetainedStepResult(s *session, sessionID string, result *StepResult, start time.Time, storedOutput StepOutput, outputBytes int, outputTruncated bool) {
	result.DurationMs = time.Since(start).Milliseconds()

	if g.metrics != nil {
		stepType := result.Name
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

	stepRecord := StepRecord{
		Name:            result.Name,
		Input:           result.Input,
		Output:          storedOutput,
		OutputBytes:     outputBytes,
		OutputTruncated: outputTruncated,
		DurationMs:      result.DurationMs,
		Timestamp:       result.Timestamp,
	}
	globalIdx := s.History.Add(stepRecord)

	result.Index = globalIdx
	result.SnapshotID = fmt.Sprintf("%d", globalIdx)

	obsJSON, _ := json.Marshal(storedOutput)
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

		result := StepResult{Name: step.Name, Input: inputJSON, Timestamp: start}

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
		g.recordStepResult(s, sessionID, &result, start)
		resp.Results = append(resp.Results, result)
	}

	resp.TotalDurationMs = time.Since(totalStart).Milliseconds()
	g.touchLastTaskTime(sessionID)
	g.store.SyncHistory(sessionID)

	if g.checkpointStore != nil && g.gwConfig.SandboxCheckpointEnabled && len(resp.Results) > 0 {
		steps := make([]int, len(resp.Results))
		for i, r := range resp.Results {
			steps[i] = r.Index
		}
		go g.persistCheckpointSteps(sessionID, podIP, steps)
	}

	return resp, nil
}

type sseOutputEvent struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// ExecuteStepsSSE streams step execution as SSE events to the HTTP response writer.
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

	var persistSteps []int
	for _, step := range req.Steps {
		start := time.Now()
		inputJSON, _ := json.Marshal(step)

		result := StepResult{Name: step.Name, Input: inputJSON, Timestamp: start}

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

		g.recordStepResult(s, sessionID, &result, start)
		persistSteps = append(persistSteps, result.Index)

		resultData, _ := json.Marshal(result)
		fmt.Fprintf(w, "event: result\ndata: %s\n\n", resultData)
		flusher.Flush()
	}

	g.touchLastTaskTime(sessionID)
	g.store.SyncHistory(sessionID)

	if g.checkpointStore != nil && g.gwConfig.SandboxCheckpointEnabled && len(persistSteps) > 0 {
		go g.persistCheckpointSteps(sessionID, podIP, persistSteps)
	}
}

// persistCheckpointSteps downloads per-step incremental tars from the sidecar
// and saves them to the checkpoint store. Runs in a background goroutine;
// failures are logged and do not affect the execute response.
func (g *Gateway) persistCheckpointSteps(sessionID, podIP string, stepIndices []int) {
	for _, idx := range stepIndices {
		checkpointStep := idx + 1
		if g.checkpointStore.HasStep(sessionID, checkpointStep) {
			log.Printf("Checkpoint persist session %s step %d: already exists, skipping", sessionID, checkpointStep)
			continue
		}
		if err := g.persistSingleCheckpointStep(sessionID, podIP, checkpointStep); err != nil {
			log.Printf("Checkpoint persist session %s step %d: %v", sessionID, checkpointStep, err)
		} else {
			log.Printf("Checkpoint persist session %s step %d: saved to store", sessionID, checkpointStep)
		}
	}
}

func (g *Gateway) persistAllCheckpoints(sessionID, podIP string) {
	sidecarHTTPPort := g.gwConfig.SidecarHTTPPort
	if sidecarHTTPPort == 0 {
		sidecarHTTPPort = 8080
	}
	listURL := fmt.Sprintf("http://%s:%d/v1/checkpoints", podIP, sidecarHTTPPort)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		log.Printf("Checkpoint persist-all %s: build list request: %v", sessionID, err)
		return
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		log.Printf("Checkpoint persist-all %s: list: %v", sessionID, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("Checkpoint persist-all %s: list HTTP %s", sessionID, resp.Status)
		return
	}

	var listResp struct {
		Steps []int `json:"steps"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		log.Printf("Checkpoint persist-all %s: decode: %v", sessionID, err)
		return
	}

	persisted := 0
	for _, step := range listResp.Steps {
		if g.checkpointStore.HasStep(sessionID, step) {
			continue
		}
		if err := g.persistSingleCheckpointStep(sessionID, podIP, step); err != nil {
			log.Printf("Checkpoint persist-all %s step %d: %v", sessionID, step, err)
		} else {
			persisted++
		}
	}
	if persisted > 0 {
		log.Printf("Checkpoint persist-all %s: saved %d/%d steps", sessionID, persisted, len(listResp.Steps))
	}
}

func (g *Gateway) persistSingleCheckpointStep(sessionID, podIP string, checkpointStep int) error {
	sidecarHTTPPort := g.gwConfig.SidecarHTTPPort
	if sidecarHTTPPort == 0 {
		sidecarHTTPPort = 8080
	}
	checkpointURL := fmt.Sprintf("http://%s:%d/v1/checkpoints/%d",
		podIP, sidecarHTTPPort, checkpointStep)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkpointURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("HTTP %s: %s", resp.Status, string(body))
	}

	return g.checkpointStore.Save(sessionID, checkpointStep, resp.Body)
}
