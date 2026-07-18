package gateway

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// CheckpointStore persists per-step checkpoint tars on a shared filesystem
// (typically a ReadWriteMany NAS PVC) so that fork can reconstruct a
// session's filesystem state after the source sandbox has been deleted.
//
// Directory layout:
//
//	{basePath}/{sessionID}/step-{N}.tar   (one incremental tar per checkpoint step)
type CheckpointStore struct {
	basePath string
}

// NewCheckpointStore creates a store rooted at basePath.
// The directory must already exist (mounted by the deployment).
func NewCheckpointStore(basePath string) *CheckpointStore {
	return &CheckpointStore{basePath: basePath}
}

func (s *CheckpointStore) stepPath(sessionID string, checkpointStep int) string {
	return filepath.Join(s.basePath, sessionID, fmt.Sprintf("step-%d.tar", checkpointStep))
}

// Save writes a single step's incremental tar to persistent storage.
// Writes to a temp file first and renames for atomicity.
func (s *CheckpointStore) Save(sessionID string, checkpointStep int, data io.Reader) error {
	dir := filepath.Join(s.basePath, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create checkpoint dir: %w", err)
	}
	tmpFile, err := os.CreateTemp(dir, ".step-*.tar.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := io.Copy(tmpFile, data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write checkpoint data: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	dst := s.stepPath(sessionID, checkpointStep)
	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename checkpoint file: %w", err)
	}
	return nil
}

// Load returns a reader for a single step's incremental tar.
func (s *CheckpointStore) Load(sessionID string, checkpointStep int) (io.ReadCloser, error) {
	return os.Open(s.stepPath(sessionID, checkpointStep))
}

// HasStep reports whether a step tar exists in the store.
func (s *CheckpointStore) HasStep(sessionID string, checkpointStep int) bool {
	_, err := os.Stat(s.stepPath(sessionID, checkpointStep))
	return err == nil
}

// ListSteps returns sorted checkpoint step numbers available for a session.
func (s *CheckpointStore) ListSteps(sessionID string) ([]int, error) {
	dir := filepath.Join(s.basePath, sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var steps []int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "step-") || !strings.HasSuffix(name, ".tar") {
			continue
		}
		numStr := strings.TrimSuffix(strings.TrimPrefix(name, "step-"), ".tar")
		n, err := strconv.Atoi(numStr)
		if err != nil {
			continue
		}
		steps = append(steps, n)
	}
	sort.Ints(steps)
	return steps, nil
}

// LoadCombined merges per-step tars for steps 1..throughStep into a single
// tar file and returns the path to a temp file containing the result.
// Later steps override earlier entries for the same path, matching the
// sidecar's handleCombinedCheckpoint semantics. The caller must remove the
// returned file when done.
func (s *CheckpointStore) LoadCombined(sessionID string, throughStep int) (string, error) {
	steps, err := s.ListSteps(sessionID)
	if err != nil {
		return "", fmt.Errorf("list steps: %w", err)
	}
	var relevant []int
	for _, step := range steps {
		if step <= throughStep {
			relevant = append(relevant, step)
		}
	}
	if len(relevant) == 0 {
		return "", fmt.Errorf("no checkpoint steps found for session %s through step %d", sessionID, throughStep)
	}

	type mergedEntry struct {
		header *tar.Header
		data   []byte
	}
	merged := make(map[string]mergedEntry)

	for _, step := range relevant {
		f, err := s.Load(sessionID, step)
		if err != nil {
			return "", fmt.Errorf("open step %d: %w", step, err)
		}
		tr := tar.NewReader(f)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				f.Close()
				return "", fmt.Errorf("read step %d tar: %w", step, err)
			}
			var data []byte
			if hdr.Typeflag == tar.TypeReg && hdr.Size > 0 {
				data, err = io.ReadAll(tr)
				if err != nil {
					f.Close()
					return "", fmt.Errorf("read step %d entry %s: %w", step, hdr.Name, err)
				}
			}
			merged[hdr.Name] = mergedEntry{header: hdr, data: data}
		}
		f.Close()
	}

	if len(merged) == 0 {
		return "", fmt.Errorf("no files in checkpoint range for session %s", sessionID)
	}

	paths := make([]string, 0, len(merged))
	for p := range merged {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	tmpFile, err := os.CreateTemp("", "arl-checkpoint-combined-*.tar")
	if err != nil {
		return "", fmt.Errorf("create combined temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	tw := tar.NewWriter(tmpFile)
	for _, p := range paths {
		e := merged[p]
		if err := tw.WriteHeader(e.header); err != nil {
			tw.Close()
			tmpFile.Close()
			os.Remove(tmpPath)
			return "", fmt.Errorf("write combined tar header %s: %w", p, err)
		}
		if len(e.data) > 0 {
			if _, err := tw.Write(e.data); err != nil {
				tw.Close()
				tmpFile.Close()
				os.Remove(tmpPath)
				return "", fmt.Errorf("write combined tar data %s: %w", p, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("close combined tar: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close combined temp file: %w", err)
	}
	return tmpPath, nil
}

// Cleanup removes all persisted checkpoint data for a session.
func (s *CheckpointStore) Cleanup(sessionID string) error {
	dir := filepath.Join(s.basePath, sessionID)
	return os.RemoveAll(dir)
}
