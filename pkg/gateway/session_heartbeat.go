package gateway

import (
	"context"
	"log"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
)

const (
	defaultRuntimeFinishedTTL = 5 * time.Minute
	runtimePatchMaxInterval   = 5 * time.Minute
	runtimePatchMinInterval   = 5 * time.Second
)

func (g *Gateway) runtimeLifecycle(createdAt, lastActivityAt time.Time, idleTimeout time.Duration) RuntimeLifecycle {
	return RuntimeLifecycle{
		CreatedAt:      createdAt,
		LastActivityAt: lastActivityAt,
		IdleTimeout:    idleTimeout,
		FinishedTTL:    defaultRuntimeFinishedTTL,
	}
}

func (g *Gateway) sessionRuntimeLifecycleLocked(s *session, at time.Time) RuntimeLifecycle {
	createdAt := s.createdAt
	if createdAt.IsZero() {
		createdAt = s.Info.CreatedAt
	}
	if createdAt.IsZero() {
		createdAt = at
	}
	return g.runtimeLifecycle(createdAt, at, s.idleTimeout)
}

// runtimePatchInterval is how often a busy session re-extends its runtime
// lifetime. The claim's ShutdownTime sits idleTimeout in the future and the
// agent-sandbox controller deletes the sandbox when it passes, so this cadence
// decides how many refreshes may be lost before a running session is killed
// underneath itself. A quarter leaves three chances; a half left only one.
func runtimePatchInterval(idleTimeout time.Duration) time.Duration {
	if idleTimeout <= 0 {
		return runtimePatchMaxInterval
	}
	interval := idleTimeout / 4
	if interval < runtimePatchMinInterval {
		return runtimePatchMinInterval
	}
	if interval > runtimePatchMaxInterval {
		return runtimePatchMaxInterval
	}
	return interval
}

func (g *Gateway) startSessionHeartbeat(sessionID string, s *session) func() {
	s.mu.RLock()
	idleTimeout := s.idleTimeout
	s.mu.RUnlock()
	if idleTimeout <= 0 {
		return func() {}
	}
	interval := runtimePatchInterval(idleTimeout)
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// Forced: the ticker already paces this. Running it through
				// the throttle as well compares an interval against itself,
				// and loses the tick whenever scheduling jitter makes the
				// elapsed time land a hair short.
				g.touchSessionActivity(sessionID, true)
			}
		}
	}()
	return func() {
		once.Do(func() { close(stop) })
	}
}

// touchLastTaskTime records activity from a request. The runtime annotation
// patch is throttled, since requests can arrive far faster than the runtime
// lifetime needs re-extending.
func (g *Gateway) touchLastTaskTime(sessionID string) {
	g.touchSessionActivity(sessionID, false)
}

// touchSessionActivity updates the in-memory lastTaskTime and, when due,
// re-extends the runtime lifetime. force skips the throttle for callers that
// already control their own cadence.
func (g *Gateway) touchSessionActivity(sessionID string, force bool) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return
	}
	now := time.Now()

	s.mu.Lock()
	s.lastTaskTime = now
	lifecycle := g.sessionRuntimeLifecycleLocked(s, now)
	shouldPatch := force || now.Sub(s.lastAnnotationPatch) >= runtimePatchInterval(s.idleTimeout)
	if shouldPatch {
		s.lastAnnotationPatch = now
	}
	allocation := s.runtimeAllocation()
	s.mu.Unlock()

	g.store.Sync(sessionID)

	if shouldPatch && g.runtimeAllocator != nil {
		go func() {
			bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer bgCancel()

			if err := g.runtimeAllocator.Touch(bgCtx, allocation, sessionID, now, lifecycle); err != nil {
				log.Printf("Warning: failed to patch last-activity for runtime %s: %v", allocation.PodName, err)
				if errors.IsNotFound(err) {
					if current, ok := g.store.Get(sessionID); ok {
						g.dropSession(sessionID, current)
					}
				}
			}
		}()
	}
}
