package gateway

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// forkInherits is what a fork copies from the session it branches off.
//
// A fork used to be created from image, profile, namespace, and mode alone,
// so everything else fell back to gateway defaults. The idle timeout is the
// damaging one: a source configured to stay alive for hours produced a fork
// the sweep reclaimed after GATEWAY_IDLE_TIMEOUT, killing long verifier runs
// partway through and reporting them as "runtime lost".
//
// Not inherited, because the session does not retain them past creation:
// configEnv and allowInternet. A fork of a session created with either will
// not reproduce it. Fixing that means persisting them on the session first.
type forkInherits struct {
	Image             string
	Profile           string
	Namespace         string
	Mode              string
	IdleTimeout       time.Duration
	PrivateContainers []PrivateContainerSpec
}

// forkInheritsFromSession snapshots the inheritable settings. Takes the read
// lock itself, so the caller must not hold it.
func forkInheritsFromSession(s *session) forkInherits {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inherits := forkInherits{
		Image:       s.Info.Image,
		Profile:     s.Info.Profile,
		Namespace:   s.Info.Namespace,
		Mode:        s.Info.Mode,
		IdleTimeout: s.idleTimeout,
	}
	if len(s.privateContainers) > 0 {
		names := make([]string, 0, len(s.privateContainers))
		for name := range s.privateContainers {
			names = append(names, name)
		}
		sort.Strings(names)
		inherits.PrivateContainers = make([]PrivateContainerSpec, 0, len(names))
		for _, name := range names {
			inherits.PrivateContainers = append(inherits.PrivateContainers, s.privateContainers[name])
		}
	}
	return inherits
}

func (f forkInherits) createRequest() CreateSessionRequest {
	req := CreateSessionRequest{
		Image:             f.Image,
		Profile:           f.Profile,
		Namespace:         f.Namespace,
		Mode:              f.Mode,
		PrivateContainers: f.PrivateContainers,
	}
	if f.IdleTimeout > 0 {
		req.IdleTimeoutSeconds = int(f.IdleTimeout.Seconds())
	}
	return req
}

// ForkSession creates a new session from the filesystem state of a source
// session at a given checkpoint step. The flow:
//  1. Try to load checkpoint from persistent store (works after source deletion)
//  2. Fall back to downloading from the source executor HTTP endpoint
//  3. Create a new session with the same image/profile/namespace
//  4. Upload the tar to the new session and extract it
//  5. Return the new session info with fork metadata
func (g *Gateway) ForkSession(ctx context.Context, sourceID string, req ForkSessionRequest) (*ForkSessionResponse, error) {
	if !g.gwConfig.SandboxCheckpointEnabled {
		return nil, fmt.Errorf("checkpoint not enabled")
	}

	source, ok := g.store.Get(sourceID)
	if !ok {
		// Source session may be deleted; check persistent store
		if g.checkpointStore == nil {
			return nil, fmt.Errorf("source session %s not found", sourceID)
		}
		return g.forkFromStore(ctx, sourceID, req)
	}

	inherits := forkInheritsFromSession(source)
	source.mu.RLock()
	sourceClosed := source.closed
	sourcePodIP := source.Info.PodIP
	source.mu.RUnlock()

	if sourceClosed {
		if g.checkpointStore != nil {
			return g.forkFromStoreWithMeta(ctx, sourceID, req, inherits)
		}
		return nil, fmt.Errorf("source session %s not found", sourceID)
	}

	// Execute returns 0-based step indices; checkpoint dirs are 1-based.
	checkpointStep := req.Step + 1

	if sourcePodIP == "" {
		return nil, fmt.Errorf("source session %s has no pod IP", sourceID)
	}

	if g.executorClient == nil {
		return nil, fmt.Errorf("executor client not configured")
	}

	// Download combined checkpoint tar directly from executor.
	// The PVC persistent store is only used when the source session
	// has been deleted (handled by forkFromStore above).
	tmpFile, err := os.CreateTemp("", "arl-fork-checkpoint-*.tar")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Hold the source busy for the transfer. A checkpoint download can run for
	// minutes, and without this the source looks idle to the sweep and its
	// claim's ShutdownTime stops being extended mid-read.
	_, acquiredPodIP, releaseSource, err := g.acquireSessionPodIP(ctx, sourceID)
	if err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("acquire source session %s for checkpoint download: %w", sourceID, err)
	}
	if acquiredPodIP != "" {
		sourcePodIP = acquiredPodIP
	}
	downloadStart := time.Now()
	downloadErr := g.executorClient.DownloadCheckpoint(ctx, sourcePodIP, checkpointStep, tmpFile)
	releaseSource()
	downloadDuration := time.Since(downloadStart)
	if downloadErr != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("download checkpoint from executor: %w", downloadErr)
	}
	size := int64(-1)
	if st, statErr := tmpFile.Stat(); statErr == nil {
		size = st.Size()
	}
	tmpFile.Close()
	log.Printf("Fork: downloaded checkpoint for %s step %d from pod %s (%d bytes in %s, source held active)",
		sourceID, checkpointStep, sourcePodIP, size, downloadDuration.Round(time.Millisecond))

	return g.completeFork(ctx, sourceID, req, tmpPath, inherits)
}

// forkFromStore handles fork when the source session has been deleted from the
// store entirely. Session metadata comes from the historical record, or from
// the request's Image/Profile fields as a fallback (e.g. after gateway restart
// with in-memory store).
func (g *Gateway) forkFromStore(ctx context.Context, sourceID string, req ForkSessionRequest) (*ForkSessionResponse, error) {
	historical, ok := g.GetHistoricalSession(sourceID)
	if ok {
		return g.forkFromStoreWithMeta(ctx, sourceID, req, forkInheritsFromSession(historical))
	}

	if req.Image == "" {
		return nil, fmt.Errorf("source session %s not found and no image specified in fork request", sourceID)
	}
	profile := req.Profile
	if profile == "" {
		profile = "default"
	}
	log.Printf("Fork: no historical record for %s, using request-provided image=%s profile=%s", sourceID, req.Image, profile)
	return g.forkFromStoreWithMeta(ctx, sourceID, req, forkInherits{Image: req.Image, Profile: profile})
}

func (g *Gateway) forkFromStoreWithMeta(ctx context.Context, sourceID string, req ForkSessionRequest, inherits forkInherits) (*ForkSessionResponse, error) {
	checkpointStep := req.Step + 1
	tmpPath, err := g.checkpointStore.LoadCombined(sourceID, checkpointStep)
	if err != nil {
		return nil, fmt.Errorf("load checkpoint from store for session %s step %d: %w", sourceID, checkpointStep, err)
	}
	defer os.Remove(tmpPath)

	return g.completeFork(ctx, sourceID, req, tmpPath, inherits)
}

// completeFork creates a new session and applies the checkpoint tar.
func (g *Gateway) completeFork(ctx context.Context, sourceID string, req ForkSessionRequest, tarPath string, inherits forkInherits) (*ForkSessionResponse, error) {
	newInfo, err := g.CreateSession(ctx, inherits.createRequest())
	if err != nil {
		return nil, fmt.Errorf("create fork session: %w", err)
	}

	if err := g.applyCheckpointToSession(ctx, newInfo.ID, tarPath); err != nil {
		log.Printf("Fork checkpoint apply failed for %s (from %s step %d): %v", newInfo.ID, sourceID, req.Step, err)
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if delErr := g.deleteSession(cleanupCtx, newInfo.ID, "fork checkpoint apply failed"); delErr != nil {
			log.Printf("Warning: failed to cleanup fork session %s: %v", newInfo.ID, delErr)
		}
		return nil, fmt.Errorf("apply checkpoint to fork session: %w", err)
	}

	newSession, ok := g.store.Get(newInfo.ID)
	if ok {
		newSession.mu.Lock()
		newSession.Info.ParentSessionID = sourceID
		newSession.Info.ForkStep = req.Step
		newSession.mu.Unlock()
	}

	newInfo.ParentSessionID = sourceID
	newInfo.ForkStep = req.Step

	idleDesc := "gateway default"
	if inherits.IdleTimeout > 0 {
		idleDesc = inherits.IdleTimeout.String()
	}
	log.Printf("Forked session %s from %s at step %d (inherited idle_timeout=%s, private_containers=%d, image=%s, profile=%s)",
		newInfo.ID, sourceID, req.Step, idleDesc, len(inherits.PrivateContainers), inherits.Image, inherits.Profile)

	return &ForkSessionResponse{
		Session:  newInfo,
		ParentID: sourceID,
		ForkStep: req.Step,
	}, nil
}

// applyCheckpointToSession uploads a checkpoint tar to a session and extracts
// it, restoring filesystem state. Uses the executor client for file upload
// and command execution.
func (g *Gateway) applyCheckpointToSession(ctx context.Context, sessionID, tarPath string) error {
	if g.executorClient == nil {
		return fmt.Errorf("executor client not configured")
	}

	// Uploading and extracting the tar is unbounded work; keep the session
	// marked busy so neither sweep nor the claim deadline reclaims it midway.
	_, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return err
	}
	defer releaseSession()

	if podIP == "" {
		return fmt.Errorf("session %s has no pod IP", sessionID)
	}

	applyStart := time.Now()
	tarFile, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("open checkpoint tar: %w", err)
	}
	defer tarFile.Close()

	// Upload tar via executor WriteFile (streams, no full-file memory alloc)
	const restorePath = "/tmp/arl-restore.tar"
	if _, err := g.executorClient.WriteFile(ctx, podIP, restorePath, tarFile, ""); err != nil {
		return fmt.Errorf("upload checkpoint tar: %w", err)
	}

	// Extract tar to root filesystem
	extractReq := &interfaces.ExecRequest{
		Command:        []string{"tar", "xf", restorePath, "-C", "/"},
		TimeoutSeconds: 120,
	}
	result, err := g.executorClient.Execute(ctx, podIP, extractReq)
	if err != nil {
		return fmt.Errorf("extract checkpoint: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("checkpoint extraction failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
	log.Printf("Fork: applied checkpoint to session %s on pod %s in %s (session held active)",
		sessionID, podIP, time.Since(applyStart).Round(time.Millisecond))

	// Cleanup the temp tar
	cleanupReq := &interfaces.ExecRequest{
		Command:        []string{"rm", "-f", restorePath},
		TimeoutSeconds: 10,
	}
	g.executorClient.Execute(ctx, podIP, cleanupReq) //nolint:errcheck

	return nil
}
