package gateway

import (
	"archive/tar"
	"bytes"
	"os"
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

func TestLoadCombinedPrefersTheHighestStepInRange(t *testing.T) {
	store := NewCheckpointStore(t.TempDir())
	for _, step := range []int{1, 2, 5} {
		var buf bytes.Buffer
		w := tar.NewWriter(&buf)
		name := "step"
		w.WriteHeader(&tar.Header{Name: name, Size: int64(step)})
		w.Write(bytes.Repeat([]byte("x"), step))
		w.Close()
		if err := store.Save("sess-1", step, bytes.NewReader(buf.Bytes())); err != nil {
			t.Fatalf("save step %d: %v", step, err)
		}
	}

	path, err := store.LoadCombined("sess-1", 3)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer os.Remove(path)

	f, _ := os.Open(path)
	defer f.Close()
	h, err := tar.NewReader(f).Next()
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	// Each step tar is already cumulative, so step 2 is the answer for
	// "through step 3", not step 5 and not a merge.
	if h.Size != 2 {
		t.Errorf("got the step-%d archive, want step 2", h.Size)
	}
}
