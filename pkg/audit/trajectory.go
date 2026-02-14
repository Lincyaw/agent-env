package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/driver/clickhouse"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TrajectoryEntry represents a single trajectory entry stored in ClickHouse
type TrajectoryEntry struct {
	SessionID   string          `gorm:"column:session_id;type:String" json:"session_id"`
	Step        int             `gorm:"column:step;type:Int32" json:"step"`
	Name        string          `gorm:"column:name;type:String" json:"name"`
	Action      json.RawMessage `gorm:"column:action;type:String" json:"action"`
	Observation json.RawMessage `gorm:"column:observation;type:String" json:"observation"`
	SnapshotID  string          `gorm:"column:snapshot_id;type:String" json:"snapshot_id"`
	DurationMs  int64           `gorm:"column:duration_ms;type:Int64" json:"duration_ms"`
	Timestamp   time.Time       `gorm:"column:timestamp;type:DateTime64(3)" json:"timestamp"`
	CreatedAt   time.Time       `gorm:"column:created_at;type:DateTime64(3);autoCreateTime:milli" json:"created_at"`
}

// TableName specifies the table name for GORM
func (TrajectoryEntry) TableName() string {
	return "trajectory"
}

// TrajectoryWriter manages trajectory storage in ClickHouse using GORM
type TrajectoryWriter struct {
	db *gorm.DB
}

// TrajectoryConfig holds configuration for trajectory storage
type TrajectoryConfig struct {
	Addr     string
	Database string
	Username string
	Password string
	Debug    bool
}

// NewTrajectoryWriter creates a new trajectory writer with GORM
func NewTrajectoryWriter(cfg TrajectoryConfig) (*TrajectoryWriter, error) {
	dsn := fmt.Sprintf("clickhouse://%s:%s@%s/%s?dial_timeout=10s&read_timeout=20s",
		cfg.Username, cfg.Password, cfg.Addr, cfg.Database)

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}
	if cfg.Debug {
		gormConfig.Logger = logger.Default.LogMode(logger.Info)
	}

	db, err := gorm.Open(clickhouse.Open(dsn), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to clickhouse: %w", err)
	}

	// Auto-migrate table schema
	if err := db.AutoMigrate(&TrajectoryEntry{}); err != nil {
		return nil, fmt.Errorf("failed to migrate trajectory table: %w", err)
	}

	// Create custom indexes and TTL (GORM doesn't support all ClickHouse features)
	// This SQL will be executed only if table doesn't have the proper engine
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// Check if table exists and recreate with proper engine if needed
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS trajectory (
		session_id String,
		step Int32,
		name String,
		action String,
		observation String,
		snapshot_id String,
		duration_ms Int64,
		timestamp DateTime64(3),
		created_at DateTime64(3) DEFAULT now64(3)
	) ENGINE = MergeTree()
	PARTITION BY toYYYYMMDD(created_at)
	ORDER BY (created_at, session_id, step)
	TTL toDateTime(created_at) + INTERVAL 90 DAY
	`
	if _, err := sqlDB.Exec(createTableSQL); err != nil {
		return nil, fmt.Errorf("failed to create trajectory table: %w", err)
	}

	return &TrajectoryWriter{db: db}, nil
}

// WriteEntry writes a single trajectory entry
func (w *TrajectoryWriter) WriteEntry(ctx context.Context, entry TrajectoryEntry) error {
	if err := w.db.WithContext(ctx).Create(&entry).Error; err != nil {
		return fmt.Errorf("failed to write trajectory entry: %w", err)
	}
	return nil
}

// WriteBatch writes multiple trajectory entries in a batch
func (w *TrajectoryWriter) WriteBatch(ctx context.Context, entries []TrajectoryEntry) error {
	if len(entries) == 0 {
		return nil
	}

	if err := w.db.WithContext(ctx).CreateInBatches(entries, 100).Error; err != nil {
		return fmt.Errorf("failed to write trajectory batch: %w", err)
	}
	return nil
}

// GetTrajectory retrieves trajectory entries for a session
func (w *TrajectoryWriter) GetTrajectory(ctx context.Context, sessionID string) ([]TrajectoryEntry, error) {
	var entries []TrajectoryEntry
	if err := w.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("step ASC").
		Find(&entries).Error; err != nil {
		return nil, fmt.Errorf("failed to get trajectory: %w", err)
	}
	return entries, nil
}

// GetTrajectoryUpTo retrieves trajectory entries up to a specific step
func (w *TrajectoryWriter) GetTrajectoryUpTo(ctx context.Context, sessionID string, maxStep int) ([]TrajectoryEntry, error) {
	var entries []TrajectoryEntry
	if err := w.db.WithContext(ctx).
		Where("session_id = ? AND step <= ?", sessionID, maxStep).
		Order("step ASC").
		Find(&entries).Error; err != nil {
		return nil, fmt.Errorf("failed to get trajectory up to step %d: %w", maxStep, err)
	}
	return entries, nil
}

// DeleteTrajectory deletes all trajectory entries for a session
func (w *TrajectoryWriter) DeleteTrajectory(ctx context.Context, sessionID string) error {
	if err := w.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Delete(&TrajectoryEntry{}).Error; err != nil {
		return fmt.Errorf("failed to delete trajectory: %w", err)
	}
	return nil
}

// Close closes the database connection
func (w *TrajectoryWriter) Close() error {
	sqlDB, err := w.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// GetStats returns trajectory statistics
func (w *TrajectoryWriter) GetStats(ctx context.Context, sessionID string) (map[string]interface{}, error) {
	var result struct {
		TotalSteps    int64   `gorm:"column:total_steps"`
		AvgDuration   float64 `gorm:"column:avg_duration"`
		TotalDuration int64   `gorm:"column:total_duration"`
	}

	if err := w.db.WithContext(ctx).
		Model(&TrajectoryEntry{}).
		Where("session_id = ?", sessionID).
		Select("COUNT(*) as total_steps, AVG(duration_ms) as avg_duration, SUM(duration_ms) as total_duration").
		Scan(&result).Error; err != nil {
		return nil, fmt.Errorf("failed to get trajectory stats: %w", err)
	}

	return map[string]interface{}{
		"total_steps":       result.TotalSteps,
		"avg_duration_ms":   result.AvgDuration,
		"total_duration_ms": result.TotalDuration,
	}, nil
}
