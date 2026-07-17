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
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

func TestExecuteStepsOperationIsIdempotent(t *testing.T) {
	store := NewMemoryStore()
	sessionID := "gw-op"
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

	var executeCalls atomic.Int32
	sidecarClient := &mockclient.MockSidecarClient{
		ExecuteFunc: func(ctx context.Context, podIP string, req interfaces.ExecRequest) (interfaces.ExecResponse, error) {
			executeCalls.Add(1)
			return &sidecar.ExecLog{Stdout: "ok\n", Stderr: "", ExitCode: 0, Done: true}, nil
		},
	}
	gw := New(nil, &operationRuntimeAllocator{}, sidecarClient, nil, nil, GatewayConfig{}, store)
	req := ExecuteRequest{
		OperationID: "op-1",
		Steps: []StepRequest{{
			Name:    "echo",
			Command: []string{"echo", "ok"},
		}},
	}

	first, err := gw.ExecuteSteps(context.Background(), sessionID, req)
	if err != nil {
		t.Fatalf("first ExecuteSteps returned error: %v", err)
	}
	second, err := gw.ExecuteSteps(context.Background(), sessionID, req)
	if err != nil {
		t.Fatalf("second ExecuteSteps returned error: %v", err)
	}
	if executeCalls.Load() != 1 {
		t.Fatalf("sidecar execute calls = %d, want 1", executeCalls.Load())
	}
	if first.Results[0].Output.Stdout != second.Results[0].Output.Stdout {
		t.Fatalf("second operation did not reuse first result: first=%#v second=%#v", first, second)
	}

	info, err := gw.OperationStatus(sessionID, "op-1")
	if err != nil {
		t.Fatalf("OperationStatus returned error: %v", err)
	}
	if info.Status != executeOperationDone || info.Result == nil {
		t.Fatalf("operation info = %#v, want done with result", info)
	}
}

func TestExecuteStepsOperationReturnsFullOutput(t *testing.T) {
	store := NewMemoryStore()
	sessionID := "gw-op-full"
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

	sidecarClient := &mockclient.MockSidecarClient{
		ExecuteFunc: func(ctx context.Context, podIP string, req interfaces.ExecRequest) (interfaces.ExecResponse, error) {
			return &sidecar.ExecLog{Stdout: "abcdef", Stderr: "UVWXYZ", ExitCode: 0, Done: true}, nil
		},
	}
	gw := New(nil, &operationRuntimeAllocator{}, sidecarClient, nil, nil, GatewayConfig{ObservationPreviewBytes: 4}, store)

	resp, err := gw.ExecuteSteps(context.Background(), sessionID, ExecuteRequest{
		OperationID: "op-full",
		Steps: []StepRequest{{
			Name:    "echo",
			Command: []string{"echo", "ok"},
		}},
	})
	if err != nil {
		t.Fatalf("ExecuteSteps returned error: %v", err)
	}
	if got := resp.Results[0].Output.Stdout; got != "abcdef" {
		t.Fatalf("operation response stdout = %q, want full output", got)
	}
	if got := resp.Results[0].Output.Stderr; got != "UVWXYZ" {
		t.Fatalf("operation response stderr = %q, want full output", got)
	}

	info, err := gw.OperationStatus(sessionID, "op-full")
	if err != nil {
		t.Fatalf("OperationStatus returned error: %v", err)
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
	store := NewMemoryStore()
	sessionID := "gw-preview"
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

	sidecarClient := &mockclient.MockSidecarClient{
		ExecuteFunc: func(ctx context.Context, podIP string, req interfaces.ExecRequest) (interfaces.ExecResponse, error) {
			return &sidecar.ExecLog{Stdout: "abcdef", Stderr: "UVWXYZ", ExitCode: 0, Done: true}, nil
		},
	}
	gw := New(nil, &operationRuntimeAllocator{}, sidecarClient, nil, nil, GatewayConfig{ObservationPreviewBytes: 4}, store)

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
