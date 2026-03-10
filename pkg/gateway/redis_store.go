package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisSessionPrefix = "arl:session:"
	redisCountKey      = "arl:session_count"
)

// redisSessionData is the JSON-serializable representation of a session
// stored in Redis. Fields that cannot be serialized (sync.RWMutex, StepHistory)
// are managed separately or reconstructed on load.
type redisSessionData struct {
	Info                SessionInfo   `json:"info"`
	Managed             bool          `json:"managed"`
	ExperimentID        string        `json:"experimentId"`
	LastTaskTime        time.Time     `json:"lastTaskTime"`
	LastAnnotationPatch time.Time     `json:"lastAnnotationPatch"`
	IdleTimeout         time.Duration `json:"idleTimeout"`
	MaxLifetime         time.Duration `json:"maxLifetime"`
	CreatedAt           time.Time     `json:"createdAt"`
	HistoryRecords      []StepRecord  `json:"historyRecords"`
	HistoryNextIndex    int           `json:"historyNextIndex"`
}

func sessionToRedisData(s *session) redisSessionData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := redisSessionData{
		Info:                s.Info,
		Managed:             s.managed,
		ExperimentID:        s.experimentID,
		LastTaskTime:        s.lastTaskTime,
		LastAnnotationPatch: s.lastAnnotationPatch,
		IdleTimeout:         s.idleTimeout,
		MaxLifetime:         s.maxLifetime,
		CreatedAt:           s.createdAt,
	}

	if s.History != nil {
		data.HistoryRecords = s.History.GetAll()
		s.History.mu.RLock()
		data.HistoryNextIndex = s.History.nextIndex
		s.History.mu.RUnlock()
	}

	return data
}

func redisDataToSession(data redisSessionData) *session {
	h := NewStepHistory()
	h.records = data.HistoryRecords
	h.nextIndex = data.HistoryNextIndex

	return &session{
		Info:                data.Info,
		History:             h,
		managed:             data.Managed,
		experimentID:        data.ExperimentID,
		lastTaskTime:        data.LastTaskTime,
		lastAnnotationPatch: data.LastAnnotationPatch,
		idleTimeout:         data.IdleTimeout,
		maxLifetime:         data.MaxLifetime,
		createdAt:           data.CreatedAt,
	}
}

// RedisStore is a SessionStore backed by Redis.
// It keeps a local cache (sync.Map) for hot-path reads (avoiding
// Redis round-trips on every GetSession/ExecuteSteps) and persists
// mutations to Redis for cross-replica consistency.
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

func (rs *RedisStore) Get(sessionID string) (*session, bool) {
	// Check local cache first (hot path)
	if val, ok := rs.cache.Load(sessionID); ok {
		return val.(*session), true
	}

	// Fall back to Redis
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	raw, err := rs.client.Get(ctx, rs.redisKey(sessionID)).Bytes()
	if err != nil {
		return nil, false
	}

	var data redisSessionData
	if err := json.Unmarshal(raw, &data); err != nil {
		log.Printf("Warning: failed to unmarshal session %s from Redis: %v", sessionID, err)
		return nil, false
	}

	s := redisDataToSession(data)
	rs.cache.Store(sessionID, s)
	return s, true
}

func (rs *RedisStore) Set(sessionID string, s *session) {
	rs.cache.Store(sessionID, s)
	rs.persistToRedis(sessionID, s)
}

func (rs *RedisStore) Delete(sessionID string) {
	rs.cache.Delete(sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := rs.client.Del(ctx, rs.redisKey(sessionID)).Err(); err != nil {
		log.Printf("Warning: failed to delete session %s from Redis: %v", sessionID, err)
	}
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
	return val
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

func (rs *RedisStore) persistToRedis(sessionID string, s *session) {
	data := sessionToRedisData(s)
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
}
