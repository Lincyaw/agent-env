package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The CLI silently spoke a file API the gateway stopped serving in 7c8e6ff,
// so every upload and download 404'd. These tests pin the request shape to
// what pkg/gateway/router.go actually routes.

func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{base: srv.URL, http: srv.Client()}, srv
}

func TestUploadFileHitsTheRoutedEndpoint(t *testing.T) {
	var gotMethod, gotPath, gotHeaderPath, gotSHA, gotBody string
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotHeaderPath = r.Header.Get("X-ARL-Path")
		gotSHA = r.Header.Get("X-ARL-SHA256")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"path":"/tmp/x","bytesWritten":5,"sha256":"abc"}`))
	}))

	resp, err := client.UploadFile("sess-1", "/tmp/x", strings.NewReader("hello"), "abc")
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/sessions/sess-1/upload-file" {
		t.Errorf("path = %q, want /v1/sessions/sess-1/upload-file", gotPath)
	}
	if gotHeaderPath != "/tmp/x" {
		t.Errorf("X-ARL-Path = %q, want /tmp/x (absolute path must survive)", gotHeaderPath)
	}
	if gotSHA != "abc" {
		t.Errorf("X-ARL-SHA256 = %q, want abc", gotSHA)
	}
	if gotBody != "hello" {
		t.Errorf("body = %q, want hello", gotBody)
	}
	if resp.BytesWritten != 5 {
		t.Errorf("BytesWritten = %d, want 5", resp.BytesWritten)
	}
}

func TestDownloadFileHitsTheRoutedEndpoint(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]string
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte("file-contents"))
	}))

	resp, err := client.DownloadFile("sess-1", "/tmp/x")
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	defer resp.Body.Close()

	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/sessions/sess-1/download-file" {
		t.Errorf("path = %q, want /v1/sessions/sess-1/download-file", gotPath)
	}
	if gotBody["path"] != "/tmp/x" {
		t.Errorf("body path = %q, want /tmp/x", gotBody["path"])
	}
	if body, _ := io.ReadAll(resp.Body); string(body) != "file-contents" {
		t.Errorf("body = %q", body)
	}
}

func TestSessionFilePath(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		// Absolute paths must not be silently rewritten into workspace-relative
		// ones; the gateway resolves what it is given.
		{name: "absolute preserved", in: "/tmp/a.txt", want: "/tmp/a.txt"},
		{name: "relative preserved", in: "data/a.txt", want: "data/a.txt"},
		{name: "cleaned", in: "/tmp//./a.txt", want: "/tmp/a.txt"},
		{name: "backslashes normalized", in: `\tmp\a.txt`, want: "/tmp/a.txt"},
		// Spaces must survive: the path travels in a header, not a URL.
		{name: "spaces kept verbatim", in: "/tmp/my file.txt", want: "/tmp/my file.txt"},
		{name: "empty rejected", in: "   ", wantErr: true},
		{name: "root rejected", in: "/", wantErr: true},
		{name: "escape rejected", in: "../secrets", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sessionFilePath(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("sessionFilePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
