package gateway

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

// ForkSession creates a new session from the filesystem state of a source
// session at a given checkpoint step. The flow:
//  1. Try to load checkpoint from persistent store (works after source deletion)
//  2. Fall back to downloading from the source sidecar HTTP endpoint
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

	source.mu.RLock()
	sourceClosed := source.closed
	sourceImage := source.Info.Image
	sourceProfile := source.Info.Profile
	sourceNS := source.Info.Namespace
	sourceMode := source.Info.Mode
	sourcePodIP := source.Info.PodIP
	source.mu.RUnlock()

	if sourceClosed {
		if g.checkpointStore != nil {
			return g.forkFromStoreWithMeta(ctx, sourceID, req, sourceImage, sourceProfile, sourceNS, sourceMode)
		}
		return nil, fmt.Errorf("source session %s not found", sourceID)
	}

	// Execute returns 0-based step indices; checkpoint dirs are 1-based.
	checkpointStep := req.Step + 1

	// Try persistent store first (avoids hitting the sidecar)
	if g.checkpointStore != nil {
		tmpPath, err := g.checkpointStore.LoadCombined(sourceID, checkpointStep)
		if err == nil {
			defer os.Remove(tmpPath)
			return g.completeFork(ctx, sourceID, req, tmpPath, sourceImage, sourceProfile, sourceNS, sourceMode)
		}
		log.Printf("Fork: persistent store miss for %s step %d, falling back to sidecar: %v", sourceID, checkpointStep, err)
	}

	if sourcePodIP == "" {
		return nil, fmt.Errorf("source session %s has no pod IP", sourceID)
	}

	// Download combined checkpoint tar from source sidecar
	sidecarHTTPPort := g.gwConfig.SidecarHTTPPort
	if sidecarHTTPPort == 0 {
		sidecarHTTPPort = 8080
	}
	checkpointURL := fmt.Sprintf("http://%s:%d/v1/checkpoints/combined?through=%d",
		sourcePodIP, sidecarHTTPPort, checkpointStep)

	httpClient := &http.Client{Timeout: 5 * time.Minute}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, checkpointURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build checkpoint request: %w", err)
	}
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("download checkpoint from source: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1024))
		return nil, fmt.Errorf("checkpoint download failed (%s): %s", httpResp.Status, string(body))
	}

	tmpFile, err := os.CreateTemp("", "arl-fork-checkpoint-*.tar")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, httpResp.Body); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("save checkpoint tar: %w", err)
	}
	tmpFile.Close()

	return g.completeFork(ctx, sourceID, req, tmpPath, sourceImage, sourceProfile, sourceNS, sourceMode)
}

// forkFromStore handles fork when the source session has been deleted from the
// store entirely. Session metadata must come from the historical record or the
// request will fail.
func (g *Gateway) forkFromStore(ctx context.Context, sourceID string, req ForkSessionRequest) (*ForkSessionResponse, error) {
	historical, ok := g.GetHistoricalSession(sourceID)
	if !ok {
		return nil, fmt.Errorf("source session %s not found (no historical record)", sourceID)
	}
	historical.mu.RLock()
	image := historical.Info.Image
	profile := historical.Info.Profile
	ns := historical.Info.Namespace
	mode := historical.Info.Mode
	historical.mu.RUnlock()

	return g.forkFromStoreWithMeta(ctx, sourceID, req, image, profile, ns, mode)
}

func (g *Gateway) forkFromStoreWithMeta(ctx context.Context, sourceID string, req ForkSessionRequest, image, profile, ns, mode string) (*ForkSessionResponse, error) {
	checkpointStep := req.Step + 1
	tmpPath, err := g.checkpointStore.LoadCombined(sourceID, checkpointStep)
	if err != nil {
		return nil, fmt.Errorf("load checkpoint from store for session %s step %d: %w", sourceID, checkpointStep, err)
	}
	defer os.Remove(tmpPath)

	return g.completeFork(ctx, sourceID, req, tmpPath, image, profile, ns, mode)
}

// completeFork creates a new session and applies the checkpoint tar.
func (g *Gateway) completeFork(ctx context.Context, sourceID string, req ForkSessionRequest, tarPath, image, profile, ns, mode string) (*ForkSessionResponse, error) {
	newReq := CreateSessionRequest{
		Image:     image,
		Profile:   profile,
		Namespace: ns,
		Mode:      mode,
	}

	newInfo, err := g.CreateSession(ctx, newReq)
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

	log.Printf("Forked session %s from %s at step %d", newInfo.ID, sourceID, req.Step)

	return &ForkSessionResponse{
		Session:  newInfo,
		ParentID: sourceID,
		ForkStep: req.Step,
	}, nil
}

// applyCheckpointToSession uploads a checkpoint tar to a session and extracts
// it, restoring filesystem state. Uses the sidecar gRPC client for file upload
// and command execution.
func (g *Gateway) applyCheckpointToSession(ctx context.Context, sessionID, tarPath string) error {
	sess, ok := g.store.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	sess.mu.RLock()
	podIP := sess.Info.PodIP
	sess.mu.RUnlock()

	if podIP == "" {
		return fmt.Errorf("session %s has no pod IP", sessionID)
	}
	if g.sidecarClient == nil {
		return fmt.Errorf("sidecar client not configured")
	}

	tarFile, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("open checkpoint tar: %w", err)
	}
	defer tarFile.Close()

	// Upload tar via sidecar WriteFile RPC (streams, no full-file memory alloc)
	const restorePath = "/tmp/arl-restore.tar"
	if _, err := g.sidecarClient.WriteFile(ctx, podIP, restorePath, tarFile, ""); err != nil {
		return fmt.Errorf("upload checkpoint tar: %w", err)
	}

	// Extract tar to root filesystem
	extractReq := &sidecar.ExecRequest{
		Command:        []string{"tar", "xf", restorePath, "-C", "/"},
		TimeoutSeconds: 120,
	}
	result, err := g.sidecarClient.Execute(ctx, podIP, extractReq)
	if err != nil {
		return fmt.Errorf("extract checkpoint: %w", err)
	}
	if result.GetExitCode() != 0 {
		return fmt.Errorf("checkpoint extraction failed (exit %d): %s", result.GetExitCode(), result.GetStderr())
	}

	// Cleanup the temp tar
	cleanupReq := &sidecar.ExecRequest{
		Command:        []string{"rm", "-f", restorePath},
		TimeoutSeconds: 10,
	}
	g.sidecarClient.Execute(ctx, podIP, cleanupReq) //nolint:errcheck

	return nil
}
