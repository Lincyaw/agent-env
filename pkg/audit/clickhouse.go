package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// ClickHouseWriter implements AuditWriter using ClickHouse
type ClickHouseWriter struct {
	db            *sql.DB
	batchSize     int
	flushInterval time.Duration

	stepRecords    []interfaces.SessionStepAuditRecord
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
	encodedPassword := url.QueryEscape(cfg.Password)
	dsn := fmt.Sprintf("clickhouse://%s:%s@%s/%s",
		cfg.Username, encodedPassword, cfg.Addr, cfg.Database)

	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open clickhouse connection: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping clickhouse: %w", err)
	}

	if _, err := db.Exec(SessionStepAuditTableSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create session_step_audit table: %w", err)
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

// WriteSessionStep writes a session step audit record
func (w *ClickHouseWriter) WriteSessionStep(_ context.Context, record interfaces.SessionStepAuditRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.stepRecords = append(w.stepRecords, record)

	if len(w.stepRecords) >= w.batchSize {
		return w.flushStepRecordsLocked()
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

	if err := w.flushStepRecordsLocked(); err != nil {
		return err
	}
	return w.flushSandboxRecordsLocked()
}

// Close closes the audit writer
func (w *ClickHouseWriter) Close() error {
	close(w.stopCh)
	<-w.doneCh

	w.mu.Lock()
	defer w.mu.Unlock()

	var errs []error
	if err := w.flushStepRecordsLocked(); err != nil {
		errs = append(errs, fmt.Errorf("flush step records: %w", err))
	}
	if err := w.flushSandboxRecordsLocked(); err != nil {
		errs = append(errs, fmt.Errorf("flush sandbox records: %w", err))
	}
	if err := w.db.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close db: %w", err))
	}
	return errors.Join(errs...)
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
			_ = w.flushStepRecordsLocked()
			_ = w.flushSandboxRecordsLocked()
			w.mu.Unlock()
		}
	}
}

func (w *ClickHouseWriter) flushStepRecordsLocked() error {
	if len(w.stepRecords) == 0 {
		return nil
	}

	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO session_step_audit (
			session_id, trace_id, namespace, step_index, step_name, step_type,
			input, stdout, stderr, exit_code, snapshot_id, duration_ms, timestamp
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, r := range w.stepRecords {
		if _, err := stmt.Exec(
			r.SessionID, r.TraceID, r.Namespace, r.StepIndex, r.StepName, r.StepType,
			r.Input, r.Stdout, r.Stderr, r.ExitCode, r.SnapshotID, r.DurationMs, r.Timestamp,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to insert session step audit record: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	w.stepRecords = w.stepRecords[:0]
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
