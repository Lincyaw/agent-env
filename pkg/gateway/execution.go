package gateway

import (
	"context"
	"encoding/json"
	"fmt"
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

// resolveSessionPodIP validates the session's SandboxClaim binding before
// returning the current pod IP.
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
		g.store.Sync(sessionID)
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

	g.touchLastTaskTime(sessionID)

	g.store.Sync(sessionID)

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

		resultData, _ := json.Marshal(result)
		fmt.Fprintf(w, "event: result\ndata: %s\n\n", resultData)
		flusher.Flush()
	}

	_ = time.Since(totalStart)

	g.touchLastTaskTime(sessionID)

	g.store.Sync(sessionID)
}
