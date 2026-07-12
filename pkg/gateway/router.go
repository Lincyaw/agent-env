package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// SetupRoutes registers all public gateway routes on mux.
// When authCfg is non-nil and Enabled, every route requires a valid Bearer
// token. Pool management endpoints require the admin role; session and
// execution endpoints require at least the user role.
func SetupRoutes(mux *http.ServeMux, gw *Gateway, authCfg *AuthConfig) {
	user := func(h http.HandlerFunc) http.HandlerFunc { return h }
	admin := func(h http.HandlerFunc) http.HandlerFunc { return h }

	if authCfg != nil && authCfg.Enabled {
		user = func(h http.HandlerFunc) http.HandlerFunc {
			return requireAuth(authCfg, RoleUser, h)
		}
		admin = func(h http.HandlerFunc) http.HandlerFunc {
			return requireAuth(authCfg, RoleAdmin, h)
		}
	}
	route := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, instrumentGatewayRoute(gw, pattern, h))
	}

	// Session management (user role)
	route("POST /v1/sessions", user(handleCreateSession(gw)))
	route("GET /v1/sessions/{id}", user(handleGetSession(gw)))
	route("DELETE /v1/sessions/{id}", user(handleDeleteSession(gw)))
	route("POST /v1/sessions/{id}/suspend", user(handleSuspendSession(gw)))
	route("POST /v1/sessions/{id}/resume", user(handleResumeSession(gw)))

	// Execution (user role)
	route("POST /v1/sessions/{id}/execute", user(handleExecute(gw)))
	route("POST /v1/sessions/{id}/containers/{container}/execute", user(handleExecuteContainer(gw)))
	route("GET /v1/sessions/{id}/operations/{operationID}", user(handleGetExecuteOperation(gw)))
	route("PUT /v1/sessions/{id}/files/{path...}", user(handleUploadFile(gw)))
	route("GET /v1/sessions/{id}/files/{path...}", user(handleDownloadFile(gw)))
	route("GET /v1/sessions/{id}/stat/{path...}", user(handleStat(gw)))
	route("GET /v1/sessions/{id}/ls/{path...}", user(handleListDir(gw)))
	route("POST /v1/sessions/{id}/stdin", user(handleWriteStdin(gw)))
	route("POST /v1/sessions/{id}/restore", user(handleRestore(gw)))
	route("POST /v1/sessions/{id}/replay", user(handleReplay(gw)))

	// Interactive shell — WebSocket (user role; token may come via query param)
	mux.HandleFunc("/v1/sessions/{id}/shell", user(handleShell(gw, authCfg)))

	// TCP tunnel — WebSocket to pod:port relay (user role)
	mux.HandleFunc("/v1/sessions/{id}/tunnel/{port}", user(handleTunnel(gw, authCfg)))

	// History, trajectory, and logs (user role)
	route("GET /v1/sessions/{id}/history", user(handleGetHistory(gw)))
	route("GET /v1/sessions/{id}/trajectory", user(handleGetTrajectory(gw)))
	route("GET /v1/sessions/{id}/logs", user(handleSessionLogs(gw)))

	// List endpoints (admin role)
	route("GET /v1/sessions", admin(handleListSessions(gw)))
	route("GET /v1/summary", admin(handleSummary(gw)))
	route("GET /v1/pools", admin(handleListPools(gw)))
	route("GET /v1/managed/experiments", admin(handleListExperiments(gw)))

	// Pool management (admin role)
	route("POST /v1/pools", admin(handleCreatePool(gw)))
	route("GET /v1/pools/{name}", admin(handleGetPool(gw)))
	route("PATCH /v1/pools/{name}", admin(handleScalePool(gw)))
	route("DELETE /v1/pools/{name}", admin(handleDeletePool(gw)))
	route("POST /v1/pools/{name}/destroy", admin(handleDestroyPool(gw)))
	route("POST /v1/pools/{name}/prefetch", admin(handlePrefetchPool(gw)))
	route("GET /v1/pools/{name}/logs", admin(handlePoolLogs(gw)))

	// Managed sessions (admin role — creates infrastructure)
	route("POST /v1/managed/sessions", admin(handleCreateManagedSession(gw)))
	route("GET /v1/managed/experiments/{id}/sessions", user(handleListExperimentSessions(gw)))
	route("DELETE /v1/managed/experiments/{id}", admin(handleDeleteExperiment(gw)))

	// Health probe (unauthenticated — needed by K8s liveness/readiness probes)
	route("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

type metricResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *metricResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *metricResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *metricResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func instrumentGatewayRoute(gw *Gateway, route string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if gw == nil || gw.metrics == nil {
			h(w, r)
			return
		}
		recorder := &metricResponseWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		defer func() {
			gw.metrics.RecordHTTPRequestDuration(r.Method, route, strconv.Itoa(recorder.status), time.Since(start))
		}()
		h(recorder, r)
	}
}

// SetupInternalRoutes registers debug, metrics, and webhook routes on a
// separate mux intended to be served on an internal-only port.
func SetupInternalRoutes(mux *http.ServeMux, hc *HealthChecker) {
	if hc != nil {
		mux.HandleFunc("GET /debug/health", hc.HandleDebugHealth())
		mux.HandleFunc("POST /internal/alertmanager-webhook", hc.HandleAlertManagerWebhook())
	}

	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)

	mux.Handle("GET /metrics", promhttp.HandlerFor(ctrlmetrics.Registry, promhttp.HandlerOpts{}))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

// checkOwnership is a helper that looks up the session and verifies ownership.
// Returns the session on success, or writes an HTTP error and returns nil.
func checkOwnership(gw *Gateway, w http.ResponseWriter, r *http.Request, sessionID string) *session {
	s, ok := gw.store.Get(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "session "+sessionID+" not found")
		return nil
	}
	s.mu.RLock()
	ownerHash := s.ownerKeyHash
	s.mu.RUnlock()
	if err := CheckSessionOwnership(r.Context(), ownerHash); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return nil
	}
	return s
}

func handleCreateSession(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if req.Image == "" && req.Profile == "" {
			writeError(w, http.StatusBadRequest, "image or profile is required")
			return
		}
		if req.Mode != "" && !validSessionMode(req.Mode) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid session mode: %q", req.Mode))
			return
		}

		info, err := gw.CreateSession(r.Context(), req)
		if err != nil {
			writeGatewayError(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, info)
	}
}

func handleGetSession(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if s, ok := gw.store.Get(id); ok {
			s.mu.RLock()
			ownerHash := s.ownerKeyHash
			s.mu.RUnlock()
			if err := CheckSessionOwnership(r.Context(), ownerHash); err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
			info, err := gw.GetSession(id)
			if err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, info)
			return
		}
		if historical, ok := gw.GetHistoricalSession(id); ok {
			historical.mu.RLock()
			ownerHash := historical.ownerKeyHash
			info := historical.Info
			reason := historical.deletionReason
			if reason == "" {
				reason = info.DeletionReason
			}
			deletedAt := historical.deletedAt
			historical.mu.RUnlock()
			if err := CheckSessionOwnership(r.Context(), ownerHash); err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
			if info.Status == "" {
				info.Status = "deleted"
			}
			info.DeletionReason = reason
			info.DeletedAt = deletedAt
			writeJSON(w, http.StatusGone, ErrorResponse{
				Error:  "session " + id + " is no longer active",
				Detail: sessionDeletionDetail(reason, deletedAt),
			})
			return
		}
		writeError(w, http.StatusNotFound, "session "+id+" not found")
	}
}

func sessionDeletionDetail(reason string, deletedAt *time.Time) string {
	if reason == "" && deletedAt == nil {
		return ""
	}
	var parts []string
	if reason != "" {
		parts = append(parts, "reason="+reason)
	}
	if deletedAt != nil {
		parts = append(parts, "deletedAt="+deletedAt.Format(time.RFC3339))
	}
	return strings.Join(parts, " ")
}

func handleDeleteSession(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}
		if err := gw.DeleteSession(r.Context(), id); err != nil {
			writeError(w, httpStatusForError(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleSuspendSession(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}
		if err := gw.SuspendSession(r.Context(), id); err != nil {
			writeError(w, httpStatusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "suspended"})
	}
}

func handleResumeSession(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}
		if err := gw.ResumeSession(r.Context(), id); err != nil {
			writeError(w, httpStatusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
	}
}

func handleExecute(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}

		var req ExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if len(req.Steps) == 0 {
			writeError(w, http.StatusBadRequest, "steps is required")
			return
		}

		if r.Header.Get("Accept") == "text/event-stream" && req.OperationID == "" {
			gw.ExecuteStepsSSE(w, r.Context(), id, req)
			return
		}

		resp, err := gw.ExecuteSteps(r.Context(), id, req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func handleGetExecuteOperation(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}
		operationID := r.PathValue("operationID")
		if operationID == "" {
			writeError(w, http.StatusBadRequest, "operationID is required")
			return
		}
		info, err := gw.ExecuteOperationStatus(id, operationID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

func handleExecuteContainer(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}
		container := r.PathValue("container")
		if container == "" {
			writeError(w, http.StatusBadRequest, "container is required")
			return
		}

		var req ContainerExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(req.Steps) == 0 {
			writeError(w, http.StatusBadRequest, "steps is required")
			return
		}
		resp, err := gw.ExecuteContainerSteps(r.Context(), id, container, req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleUploadFile(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}

		filePath := r.PathValue("path")
		if filePath == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}
		if _, err := sanitizeUploadPath(filePath); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		resp, err := gw.UploadFile(r.Context(), id, filePath, r.Body, r.Header.Get("X-ARL-SHA256"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func handleDownloadFile(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}

		filePath := r.PathValue("path")
		if filePath == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}
		if _, err := sanitizeUploadPath(filePath); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		streamWriter := &downloadResponseWriter{w: w, filePath: filePath}
		result, err := gw.DownloadFile(r.Context(), id, filePath, streamWriter)
		if err != nil {
			if !streamWriter.started {
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		streamWriter.ensureStarted()
		w.Header().Set("X-ARL-Size-Bytes", strconv.FormatInt(result.SizeBytes, 10))
		w.Header().Set("X-ARL-SHA256", result.SHA256)
	}
}

type downloadResponseWriter struct {
	w        http.ResponseWriter
	filePath string
	started  bool
}

func (w *downloadResponseWriter) Write(p []byte) (int, error) {
	w.ensureStarted()
	return w.w.Write(p)
}

func (w *downloadResponseWriter) ensureStarted() {
	if w.started {
		return
	}
	header := w.w.Header()
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Disposition", "attachment; filename="+strconv.Quote(pathBaseForHeader(w.filePath)))
	header.Add("Trailer", "X-ARL-Size-Bytes")
	header.Add("Trailer", "X-ARL-SHA256")
	w.w.WriteHeader(http.StatusOK)
	w.started = true
}

func pathBaseForHeader(filePath string) string {
	base := path.Base(strings.ReplaceAll(filePath, "\\", "/"))
	if base == "." || base == "/" || base == "" {
		return "download"
	}
	return strings.ReplaceAll(base, "\x00", "")
}

func handleStat(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}

		filePath := r.PathValue("path")
		if filePath == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}

		resp, err := gw.StatFile(r.Context(), id, filePath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func handleListDir(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}

		filePath := r.PathValue("path")
		if filePath == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}

		recursive := r.URL.Query().Get("recursive") == "true"

		resp, err := gw.ListDir(r.Context(), id, filePath, recursive)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func handleWriteStdin(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}

		var req WriteStdinRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if req.Handle == "" {
			writeError(w, http.StatusBadRequest, "handle is required")
			return
		}

		if err := gw.WriteStdin(r.Context(), id, req.Handle, req.Data); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func handleReplay(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}

		var req ReplayRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.SourceSessionID == "" {
			writeError(w, http.StatusBadRequest, "sourceSessionID is required")
			return
		}

		resp, err := gw.ReplayFrom(r.Context(), id, req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleRestore(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}

		var req RestoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if req.SnapshotID == "" {
			writeError(w, http.StatusBadRequest, "snapshot_id is required")
			return
		}

		if err := gw.Restore(r.Context(), id, req.SnapshotID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
	}
}

func handleGetHistory(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}
		records, err := gw.GetHistory(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, records)
	}
}

func handleGetTrajectory(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}
		data, err := gw.ExportTrajectory(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

func handleCreatePool(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreatePoolRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if req.Name == "" || req.Image == "" {
			writeError(w, http.StatusBadRequest, "name and image are required")
			return
		}

		if err := gw.CreatePool(r.Context(), req); err != nil {
			writeGatewayError(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name, "status": "created"})
	}
}

func handleGetPool(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		ns := r.URL.Query().Get("namespace")
		info, err := gw.GetPool(r.Context(), name, ns)
		if err != nil {
			if errors.Is(err, ErrNamespaceNotAllowed) {
				writeGatewayError(w, err)
				return
			}
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

func handleScalePool(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")

		var req ScalePoolRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if req.Replicas < 0 {
			writeError(w, http.StatusBadRequest, "replicas must be non-negative")
			return
		}

		info, err := gw.ScalePool(r.Context(), name, req)
		if err != nil {
			writeGatewayError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, info)
	}
}

func handleDeletePool(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		ns := r.URL.Query().Get("namespace")

		if err := gw.DeletePool(r.Context(), name, ns); err != nil {
			writeGatewayError(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDestroyPool(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		ns := r.URL.Query().Get("namespace")

		if err := gw.DestroyPool(r.Context(), name, ns); err != nil {
			writeGatewayError(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func handlePrefetchPool(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")

		var req PrefetchPoolRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if err := gw.PrefetchImage(r.Context(), name, req.Namespace); err != nil {
			writeGatewayError(w, err)
			return
		}

		writeJSON(w, http.StatusAccepted, map[string]string{
			"name":   name,
			"status": "prefetch_started",
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

func writeGatewayError(w http.ResponseWriter, err error) {
	writeError(w, httpStatusForError(err), err.Error())
}

func parseLogParams(r *http.Request) (bool, int32) {
	follow := r.URL.Query().Get("follow") == "true"
	tail := int32(100)
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tail = int32(n)
		}
	}
	return follow, tail
}

func handleSessionLogs(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if checkOwnership(gw, w, r, id) == nil {
			return
		}
		follow, tail := parseLogParams(r)

		ch, err := gw.StreamSessionLogs(r.Context(), id, follow, tail)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		enc := json.NewEncoder(w)
		for entry := range ch {
			enc.Encode(entry)
			flusher.Flush()
		}
	}
}

type poolLogJSON struct {
	PodName   string `json:"podName"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Source    string `json:"source"`
}

func handlePoolLogs(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		ns := r.URL.Query().Get("namespace")
		follow, tail := parseLogParams(r)

		ch, err := gw.StreamPoolLogs(r.Context(), name, ns, follow, tail)
		if err != nil {
			writeGatewayError(w, err)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		enc := json.NewEncoder(w)
		for entry := range ch {
			enc.Encode(poolLogJSON{
				PodName:   entry.PodName,
				Timestamp: entry.Entry.Timestamp,
				Level:     entry.Entry.Level,
				Message:   entry.Entry.Message,
				Source:    entry.Entry.Source,
			})
			flusher.Flush()
		}
	}
}

func handleListSessions(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit := 0
		if rawLimit := strings.TrimSpace(q.Get("limit")); rawLimit != "" {
			parsed, err := strconv.Atoi(rawLimit)
			if err != nil || parsed < 0 {
				writeGatewayError(w, fmt.Errorf("limit must be a non-negative integer"))
				return
			}
			limit = parsed
		}
		page := gw.ListSessionsPage(SessionListOptions{
			Profile:      q.Get("profile"),
			ExperimentID: q.Get("experiment"),
			Status:       q.Get("status"),
			Limit:        limit,
			Cursor:       q.Get("cursor"),
		})
		if page.NextCursor != "" {
			w.Header().Set("X-Next-Cursor", page.NextCursor)
		}
		writeJSON(w, http.StatusOK, page.Items)
	}
}

func handleSummary(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		summary, err := gw.Summary(r.Context())
		if err != nil {
			writeGatewayError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, summary)
	}
}

func handleListPools(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		includeStopped := parseBoolQuery(q.Get("includeStopped")) || parseBoolQuery(q.Get("all"))
		pools, err := gw.ListPoolsWithOptions(r.Context(), PoolListOptions{
			Namespace:      q.Get("namespace"),
			IncludeStopped: includeStopped,
		})
		if err != nil {
			writeGatewayError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, pools)
	}
}

func parseBoolQuery(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func handleListExperiments(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		exps := gw.ListExperiments()
		writeJSON(w, http.StatusOK, exps)
	}
}

func handleCreateManagedSession(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateManagedSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if req.Image == "" {
			writeError(w, http.StatusBadRequest, "image is required")
			return
		}
		if req.ExperimentID == "" {
			writeError(w, http.StatusBadRequest, "experimentId is required")
			return
		}
		if req.Mode != "" && !validSessionMode(req.Mode) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid session mode: %q", req.Mode))
			return
		}

		info, err := gw.CreateManagedSession(r.Context(), req)
		if err != nil {
			if errors.Is(err, ErrNamespaceNotAllowed) {
				writeGatewayError(w, err)
				return
			}
			if errors.Is(err, ErrPoolAtCapacity) {
				writeError(w, http.StatusTooManyRequests, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, info)
	}
}

func handleListExperimentSessions(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		sessions := gw.ListExperimentSessions(id)
		writeJSON(w, http.StatusOK, sessions)
	}
}

func handleDeleteExperiment(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		deleted, err := gw.DeleteExperiment(r.Context(), id)
		resp := map[string]any{"deleted": deleted}
		if err != nil {
			resp["error"] = err.Error()
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
