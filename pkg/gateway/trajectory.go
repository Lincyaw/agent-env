package gateway

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Lincyaw/agent-env/pkg/audit"
)

// SetTrajectoryWriter installs a ClickHouse writer after gateway startup and
// starts the trajectory worker. If the worker is already running, the new
// writer is closed and ignored.
func (g *Gateway) SetTrajectoryWriter(writer *audit.TrajectoryWriter) {
	if writer == nil {
		return
	}
	g.trajMu.Lock()
	if g.trajCh != nil || g.trajectoryWriter != nil {
		g.trajMu.Unlock()
		writer.Close()
		return
	}
	g.trajectoryWriter = writer
	g.trajMu.Unlock()
	g.StartTrajectoryWorker()
}

// StartTrajectoryWorker starts a single background goroutine to drain the
// trajectory write channel. Must be called after New() if trajectoryWriter is set.
func (g *Gateway) StartTrajectoryWorker() {
	g.trajMu.Lock()
	if g.trajectoryWriter == nil {
		g.trajMu.Unlock()
		return
	}
	if g.trajCh != nil {
		g.trajMu.Unlock()
		return
	}
	writer := g.trajectoryWriter
	ch := make(chan audit.TrajectoryEntry, 4096)
	g.trajCh = ch
	g.trajWg.Add(1)
	g.trajMu.Unlock()

	go func() {
		defer g.trajWg.Done()
		for entry := range ch {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := writer.WriteEntry(ctx, entry); err != nil {
				log.Printf("Warning: failed to write trajectory entry: %v", err)
			}
			cancel()
		}
	}()
}

// StopTrajectoryWorker closes the trajectory channel and waits for the worker to drain.
func (g *Gateway) StopTrajectoryWorker() {
	g.trajMu.Lock()
	ch := g.trajCh
	writer := g.trajectoryWriter
	g.trajCh = nil
	g.trajectoryWriter = nil
	g.trajMu.Unlock()

	if ch != nil {
		close(ch)
	}
	g.trajWg.Wait()
	if writer != nil {
		writer.Close()
	}
}

func (g *Gateway) enqueueTrajectory(entry audit.TrajectoryEntry, sessionID string, step int) {
	g.trajMu.RLock()
	defer g.trajMu.RUnlock()
	if g.trajCh == nil {
		return
	}
	select {
	case g.trajCh <- entry:
	default:
		log.Printf("Warning: trajectory channel full, dropping entry for session %s step %d", sessionID, step)
	}
}

// GetHistory returns the execution history for a session.
func (g *Gateway) GetHistory(sessionID string) ([]StepRecord, error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return s.History.GetAll(), nil
}

// ExportTrajectory exports the trajectory as JSONL.
func (g *Gateway) ExportTrajectory(sessionID string) ([]byte, error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return s.History.ExportTrajectory(sessionID)
}
