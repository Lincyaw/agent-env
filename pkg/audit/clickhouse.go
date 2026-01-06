package audit

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// ClickHouseWriter implements AuditWriter using ClickHouse
type ClickHouseWriter struct {
	db            *sql.DB
	batchSize     int
	flushInterval time.Duration

	taskRecords    []interfaces.TaskAuditRecord
	sandboxRecords []interfaces.SandboxAuditRecord
	mu             sync.Mutex

	stopCh chan struct{}
	doneCh chan struct{}
}

// ClickHouseConfig holds configuration for ClickHouse connection
type ClickHouseConfig struct {
	Addr          string
	Database      string
	Username      string
	Password      string
	BatchSize     int
	FlushInterval time.Duration
}

// NewClickHouseWriter creates a new ClickHouse audit writer
func NewClickHouseWriter(cfg ClickHouseConfig) (*ClickHouseWriter, error) {
	dsn := fmt.Sprintf("clickhouse://%s:%s@%s/%s",
		cfg.Username, cfg.Password, cfg.Addr, cfg.Database)

	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open clickhouse connection: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping clickhouse: %w", err)
	}

	// Create tables if they don't exist
	if _, err := db.Exec(TaskAuditTableSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create task_audit table: %w", err)
	}
	if _, err := db.Exec(SandboxAuditTableSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create sandbox_audit table: %w", err)
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	flushInterval := cfg.FlushInterval
	if flushInterval <= 0 {
		flushInterval = 10 * time.Second
	}

	w := &ClickHouseWriter{
		db:            db,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}

	go w.flushLoop()

	return w, nil
}

// WriteTaskCompletion writes a task completion audit record
func (w *ClickHouseWriter) WriteTaskCompletion(_ context.Context, record interfaces.TaskAuditRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.taskRecords = append(w.taskRecords, record)

	if len(w.taskRecords) >= w.batchSize {
		return w.flushTaskRecordsLocked()
	}

	return nil
}

// WriteSandboxEvent writes a sandbox lifecycle event audit record
func (w *ClickHouseWriter) WriteSandboxEvent(_ context.Context, record interfaces.SandboxAuditRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.sandboxRecords = append(w.sandboxRecords, record)

	if len(w.sandboxRecords) >= w.batchSize {
		return w.flushSandboxRecordsLocked()
	}

	return nil
}

// Flush flushes any buffered audit records
func (w *ClickHouseWriter) Flush(_ context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.flushTaskRecordsLocked(); err != nil {
		return err
	}
	return w.flushSandboxRecordsLocked()
}

// Close closes the audit writer
func (w *ClickHouseWriter) Close() error {
	close(w.stopCh)
	<-w.doneCh

	w.mu.Lock()
	_ = w.flushTaskRecordsLocked()
	_ = w.flushSandboxRecordsLocked()
	w.mu.Unlock()

	return w.db.Close()
}

func (w *ClickHouseWriter) flushLoop() {
	defer close(w.doneCh)
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.mu.Lock()
			_ = w.flushTaskRecordsLocked()
			_ = w.flushSandboxRecordsLocked()
			w.mu.Unlock()
		}
	}
}

func (w *ClickHouseWriter) flushTaskRecordsLocked() error {
	if len(w.taskRecords) == 0 {
		return nil
	}

	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO task_audit (
			trace_id, namespace, name, sandbox_ref, state, exit_code,
			duration, start_time, completion_time, step_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, r := range w.taskRecords {
		if _, err := stmt.Exec(
			r.TraceID, r.Namespace, r.Name, r.SandboxRef, r.State, r.ExitCode,
			r.Duration, r.StartTime, r.CompletionTime, r.StepCount,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to insert task audit record: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	w.taskRecords = w.taskRecords[:0]
	return nil
}

func (w *ClickHouseWriter) flushSandboxRecordsLocked() error {
	if len(w.sandboxRecords) == 0 {
		return nil
	}

	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO sandbox_audit (
			trace_id, namespace, name, pool_ref, phase, pod_name, event
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, r := range w.sandboxRecords {
		if _, err := stmt.Exec(
			r.TraceID, r.Namespace, r.Name, r.PoolRef, r.Phase, r.PodName, r.Event,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to insert sandbox audit record: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	w.sandboxRecords = w.sandboxRecords[:0]
	return nil
}
