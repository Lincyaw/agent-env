package gateway

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"sort"
	"strings"
	"testing"
)

// emptyTar is what a step that changed nothing on disk produces: a valid
// archive with no members, 1024 bytes of zero blocks.
func emptyTar(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := tar.NewWriter(&buf).Close(); err != nil {
		t.Fatalf("build empty tar: %v", err)
	}
	return buf.Bytes()
}

func TestLoadCombinedAcceptsAStepThatChangedNothing(t *testing.T) {
	store := NewCheckpointStore(t.TempDir())
	blank := emptyTar(t)
	if len(blank) != 1024 {
		t.Fatalf("expected a 1024-byte empty tar, got %d", len(blank))
	}
	if err := store.Save("sess-1", 1, bytes.NewReader(blank)); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Forking such a step works from a live sandbox, which extracts whatever it
	// is given. Rejecting it here made the same fork fail once the source was
	// gone.
	path, err := store.LoadCombined("sess-1", 1)
	if err != nil {
		t.Fatalf("an empty-but-valid checkpoint must load: %v", err)
	}
	defer os.Remove(path)

	fi, err := os.Stat(path)
	if err != nil || fi.Size() != int64(len(blank)) {
		t.Errorf("loaded tar is %v bytes, want %d", fi.Size(), len(blank))
	}
	// It must still be an archive tar can read.
	f, _ := os.Open(path)
	defer f.Close()
	if _, err := tar.NewReader(f).Next(); err == nil {
		t.Error("expected no entries in the archive")
	}
}

func TestLoadCombinedRejectsATruncatedTar(t *testing.T) {
	store := NewCheckpointStore(t.TempDir())
	if err := store.Save("sess-1", 1, bytes.NewReader(nil)); err != nil {
		t.Fatalf("save: %v", err)
	}

	_, err := store.LoadCombined("sess-1", 1)
	if err == nil {
		t.Fatal("a zero-byte file has no archive terminator and must be refused")
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("error should say the file is truncated, got: %v", err)
	}
}

// saveStep stores a step tar holding one entry per name.
func saveStep(t *testing.T, store *CheckpointStore, step int, contents map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	names := make([]string, 0, len(contents))
	for name := range contents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		body := contents[name]
		if err := w.WriteHeader(&tar.Header{Name: name, Size: int64(len(body))}); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("write body %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close step %d tar: %v", step, err)
	}
	if err := store.Save("sess-1", step, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("save step %d: %v", step, err)
	}
}

func readAll(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open combined: %v", err)
	}
	defer f.Close()
	got := make(map[string]string)
	tr := tar.NewReader(f)
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		body, _ := io.ReadAll(tr)
		if _, dup := got[h.Name]; dup {
			t.Errorf("%s appears twice in the combined tar", h.Name)
		}
		got[h.Name] = string(body)
	}
	return got
}

func TestLoadCombinedMergesStepDeltas(t *testing.T) {
	store := NewCheckpointStore(t.TempDir())
	// Each step tar holds only that step's overlay delta, so reconstructing
	// state through step 3 means merging steps 1..3 -- not picking one of them.
	saveStep(t, store, 1, map[string]string{"a.txt": "a1", "keep.txt": "kept"})
	saveStep(t, store, 2, map[string]string{"b.txt": "b2"})
	saveStep(t, store, 3, map[string]string{"a.txt": "a3"})
	saveStep(t, store, 4, map[string]string{"late.txt": "too-late"})

	path, err := store.LoadCombined("sess-1", 3)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer os.Remove(path)

	got := readAll(t, path)
	want := map[string]string{"a.txt": "a3", "b.txt": "b2", "keep.txt": "kept"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for name, body := range want {
		if got[name] != body {
			t.Errorf("%s = %q, want %q", name, got[name], body)
		}
	}
}

func TestLoadCombinedRejectsAMissingStep(t *testing.T) {
	store := NewCheckpointStore(t.TempDir())
	// Step 2 never persisted. Its writes exist in no other tar, so a merge
	// would silently produce a filesystem missing whatever step 2 wrote.
	saveStep(t, store, 1, map[string]string{"a.txt": "a1"})
	saveStep(t, store, 3, map[string]string{"c.txt": "c3"})

	_, err := store.LoadCombined("sess-1", 3)
	if err == nil {
		t.Fatal("a gap in the step sequence must be refused")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should name the missing steps, got: %v", err)
	}
}
