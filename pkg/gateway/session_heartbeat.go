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

func (g *Gateway) runtimeLifecycle(createdAt, lastActivityAt time.Time, idleTimeout, maxLifetime time.Duration) RuntimeLifecycle {
	return RuntimeLifecycle{
		CreatedAt:      createdAt,
		LastActivityAt: lastActivityAt,
		IdleTimeout:    idleTimeout,
		MaxLifetime:    maxLifetime,
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
	return g.runtimeLifecycle(createdAt, at, s.idleTimeout, s.maxLifetime)
}

func runtimePatchInterval(idleTimeout time.Duration) time.Duration {
	if idleTimeout <= 0 {
		return runtimePatchMaxInterval
	}
	interval := idleTimeout / 2
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
				g.touchLastTaskTime(sessionID)
			}
		}
	}()
	return func() {
		once.Do(func() { close(stop) })
	}
}

// touchLastTaskTime updates the in-memory lastTaskTime for session idle tracking
// and asynchronously patches runtime last-activity annotations (throttled to at most once per 30s).
func (g *Gateway) touchLastTaskTime(sessionID string) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return
	}
	now := time.Now()

	s.mu.Lock()
	s.lastTaskTime = now
	lifecycle := g.sessionRuntimeLifecycleLocked(s, now)
	shouldPatch := now.Sub(s.lastAnnotationPatch) >= runtimePatchInterval(s.idleTimeout)
	if shouldPatch {
		s.lastAnnotationPatch = now
	}
	allocation := s.runtimeAllocation()
	s.mu.Unlock()

	if rs, ok := g.store.(*RedisStore); ok {
		rs.Sync(sessionID)
	}

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
