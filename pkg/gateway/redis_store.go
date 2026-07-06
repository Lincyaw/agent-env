package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisSessionPrefix        = "arl:session:"
	redisSessionActionsPrefix = "arl:session_actions:"
	redisExperimentPrefix     = "arl:experiment:"
	redisProfilePrefix        = "arl:profile:"
	redisStatusPrefix         = "arl:status:"
	redisCountKey             = "arl:session_count"
)

// redisSessionData is the JSON-serializable representation of a session
// stored in Redis. Fields that cannot be serialized (sync.RWMutex, StepHistory)
// are managed separately or reconstructed on load.
type redisSessionData struct {
	Info                SessionInfo            `json:"info"`
	Runtime             RuntimeAllocation      `json:"runtime,omitempty"`
	Managed             bool                   `json:"managed"`
	ExperimentID        string                 `json:"experimentId"`
	Mode                string                 `json:"mode,omitempty"`
	OwnerKeyHash        string                 `json:"ownerKeyHash,omitempty"`
	Deleted             bool                   `json:"deleted,omitempty"`
	DeletedAt           *time.Time             `json:"deletedAt,omitempty"`
	DeletionReason      string                 `json:"deletionReason,omitempty"`
	LastTaskTime        time.Time              `json:"lastTaskTime"`
	LastAnnotationPatch time.Time              `json:"lastAnnotationPatch"`
	IdleTimeout         time.Duration          `json:"idleTimeout"`
	CreatedAt           time.Time              `json:"createdAt"`
	PrivateContainers   []PrivateContainerSpec `json:"privateContainers,omitempty"`

	// Legacy monolithic session keys may still contain history. Recovery reads
	// only replayable action fields and intentionally ignores legacy output.
	HistoryRecords   []redisLegacyStepRecord `json:"historyRecords,omitempty"`
	HistoryNextIndex int                     `json:"historyNextIndex,omitempty"`
}

type redisSessionActionsData struct {
	Records      []StepRecord            `json:"records"`
	ReplayInputs map[int]json.RawMessage `json:"replayInputs,omitempty"`
	NextIndex    int                     `json:"nextIndex"`
}

type redisLegacyStepRecord struct {
	Index      int             `json:"index"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
	SnapshotID string          `json:"snapshot_id"`
	DurationMs int64           `json:"duration_ms"`
	Timestamp  time.Time       `json:"timestamp"`
}

func sessionToRedisData(s *session) redisSessionData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := redisSessionData{
		Info:                s.Info,
		Runtime:             s.Runtime,
		Managed:             s.managed,
		ExperimentID:        s.experimentID,
		Mode:                s.mode,
		OwnerKeyHash:        s.ownerKeyHash,
		Deleted:             s.closed,
		DeletedAt:           s.deletedAt,
		DeletionReason:      s.deletionReason,
		LastTaskTime:        s.lastTaskTime,
		LastAnnotationPatch: s.lastAnnotationPatch,
		IdleTimeout:         s.idleTimeout,
		CreatedAt:           s.createdAt,
	}
	if len(s.privateContainers) > 0 {
		data.PrivateContainers = make([]PrivateContainerSpec, 0, len(s.privateContainers))
		for _, spec := range s.privateContainers {
			data.PrivateContainers = append(data.PrivateContainers, spec)
		}
	}

	return data
}

func sessionActionsToRedisData(s *session) redisSessionActionsData {
	if s.History == nil {
		return redisSessionActionsData{}
	}
	data := redisSessionActionsData{
		Records:      s.History.GetAll(),
		ReplayInputs: make(map[int]json.RawMessage),
	}
	for _, record := range data.Records {
		if len(record.ReplayInput) > 0 {
			data.ReplayInputs[record.Index] = append(json.RawMessage(nil), record.ReplayInput...)
		}
	}
	if len(data.ReplayInputs) == 0 {
		data.ReplayInputs = nil
	}
	s.History.mu.RLock()
	data.NextIndex = s.History.nextIndex
	s.History.mu.RUnlock()
	return data
}

func redisDataToSession(data redisSessionData, actions redisSessionActionsData) *session {
	h := redisActionsToHistory(actions)
	if h == nil {
		h = redisLegacyHistoryToHistory(data.HistoryRecords, data.HistoryNextIndex)
	}

	return &session{
		Info:                data.Info,
		Runtime:             data.Runtime,
		History:             h,
		managed:             data.Managed,
		experimentID:        data.ExperimentID,
		mode:                data.Mode,
		ownerKeyHash:        data.OwnerKeyHash,
		closed:              data.Deleted,
		deletedAt:           data.DeletedAt,
		deletionReason:      data.DeletionReason,
		lastTaskTime:        data.LastTaskTime,
		lastAnnotationPatch: data.LastAnnotationPatch,
		idleTimeout:         data.IdleTimeout,
		createdAt:           data.CreatedAt,
		operations:          make(map[string]*executeOperation),
		privateContainers:   privateContainerMap(data.PrivateContainers),
	}
}

func redisActionsToHistory(data redisSessionActionsData) *StepHistory {
	if len(data.Records) == 0 && data.NextIndex == 0 {
		return nil
	}
	h := NewStepHistory()
	h.records = data.Records
	for i := range h.records {
		if replayInput, ok := data.ReplayInputs[h.records[i].Index]; ok {
			h.records[i].ReplayInput = append(json.RawMessage(nil), replayInput...)
		}
	}
	h.nextIndex = data.NextIndex
	return h
}

func redisLegacyHistoryToHistory(records []redisLegacyStepRecord, nextIndex int) *StepHistory {
	h := NewStepHistory()
	h.records = make([]StepRecord, 0, len(records))
	for _, record := range records {
		step := StepRecord{
			Index:      record.Index,
			Name:       record.Name,
			Input:      record.Input,
			SnapshotID: record.SnapshotID,
			DurationMs: record.DurationMs,
			Timestamp:  record.Timestamp,
		}
		h.records = append(h.records, step)
	}
	h.nextIndex = nextIndex
	if h.nextIndex == 0 && len(h.records) > 0 {
		h.nextIndex = h.records[len(h.records)-1].Index + 1
	}
	return h
}

func redisSessionNeedsLegacyCompaction(data redisSessionData) bool {
	return len(data.HistoryRecords) > 0 || data.HistoryNextIndex > 0
}

// RedisStore is a SessionStore backed by Redis.
// It keeps a local cache (sync.Map) for active hot-path reads and persists
// mutations to Redis for durable history/recovery.
type RedisStore struct {
	client *redis.Client
	ttl    time.Duration

	// Local cache for hot-path reads. Writes go to both cache and Redis.
	cache sync.Map // sessionID -> *session
}

// RedisStoreConfig holds configuration for the Redis session store.
type RedisStoreConfig struct {
	Addr     string
	Password string
	DB       int
	TTL      time.Duration // TTL for session keys; 0 means no expiry
}

// NewRedisStore creates a new Redis-backed session store.
func NewRedisStore(cfg RedisStoreConfig) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 2 * time.Hour // default TTL
	}

	return &RedisStore{
		client: client,
		ttl:    ttl,
	}, nil
}

func (rs *RedisStore) redisKey(sessionID string) string {
	return redisSessionPrefix + sessionID
}

func (rs *RedisStore) redisActionsKey(sessionID string) string {
	return redisSessionActionsPrefix + sessionID
}

func (rs *RedisStore) Get(sessionID string) (*session, bool) {
	if val, ok := rs.cache.Load(sessionID); ok {
		return val.(*session), true
	}
	return nil, false
}

func (rs *RedisStore) Set(sessionID string, s *session) {
	rs.cache.Store(sessionID, s)
	rs.persistToRedis(sessionID, s)
	rs.persistActionsToRedis(sessionID, s)
}

func (rs *RedisStore) Delete(sessionID string) {
	var data *redisSessionData
	if val, ok := rs.cache.Load(sessionID); ok {
		d := sessionToRedisData(val.(*session))
		data = &d
	} else if d, ok := rs.loadRedisData(sessionID); ok {
		data = &d
	}

	rs.cache.Delete(sessionID)
	if data == nil {
		return
	}

	// Keep a tombstoned Redis record for history/replay, but make active
	// lookups return "not found" so deleted sessions cannot be resurrected.
	data.Deleted = true
	if data.DeletedAt == nil {
		now := time.Now()
		data.DeletedAt = &now
	}
	if data.DeletionReason == "" {
		data.DeletionReason = "deleted"
	}
	rs.persistDataToRedis(sessionID, *data)
	rs.expireActionsKey(sessionID)
}

func (rs *RedisStore) Range(fn func(sessionID string, s *session) bool) {
	// Iterate local cache. For Range to be correct across replicas,
	// callers should hydrate the cache on startup (e.g., via Recover).
	rs.cache.Range(func(key, value any) bool {
		return fn(key.(string), value.(*session))
	})
}

func (rs *RedisStore) Count() int64 {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := rs.client.Get(ctx, redisCountKey).Int64()
	if err != nil {
		return 0
	}
	return val
}

func (rs *RedisStore) IncrCount(delta int64) int64 {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := rs.client.IncrBy(ctx, redisCountKey, delta).Result()
	if err != nil {
		log.Printf("Warning: failed to increment session count in Redis: %v", err)
		return 0
	}
	if val < 0 {
		return rs.SetCount(0)
	}
	return val
}

func (rs *RedisStore) SetCount(count int64) int64 {
	if count < 0 {
		count = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := rs.client.Set(ctx, redisCountKey, count, 0).Err(); err != nil {
		log.Printf("Warning: failed to set session count in Redis: %v", err)
		return rs.Count()
	}
	return count
}

func (rs *RedisStore) Close() error {
	return rs.client.Close()
}

// Sync persists the current in-memory state of a session to Redis.
// Call this after mutations to session fields (e.g., after touchLastTaskTime,
// Restore, or History.Add).
func (rs *RedisStore) Sync(sessionID string) {
	val, ok := rs.cache.Load(sessionID)
	if !ok {
		return
	}
	rs.persistToRedis(sessionID, val.(*session))
}

func (rs *RedisStore) SyncHistory(sessionID string) {
	val, ok := rs.cache.Load(sessionID)
	if !ok {
		return
	}
	rs.persistActionsToRedis(sessionID, val.(*session))
}

// GetHistorical retrieves a session record for replay even if it has been
// tombstoned by Delete. Active request paths must use Get instead.
func (rs *RedisStore) GetHistorical(sessionID string) (*session, bool) {
	data, ok := rs.loadRedisData(sessionID)
	if !ok {
		return nil, false
	}
	actions := rs.loadRedisActions(sessionID)
	s := redisDataToSession(data, actions)
	rs.compactLegacySession(sessionID, data, s)
	return s, true
}

func (rs *RedisStore) loadRedisData(sessionID string) (redisSessionData, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return rs.loadRedisDataContext(ctx, sessionID)
}

func (rs *RedisStore) loadRedisDataContext(ctx context.Context, sessionID string) (redisSessionData, bool) {
	data, found, err := rs.loadRedisDataContextResult(ctx, sessionID)
	if err != nil {
		log.Printf("Warning: failed to load session %s from Redis: %v", sessionID, err)
		return redisSessionData{}, false
	}
	return data, found
}

func (rs *RedisStore) loadRedisDataContextResult(ctx context.Context, sessionID string) (redisSessionData, bool, error) {
	raw, err := rs.client.Get(ctx, rs.redisKey(sessionID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return redisSessionData{}, false, nil
		}
		return redisSessionData{}, false, err
	}

	var data redisSessionData
	if err := json.Unmarshal(raw, &data); err != nil {
		return redisSessionData{}, false, err
	}
	return data, true, nil
}

func (rs *RedisStore) loadRedisActions(sessionID string) redisSessionActionsData {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	data, err := rs.loadRedisActionsContext(ctx, sessionID)
	if err != nil {
		log.Printf("Warning: failed to load session actions %s from Redis: %v", sessionID, err)
		return redisSessionActionsData{}
	}
	return data
}

func (rs *RedisStore) loadRedisActionsContext(ctx context.Context, sessionID string) (redisSessionActionsData, error) {
	raw, err := rs.client.Get(ctx, rs.redisActionsKey(sessionID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return redisSessionActionsData{}, nil
		}
		return redisSessionActionsData{}, err
	}

	var data redisSessionActionsData
	if err := json.Unmarshal(raw, &data); err != nil {
		return redisSessionActionsData{}, err
	}
	return data, nil
}

func (rs *RedisStore) RecoverSession(ctx context.Context, sessionID string) (sessionRecoveryRecord, error) {
	data, found, err := rs.loadRedisDataContextResult(ctx, sessionID)
	if err != nil {
		return sessionRecoveryRecord{}, err
	}
	if !found {
		rs.cache.Delete(sessionID)
		return sessionRecoveryRecord{}, nil
	}
	if data.Deleted {
		rs.cache.Delete(sessionID)
		return sessionRecoveryRecord{found: true, deleted: true}, nil
	}

	actions, err := rs.loadRedisActionsContext(ctx, sessionID)
	if err != nil {
		return sessionRecoveryRecord{}, err
	}
	s := redisDataToSession(data, actions)
	rs.cache.Store(sessionID, s)
	rs.compactLegacySession(sessionID, data, s)
	return sessionRecoveryRecord{session: s, found: true}, nil
}

func (rs *RedisStore) RecoverActiveSessions(ctx context.Context) (map[string]*session, error) {
	recovered := make(map[string]*session)
	var cursor uint64
	for {
		keys, nextCursor, err := rs.client.Scan(ctx, cursor, redisSessionPrefix+"*", 100).Result()
		if err != nil {
			return recovered, fmt.Errorf("scan redis sessions: %w", err)
		}
		for _, key := range keys {
			sessionID := strings.TrimPrefix(key, redisSessionPrefix)
			if sessionID == "" || sessionID == key {
				continue
			}
			data, ok := rs.loadRedisDataContext(ctx, sessionID)
			if !ok || data.Deleted {
				rs.cache.Delete(sessionID)
				continue
			}
			actions, err := rs.loadRedisActionsContext(ctx, sessionID)
			if err != nil {
				return recovered, fmt.Errorf("load redis session actions %s: %w", sessionID, err)
			}
			s := redisDataToSession(data, actions)
			rs.cache.Store(sessionID, s)
			rs.compactLegacySession(sessionID, data, s)
			recovered[sessionID] = s
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return recovered, nil
}

func (rs *RedisStore) compactLegacySession(sessionID string, data redisSessionData, s *session) {
	if s == nil || !redisSessionNeedsLegacyCompaction(data) {
		return
	}
	rs.persistActionsToRedis(sessionID, s)
	rs.persistToRedis(sessionID, s)
	log.Printf("Compacted legacy Redis session %s into metadata/action keys (%d history records)", sessionID, len(data.HistoryRecords))
	debug.FreeOSMemory()
}

func (rs *RedisStore) persistToRedis(sessionID string, s *session) {
	rs.persistDataToRedis(sessionID, sessionToRedisData(s))
}

func (rs *RedisStore) persistActionsToRedis(sessionID string, s *session) {
	raw, err := json.Marshal(sessionActionsToRedisData(s))
	if err != nil {
		log.Printf("Warning: failed to marshal session actions %s for Redis: %v", sessionID, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := rs.client.Set(ctx, rs.redisActionsKey(sessionID), raw, rs.ttl).Err(); err != nil {
		log.Printf("Warning: failed to persist session actions %s to Redis: %v", sessionID, err)
	}
}

func (rs *RedisStore) expireActionsKey(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rs.client.Expire(ctx, rs.redisActionsKey(sessionID), rs.ttl)
}

func (rs *RedisStore) persistDataToRedis(sessionID string, data redisSessionData) {
	raw, err := json.Marshal(data)
	if err != nil {
		log.Printf("Warning: failed to marshal session %s for Redis: %v", sessionID, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := rs.client.Set(ctx, rs.redisKey(sessionID), raw, rs.ttl).Err(); err != nil {
		log.Printf("Warning: failed to persist session %s to Redis: %v", sessionID, err)
	}

	if data.ExperimentID != "" {
		expKey := redisExperimentPrefix + data.ExperimentID
		rs.client.SAdd(ctx, expKey, sessionID)
		rs.client.Expire(ctx, expKey, rs.ttl)
	}
	if data.Info.Profile != "" {
		profileKey := redisProfilePrefix + data.Info.Profile
		rs.client.SAdd(ctx, profileKey, sessionID)
		rs.client.Expire(ctx, profileKey, rs.ttl)
	}
	status := data.Info.Status
	if status == "" && !data.Deleted {
		status = "active"
	}
	if status != "" {
		statusKey := redisStatusPrefix + status
		rs.client.SAdd(ctx, statusKey, sessionID)
		rs.client.Expire(ctx, statusKey, rs.ttl)
	}
}

// FindByExperiment returns session IDs associated with an experiment,
// including sessions that have been soft-deleted from cache but still
// exist in Redis (within the TTL window).
func (rs *RedisStore) FindByExperiment(experimentID string) []string {
	return rs.findBySet(redisExperimentPrefix + experimentID)
}

func (rs *RedisStore) FindByProfile(profile string) []string {
	return rs.findBySet(redisProfilePrefix + profile)
}

func (rs *RedisStore) FindByStatus(status string) []string {
	return rs.findBySet(redisStatusPrefix + status)
}

func (rs *RedisStore) findBySet(key string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ids, err := rs.client.SMembers(ctx, key).Result()
	if err != nil {
		return nil
	}
	return ids
}
