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
	indexMu      sync.RWMutex
	experiments  map[string]map[string]struct{}
	profiles     map[string]map[string]struct{}
	statuses     map[string]map[string]struct{}
}

// NewMemoryStore creates a new in-memory session store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		experiments: make(map[string]map[string]struct{}),
		profiles:    make(map[string]map[string]struct{}),
		statuses:    make(map[string]map[string]struct{}),
	}
}

func (ms *MemoryStore) Get(sessionID string) (*session, bool) {
	val, ok := ms.sessions.Load(sessionID)
	if !ok {
		return nil, false
	}
	return val.(*session), true
}

func (ms *MemoryStore) Set(sessionID string, s *session) {
	if val, ok := ms.sessions.Load(sessionID); ok {
		ms.removeFromIndexes(sessionID, val.(*session))
	}
	ms.sessions.Store(sessionID, s)
	ms.addToIndexes(sessionID, s)
}

func (ms *MemoryStore) Delete(sessionID string) {
	if val, ok := ms.sessions.Load(sessionID); ok {
		s := val.(*session)
		ms.tombstones.Store(sessionID, s)
		ms.removeFromIndexes(sessionID, s)
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

func (ms *MemoryStore) FindByExperiment(experimentID string) []string {
	return ms.idsFromIndex(ms.experiments, experimentID)
}

func (ms *MemoryStore) FindByProfile(profile string) []string {
	return ms.idsFromIndex(ms.profiles, profile)
}

func (ms *MemoryStore) FindByStatus(status string) []string {
	return ms.idsFromIndex(ms.statuses, status)
}

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

func (ms *MemoryStore) addToIndexes(sessionID string, s *session) {
	if s == nil {
		return
	}
	s.mu.RLock()
	experimentID := s.experimentID
	profile := s.Info.Profile
	status := s.Info.Status
	s.mu.RUnlock()
	if status == "" {
		status = "active"
	}

	ms.indexMu.Lock()
	defer ms.indexMu.Unlock()
	ms.ensureIndexesLocked()
	addSessionIndex(ms.experiments, experimentID, sessionID)
	addSessionIndex(ms.profiles, profile, sessionID)
	addSessionIndex(ms.statuses, status, sessionID)
}

func (ms *MemoryStore) removeFromIndexes(sessionID string, s *session) {
	if s == nil {
		return
	}
	s.mu.RLock()
	experimentID := s.experimentID
	profile := s.Info.Profile
	status := s.Info.Status
	s.mu.RUnlock()
	if status == "" {
		status = "active"
	}

	ms.indexMu.Lock()
	defer ms.indexMu.Unlock()
	ms.ensureIndexesLocked()
	removeSessionIndex(ms.experiments, experimentID, sessionID)
	removeSessionIndex(ms.profiles, profile, sessionID)
	removeSessionIndex(ms.statuses, status, sessionID)
}

func (ms *MemoryStore) idsFromIndex(index map[string]map[string]struct{}, value string) []string {
	ms.indexMu.RLock()
	defer ms.indexMu.RUnlock()
	ids := index[value]
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out
}

func (ms *MemoryStore) ensureIndexesLocked() {
	if ms.experiments == nil {
		ms.experiments = make(map[string]map[string]struct{})
	}
	if ms.profiles == nil {
		ms.profiles = make(map[string]map[string]struct{})
	}
	if ms.statuses == nil {
		ms.statuses = make(map[string]map[string]struct{})
	}
}

func addSessionIndex(index map[string]map[string]struct{}, value, sessionID string) {
	if value == "" {
		return
	}
	ids := index[value]
	if ids == nil {
		ids = make(map[string]struct{})
		index[value] = ids
	}
	ids[sessionID] = struct{}{}
}

func removeSessionIndex(index map[string]map[string]struct{}, value, sessionID string) {
	if value == "" {
		return
	}
	ids := index[value]
	if ids == nil {
		return
	}
	delete(ids, sessionID)
	if len(ids) == 0 {
		delete(index, value)
	}
}
