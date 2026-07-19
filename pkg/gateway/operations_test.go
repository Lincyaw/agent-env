package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	mockclient "github.com/Lincyaw/agent-env/pkg/client"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

func newTestSessionStore(sessionID string) *MemoryStore {
	store := NewMemoryStore()
	store.Set(sessionID, &session{
		Info: SessionInfo{
			ID:        sessionID,
			Namespace: "default",
			PoolRef:   "code",
			PodIP:     "10.0.0.1",
			PodName:   "pod-1",
			Status:    "active",
		},
		Runtime: RuntimeAllocation{
			Backend:   runtimeBackendSandboxClaim,
			PoolRef:   "code",
			Namespace: "default",
			ClaimName: "claim-1",
			PodIP:     "10.0.0.1",
			PodName:   "pod-1",
		},
		History:      NewStepHistory(),
		lastTaskTime: time.Now(),
		createdAt:    time.Now(),
		operations:   make(map[string]*operation),
	})
	store.IncrCount(1)
	return store
}

// waitForOperation polls until the operation completes.
func waitForOperation(gw *Gateway, sessionID, opID string) (*ExecuteOperationInfo, error) {
	for i := 0; i < 100; i++ {
		info, err := gw.OperationStatus(sessionID, opID)
		if err != nil {
			return nil, err
		}
		if info.Status == executeOperationDone {
			return info, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, fmt.Errorf("operation %s did not complete", opID)
}

func TestExecuteStepsOperationIsIdempotent(t *testing.T) {
	store := newTestSessionStore("gw-op")
	sessionID := "gw-op"

	var executeCalls atomic.Int32
	executorClient := &mockclient.MockExecutorClient{
		ExecuteFunc: func(ctx context.Context, podIP string, req *interfaces.ExecRequest) (*interfaces.ExecResponse, error) {
			executeCalls.Add(1)
			return &interfaces.ExecResponse{Stdout: "ok\n", Stderr: "", ExitCode: 0, Done: true}, nil
		},
	}
	gw := New(nil, &operationRuntimeAllocator{}, executorClient, nil, nil, GatewayConfig{}, store)
	req := ExecuteRequest{
		OperationID: "op-1",
		Steps: []StepRequest{{
			Name:    "echo",
			Command: []string{"echo", "ok"},
		}},
	}

	// First call returns OperationPending (202) since operation is started async.
	_, err := gw.ExecuteSteps(context.Background(), sessionID, req)
	if err == nil {
		t.Fatal("expected OperationPending error from first call")
	}
	var pending *OperationPending
	if !isOperationPending(err, &pending) {
		t.Fatalf("expected OperationPending, got: %v", err)
	}

	// Wait for the background operation to finish.
	info, err := waitForOperation(gw, sessionID, "op-1")
	if err != nil {
		t.Fatalf("waitForOperation: %v", err)
	}
	if info.Status != executeOperationDone || info.Result == nil {
		t.Fatalf("operation info = %#v, want done with result", info)
	}

	// Second call with same operationID returns cached result directly.
	second, err := gw.ExecuteSteps(context.Background(), sessionID, req)
	if err != nil {
		t.Fatalf("second ExecuteSteps returned error: %v", err)
	}
	if second.Results[0].Output.Stdout != "ok\n" {
		t.Fatalf("second call stdout = %q, want %q", second.Results[0].Output.Stdout, "ok\n")
	}
	if executeCalls.Load() != 1 {
		t.Fatalf("executor execute calls = %d, want 1", executeCalls.Load())
	}
}

func isOperationPending(err error, target **OperationPending) bool {
	if p, ok := err.(*OperationPending); ok {
		*target = p
		return true
	}
	return false
}

func TestExecuteStepsOperationReturnsFullOutput(t *testing.T) {
	store := newTestSessionStore("gw-op-full")
	sessionID := "gw-op-full"

	executorClient := &mockclient.MockExecutorClient{
		ExecuteFunc: func(ctx context.Context, podIP string, req *interfaces.ExecRequest) (*interfaces.ExecResponse, error) {
			return &interfaces.ExecResponse{Stdout: "abcdef", Stderr: "UVWXYZ", ExitCode: 0, Done: true}, nil
		},
	}
	gw := New(nil, &operationRuntimeAllocator{}, executorClient, nil, nil, GatewayConfig{ObservationPreviewBytes: 4}, store)

	_, err := gw.ExecuteSteps(context.Background(), sessionID, ExecuteRequest{
		OperationID: "op-full",
		Steps: []StepRequest{{
			Name:    "echo",
			Command: []string{"echo", "ok"},
		}},
	})
	if err == nil {
		t.Fatal("expected OperationPending")
	}

	info, err := waitForOperation(gw, sessionID, "op-full")
	if err != nil {
		t.Fatalf("waitForOperation: %v", err)
	}
	if info.Result == nil {
		t.Fatalf("operation info result is nil, want result")
	}
	var polledResult ExecuteResponse
	if err := json.Unmarshal(info.Result, &polledResult); err != nil {
		t.Fatalf("failed to unmarshal operation result: %v", err)
	}
	if len(polledResult.Results) != 1 {
		t.Fatalf("operation info result count = %d, want 1", len(polledResult.Results))
	}
	if got := polledResult.Results[0].Output.Stdout; got != "abcdef" {
		t.Fatalf("polled operation stdout = %q, want full output", got)
	}
	if got := polledResult.Results[0].Output.Stderr; got != "UVWXYZ" {
		t.Fatalf("polled operation stderr = %q, want full output", got)
	}

	s, ok := store.Get(sessionID)
	if !ok {
		t.Fatal("session missing after execute")
	}
	records := s.History.GetAll()
	if len(records) != 1 {
		t.Fatalf("history records = %d, want 1", len(records))
	}
	if !records[0].OutputTruncated {
		t.Fatal("history output was not marked truncated")
	}
	if got := len(records[0].Output.Stdout) + len(records[0].Output.Stderr); got != 4 {
		t.Fatalf("stored history output bytes = %d, want preview length 4", got)
	}
}

func TestExecuteStepsStoresObservationPreviewByDefault(t *testing.T) {
	store := newTestSessionStore("gw-preview")
	sessionID := "gw-preview"

	executorClient := &mockclient.MockExecutorClient{
		ExecuteFunc: func(ctx context.Context, podIP string, req *interfaces.ExecRequest) (*interfaces.ExecResponse, error) {
			return &interfaces.ExecResponse{Stdout: "abcdef", Stderr: "UVWXYZ", ExitCode: 0, Done: true}, nil
		},
	}
	gw := New(nil, &operationRuntimeAllocator{}, executorClient, nil, nil, GatewayConfig{ObservationPreviewBytes: 4}, store)

	resp, err := gw.ExecuteSteps(context.Background(), sessionID, ExecuteRequest{
		Steps: []StepRequest{{
			Name:    "echo",
			Command: []string{"echo", "ok"},
		}},
	})
	if err != nil {
		t.Fatalf("ExecuteSteps returned error: %v", err)
	}
	if got := resp.Results[0].Output.Stdout; got != "abcdef" {
		t.Fatalf("response stdout = %q, want full stdout", got)
	}

	s, ok := store.Get(sessionID)
	if !ok {
		t.Fatal("session missing after execute")
	}
	records := s.History.GetAll()
	if len(records) != 1 {
		t.Fatalf("history records = %d, want 1", len(records))
	}
	if !records[0].OutputTruncated {
		t.Fatal("history output was not marked truncated")
	}
	if records[0].OutputBytes != len("abcdef")+len("UVWXYZ") {
		t.Fatalf("OutputBytes = %d, want 12", records[0].OutputBytes)
	}
	if got := len(records[0].Output.Stdout) + len(records[0].Output.Stderr); got != 4 {
		t.Fatalf("stored output bytes = %d, want preview length 4", got)
	}
}

type operationRuntimeAllocator struct{}

func (a *operationRuntimeAllocator) Start(ctx context.Context) error { return nil }
func (a *operationRuntimeAllocator) Stop()                           {}

func (a *operationRuntimeAllocator) Allocate(ctx context.Context, req RuntimeAllocateRequest) (*RuntimeAllocation, error) {
	return nil, fmt.Errorf("unexpected Allocate")
}

func (a *operationRuntimeAllocator) Release(ctx context.Context, allocation RuntimeAllocation) error {
	return nil
}

func (a *operationRuntimeAllocator) Resolve(ctx context.Context, allocation RuntimeAllocation, sessionID string) (*RuntimeAllocation, error) {
	return &allocation, nil
}

func (a *operationRuntimeAllocator) Touch(ctx context.Context, allocation RuntimeAllocation, sessionID string, at time.Time, lifecycle RuntimeLifecycle) error {
	return nil
}

func (a *operationRuntimeAllocator) DiagnosticStats() map[string]AllocatorPoolStats {
	return nil
}
