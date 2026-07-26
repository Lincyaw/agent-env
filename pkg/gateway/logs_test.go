package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	sandboxcontrollers "sigs.k8s.io/agent-sandbox/controllers"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// logAPIServer stands in for the Kubernetes API server's pods/log subresource
// so the streaming path is exercised end to end: real Clientset, real HTTP,
// real line scanning.
type logAPIServer struct {
	*httptest.Server
	requests chan url.Values
	paths    chan string
}

func newLogAPIServer(t *testing.T, body string) *logAPIServer {
	t.Helper()
	s := &logAPIServer{
		requests: make(chan url.Values, 8),
		paths:    make(chan string, 8),
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Non-blocking: pool tests fan out more requests than the recording
		// buffers hold, and a blocked handler would deadlock the test.
		select {
		case s.paths <- r.URL.Path:
		default:
		}
		select {
		case s.requests <- r.URL.Query():
		default:
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *logAPIServer) clientset(t *testing.T) kubernetes.Interface {
	t.Helper()
	cs, err := kubernetes.NewForConfig(&rest.Config{Host: s.URL})
	if err != nil {
		t.Fatalf("build clientset: %v", err)
	}
	return cs
}

func testSession(podName, namespace string) *session {
	return &session{Info: SessionInfo{PodName: podName, Namespace: namespace}}
}

func collect(t *testing.T, ch <-chan interfaces.LogEntry) []interfaces.LogEntry {
	t.Helper()
	var out []interfaces.LogEntry
	for entry := range ch {
		out = append(out, entry)
	}
	return out
}

func TestStreamSessionLogsReturnsParsedEntries(t *testing.T) {
	api := newLogAPIServer(t, "2026-07-26T06:58:18.123456789Z [INFO] starting executor-agent\n"+
		"2026-07-26T06:58:19.000000000Z [ERROR] iroh endpoint error: boom\n")

	store := NewMemoryStore()
	store.Set("sess-1", testSession("pod-1", "arl"))
	gw := &Gateway{store: store, k8sClientset: api.clientset(t)}

	ch, err := gw.StreamSessionLogs(context.Background(), "sess-1", false, 100)
	if err != nil {
		t.Fatalf("StreamSessionLogs: %v", err)
	}
	entries := collect(t, ch)

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Timestamp != "2026-07-26T06:58:18.123456789Z" {
		t.Errorf("timestamp not split off: %q", entries[0].Timestamp)
	}
	if entries[0].Message != "[INFO] starting executor-agent" {
		t.Errorf("unexpected message: %q", entries[0].Message)
	}
	if entries[0].Level != "info" {
		t.Errorf("expected info level, got %q", entries[0].Level)
	}
	if entries[0].Source != executorContainerName {
		t.Errorf("expected source %q, got %q", executorContainerName, entries[0].Source)
	}
	if entries[1].Level != "error" {
		t.Errorf("expected error level, got %q", entries[1].Level)
	}

	// The request must target the session's pod, in its namespace, asking for
	// the executor container with timestamps and the requested tail.
	if got, want := <-api.paths, "/api/v1/namespaces/arl/pods/pod-1/log"; got != want {
		t.Errorf("request path = %q, want %q", got, want)
	}
	q := <-api.requests
	if q.Get("container") != executorContainerName {
		t.Errorf("container = %q, want %q", q.Get("container"), executorContainerName)
	}
	if q.Get("timestamps") != "true" {
		t.Errorf("timestamps = %q, want true", q.Get("timestamps"))
	}
	if q.Get("tailLines") != "100" {
		t.Errorf("tailLines = %q, want 100", q.Get("tailLines"))
	}
	if q.Get("follow") == "true" {
		t.Errorf("follow should be unset when not requested")
	}
}

func TestStreamSessionLogsFollowAndUntailed(t *testing.T) {
	api := newLogAPIServer(t, "line\n")
	store := NewMemoryStore()
	store.Set("sess-1", testSession("pod-1", "arl"))
	gw := &Gateway{store: store, k8sClientset: api.clientset(t)}

	ch, err := gw.StreamSessionLogs(context.Background(), "sess-1", true, 0)
	if err != nil {
		t.Fatalf("StreamSessionLogs: %v", err)
	}
	collect(t, ch)

	<-api.paths
	q := <-api.requests
	if q.Get("follow") != "true" {
		t.Errorf("follow = %q, want true", q.Get("follow"))
	}
	if _, ok := q["tailLines"]; ok {
		t.Errorf("tailLines should be omitted when tail <= 0, got %q", q.Get("tailLines"))
	}
}

// historicalOnlyStore reports every session as gone from the active set while
// still serving it from history, mirroring a soft-deleted session in Redis.
type historicalOnlyStore struct {
	*MemoryStore
}

func (s historicalOnlyStore) Get(string) (*session, bool) { return nil, false }

func TestStreamSessionLogsFallsBackToHistorical(t *testing.T) {
	api := newLogAPIServer(t, "post-mortem line\n")
	inner := NewMemoryStore()
	inner.Set("sess-1", testSession("pod-1", "arl"))
	gw := &Gateway{store: historicalOnlyStore{inner}, k8sClientset: api.clientset(t)}

	ch, err := gw.StreamSessionLogs(context.Background(), "sess-1", false, 10)
	if err != nil {
		t.Fatalf("deleted session should still stream logs: %v", err)
	}
	if entries := collect(t, ch); len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestStreamSessionLogsErrors(t *testing.T) {
	api := newLogAPIServer(t, "")
	store := NewMemoryStore()
	store.Set("no-pod", testSession("", "arl"))

	tests := []struct {
		name      string
		gw        *Gateway
		sessionID string
		wantMsg   string
	}{
		{
			name:      "missing clientset",
			gw:        &Gateway{store: store},
			sessionID: "no-pod",
			wantMsg:   "no Kubernetes REST client",
		},
		{
			name:      "unknown session",
			gw:        &Gateway{store: store, k8sClientset: api.clientset(t)},
			sessionID: "nope",
			wantMsg:   "not found",
		},
		{
			name:      "session without pod",
			gw:        &Gateway{store: store, k8sClientset: api.clientset(t)},
			sessionID: "no-pod",
			wantMsg:   "no pod bound yet",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.gw.StreamSessionLogs(context.Background(), tc.sessionID, false, 10)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q does not contain %q", err, tc.wantMsg)
			}
		})
	}

	// An unknown session must map to 404, not the old blanket 500.
	gw := &Gateway{store: store, k8sClientset: api.clientset(t)}
	_, err := gw.StreamSessionLogs(context.Background(), "nope", false, 10)
	if got := httpStatusForError(err); got != http.StatusNotFound {
		t.Errorf("httpStatusForError = %d, want 404", got)
	}
}

func TestStreamSessionLogsReportsStreamFailure(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failing.Close()

	cs, err := kubernetes.NewForConfig(&rest.Config{Host: failing.URL})
	if err != nil {
		t.Fatalf("build clientset: %v", err)
	}
	store := NewMemoryStore()
	store.Set("sess-1", testSession("pod-1", "arl"))
	gw := &Gateway{store: store, k8sClientset: cs}

	ch, err := gw.StreamSessionLogs(context.Background(), "sess-1", false, 10)
	if err != nil {
		t.Fatalf("StreamSessionLogs: %v", err)
	}
	entries := collect(t, ch)
	if len(entries) != 1 || entries[0].Level != "error" || entries[0].Source != "gateway" {
		t.Fatalf("expected one gateway error entry, got %+v", entries)
	}
}

func TestStreamSessionLogsStopsWhenContextCancelled(t *testing.T) {
	api := newLogAPIServer(t, "a\nb\nc\n")
	store := NewMemoryStore()
	store.Set("sess-1", testSession("pod-1", "arl"))
	gw := &Gateway{store: store, k8sClientset: api.clientset(t)}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := gw.StreamSessionLogs(ctx, "sess-1", false, 10)
	if err != nil {
		t.Fatalf("StreamSessionLogs: %v", err)
	}
	cancel()
	// Draining must terminate rather than block once the consumer goes away.
	for range ch {
	}
}

func TestStreamSessionLogsDefaultsNamespace(t *testing.T) {
	api := newLogAPIServer(t, "line\n")
	store := NewMemoryStore()
	store.Set("sess-1", testSession("pod-1", ""))
	gw := &Gateway{
		store:        store,
		k8sClientset: api.clientset(t),
		gwConfig:     GatewayConfig{Namespace: "arl-default"},
	}

	ch, err := gw.StreamSessionLogs(context.Background(), "sess-1", false, 10)
	if err != nil {
		t.Fatalf("StreamSessionLogs: %v", err)
	}
	collect(t, ch)

	if got, want := <-api.paths, "/api/v1/namespaces/arl-default/pods/pod-1/log"; got != want {
		t.Errorf("request path = %q, want %q", got, want)
	}
}

func TestParseLogLine(t *testing.T) {
	tests := []struct {
		line    string
		wantTS  string
		wantMsg string
		wantLvl string
	}{
		{
			line:    "2026-07-26T06:58:18Z hello world",
			wantTS:  "2026-07-26T06:58:18Z",
			wantMsg: "hello world",
			wantLvl: "info",
		},
		{
			// No timestamp prefix: the whole line is the message.
			line:    "plain line without timestamp",
			wantTS:  "",
			wantMsg: "plain line without timestamp",
			wantLvl: "info",
		},
		{
			line:    "2026-07-26T06:58:18Z WARN pool nearly full",
			wantTS:  "2026-07-26T06:58:18Z",
			wantMsg: "WARN pool nearly full",
			wantLvl: "warn",
		},
		{
			line:    "2026-07-26T06:58:18Z DEBUG socket accepted",
			wantTS:  "2026-07-26T06:58:18Z",
			wantMsg: "DEBUG socket accepted",
			wantLvl: "debug",
		},
		{
			line:    "",
			wantTS:  "",
			wantMsg: "",
			wantLvl: "info",
		},
	}
	for _, tc := range tests {
		got := parseLogLine(tc.line)
		if got.Timestamp != tc.wantTS || got.Message != tc.wantMsg || got.Level != tc.wantLvl {
			t.Errorf("parseLogLine(%q) = %+v, want ts=%q msg=%q lvl=%q",
				tc.line, got, tc.wantTS, tc.wantMsg, tc.wantLvl)
		}
	}
}

func TestExecutorEnvInjectsRustLog(t *testing.T) {
	tests := []struct {
		name     string
		level    string
		wantEnv  bool
		wantVal  string
		relayURL string
	}{
		{name: "default info", level: "info", wantEnv: true, wantVal: "info"},
		{name: "custom filter", level: "info,executor_agent=debug", wantEnv: true, wantVal: "info,executor_agent=debug"},
		{name: "explicitly disabled", level: "", wantEnv: false},
		{name: "whitespace only", level: "   ", wantEnv: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gw := &Gateway{gwConfig: GatewayConfig{ExecutorLogLevel: tc.level}}
			var found *string
			for _, env := range gw.executorEnv() {
				if env.Name == "RUST_LOG" {
					v := env.Value
					found = &v
				}
			}
			if tc.wantEnv {
				if found == nil {
					t.Fatal("RUST_LOG not injected")
				}
				if *found != tc.wantVal {
					t.Errorf("RUST_LOG = %q, want %q", *found, tc.wantVal)
				}
				return
			}
			if found != nil {
				t.Errorf("RUST_LOG should not be injected, got %q", *found)
			}
		})
	}
}

func poolPod(name, namespace, poolRef string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				sandboxv1beta1.SandboxWarmPoolLabel: sandboxcontrollers.NameHash(poolRef),
			},
		},
	}
}

func newPoolLogGateway(t *testing.T, api *logAPIServer, pods ...client.Object) *Gateway {
	t.Helper()
	scheme := newGatewayTestScheme(t)
	return &Gateway{
		k8sClient:    fake.NewClientBuilder().WithScheme(scheme).WithObjects(pods...).Build(),
		k8sClientset: api.clientset(t),
		gwConfig:     GatewayConfig{Namespace: "arl"},
	}
}

func TestStreamPoolLogsFansInAllPods(t *testing.T) {
	api := newLogAPIServer(t, "2026-07-26T06:58:18Z pool line\n")
	gw := newPoolLogGateway(t, api,
		poolPod("pod-a", "arl", "pool-1"),
		poolPod("pod-b", "arl", "pool-1"),
		poolPod("other", "arl", "pool-2"),
	)

	ch, err := gw.StreamPoolLogs(context.Background(), "pool-1", "arl", false, 50)
	if err != nil {
		t.Fatalf("StreamPoolLogs: %v", err)
	}

	seen := map[string]int{}
	for entry := range ch {
		seen[entry.PodName]++
		if entry.Entry.Message != "pool line" {
			t.Errorf("unexpected message %q", entry.Entry.Message)
		}
	}
	if len(seen) != 2 || seen["pod-a"] != 1 || seen["pod-b"] != 1 {
		t.Fatalf("expected one entry from pod-a and pod-b only, got %+v", seen)
	}
}

func TestStreamPoolLogsSkipsTerminatingPods(t *testing.T) {
	api := newLogAPIServer(t, "line\n")
	terminating := poolPod("pod-dying", "arl", "pool-1")
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	terminating.Finalizers = []string{"test/keep"}

	gw := newPoolLogGateway(t, api, poolPod("pod-a", "arl", "pool-1"), terminating)

	ch, err := gw.StreamPoolLogs(context.Background(), "pool-1", "arl", false, 50)
	if err != nil {
		t.Fatalf("StreamPoolLogs: %v", err)
	}
	for entry := range ch {
		if entry.PodName == "pod-dying" {
			t.Fatal("terminating pod should not be streamed")
		}
	}
}

func TestStreamPoolLogsWarnsWhenTruncated(t *testing.T) {
	api := newLogAPIServer(t, "line\n")
	pods := make([]client.Object, 0, maxPoolLogPods+3)
	for i := 0; i < maxPoolLogPods+3; i++ {
		pods = append(pods, poolPod(fmt.Sprintf("pod-%02d", i), "arl", "pool-1"))
	}
	gw := newPoolLogGateway(t, api, pods...)

	ch, err := gw.StreamPoolLogs(context.Background(), "pool-1", "arl", false, 50)
	if err != nil {
		t.Fatalf("StreamPoolLogs: %v", err)
	}

	streamed := map[string]bool{}
	warned := false
	for entry := range ch {
		if entry.Entry.Source == "gateway" {
			warned = true
			if !strings.Contains(entry.Entry.Message, "3 pod(s) omitted") {
				t.Errorf("warning should name the omitted count, got %q", entry.Entry.Message)
			}
			continue
		}
		streamed[entry.PodName] = true
	}
	if len(streamed) != maxPoolLogPods {
		t.Errorf("streamed %d pods, want %d", len(streamed), maxPoolLogPods)
	}
	if !warned {
		t.Error("truncation must be reported, not silent")
	}
}

func TestStreamPoolLogsErrors(t *testing.T) {
	api := newLogAPIServer(t, "line\n")

	t.Run("no pods", func(t *testing.T) {
		gw := newPoolLogGateway(t, api)
		if _, err := gw.StreamPoolLogs(context.Background(), "pool-1", "arl", false, 50); err == nil ||
			!strings.Contains(err.Error(), "no running pods") {
			t.Fatalf("expected no-pods error, got %v", err)
		}
	})

	t.Run("namespace not allowed", func(t *testing.T) {
		gw := newPoolLogGateway(t, api, poolPod("pod-a", "arl", "pool-1"))
		_, err := gw.StreamPoolLogs(context.Background(), "pool-1", "other-ns", false, 50)
		if err == nil {
			t.Fatal("expected a namespace error")
		}
		if !errors.Is(err, ErrNamespaceNotAllowed) {
			t.Fatalf("expected ErrNamespaceNotAllowed, got %v", err)
		}
	})
}

func TestRewriteIrohAddrKeepsExecutorRelayTruth(t *testing.T) {
	gw := &Gateway{gwConfig: GatewayConfig{IrohRelayExternalURL: "http://relay.example:3340"}}

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "connected relay is translated to the external URL",
			raw:  `{"id":"abc","relay_url":"http://in-cluster:3340","direct_addresses":["10.0.0.1:1"]}`,
			want: "http://relay.example:3340",
		},
		{
			// The executor could not reach a relay; claiming one would point
			// clients at a path that cannot reach the sandbox.
			name: "null relay is left alone",
			raw:  `{"id":"abc","relay_url":null,"direct_addresses":["10.0.0.1:1"]}`,
			want: "",
		},
		{
			name: "absent relay is left alone",
			raw:  `{"id":"abc","direct_addresses":["10.0.0.1:1"]}`,
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := gw.rewriteIrohAddr(tc.raw)

			var parsed map[string]any
			if err := json.Unmarshal([]byte(got), &parsed); err != nil {
				t.Fatalf("result is not JSON: %v", err)
			}
			relay, _ := parsed["relay_url"].(string)
			if relay != tc.want {
				t.Errorf("relay_url = %q, want %q (full: %s)", relay, tc.want, got)
			}
			// Direct addresses must survive the rewrite either way.
			if addrs, ok := parsed["direct_addresses"].([]any); !ok || len(addrs) != 1 {
				t.Errorf("direct_addresses lost in rewrite: %s", got)
			}
		})
	}
}

func TestExecutorEnvNeverCarriesTheAuthMasterSecret(t *testing.T) {
	const master = "super-secret-master-value"
	gw := &Gateway{gwConfig: GatewayConfig{
		GRPCAuthToken:    master,
		ExecutorLogLevel: "info",
		IrohRelayURL:     "http://relay:3340",
	}}

	for _, env := range gw.executorEnv() {
		if strings.Contains(env.Value, master) {
			t.Fatalf("sandbox env %s leaks the auth master secret", env.Name)
		}
		if env.Name == "EXECUTOR_AUTH_REQUIRED" {
			t.Errorf("auth must stay enabled when a master secret is configured")
		}
	}
}

func TestExecutorEnvDisablesAuthWithoutAMasterSecret(t *testing.T) {
	// Local development has no secret; the executor would otherwise refuse
	// every request with no way to authenticate.
	gw := &Gateway{gwConfig: GatewayConfig{}}

	var disabled bool
	for _, env := range gw.executorEnv() {
		if env.Name == "EXECUTOR_AUTH_REQUIRED" && env.Value == "0" {
			disabled = true
		}
	}
	if !disabled {
		t.Error("expected auth to be explicitly disabled when unconfigured")
	}
}
