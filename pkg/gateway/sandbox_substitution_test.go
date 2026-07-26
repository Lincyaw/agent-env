package gateway

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// substitutingAllocator resolves a claim to a different sandbox than the one
// the session was created on, which is what the controller does after the
// original sandbox is destroyed: the claim rebinds to a fresh warm sandbox.
type substitutingAllocator struct {
	sandboxName string
	podIP       string
	podName     string
}

func (a *substitutingAllocator) Start(context.Context) error { return nil }
func (a *substitutingAllocator) Stop()                       {}
func (a *substitutingAllocator) Allocate(context.Context, RuntimeAllocateRequest) (*RuntimeAllocation, error) {
	return &RuntimeAllocation{}, nil
}
func (a *substitutingAllocator) Release(context.Context, RuntimeAllocation) error { return nil }
func (a *substitutingAllocator) Resolve(_ context.Context, alloc RuntimeAllocation, _ string) (*RuntimeAllocation, error) {
	alloc.SandboxName = a.sandboxName
	alloc.PodIP = a.podIP
	alloc.PodName = a.podName
	return &alloc, nil
}
func (a *substitutingAllocator) Touch(context.Context, RuntimeAllocation, string, time.Time, RuntimeLifecycle) error {
	return nil
}
func (a *substitutingAllocator) DiagnosticStats() map[string]AllocatorPoolStats { return nil }

func sessionOnSandbox(name, podIP, podName string) *session {
	s := &session{}
	s.Info.SandboxName = name
	s.Info.PodIP = podIP
	s.Info.PodName = podName
	s.Runtime = RuntimeAllocation{SandboxName: name, PodIP: podIP, PodName: podName}
	return s
}

// Destroying a sandbox used to be invisible: the claim rebound, the session
// silently pointed at an empty replacement, and exec kept succeeding against a
// workspace that no longer held any of its state.
func TestSessionIsDroppedWhenItsSandboxIsReplaced(t *testing.T) {
	store := NewMemoryStore()
	s := sessionOnSandbox("sandbox-old", "10.0.0.1", "pod-old")
	store.Set("sess-1", s)

	gw := &Gateway{
		store: store,
		runtimeAllocator: &substitutingAllocator{
			sandboxName: "sandbox-new", podIP: "10.0.0.2", podName: "pod-new",
		},
	}

	_, _, err := gw.resolveSessionPodIP(context.Background(), "sess-1")
	if err == nil {
		t.Fatal("a replaced sandbox must not be served as if nothing happened")
	}
	if !strings.Contains(err.Error(), "lost its sandbox") {
		t.Errorf("error should say the sandbox was lost, got: %v", err)
	}

	// The session must be gone, not left pointing at the replacement.
	if _, ok := store.Get("sess-1"); ok {
		t.Error("the session outlived the sandbox holding its state")
	}
}

func TestSessionFollowsItsOwnSandboxToANewPodIP(t *testing.T) {
	// Same sandbox, new pod IP: the state is still there, so this is an
	// ordinary re-resolve and must not end the session.
	store := NewMemoryStore()
	s := sessionOnSandbox("sandbox-1", "10.0.0.1", "pod-1")
	store.Set("sess-1", s)

	gw := &Gateway{
		store: store,
		runtimeAllocator: &substitutingAllocator{
			sandboxName: "sandbox-1", podIP: "10.0.0.9", podName: "pod-1",
		},
		executorClient: nil,
	}

	_, podIP, err := gw.resolveSessionPodIP(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("same sandbox on a new IP should resolve: %v", err)
	}
	if podIP != "10.0.0.9" {
		t.Errorf("podIP = %q, want the new address", podIP)
	}
	if _, ok := store.Get("sess-1"); !ok {
		t.Error("the session should have survived")
	}
}

func TestUnknownSandboxNameDoesNotTriggerADrop(t *testing.T) {
	// Older sessions may carry no sandbox name. Absent evidence of a
	// substitution, keep the previous behaviour rather than dropping them.
	store := NewMemoryStore()
	s := sessionOnSandbox("", "10.0.0.1", "pod-1")
	store.Set("sess-1", s)

	gw := &Gateway{
		store: store,
		runtimeAllocator: &substitutingAllocator{
			sandboxName: "sandbox-new", podIP: "10.0.0.2", podName: "pod-new",
		},
	}

	if _, _, err := gw.resolveSessionPodIP(context.Background(), "sess-1"); err != nil {
		t.Errorf("unknown prior sandbox should not be treated as a loss: %v", err)
	}
	if _, ok := store.Get("sess-1"); !ok {
		t.Error("session should have survived")
	}
	_ = atomic.LoadInt32(&s.activeExecs)
}
