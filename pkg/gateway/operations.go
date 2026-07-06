package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const (
	executeOperationRunning = "running"
	executeOperationDone    = "done"
	executeOperationError   = "error"
)

type executeOperation struct {
	id          string
	sessionID   string
	requestHash string
	createdAt   time.Time
	startedAt   time.Time
	finishedAt  *time.Time
	done        chan struct{}
	resp        *ExecuteResponse
	err         error
}

func executeRequestHash(req ExecuteRequest) string {
	cp := req
	cp.OperationID = ""
	raw, _ := json.Marshal(cp)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (g *Gateway) ExecuteOperationStatus(sessionID, operationID string) (*ExecuteOperationInfo, error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	s.mu.RLock()
	op := s.operations[operationID]
	s.mu.RUnlock()
	if op == nil {
		return nil, fmt.Errorf("operation %s not found", operationID)
	}
	return op.info(), nil
}

func (op *executeOperation) info() *ExecuteOperationInfo {
	select {
	case <-op.done:
	default:
		return &ExecuteOperationInfo{
			OperationID: op.id,
			SessionID:   op.sessionID,
			Status:      executeOperationRunning,
			CreatedAt:   op.createdAt,
			StartedAt:   op.startedAt,
		}
	}

	status := executeOperationDone
	errText := ""
	if op.err != nil {
		status = executeOperationError
		errText = op.err.Error()
	}
	return &ExecuteOperationInfo{
		OperationID: op.id,
		SessionID:   op.sessionID,
		Status:      status,
		Result:      op.resp,
		Error:       errText,
		CreatedAt:   op.createdAt,
		StartedAt:   op.startedAt,
		FinishedAt:  op.finishedAt,
	}
}

func (g *Gateway) executeStepsWithOperation(ctx context.Context, sessionID string, req ExecuteRequest) (*ExecuteResponse, error) {
	if req.OperationID == "" {
		return g.executeStepsNow(ctx, sessionID, req)
	}

	op, started, err := g.getOrStartExecuteOperation(sessionID, req)
	if err != nil {
		return nil, err
	}

	select {
	case <-op.done:
	case <-ctx.Done():
		if g.metrics != nil {
			g.metrics.IncrementExecuteOperationResult("client_disconnected")
		}
		return nil, fmt.Errorf("operation %s still running after client context closed: %w", req.OperationID, ctx.Err())
	}

	if !started && g.metrics != nil {
		g.metrics.IncrementExecuteOperationResult("reused")
	}
	if op.err != nil {
		return nil, op.err
	}
	return op.resp, nil
}

func (g *Gateway) getOrStartExecuteOperation(sessionID string, req ExecuteRequest) (*executeOperation, bool, error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, false, fmt.Errorf("session %s not found", sessionID)
	}
	hash := executeRequestHash(req)
	now := time.Now()

	s.mu.Lock()
	if s.operations == nil {
		s.operations = make(map[string]*executeOperation)
	}
	if op := s.operations[req.OperationID]; op != nil {
		s.mu.Unlock()
		if op.requestHash != hash {
			return nil, false, fmt.Errorf("operation %s already exists with different execute request", req.OperationID)
		}
		return op, false, nil
	}
	op := &executeOperation{
		id:          req.OperationID,
		sessionID:   sessionID,
		requestHash: hash,
		createdAt:   now,
		startedAt:   now,
		done:        make(chan struct{}),
	}
	s.operations[req.OperationID] = op
	s.mu.Unlock()

	go func() {
		defer close(op.done)
		bgCtx := context.Background()
		resp, err := g.executeStepsNow(bgCtx, sessionID, req)
		finished := time.Now()
		if resp != nil {
			resp.OperationID = req.OperationID
		}
		op.resp = g.cachedExecuteResponse(resp)
		op.err = err
		op.finishedAt = &finished
		if g.metrics != nil {
			result := "success"
			if err != nil {
				result = "error"
			}
			g.metrics.IncrementExecuteOperationResult(result)
		}

		operationID := req.OperationID
		time.AfterFunc(10*time.Minute, func() {
			s, ok := g.store.Get(sessionID)
			if !ok {
				return
			}
			s.mu.Lock()
			delete(s.operations, operationID)
			s.mu.Unlock()
		})
	}()

	return op, true, nil
}

func (g *Gateway) cachedExecuteResponse(resp *ExecuteResponse) *ExecuteResponse {
	if resp == nil {
		return nil
	}
	cached := &ExecuteResponse{
		SessionID:       resp.SessionID,
		TotalDurationMs: resp.TotalDurationMs,
		OperationID:     resp.OperationID,
	}
	if len(resp.Results) == 0 {
		return cached
	}
	cached.Results = make([]StepResult, len(resp.Results))
	for i := range resp.Results {
		result := resp.Results[i]
		result.Output, _, _ = g.retainedStepOutput(result.Output)
		cached.Results[i] = result
	}
	return cached
}
