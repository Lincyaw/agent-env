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
// combined checkpoint semantics. The caller must remove the
// returned file when done.
func (s *CheckpointStore) LoadCombined(sessionID string, throughStep int) (string, error) {
	steps, err := s.ListSteps(sessionID)
	if err != nil {
		return "", fmt.Errorf("list steps: %w", err)
	}
	var selected []int
	for _, step := range steps {
		if step <= throughStep {
			selected = append(selected, step)
		}
	}
	if len(selected) == 0 {
		return "", fmt.Errorf("no checkpoint steps found for session %s through step %d", sessionID, throughStep)
	}

	// Steps are overlay deltas, so every one of them is load-bearing: a gap
	// means the files that step wrote are simply absent from the fork. The
	// store used to hold cumulative tars, where the newest file alone carried
	// the whole state and a missing step healed itself, so this check has no
	// predecessor. Persisting is best-effort and asynchronous, which makes
	// gaps rare but real -- fail loudly rather than fork a filesystem that is
	// silently missing a step's worth of writes.
	if missing := missingSteps(selected); len(missing) > 0 {
		return "", fmt.Errorf(
			"checkpoint steps %v missing for session %s (have %d of steps 1..%d); "+
				"cannot reconstruct filesystem state, since each step tar holds only "+
				"that step's changes",
			missing, sessionID, len(selected), selected[len(selected)-1])
	}

	// A path rewritten on many steps appears in each of their tars. Extraction
	// would land on the right content either way because later entries
	// overwrite earlier ones, but emitting every version would put the
	// per-step redundancy straight back into the fork tar. Resolve the winning
	// step per path first, then write each path once.
	winner, err := s.lastWriterPerPath(sessionID, selected)
	if err != nil {
		return "", err
	}

	tmpFile, err := os.CreateTemp("", "arl-checkpoint-combined-*.tar")
	if err != nil {
		return "", fmt.Errorf("create combined temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	fail := func(err error) (string, error) {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", err
	}

	tw := tar.NewWriter(tmpFile)
	for _, step := range selected {
		err := s.forEachEntry(sessionID, step, func(hdr *tar.Header, tr *tar.Reader) error {
			if winner[hdr.Name] != step {
				return nil
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return fmt.Errorf("write header %q: %w", hdr.Name, err)
			}
			if hdr.Typeflag != tar.TypeReg {
				return nil
			}
			if _, err := io.Copy(tw, tr); err != nil {
				return fmt.Errorf("copy %q from step %d: %w", hdr.Name, step, err)
			}
			return nil
		})
		if err != nil {
			return fail(err)
		}
	}
	if err := tw.Close(); err != nil {
		return fail(fmt.Errorf("finish combined tar: %w", err))
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close combined temp file: %w", err)
	}

	// Steps that changed nothing on disk merge into a tar with no members.
	// That is 1024 bytes of zero blocks and extracts cleanly, so it is a
	// legitimate checkpoint rather than corruption; rejecting it once made
	// forking such a step work from a live sandbox and fail from the store.
	return tmpPath, nil
}

// missingSteps reports which numbers are absent from a sorted, ascending step
// list that should cover 1..last.
func missingSteps(steps []int) []int {
	var missing []int
	next := 1
	for _, step := range steps {
		for ; next < step; next++ {
			missing = append(missing, next)
		}
		next = step + 1
	}
	return missing
}

// lastWriterPerPath maps each archived path to the highest step whose tar
// contains it.
func (s *CheckpointStore) lastWriterPerPath(sessionID string, steps []int) (map[string]int, error) {
	winner := make(map[string]int)
	for _, step := range steps {
		err := s.forEachEntry(sessionID, step, func(hdr *tar.Header, _ *tar.Reader) error {
			winner[hdr.Name] = step
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return winner, nil
}

// forEachEntry walks the tar members of one step. The reader passed to fn is
// only valid until fn returns.
func (s *CheckpointStore) forEachEntry(sessionID string, step int, fn func(*tar.Header, *tar.Reader) error) error {
	f, err := os.Open(s.stepPath(sessionID, step))
	if err != nil {
		return fmt.Errorf("open step %d: %w", step, err)
	}
	defer f.Close()

	// A step that changed nothing is a valid 1024-byte archive of zero blocks
	// and must load. A zero-byte file is a different thing -- an interrupted
	// write that left no archive terminator -- and tar.Reader reports it as a
	// clean EOF rather than an error, so it has to be caught here or it passes
	// as "this step wrote nothing".
	if fi, err := f.Stat(); err == nil && fi.Size() == 0 {
		return fmt.Errorf("checkpoint tar for step %d is truncated: zero bytes on disk", step)
	}

	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read step %d tar: %w", step, err)
		}
		if err := fn(hdr, tr); err != nil {
			return err
		}
	}
}

// Cleanup removes all persisted checkpoint data for a session.
func (s *CheckpointStore) Cleanup(sessionID string) error {
	dir := filepath.Join(s.basePath, sessionID)
	return os.RemoveAll(dir)
}
