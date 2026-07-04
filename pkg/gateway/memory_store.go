package gateway

import (
	"sync"
	"sync/atomic"
)

// MemoryStore is an in-memory SessionStore backed by sync.Map.
// This is the default store for single-replica deployments and testing.
type MemoryStore struct {
	sessions     sync.Map
	tombstones   sync.Map
	sessionCount atomic.Int64
}

// NewMemoryStore creates a new in-memory session store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (ms *MemoryStore) Get(sessionID string) (*session, bool) {
	val, ok := ms.sessions.Load(sessionID)
	if !ok {
		return nil, false
	}
	return val.(*session), true
}

func (ms *MemoryStore) Set(sessionID string, s *session) {
	ms.sessions.Store(sessionID, s)
}

func (ms *MemoryStore) Delete(sessionID string) {
	if val, ok := ms.sessions.Load(sessionID); ok {
		ms.tombstones.Store(sessionID, val.(*session))
	}
	ms.sessions.Delete(sessionID)
}

func (ms *MemoryStore) Range(fn func(sessionID string, s *session) bool) {
	ms.sessions.Range(func(key, value any) bool {
		return fn(key.(string), value.(*session))
	})
}

func (ms *MemoryStore) Count() int64 {
	return ms.sessionCount.Load()
}

func (ms *MemoryStore) IncrCount(delta int64) int64 {
	count := ms.sessionCount.Add(delta)
	if count >= 0 {
		return count
	}
	ms.sessionCount.Store(0)
	return 0
}

func (ms *MemoryStore) SetCount(count int64) int64 {
	if count < 0 {
		count = 0
	}
	ms.sessionCount.Store(count)
	return count
}

func (ms *MemoryStore) Sync(_ string) {}

func (ms *MemoryStore) FindByExperiment(_ string) []string { return nil }

func (ms *MemoryStore) Close() error {
	return nil
}

// GetHistorical retrieves a session snapshot even after Delete tombstoned it.
func (ms *MemoryStore) GetHistorical(sessionID string) (*session, bool) {
	if val, ok := ms.sessions.Load(sessionID); ok {
		return val.(*session), true
	}
	if val, ok := ms.tombstones.Load(sessionID); ok {
		return val.(*session), true
	}
	return nil, false
}
