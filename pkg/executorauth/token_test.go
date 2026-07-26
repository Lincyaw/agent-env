package executorauth

import "testing"

func TestDeriveIsPerSandbox(t *testing.T) {
	const secret = "master-secret"

	a := Derive(secret, "10.0.0.1")
	b := Derive(secret, "10.0.0.2")

	if a == "" || b == "" {
		t.Fatal("tokens must be non-empty for a configured secret")
	}
	// This is the whole point: an attacker who reads their own sandbox's token
	// must not thereby hold another sandbox's token.
	if a == b {
		t.Errorf("different sandboxes share a token: %q", a)
	}
}

func TestDeriveIsDeterministic(t *testing.T) {
	// Any gateway replica, and the same replica after a restart, must derive
	// the same token or it would fail to reach already-claimed sandboxes.
	if Derive("s", "10.0.0.1") != Derive("s", "10.0.0.1") {
		t.Error("derivation is not stable")
	}
}

func TestDeriveDependsOnMasterSecret(t *testing.T) {
	if Derive("secret-one", "10.0.0.1") == Derive("secret-two", "10.0.0.1") {
		t.Error("token does not depend on the master secret")
	}
}

func TestDeriveDisabledWhenUnconfigured(t *testing.T) {
	if got := Derive("", "10.0.0.1"); got != "" {
		t.Errorf("no secret must yield no token, got %q", got)
	}
	if got := Derive("secret", ""); got != "" {
		t.Errorf("no pod IP must yield no token, got %q", got)
	}
}

func TestDeriveDoesNotLeakTheMasterSecret(t *testing.T) {
	const secret = "master-secret"

	token := Derive(secret, "10.0.0.1")

	// A sandbox holds its own token; it must not be able to read the master
	// secret out of it and mint tokens for other sandboxes.
	if len(token) < 32 {
		t.Errorf("token too short to be a full digest: %q", token)
	}
	for i := 0; i+len(secret) <= len(token); i++ {
		if token[i:i+len(secret)] == secret {
			t.Fatalf("token embeds the master secret: %q", token)
		}
	}
}
