package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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

func TestClientPoolDeleteAndDestroyUseDistinctEndpoints(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	if err := client.DeletePool("pool-1"); err != nil {
		t.Fatalf("DeletePool returned error: %v", err)
	}
	if err := client.DestroyPool("pool-1"); err != nil {
		t.Fatalf("DestroyPool returned error: %v", err)
	}

	want := []string{
		"DELETE /v1/pools/pool-1",
		"POST /v1/pools/pool-1/destroy",
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("seen endpoints = %#v, want %#v", seen, want)
	}
}

func TestClientListMethodsPassQueryFilters(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	if _, err := client.ListSessions(SessionListOptions{Profile: "cpu", ExperimentID: "exp-1", Status: "active", Limit: 25, Cursor: "gw-1"}); err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if _, err := client.ListPools(PoolListOptions{IncludeStopped: true}); err != nil {
		t.Fatalf("ListPools returned error: %v", err)
	}

	want := []string{
		"/v1/sessions?cursor=gw-1&experiment=exp-1&limit=25&profile=cpu&status=active",
		"/v1/pools?includeStopped=true",
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("seen requests = %#v, want %#v", seen, want)
	}
}

func TestClientSummaryUsesCompactEndpoint(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Method + " " + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sessions":3,"managedSessions":2,"pools":1,"readyReplicas":4,"allocatedReplicas":2,"experiments":1}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	summary, err := client.Summary()
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if seen != "GET /v1/summary" {
		t.Fatalf("request = %q, want GET /v1/summary", seen)
	}
	if summary.Sessions != 3 || summary.Pools != 1 || summary.Experiments != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestClientCreateSessionDoesNotExposeCapacityBypassPolicy(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions" {
			t.Fatalf("request = %s %s, want POST /v1/sessions", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"gw-1","sandboxName":"gw-1","namespace":"default","podIP":"10.0.0.1","podName":"pod-1","createdAt":"2026-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	if _, err := client.CreateSession(CreateSessionRequest{Image: "python:3.12"}); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	if _, ok := bodies[0]["allowColdStart"]; ok {
		t.Fatalf("create session body included allowColdStart: %#v", bodies[0])
	}
}

func TestClientCreateRequestsPassAllocationTimeout(t *testing.T) {
	var bodies []map[string]any
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if r.URL.Path == "/v1/managed/sessions" {
			_, _ = w.Write([]byte(`{"id":"gw-2","sandboxName":"gw-2","namespace":"default","podIP":"10.0.0.2","podName":"pod-2","experimentId":"exp","managed":true,"createdAt":"2026-01-01T00:00:00Z"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"gw-1","sandboxName":"gw-1","namespace":"default","podIP":"10.0.0.1","podName":"pod-1","createdAt":"2026-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	noLimit := 0
	tenMinutes := 600
	if _, err := client.CreateSession(CreateSessionRequest{Image: "python:3.12", AllocationTimeoutSeconds: &noLimit}); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if _, err := client.CreateManagedSession(CreateManagedSessionRequest{Image: "python:3.12", ExperimentID: "exp", AllocationTimeoutSeconds: &tenMinutes}); err != nil {
		t.Fatalf("CreateManagedSession returned error: %v", err)
	}

	if strings.Join(paths, ",") != "/v1/sessions,/v1/managed/sessions" {
		t.Fatalf("paths = %#v", paths)
	}
	if got := bodies[0]["allocationTimeoutSeconds"]; got != float64(0) {
		t.Fatalf("session allocationTimeoutSeconds = %#v, want 0", got)
	}
	if got := bodies[1]["allocationTimeoutSeconds"]; got != float64(600) {
		t.Fatalf("managed allocationTimeoutSeconds = %#v, want 600", got)
	}
}

func TestAllocationTimeoutSecondsFromDuration(t *testing.T) {
	got, err := allocationTimeoutSecondsFromDuration(1500 * time.Millisecond)
	if err != nil {
		t.Fatalf("allocationTimeoutSecondsFromDuration returned error: %v", err)
	}
	if got == nil || *got != 2 {
		t.Fatalf("allocation timeout seconds = %#v, want 2", got)
	}

	got, err = allocationTimeoutSecondsFromDuration(0)
	if err != nil {
		t.Fatalf("allocationTimeoutSecondsFromDuration(0) returned error: %v", err)
	}
	if got == nil || *got != 0 {
		t.Fatalf("allocation timeout seconds = %#v, want 0", got)
	}

	if _, err := allocationTimeoutSecondsFromDuration(-time.Second); err == nil {
		t.Fatal("allocationTimeoutSecondsFromDuration(-1s) returned nil error")
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
	filtered := filterSessions(nil, "missing-profile", "", "")
	if filtered == nil {
		t.Fatal("filterSessions returned nil, want empty non-nil slice")
	}
	if len(filtered) != 0 {
		t.Fatalf("filterSessions returned %d item(s), want 0", len(filtered))
	}
}
