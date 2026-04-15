package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/Lincyaw/agent-env/pkg/client"
)

func TestNormalizeUploadFileRequestBase64(t *testing.T) {
	req := UploadFileRequest{
		Path:     "./nested/hello.bin",
		Content:  base64.StdEncoding.EncodeToString([]byte("hello")),
		Encoding: "base64",
	}

	relPath, payload, err := normalizeUploadFileRequest(req)
	if err != nil {
		t.Fatalf("normalizeUploadFileRequest returned error: %v", err)
	}
	if relPath != "nested/hello.bin" {
		t.Fatalf("relPath = %q, want %q", relPath, "nested/hello.bin")
	}
	if string(payload) != "hello" {
		t.Fatalf("payload = %q, want %q", string(payload), "hello")
	}
}

func TestSanitizeUploadPathRejectsEscape(t *testing.T) {
	_, err := sanitizeUploadPath("../secret.txt")
	if err == nil {
		t.Fatal("sanitizeUploadPath accepted parent traversal")
	}
}

func TestUploadFileUsesNativeWritePathAndRecordsHistory(t *testing.T) {
	store := NewMemoryStore()
	store.Set("sess-1", &session{
		Info:    SessionInfo{ID: "sess-1", PodIP: "10.0.0.1"},
		History: NewStepHistory(),
	})

	var gotPodIP string
	var gotPath string
	var gotContent []byte

	gw := &Gateway{
		store: store,
		sidecarClient: &client.MockSidecarClient{
			WriteFileFunc: func(ctx context.Context, podIP string, path string, content []byte) (int64, error) {
				gotPodIP = podIP
				gotPath = path
				gotContent = append([]byte(nil), content...)
				return int64(len(content)), nil
			},
		},
	}

	resp, err := gw.UploadFile(context.Background(), "sess-1", UploadFileRequest{
		Path:    "nested/demo.txt",
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("UploadFile returned error: %v", err)
	}
	if resp.BytesWritten != 5 {
		t.Fatalf("BytesWritten = %d, want %d", resp.BytesWritten, 5)
	}
	if resp.Path != "nested/demo.txt" {
		t.Fatalf("Path = %q, want %q", resp.Path, "nested/demo.txt")
	}
	if gotPodIP != "10.0.0.1" {
		t.Fatalf("podIP = %q, want %q", gotPodIP, "10.0.0.1")
	}
	if gotPath != "nested/demo.txt" {
		t.Fatalf("path = %q, want %q", gotPath, "nested/demo.txt")
	}
	if string(gotContent) != "hello" {
		t.Fatalf("content = %q, want %q", string(gotContent), "hello")
	}

	sess, ok := store.Get("sess-1")
	if !ok {
		t.Fatal("session missing after UploadFile")
	}
	record := sess.History.GetAll()
	if len(record) != 1 {
		t.Fatalf("history length = %d, want %d", len(record), 1)
	}
	if record[0].Name != uploadFileStepName {
		t.Fatalf("history step name = %q, want %q", record[0].Name, uploadFileStepName)
	}
	if string(record[0].Input) == "" {
		t.Fatal("history input is empty")
	}
	if string(record[0].ReplayInput) == "" {
		t.Fatal("replay input is empty")
	}
	if string(record[0].Input) == string(record[0].ReplayInput) {
		t.Fatal("history input unexpectedly retains the replay payload")
	}
	if string(record[0].Input) == `{"path":"nested/demo.txt","content":"hello","encoding":"text"}` {
		t.Fatal("history input unexpectedly includes raw content")
	}

	var auditInput uploadFileAuditInput
	if err := json.Unmarshal(record[0].Input, &auditInput); err != nil {
		t.Fatalf("unmarshal audit input: %v", err)
	}
	if auditInput.Path != "nested/demo.txt" {
		t.Fatalf("audit path = %q, want %q", auditInput.Path, "nested/demo.txt")
	}
	if auditInput.Encoding != "text" {
		t.Fatalf("audit encoding = %q, want %q", auditInput.Encoding, "text")
	}
	if auditInput.SizeBytes != 5 {
		t.Fatalf("audit size = %d, want %d", auditInput.SizeBytes, 5)
	}

	var replayInput UploadFileRequest
	if err := json.Unmarshal(record[0].ReplayInput, &replayInput); err != nil {
		t.Fatalf("unmarshal replay input: %v", err)
	}
	if replayInput.Content != "hello" {
		t.Fatalf("replay content = %q, want %q", replayInput.Content, "hello")
	}
}

func TestRedisSessionDataRoundTripsReplayInput(t *testing.T) {
	history := NewStepHistory()
	history.Add(StepRecord{
		Name:        uploadFileStepName,
		Input:       json.RawMessage(`{"path":"nested/demo.txt","encoding":"text","size_bytes":5,"sha256":"abc"}`),
		ReplayInput: json.RawMessage(`{"path":"nested/demo.txt","content":"hello","encoding":"text"}`),
	})

	data := sessionToRedisData(&session{
		Info:    SessionInfo{ID: "sess-1"},
		History: history,
	})
	if len(data.HistoryReplayInputs) != 1 {
		t.Fatalf("HistoryReplayInputs length = %d, want %d", len(data.HistoryReplayInputs), 1)
	}

	restored := redisDataToSession(data)
	records := restored.History.GetAll()
	if len(records) != 1 {
		t.Fatalf("restored history length = %d, want %d", len(records), 1)
	}
	if string(records[0].Input) == string(records[0].ReplayInput) {
		t.Fatal("restored history input unexpectedly matches replay payload")
	}
	if string(records[0].ReplayInput) != `{"path":"nested/demo.txt","content":"hello","encoding":"text"}` {
		t.Fatalf("restored replay input = %s", string(records[0].ReplayInput))
	}
}
