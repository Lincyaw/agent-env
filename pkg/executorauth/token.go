// Package executorauth derives the per-sandbox tokens that authenticate
// connections to executor agents.
//
// A single shared token would not help: every sandbox would carry it, and every
// sandbox runs untrusted code, so an attacker could read the token from their
// own environment and replay it against someone else's sandbox. Tokens are
// therefore derived per sandbox from a master secret that stays in the gateway
// and is never mounted into a sandbox pod. Compromising one sandbox reveals
// only that sandbox's token.
package executorauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// Derive returns the token for the executor reachable at podIP.
//
// The pod IP identifies the sandbox because only one pod holds an IP at a time.
// Kubernetes does recycle IPs after a pod is deleted, so a sandbox that learned
// its own token could reuse it against a later sandbox that lands on the same
// IP; the executor claims its token on first use, which narrows that to pods
// the gateway has not reached yet.
func Derive(masterSecret, podIP string) string {
	if masterSecret == "" || podIP == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(masterSecret))
	mac.Write([]byte("arl-executor-v1\x00"))
	mac.Write([]byte(podIP))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
