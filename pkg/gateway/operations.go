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

// OperationPending is returned when an async operation has been accepted
// but the HTTP client context expired before it finished. The handler
// should respond with 202 Accepted + the operationID.
type OperationPending struct {
	OperationID string
	SessionID   string
}

func (e *OperationPending) Error() string {
	return fmt.Sprintf("operation %s pending", e.OperationID)
}

type operation struct {
	id          string
	sessionID   string
	requestHash string
	createdAt   time.Time
	startedAt   time.Time
	finishedAt  *time.Time
	done        chan struct{}
	result      any
	resultJSON  json.RawMessage // cached marshal of result, set once on completion
	err         error
}

func executeRequestHash(req ExecuteRequest) string {
	cp := req
	cp.OperationID = ""
	raw, _ := json.Marshal(cp)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (g *Gateway) OperationStatus(sessionID, operationID string) (*ExecuteOperationInfo, error) {
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

func (op *operation) info() *ExecuteOperationInfo {
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
		Result:      op.resultJSON,
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

	hash := executeRequestHash(req)
	op, started, err := g.getOrStartOperation(sessionID, req.OperationID, hash, func(bgCtx context.Context) (any, error) {
		resp, err := g.executeStepsNow(bgCtx, sessionID, req)
		if resp != nil {
			resp.OperationID = req.OperationID
		}
		return resp, err
	})
	if err != nil {
		return nil, err
	}

	select {
	case <-op.done:
	case <-ctx.Done():
		if g.metrics != nil {
			g.metrics.IncrementExecuteOperationResult("client_disconnected")
		}
		return nil, &OperationPending{OperationID: req.OperationID, SessionID: sessionID}
	}

	if !started && g.metrics != nil {
		g.metrics.IncrementExecuteOperationResult("reused")
	}
	if op.err != nil {
		return nil, op.err
	}
	resp, _ := op.result.(*ExecuteResponse)
	return resp, nil
}

func (g *Gateway) getOrStartOperation(sessionID, operationID, requestHash string, workFn func(context.Context) (any, error)) (*operation, bool, error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, false, fmt.Errorf("session %s not found", sessionID)
	}
	now := time.Now()

	s.mu.Lock()
	if s.operations == nil {
		s.operations = make(map[string]*operation)
	}
	if op := s.operations[operationID]; op != nil {
		s.mu.Unlock()
		if op.requestHash != requestHash {
			return nil, false, fmt.Errorf("operation %s already exists with different request", operationID)
		}
		return op, false, nil
	}
	op := &operation{
		id:          operationID,
		sessionID:   sessionID,
		requestHash: requestHash,
		createdAt:   now,
		startedAt:   now,
		done:        make(chan struct{}),
	}
	s.operations[operationID] = op
	s.mu.Unlock()

	go func() {
		defer close(op.done)
		bgCtx := context.Background()
		result, err := workFn(bgCtx)
		finished := time.Now()
		op.result = result
		if result != nil {
			op.resultJSON, _ = json.Marshal(result)
		}
		op.err = err
		op.finishedAt = &finished
		if g.metrics != nil {
			mResult := "success"
			if err != nil {
				mResult = "error"
			}
			g.metrics.IncrementExecuteOperationResult(mResult)
		}

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
