package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestClientListMethodsReturnEmptySlices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("null"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")

	sessions, err := client.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if sessions == nil || len(sessions) != 0 {
		t.Fatalf("ListSessions = %#v, want empty non-nil slice", sessions)
	}

	pools, err := client.ListPools()
	if err != nil {
		t.Fatalf("ListPools returned error: %v", err)
	}
	if pools == nil || len(pools) != 0 {
		t.Fatalf("ListPools = %#v, want empty non-nil slice", pools)
	}

	experiments, err := client.ListExperiments()
	if err != nil {
		t.Fatalf("ListExperiments returned error: %v", err)
	}
	if experiments == nil || len(experiments) != 0 {
		t.Fatalf("ListExperiments = %#v, want empty non-nil slice", experiments)
	}

	managed, err := client.ListExperimentSessions("exp")
	if err != nil {
		t.Fatalf("ListExperimentSessions returned error: %v", err)
	}
	if managed == nil || len(managed) != 0 {
		t.Fatalf("ListExperimentSessions = %#v, want empty non-nil slice", managed)
	}
}

func TestUploadLocalFileErrorHintsReversedArguments(t *testing.T) {
	remote, err := os.CreateTemp(t.TempDir(), "remote-*")
	if err != nil {
		t.Fatalf("CreateTemp returned error: %v", err)
	}
	if err := remote.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}

	err = uploadLocalFileError("/workspace/upload.txt", remote.Name(), os.ErrNotExist)
	if err == nil {
		t.Fatal("uploadLocalFileError returned nil")
	}
	for _, want := range []string{
		"arl session upload <id> <local-path> <remote-path>",
		"arguments may be reversed",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want substring %q", err, want)
		}
	}
}

func TestFilterSessionsReturnsEmptySlice(t *testing.T) {
	filtered := filterSessions(nil, "missing-profile", "")
	if filtered == nil {
		t.Fatal("filterSessions returned nil, want empty non-nil slice")
	}
	if len(filtered) != 0 {
		t.Fatalf("filterSessions returned %d item(s), want 0", len(filtered))
	}
}
