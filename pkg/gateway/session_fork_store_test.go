package gateway

import (
	"strings"
	"testing"
)

// A gateway with SANDBOX_CHECKPOINT_ENABLED but no CHECKPOINT_STORE_PATH takes
// checkpoints onto the sandbox's own disk only. Forking a source that has
// stopped is then impossible, and the old message blamed the source for being
// missing instead of naming the configuration.

func TestForkWithoutCheckpointStoreNamesTheConfiguration(t *testing.T) {
	err := errNoCheckpointStore("sess-1")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()

	for _, want := range []string{"sess-1", "CHECKPOINT_STORE_PATH", "checkpoint.enabled"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should mention %q, got: %s", want, msg)
		}
	}
	// The source may well still be on record; blaming its absence sent the
	// last investigation down the wrong path.
	if strings.Contains(msg, "not found") {
		t.Errorf("error should not claim the source is missing: %s", msg)
	}
}

func TestForkRejectsDeletedSourceWithoutStore(t *testing.T) {
	store := NewMemoryStore()
	gw := &Gateway{
		store:    store,
		gwConfig: GatewayConfig{SandboxCheckpointEnabled: true},
	}

	_, err := gw.ForkSession(t.Context(), "gone", ForkSessionRequest{Step: 0})
	if err == nil {
		t.Fatal("expected forking an absent source to fail")
	}
	if !strings.Contains(err.Error(), "CHECKPOINT_STORE_PATH") {
		t.Errorf("expected the configuration to be named, got: %v", err)
	}
}

func TestForkRejectsClosedSourceWithoutStore(t *testing.T) {
	store := NewMemoryStore()
	s := &session{closed: true}
	s.Info.Image = "img:1"
	store.Set("closed-1", s)
	gw := &Gateway{
		store:    store,
		gwConfig: GatewayConfig{SandboxCheckpointEnabled: true},
	}

	_, err := gw.ForkSession(t.Context(), "closed-1", ForkSessionRequest{Step: 0})
	if err == nil {
		t.Fatal("expected forking a closed source to fail")
	}
	// This source *is* on record, so "not found" would be actively wrong.
	if !strings.Contains(err.Error(), "CHECKPOINT_STORE_PATH") {
		t.Errorf("expected the configuration to be named, got: %v", err)
	}
}

func TestForkRequiresCheckpointsEnabledAtAll(t *testing.T) {
	gw := &Gateway{store: NewMemoryStore()}

	_, err := gw.ForkSession(t.Context(), "any", ForkSessionRequest{Step: 0})
	if err == nil || !strings.Contains(err.Error(), "checkpoint not enabled") {
		t.Errorf("expected the feature-disabled error, got: %v", err)
	}
}
