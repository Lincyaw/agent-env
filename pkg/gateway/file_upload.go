package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/Lincyaw/agent-env/pkg/audit"
)

const uploadFileStepName = "upload_file"

type uploadFileAuditInput struct {
	Path      string `json:"path"`
	Encoding  string `json:"encoding"`
	SizeBytes int    `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

func (g *Gateway) UploadFile(ctx context.Context, sessionID string, req UploadFileRequest) (*UploadFileResponse, error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	relPath, payload, err := normalizeUploadFileRequest(req)
	if err != nil {
		return nil, err
	}
	auditInput := marshalUploadFileAuditInput(relPath, payload, normalizedEncoding(req.Encoding))
	replayInput := mustJSONMarshal(UploadFileRequest{
		Path:     relPath,
		Content:  req.Content,
		Encoding: normalizedEncoding(req.Encoding),
	})

	s.mu.RLock()
	podIP := s.Info.PodIP
	s.mu.RUnlock()

	start := time.Now()
	written, err := g.sidecarClient.WriteFile(ctx, podIP, relPath, payload)

	result := StepResult{
		Name:      uploadFileStepName,
		Timestamp: start,
		Input:     auditInput,
		Output: StepOutput{
			ExitCode: 0,
		},
	}
	if err != nil {
		result.Output.Stderr = err.Error()
		result.Output.ExitCode = 1
	} else {
		result.Output.Stdout = fmt.Sprintf("uploaded %d bytes to %s", written, relPath)
	}
	result.DurationMs = time.Since(start).Milliseconds()

	stepRecord := StepRecord{
		Name:        result.Name,
		Input:       result.Input,
		ReplayInput: replayInput,
		Output:      result.Output,
		DurationMs:  result.DurationMs,
		Timestamp:   result.Timestamp,
	}
	globalIdx := s.History.Add(stepRecord)
	result.Index = globalIdx
	result.SnapshotID = fmt.Sprintf("%d", globalIdx)

	if g.trajCh != nil {
		obsJSON, _ := json.Marshal(result.Output)
		entry := audit.TrajectoryEntry{
			SessionID:   sessionID,
			Step:        result.Index,
			Name:        result.Name,
			Action:      result.Input,
			Observation: obsJSON,
			SnapshotID:  result.SnapshotID,
			DurationMs:  result.DurationMs,
			Timestamp:   result.Timestamp,
		}
		select {
		case g.trajCh <- entry:
		default:
		}
	}

	g.touchLastTaskTime(sessionID)

	if err != nil {
		return nil, err
	}

	return &UploadFileResponse{
		Path:         relPath,
		BytesWritten: int(written),
	}, nil
}

func (g *Gateway) DownloadFile(ctx context.Context, sessionID string, filePath string) ([]byte, error) {
	s, ok := g.store.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	relPath, err := sanitizeUploadPath(filePath)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	podIP := s.Info.PodIP
	s.mu.RUnlock()

	content, err := g.sidecarClient.ReadFile(ctx, podIP, relPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	g.touchLastTaskTime(sessionID)
	return content, nil
}

func normalizeUploadFileRequest(req UploadFileRequest) (string, []byte, error) {
	relPath, err := sanitizeUploadPath(req.Path)
	if err != nil {
		return "", nil, err
	}

	switch normalizedEncoding(req.Encoding) {
	case "text":
		return relPath, []byte(req.Content), nil
	case "base64":
		payload, err := base64.StdEncoding.DecodeString(req.Content)
		if err != nil {
			return "", nil, fmt.Errorf("decode base64 content: %w", err)
		}
		return relPath, payload, nil
	default:
		return "", nil, fmt.Errorf("unsupported encoding %q", req.Encoding)
	}
}

func normalizedEncoding(encoding string) string {
	if encoding == "" {
		return "text"
	}
	return strings.ToLower(encoding)
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

func mustJSONMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func marshalUploadFileAuditInput(relPath string, payload []byte, encoding string) json.RawMessage {
	sum := sha256.Sum256(payload)
	return mustJSONMarshal(uploadFileAuditInput{
		Path:      relPath,
		Encoding:  encoding,
		SizeBytes: len(payload),
		SHA256:    hex.EncodeToString(sum[:]),
	})
}
