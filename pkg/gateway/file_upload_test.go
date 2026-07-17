package gateway

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Lincyaw/agent-env/pkg/client"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

func TestSanitizeFilePathRejectsTraversal(t *testing.T) {
	_, err := sanitizeFilePath("../secret.txt")
	if err == nil {
		t.Fatal("sanitizeFilePath accepted parent traversal")
	}
}

func TestSanitizeFilePathAcceptsAbsolute(t *testing.T) {
	got, err := sanitizeFilePath("repo/posthog/file.py")
	if err != nil {
		t.Fatalf("sanitizeFilePath rejected absolute path: %v", err)
	}
	if got != "/repo/posthog/file.py" {
		t.Fatalf("got %q, want /repo/posthog/file.py", got)
	}
}

func TestNormalizeSHA256RejectsInvalidValue(t *testing.T) {
	_, err := normalizeSHA256("not-a-sha")
	if err == nil {
		t.Fatal("normalizeSHA256 accepted invalid checksum")
	}
}

func TestUploadFileStreamsContentAndRecordsHistory(t *testing.T) {
	store := NewMemoryStore()
	store.Set("sess-1", &session{
		Info: SessionInfo{
			ID:        "sess-1",
			Namespace: "arl",
			PodName:   "pod-1",
			PodIP:     "10.0.0.1",
		},
		History: NewStepHistory(),
	})

	var gotPodIP string
	var gotPath string
	var gotContent bytes.Buffer
	var gotChecksum string

	gw := &Gateway{
		runtimeAllocator: staticRuntimeAllocator{allocation: RuntimeAllocation{
			Backend:     runtimeBackendSandboxClaim,
			Namespace:   "arl",
			PodName:     "pod-1",
			PodIP:       "10.0.0.1",
			ClaimName:   "claim-1",
			SandboxName: "sandbox-1",
		}},
		store: store,
		sidecarClient: &client.MockSidecarClient{
			WriteFileFunc: func(ctx context.Context, podIP string, path string, content io.Reader, expectedSHA256 string) (*interfaces.FileWriteResult, error) {
				gotPodIP = podIP
				gotPath = path
				gotChecksum = expectedSHA256
				if _, err := io.Copy(&gotContent, content); err != nil {
					return nil, err
				}
				return &interfaces.FileWriteResult{
					Path:         path,
					BytesWritten: int64(gotContent.Len()),
					SHA256:       "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
				}, nil
			},
		},
	}

	resp, err := gw.UploadFile(
		context.Background(),
		"sess-1",
		"nested/demo.txt",
		strings.NewReader("hello"),
		"2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
	)
	if err != nil {
		t.Fatalf("UploadFile returned error: %v", err)
	}
	if resp.BytesWritten != 5 {
		t.Fatalf("BytesWritten = %d, want %d", resp.BytesWritten, 5)
	}
	if resp.Path != "/nested/demo.txt" {
		t.Fatalf("Path = %q, want %q", resp.Path, "/nested/demo.txt")
	}
	if gotPodIP != "10.0.0.1" {
		t.Fatalf("podIP = %q, want %q", gotPodIP, "10.0.0.1")
	}
	if gotPath != "/nested/demo.txt" {
		t.Fatalf("path = %q, want %q", gotPath, "/nested/demo.txt")
	}
	if gotContent.String() != "hello" {
		t.Fatalf("content = %q, want %q", gotContent.String(), "hello")
	}
	if gotChecksum != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("checksum = %q", gotChecksum)
	}

	sess, ok := store.Get("sess-1")
	if !ok {
		t.Fatal("session missing after UploadFile")
	}
	if got := sess.History.Len(); got != 1 {
		t.Fatalf("history length = %d, want 1", got)
	}
	records := sess.History.GetAll()
	if records[0].Name != "upload_file" {
		t.Fatalf("history record name = %q, want %q", records[0].Name, "upload_file")
	}
}

func TestDownloadFileStreamsToWriter(t *testing.T) {
	store := NewMemoryStore()
	store.Set("sess-1", &session{
		Info: SessionInfo{
			ID:        "sess-1",
			Namespace: "arl",
			PodName:   "pod-1",
			PodIP:     "10.0.0.1",
		},
		History: NewStepHistory(),
	})

	gw := &Gateway{
		runtimeAllocator: staticRuntimeAllocator{allocation: RuntimeAllocation{
			Backend:     runtimeBackendSandboxClaim,
			Namespace:   "arl",
			PodName:     "pod-1",
			PodIP:       "10.0.0.1",
			ClaimName:   "claim-1",
			SandboxName: "sandbox-1",
		}},
		store: store,
		sidecarClient: &client.MockSidecarClient{
			ReadFileFunc: func(ctx context.Context, podIP string, path string, dst io.Writer) (*interfaces.FileReadResult, error) {
				if podIP != "10.0.0.1" {
					t.Fatalf("podIP = %q, want 10.0.0.1", podIP)
				}
				if path != "/nested/demo.txt" {
					t.Fatalf("path = %q, want /nested/demo.txt", path)
				}
				if _, err := io.WriteString(dst, "hello"); err != nil {
					return nil, err
				}
				return &interfaces.FileReadResult{Path: path, SizeBytes: 5}, nil
			},
		},
	}

	var out bytes.Buffer
	result, err := gw.DownloadFile(context.Background(), "sess-1", "nested/demo.txt", &out)
	if err != nil {
		t.Fatalf("DownloadFile returned error: %v", err)
	}
	if out.String() != "hello" {
		t.Fatalf("downloaded content = %q, want hello", out.String())
	}
	if result.SizeBytes != 5 {
		t.Fatalf("size = %d, want 5", result.SizeBytes)
	}
}

type staticRuntimeAllocator struct {
	allocation RuntimeAllocation
}

func (a staticRuntimeAllocator) Start(ctx context.Context) error { return nil }

func (a staticRuntimeAllocator) Stop() {}

func (a staticRuntimeAllocator) Allocate(ctx context.Context, req RuntimeAllocateRequest) (*RuntimeAllocation, error) {
	allocation := a.allocation
	return &allocation, nil
}

func (a staticRuntimeAllocator) Release(ctx context.Context, allocation RuntimeAllocation) error {
	return nil
}

func (a staticRuntimeAllocator) Resolve(ctx context.Context, allocation RuntimeAllocation, sessionID string) (*RuntimeAllocation, error) {
	resolved := a.allocation
	return &resolved, nil
}

func (a staticRuntimeAllocator) Touch(ctx context.Context, allocation RuntimeAllocation, sessionID string, at time.Time, lifecycle RuntimeLifecycle) error {
	return nil
}

func (a staticRuntimeAllocator) DiagnosticStats() map[string]AllocatorPoolStats {
	return nil
}
