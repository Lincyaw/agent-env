package gateway

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// uploadFileStepName is kept only to skip legacy file-transfer records when
// replaying histories created by older gateway builds.
const uploadFileStepName = "upload_file"

func (g *Gateway) UploadFile(ctx context.Context, sessionID string, filePath string, content io.Reader, expectedSHA256 string) (*UploadFileResponse, error) {
	relPath, err := sanitizeUploadPath(filePath)
	if err != nil {
		return nil, err
	}
	expectedSHA256, err = normalizeSHA256(expectedSHA256)
	if err != nil {
		return nil, err
	}

	_, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	result, err := g.sidecarClient.WriteFile(ctx, podIP, relPath, content, expectedSHA256)
	if err != nil {
		return nil, err
	}

	g.touchLastTaskTime(sessionID)
	return &UploadFileResponse{
		Path:         result.Path,
		BytesWritten: int(result.BytesWritten),
		SHA256:       result.SHA256,
	}, nil
}

func (g *Gateway) DownloadFile(ctx context.Context, sessionID string, filePath string, dst io.Writer) (*interfaces.FileReadResult, error) {
	relPath, err := sanitizeUploadPath(filePath)
	if err != nil {
		return nil, err
	}

	_, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	result, err := g.sidecarClient.ReadFile(ctx, podIP, relPath, dst)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	g.touchLastTaskTime(sessionID)
	return result, nil
}

func (g *Gateway) StatFile(ctx context.Context, sessionID string, filePath string) (*StatResponse, error) {
	relPath, err := sanitizeUploadPath(filePath)
	if err != nil {
		return nil, err
	}

	_, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	result, err := g.sidecarClient.Stat(ctx, podIP, relPath)
	if err != nil {
		return nil, err
	}

	g.touchLastTaskTime(sessionID)
	return &StatResponse{
		Exists:   result.Exists,
		IsDir:    result.IsDir,
		Size:     result.Size,
		Mode:     result.Mode,
		Modified: result.Modified.Format("2006-01-02T15:04:05Z"),
	}, nil
}

func (g *Gateway) ListDir(ctx context.Context, sessionID string, filePath string, recursive bool) (*ListDirResponse, error) {
	relPath, err := sanitizeUploadPath(filePath)
	if err != nil {
		return nil, err
	}

	_, podIP, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	result, err := g.sidecarClient.ListDir(ctx, podIP, relPath, recursive)
	if err != nil {
		return nil, err
	}

	entries := make([]ListDirEntryResponse, len(result.Entries))
	for i, e := range result.Entries {
		entries[i] = ListDirEntryResponse{
			Name:  e.Name,
			IsDir: e.IsDir,
			Size:  e.Size,
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

	if err := g.sidecarClient.WriteStdin(ctx, podIP, handle, data); err != nil {
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

func sanitizeUploadPath(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("path must not contain NUL bytes")
	}
	clean := path.Clean(strings.TrimSpace(strings.ReplaceAll(p, "\\", "/")))
	if clean == "." || clean == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("path must be relative to the workspace")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path must stay within the workspace")
	}
	return clean, nil
}
