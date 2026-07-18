package gateway

import (
	"log"
	"os"
	"path/filepath"
	"time"
)

func (g *Gateway) StartCheckpointGC() {
	if g.checkpointStore == nil || g.gwConfig.CheckpointGCTTL <= 0 {
		return
	}
	g.checkpointGCWg.Add(1)
	go g.checkpointGCLoop()
}

func (g *Gateway) StopCheckpointGC() {
	if g.checkpointGCStopCh == nil {
		return
	}
	g.checkpointGCStopOnce.Do(func() {
		close(g.checkpointGCStopCh)
	})
	g.checkpointGCWg.Wait()
}

func (g *Gateway) checkpointGCLoop() {
	defer g.checkpointGCWg.Done()

	g.reconcileCheckpointGC()

	ticker := time.NewTicker(g.gwConfig.CheckpointGCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.checkpointGCStopCh:
			return
		case <-ticker.C:
			g.reconcileCheckpointGC()
		}
	}
}

func (g *Gateway) reconcileCheckpointGC() {
	ttl := g.gwConfig.CheckpointGCTTL
	if ttl <= 0 {
		return
	}
	cutoff := time.Now().Add(-ttl)

	entries, err := os.ReadDir(g.checkpointStore.basePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("checkpoint GC: read store dir: %v", err)
		}
		return
	}

	var cleaned int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionDir := filepath.Join(g.checkpointStore.basePath, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Use the directory's mod time as a proxy for last checkpoint write.
		// Each Save() writes into this dir, updating its mtime.
		if info.ModTime().After(cutoff) {
			continue
		}

		// Check if session is still active — don't GC active sessions.
		if s, ok := g.store.Get(entry.Name()); ok {
			s.mu.RLock()
			active := !s.closed
			s.mu.RUnlock()
			if active {
				continue
			}
		}

		if err := os.RemoveAll(sessionDir); err != nil {
			log.Printf("checkpoint GC: remove %s: %v", entry.Name(), err)
			continue
		}
		cleaned++
	}

	if cleaned > 0 {
		log.Printf("checkpoint GC: cleaned %d expired session checkpoint(s) (TTL %s)", cleaned, ttl)
	}
}
