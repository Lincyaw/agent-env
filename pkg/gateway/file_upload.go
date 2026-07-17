package gateway

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"path"
	"strings"
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

const uploadFileStepName = "upload_file"

func (g *Gateway) UploadFile(ctx context.Context, sessionID string, filePath string, content io.Reader, expectedSHA256 string) (*UploadFileResponse, error) {
	absPath, err := sanitizeFilePath(filePath)
	if err != nil {
		return nil, err
	}
	expectedSHA256, err = normalizeSHA256(expectedSHA256)
	if err != nil {
		return nil, err
	}

	s, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	var buf bytes.Buffer
	tee := io.TeeReader(content, &buf)

	result, err := g.sidecarClient.WriteFile(ctx, podIP, absPath, tee, expectedSHA256)
	if err != nil {
		return nil, err
	}

	g.storeUploadBlob(ctx, result.SHA256, buf.Bytes())

	inputJSON, _ := json.Marshal(uploadRecord{Path: absPath, SHA256: result.SHA256, Size: int(result.BytesWritten)})
	s.History.Add(StepRecord{
		Name:      uploadFileStepName,
		Input:     inputJSON,
		Timestamp: time.Now(),
	})
	g.store.SyncHistory(sessionID)

	g.touchLastTaskTime(sessionID)
	return &UploadFileResponse{
		Path:         result.Path,
		BytesWritten: int(result.BytesWritten),
		SHA256:       result.SHA256,
	}, nil
}

type uploadRecord struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}

func (g *Gateway) storeUploadBlob(ctx context.Context, sha256 string, content []byte) {
	if g.trajectoryWriter == nil || sha256 == "" {
		return
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := g.trajectoryWriter.StoreBlob(bgCtx, sha256, content); err != nil {
			log.Printf("Warning: failed to store file blob %s: %v", sha256[:12], err)
		}
	}()
}

func (g *Gateway) DownloadFile(ctx context.Context, sessionID string, filePath string, dst io.Writer) (*interfaces.FileReadResult, error) {
	absPath, err := sanitizeFilePath(filePath)
	if err != nil {
		return nil, err
	}

	_, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	result, err := g.sidecarClient.ReadFile(ctx, podIP, absPath, dst)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	g.touchLastTaskTime(sessionID)
	return result, nil
}

func (g *Gateway) StatFile(ctx context.Context, sessionID string, filePath string) (*StatResponse, error) {
	absPath, err := sanitizeFilePath(filePath)
	if err != nil {
		return nil, err
	}

	_, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	result, err := g.sidecarClient.Stat(ctx, podIP, absPath)
	if err != nil {
		return nil, err
	}

	g.touchLastTaskTime(sessionID)
	return &StatResponse{
		Exists:   result.Exists,
		IsDir:    result.IsDir,
		Size:     int64(result.Size),
		Mode:     result.Mode,
		Modified: result.Modified,
	}, nil
}

func (g *Gateway) ListDir(ctx context.Context, sessionID string, filePath string, recursive bool) (*ListDirResponse, error) {
	absPath, err := sanitizeFilePath(filePath)
	if err != nil {
		return nil, err
	}

	_, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	result, err := g.sidecarClient.ListDir(ctx, podIP, absPath, recursive)
	if err != nil {
		return nil, err
	}

	entries := make([]ListDirEntryResponse, len(result))
	for i, e := range result {
		entries[i] = ListDirEntryResponse{
			Name:  e.Name,
			IsDir: e.IsDir,
			Size:  int64(e.Size),
		}
	}

	g.touchLastTaskTime(sessionID)
	return &ListDirResponse{Entries: entries}, nil
}

func (g *Gateway) WriteStdin(ctx context.Context, sessionID string, handle string, data string) error {
	_, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return err
	}
	defer releaseSession()

	if err := g.sidecarClient.WriteStdin(ctx, podIP, handle, []byte(data)); err != nil {
		return err
	}

	g.touchLastTaskTime(sessionID)
	return nil
}

func normalizeSHA256(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", nil
	}
	if len(value) != 64 {
		return "", fmt.Errorf("sha256 must be 64 hex characters")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("sha256 must be hex: %w", err)
	}
	return value, nil
}

// sanitizeFilePath cleans a file path from the HTTP API and converts it to
// an absolute container path.  The SDK strips the leading "/" before placing
// the path in the URL, so we always prepend "/" to restore the absolute path.
// Traversal beyond "/" is blocked (path.Clean already normalises "..").
func sanitizeFilePath(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("path must not contain NUL bytes")
	}
	clean := path.Clean(strings.TrimSpace(strings.ReplaceAll(p, "\\", "/")))
	if clean == "." || clean == "/" || clean == "" {
		return "", fmt.Errorf("path is required")
	}
	return clean, nil
}
