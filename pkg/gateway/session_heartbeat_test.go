package gateway

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// stubAllocator resolves to a fixed pod so the busy-marking contract can be
// exercised without a cluster.
type stubAllocator struct {
	touches int32
}

func (a *stubAllocator) Start(context.Context) error { return nil }
func (a *stubAllocator) Stop()                       {}
func (a *stubAllocator) Allocate(context.Context, RuntimeAllocateRequest) (*RuntimeAllocation, error) {
	return &RuntimeAllocation{PodIP: "10.0.0.1", PodName: "pod-1"}, nil
}
func (a *stubAllocator) Release(context.Context, RuntimeAllocation) error { return nil }
func (a *stubAllocator) Resolve(_ context.Context, alloc RuntimeAllocation, _ string) (*RuntimeAllocation, error) {
	alloc.PodIP = "10.0.0.1"
	alloc.PodName = "pod-1"
	return &alloc, nil
}
func (a *stubAllocator) Touch(context.Context, RuntimeAllocation, string, time.Time, RuntimeLifecycle) error {
	atomic.AddInt32(&a.touches, 1)
	return nil
}
func (a *stubAllocator) DiagnosticStats() map[string]AllocatorPoolStats { return nil }

// The claim's ShutdownTime sits idleTimeout in the future and the
// agent-sandbox controller deletes the sandbox once it passes. How many
// refreshes may be lost before that happens is decided here.

func TestRuntimePatchIntervalLeavesRoomForLostRefreshes(t *testing.T) {
	for _, idleTimeout := range []time.Duration{
		600 * time.Second,
		30 * time.Minute,
		2 * time.Hour,
	} {
		interval := runtimePatchInterval(idleTimeout)
		if interval <= 0 {
			t.Fatalf("idleTimeout %s produced interval %s", idleTimeout, interval)
		}
		// At interval == idleTimeout/2 a single missed refresh puts the
		// deadline exactly at the next attempt, which is how a busy session
		// got deleted mid-exec. Require slack for at least two misses.
		if got := idleTimeout / interval; got < 3 {
			t.Errorf("idleTimeout %s: only %d refresh attempts before the deadline (interval=%s), want >= 3",
				idleTimeout, got, interval)
		}
	}
}

func TestRuntimePatchIntervalStaysWithinBounds(t *testing.T) {
	if got := runtimePatchInterval(0); got != runtimePatchMaxInterval {
		t.Errorf("no idle timeout: interval = %s, want the max %s", got, runtimePatchMaxInterval)
	}
	if got := runtimePatchInterval(time.Second); got != runtimePatchMinInterval {
		t.Errorf("tiny idle timeout: interval = %s, want the floor %s", got, runtimePatchMinInterval)
	}
	if got := runtimePatchInterval(100 * time.Hour); got != runtimePatchMaxInterval {
		t.Errorf("huge idle timeout: interval = %s, want the ceiling %s", got, runtimePatchMaxInterval)
	}
}

// The heartbeat ticker fires every runtimePatchInterval. Feeding those ticks
// through a throttle of the same length compares an interval against itself,
// so any scheduling jitter that makes the elapsed time land a hair short drops
// the refresh entirely.

func TestHeartbeatRefreshSurvivesTickJitter(t *testing.T) {
	store := NewMemoryStore()
	s := &session{idleTimeout: 600 * time.Second}
	s.Info.PodIP = "10.0.0.1"
	store.Set("sess-1", s)
	alloc := &stubAllocator{}
	gw := &Gateway{store: store, runtimeAllocator: alloc}

	interval := runtimePatchInterval(s.idleTimeout)

	// Model a tick landing a hair early relative to the previous patch.
	s.mu.Lock()
	s.lastAnnotationPatch = time.Now().Add(-interval + time.Millisecond)
	s.mu.Unlock()

	gw.touchSessionActivity("sess-1", true)

	s.mu.RLock()
	patchedAt := s.lastAnnotationPatch
	s.mu.RUnlock()
	if time.Since(patchedAt) > time.Second {
		t.Error("a forced refresh was dropped; the runtime lifetime would stop being extended")
	}
}

func TestRequestTouchStaysThrottled(t *testing.T) {
	store := NewMemoryStore()
	s := &session{idleTimeout: 600 * time.Second}
	s.Info.PodIP = "10.0.0.1"
	store.Set("sess-1", s)
	gw := &Gateway{store: store, runtimeAllocator: &stubAllocator{}}

	// Requests can arrive far faster than the lifetime needs extending, so the
	// unforced path must not patch the claim on every one.
	recent := time.Now()
	s.mu.Lock()
	s.lastAnnotationPatch = recent
	s.mu.Unlock()

	gw.touchLastTaskTime("sess-1")

	s.mu.RLock()
	patchedAt := s.lastAnnotationPatch
	lastTask := s.lastTaskTime
	s.mu.RUnlock()
	if !patchedAt.Equal(recent) {
		t.Error("request touch patched the claim despite a recent patch")
	}
	// In-memory activity must still advance, or the idle sweep would reclaim
	// a session that is serving requests.
	if lastTask.Before(recent) {
		t.Error("request touch did not record activity")
	}
}

func TestHeartbeatIsInertWithoutAnIdleTimeout(t *testing.T) {
	// No idle timeout means no ShutdownTime on the claim, so there is nothing
	// to keep extending.
	store := NewMemoryStore()
	s := &session{}
	store.Set("sess-1", s)
	gw := &Gateway{store: store}

	stop := gw.startSessionHeartbeat("sess-1", s)
	if stop == nil {
		t.Fatal("expected a stop function")
	}
	stop()
}
