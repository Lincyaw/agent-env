package gateway

import (
	"sync/atomic"
	"testing"
	"time"
)

// A fork used to be built from image/profile/namespace/mode only, so it fell
// back to GATEWAY_IDLE_TIMEOUT no matter how its source was configured. Forks
// of long-lived sessions were then reclaimed mid-run and surfaced as
// "runtime lost".

func TestForkInheritsIdleTimeoutFromSource(t *testing.T) {
	source := &session{idleTimeout: 2 * time.Hour}
	source.Info.Image = "repo/img:tag"
	source.Info.Profile = "big"
	source.Info.Namespace = "arl"
	source.Info.Mode = "sandbox"

	req := forkInheritsFromSession(source).createRequest()

	if req.IdleTimeoutSeconds != int((2 * time.Hour).Seconds()) {
		t.Errorf("IdleTimeoutSeconds = %d, want the source's 7200", req.IdleTimeoutSeconds)
	}
	if req.Image != "repo/img:tag" || req.Profile != "big" {
		t.Errorf("image/profile not carried: %+v", req)
	}
	if req.Namespace != "arl" || req.Mode != "sandbox" {
		t.Errorf("namespace/mode not carried: %+v", req)
	}
}

func TestForkLeavesIdleTimeoutUnsetWhenSourceHasNone(t *testing.T) {
	// Zero must stay zero so resolveIdleTimeout applies the gateway default
	// rather than a literal zero-second timeout.
	source := &session{}

	req := forkInheritsFromSession(source).createRequest()

	if req.IdleTimeoutSeconds != 0 {
		t.Errorf("IdleTimeoutSeconds = %d, want 0 so the default applies", req.IdleTimeoutSeconds)
	}
}

func TestForkInheritsPrivateContainersDeterministically(t *testing.T) {
	source := &session{
		privateContainers: map[string]PrivateContainerSpec{
			"verifier": {Name: "verifier", Image: "verifier:1"},
			"agent":    {Name: "agent", Image: "agent:1"},
			"proxy":    {Name: "proxy", Image: "proxy:1"},
		},
	}

	first := forkInheritsFromSession(source).createRequest()
	second := forkInheritsFromSession(source).createRequest()

	if len(first.PrivateContainers) != 3 {
		t.Fatalf("expected 3 private containers, got %d", len(first.PrivateContainers))
	}
	// Map iteration order is random; a fork must not produce a different pod
	// spec each time it runs.
	for i := range first.PrivateContainers {
		if first.PrivateContainers[i].Name != second.PrivateContainers[i].Name {
			t.Fatalf("ordering is not deterministic: %v vs %v",
				first.PrivateContainers, second.PrivateContainers)
		}
	}
	if first.PrivateContainers[0].Name != "agent" {
		t.Errorf("expected name-sorted order, got %s first", first.PrivateContainers[0].Name)
	}
}

func TestForkWithoutPrivateContainersSendsNone(t *testing.T) {
	req := forkInheritsFromSession(&session{}).createRequest()

	if req.PrivateContainers != nil {
		t.Errorf("expected no private containers, got %+v", req.PrivateContainers)
	}
}

// acquireSessionPodIP is what marks a session busy: it bumps activeExecs so the
// sweep and the claim reaper skip it, and starts the heartbeat that keeps
// extending the controller-enforced ShutdownTime. The fork checkpoint paths
// used to read Info.PodIP directly and bypass all of it.

func TestAcquireSessionPodIPMarksSessionBusy(t *testing.T) {
	store := NewMemoryStore()
	s := &session{idleTimeout: time.Hour}
	s.Info.PodIP = "10.0.0.1"
	s.Info.PodName = "pod-1"
	store.Set("sess-1", s)
	gw := &Gateway{store: store, runtimeAllocator: &stubAllocator{}}

	if got := atomic.LoadInt32(&s.activeExecs); got != 0 {
		t.Fatalf("activeExecs = %d before acquire, want 0", got)
	}

	_, podIP, release, err := gw.acquireSessionPodIP(t.Context(), "sess-1")
	if err != nil {
		t.Fatalf("acquireSessionPodIP: %v", err)
	}
	if podIP != "10.0.0.1" {
		t.Errorf("podIP = %q, want 10.0.0.1", podIP)
	}
	if got := atomic.LoadInt32(&s.activeExecs); got != 1 {
		t.Errorf("activeExecs = %d while held, want 1", got)
	}

	release()
	if got := atomic.LoadInt32(&s.activeExecs); got != 0 {
		t.Errorf("activeExecs = %d after release, want 0", got)
	}

	// Release is called from defer paths that may also run explicitly.
	release()
	if got := atomic.LoadInt32(&s.activeExecs); got != 0 {
		t.Errorf("activeExecs = %d after double release, want 0", got)
	}
}

func TestAcquireSessionPodIPRejectsClosedSession(t *testing.T) {
	store := NewMemoryStore()
	s := &session{closed: true}
	s.Info.PodIP = "10.0.0.1"
	store.Set("sess-1", s)
	gw := &Gateway{store: store}

	if _, _, _, err := gw.acquireSessionPodIP(t.Context(), "sess-1"); err == nil {
		t.Error("expected a closed session to be refused")
	}
	if got := atomic.LoadInt32(&s.activeExecs); got != 0 {
		t.Errorf("activeExecs leaked to %d on the refused path", got)
	}
}
