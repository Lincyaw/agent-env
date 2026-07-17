package sidecar

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type checkpointListResponse struct {
	Steps []int `json:"steps"`
}

// handleListCheckpoints returns available checkpoint step numbers.
func (s *Server) handleListCheckpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	steps, err := s.listCheckpointSteps()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(checkpointListResponse{Steps: steps})
}

// handleGetCheckpoint streams a tar of one step's upper dir.
// Path: /v1/checkpoints/{step}
func (s *Server) handleGetCheckpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	stepStr := strings.TrimPrefix(r.URL.Path, "/v1/checkpoints/")
	step, err := strconv.Atoi(stepStr)
	if err != nil || step <= 0 {
		http.Error(w, "invalid step number", http.StatusBadRequest)
		return
	}

	upperDir := filepath.Join(s.checkpointDir, fmt.Sprintf("step-%d", step), "upper")
	if _, err := os.Stat(upperDir); os.IsNotExist(err) {
		http.Error(w, fmt.Sprintf("step %d not found", step), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/x-tar")
	tw := tar.NewWriter(w)
	defer tw.Close()

	if err := tarDirectory(tw, upperDir, ""); err != nil {
		log.Printf("checkpoint tar step %d: %v", step, err)
	}
}

// handleCombinedCheckpoint merges steps 1..N into a single tar.
// Later steps override earlier ones for the same path.
// Query: ?through={step}
func (s *Server) handleCombinedCheckpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	throughStr := r.URL.Query().Get("through")
	if throughStr == "" {
		http.Error(w, "through query parameter is required", http.StatusBadRequest)
		return
	}
	through, err := strconv.Atoi(throughStr)
	if err != nil || through <= 0 {
		http.Error(w, "invalid through parameter", http.StatusBadRequest)
		return
	}

	steps, err := s.listCheckpointSteps()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter steps <= through
	var relevant []int
	for _, step := range steps {
		if step <= through {
			relevant = append(relevant, step)
		}
	}
	if len(relevant) == 0 {
		http.Error(w, "no checkpoint steps found", http.StatusNotFound)
		return
	}

	// Build merged file map: path -> {step, full filesystem path}
	// Later steps override earlier ones.
	type fileEntry struct {
		step    int
		absPath string
		info    os.FileInfo
	}
	merged := make(map[string]fileEntry)

	for _, step := range relevant {
		upperDir := filepath.Join(s.checkpointDir, fmt.Sprintf("step-%d", step), "upper")
		if err := filepath.Walk(upperDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(upperDir, path)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			merged[rel] = fileEntry{step: step, absPath: path, info: info}
			return nil
		}); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			log.Printf("checkpoint walk step %d: %v", step, err)
		}
	}

	if len(merged) == 0 {
		http.Error(w, "no files in checkpoint range", http.StatusNotFound)
		return
	}

	// Sort paths for deterministic output
	paths := make([]string, 0, len(merged))
	for p := range merged {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	w.Header().Set("Content-Type", "application/x-tar")
	tw := tar.NewWriter(w)
	defer tw.Close()

	for _, rel := range paths {
		entry := merged[rel]
		if err := addTarEntry(tw, entry.absPath, rel, entry.info); err != nil {
			log.Printf("checkpoint combined tar %s: %v", rel, err)
			return
		}
	}
}

func (s *Server) listCheckpointSteps() ([]int, error) {
	entries, err := os.ReadDir(s.checkpointDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read checkpoint dir: %w", err)
	}

	var steps []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "step-") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(name, "step-"))
		if err != nil {
			continue
		}
		steps = append(steps, n)
	}
	sort.Ints(steps)
	return steps, nil
}

// tarDirectory writes all files under dir into the tar writer
// with paths relative to prefix.
func tarDirectory(tw *tar.Writer, dir string, prefix string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.Join(prefix, rel)
		return addTarEntry(tw, path, name, info)
	})
}

func addTarEntry(tw *tar.Writer, absPath, name string, info os.FileInfo) error {
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = name

	if info.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(absPath)
		if err != nil {
			return err
		}
		header.Linkname = link
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	if info.IsDir() || !info.Mode().IsRegular() {
		return nil
	}

	f, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(tw, f)
	return err
}
