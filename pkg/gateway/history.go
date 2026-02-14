package gateway

import (
	"encoding/json"
	"sync"
	"time"
)

// StepRecord records one step execution for history and trajectory export.
type StepRecord struct {
	Index      int             `json:"index"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
	Output     StepOutput      `json:"output"`
	SnapshotID string          `json:"snapshot_id"`
	DurationMs int64           `json:"duration_ms"`
	Timestamp  time.Time       `json:"timestamp"`
}

// StepHistory is a thread-safe history of step executions.
type StepHistory struct {
	mu        sync.RWMutex
	records   []StepRecord
	nextIndex int
}

// NewStepHistory creates a new step history.
func NewStepHistory() *StepHistory {
	return &StepHistory{}
}

// Add adds a step record to the history, assigning the next global index.
func (h *StepHistory) Add(record StepRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	record.Index = h.nextIndex
	h.nextIndex++
	h.records = append(h.records, record)
}

// Len returns the total number of records.
func (h *StepHistory) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.records)
}

// GetAll returns all step records.
func (h *StepHistory) GetAll() []StepRecord {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]StepRecord, len(h.records))
	copy(result, h.records)
	return result
}

// GetUpTo returns all records with Index <= target.
func (h *StepHistory) GetUpTo(target int) []StepRecord {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var result []StepRecord
	for _, r := range h.records {
		if r.Index <= target {
			result = append(result, r)
		}
	}
	return result
}

// TruncateTo keeps only records with Index <= target and resets nextIndex.
func (h *StepHistory) TruncateTo(target int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var kept []StepRecord
	for _, r := range h.records {
		if r.Index <= target {
			kept = append(kept, r)
		}
	}
	h.records = kept
	h.nextIndex = target + 1
}

// ExportTrajectory exports all steps as JSONL trajectory lines.
func (h *StepHistory) ExportTrajectory(sessionID string) ([]byte, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var buf []byte
	for _, r := range h.records {
		obs, _ := json.Marshal(r.Output)
		entry := TrajectoryEntry{
			SessionID:   sessionID,
			Step:        r.Index,
			Action:      r.Input,
			Observation: obs,
			SnapshotID:  r.SnapshotID,
			Timestamp:   r.Timestamp,
		}
		line, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return buf, nil
}
