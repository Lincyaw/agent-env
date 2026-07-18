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

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// SetupRoutes builds the public chi.Router for the gateway. The returned
// router implements http.Handler and can be wrapped with additional
// middleware (rate-limiter, gzip, OTEL) by the caller.
func SetupRoutes(gw *Gateway, authCfg *AuthConfig) chi.Router {
	r := chi.NewRouter()
	r.Use(chiMiddleware.Recoverer)
	r.Use(instrumentMiddleware(gw))

	authUser := noopMiddleware
	authAdmin := noopMiddleware
	if authCfg != nil && authCfg.Enabled {
		authUser = requireAuthMiddleware(authCfg, RoleUser)
		authAdmin = requireAuthMiddleware(authCfg, RoleAdmin)
	}

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	r.Route("/v1", func(r chi.Router) {
		// Session creation (user role, no ownership)
		r.With(authUser).Post("/sessions", handleCreateSession(gw))

		// Session-scoped endpoints
		r.Route("/sessions/{id}", func(r chi.Router) {
			r.Use(authUser)
			// GET has custom ownership logic (historical sessions)
			r.Get("/", handleGetSession(gw))

			// All other operations require session ownership
			r.Group(func(r chi.Router) {
				r.Use(sessionOwnership(gw))
				r.Delete("/", handleDeleteSession(gw))
				r.Post("/suspend", handleSuspendSession(gw))
				r.Post("/resume", handleResumeSession(gw))
				r.Post("/fork", handleForkSession(gw))
				r.Get("/iroh-addr", handleGetIrohAddr(gw))
				r.Post("/execute", handleExecute(gw))
				r.Post("/containers/{container}/execute", handleExecuteContainer(gw))
				r.Get("/operations/{operationID}", handleGetExecuteOperation(gw))
				r.Post("/upload-file", handleUploadFile(gw))
				r.Post("/download-file", handleDownloadFile(gw))
				r.Post("/stdin", handleWriteStdin(gw))
				r.Post("/restore", handleRestore(gw))
				r.Post("/replay", handleReplay(gw))
				r.Get("/shell", handleShell(gw, authCfg))
				r.Get("/tunnel/{port}", handleTunnel(gw, authCfg))
				r.Get("/history", handleGetHistory(gw))
				r.Get("/trajectory", handleGetTrajectory(gw))
				r.Get("/logs", handleSessionLogs(gw))
			})
		})

		// Admin endpoints
		r.Group(func(r chi.Router) {
			r.Use(authAdmin)
			r.Get("/sessions", handleListSessions(gw))
			r.Get("/summary", handleSummary(gw))
			r.Get("/pools", handleListPools(gw))
			r.Get("/managed/experiments", handleListExperiments(gw))
			r.Post("/pools", handleCreatePool(gw))
			r.Route("/pools/{name}", func(r chi.Router) {
				r.Get("/", handleGetPool(gw))
				r.Patch("/", handleScalePool(gw))
				r.Delete("/", handleDeletePool(gw))
				r.Post("/destroy", handleDestroyPool(gw))
				r.Post("/prefetch", handlePrefetchPool(gw))
				r.Get("/logs", handlePoolLogs(gw))
			})
			r.Post("/managed/sessions", handleCreateManagedSession(gw))
			r.Delete("/managed/experiments/{id}", handleDeleteExperiment(gw))
		})

		// Experiment sessions listing (user role)
		r.With(authUser).Get("/managed/experiments/{id}/sessions", handleListExperimentSessions(gw))
	})

	return r
}

func noopMiddleware(next http.Handler) http.Handler { return next }

func requireAuthMiddleware(authCfg *AuthConfig, minRole Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return requireAuth(authCfg, minRole, next.ServeHTTP)
	}
}

// sessionOwnership is a chi middleware that validates session existence and
// caller ownership via the {id} URL parameter. Handlers behind this
// middleware can skip checkOwnership entirely.
func sessionOwnership(gw *Gateway) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			s, ok := gw.store.Get(id)
			if !ok {
				writeError(w, http.StatusNotFound, "session "+id+" not found")
				return
			}
			s.mu.RLock()
			ownerHash := s.ownerKeyHash
			s.mu.RUnlock()
			if err := CheckSessionOwnership(r.Context(), ownerHash); err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// instrumentMiddleware records per-route HTTP request duration via Prometheus.
func instrumentMiddleware(gw *Gateway) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if gw == nil || gw.metrics == nil {
				next.ServeHTTP(w, r)
				return
			}
			recorder := &metricResponseWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			defer func() {
				pattern := chi.RouteContext(r.Context()).RoutePattern()
				if pattern == "" {
					pattern = r.URL.Path
				}
				gw.metrics.RecordHTTPRequestDuration(r.Method, pattern, strconv.Itoa(recorder.status), time.Since(start))
			}()
			next.ServeHTTP(recorder, r)
		})
	}
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

func (w *metricResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// SetupInternalRoutes builds a chi.Router for the internal-only port
// (metrics, debug, alertmanager webhook). No authentication.
func SetupInternalRoutes(hc *HealthChecker) chi.Router {
	r := chi.NewRouter()

	if hc != nil {
		r.Get("/debug/health", hc.HandleDebugHealth())
		r.Post("/internal/alertmanager-webhook", hc.HandleAlertManagerWebhook())
	}

	r.Get("/debug/pprof/", pprof.Index)
	r.Get("/debug/pprof/cmdline", pprof.Cmdline)
	r.Get("/debug/pprof/profile", pprof.Profile)
	r.Get("/debug/pprof/symbol", pprof.Symbol)
	r.Get("/debug/pprof/trace", pprof.Trace)

	r.Handle("/metrics", promhttp.HandlerFor(ctrlmetrics.Registry, promhttp.HandlerOpts{}))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	return r
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
		id := chi.URLParam(r, "id")
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
		id := chi.URLParam(r, "id")
		if err := gw.DeleteSession(r.Context(), id); err != nil {
			writeError(w, httpStatusForError(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleSuspendSession(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := gw.SuspendSession(r.Context(), id); err != nil {
			writeError(w, httpStatusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "suspended"})
	}
}

func handleResumeSession(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := gw.ResumeSession(r.Context(), id); err != nil {
			writeError(w, httpStatusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
	}
}

func handleForkSession(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req ForkSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Step < 0 {
			writeError(w, http.StatusBadRequest, "step must be >= 0")
			return
		}
		resp, err := gw.ForkSession(r.Context(), id, req)
		if err != nil {
			writeGatewayError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, resp)
	}
}

func handleGetIrohAddr(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		addr, err := gw.GetIrohAddr(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"addr": addr})
	}
}

func handleExecute(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

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
			writeGatewayError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func handleGetExecuteOperation(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		operationID := chi.URLParam(r, "operationID")
		if operationID == "" {
			writeError(w, http.StatusBadRequest, "operationID is required")
			return
		}
		info, err := gw.OperationStatus(id, operationID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

func handleExecuteContainer(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		container := chi.URLParam(r, "container")
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
		id := chi.URLParam(r, "id")

		filePath := r.Header.Get("X-ARL-Path")
		if filePath == "" {
			writeError(w, http.StatusBadRequest, "X-ARL-Path header is required")
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
		id := chi.URLParam(r, "id")

		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Path == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}

		streamWriter := &downloadResponseWriter{w: w, filePath: req.Path}
		result, err := gw.DownloadFile(r.Context(), id, req.Path, streamWriter)
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

func handleWriteStdin(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

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
		id := chi.URLParam(r, "id")

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
			writeGatewayError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleRestore(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		var req RestoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if req.SnapshotID == "" {
			writeError(w, http.StatusBadRequest, "snapshot_id is required")
			return
		}

		resp, err := gw.Restore(r.Context(), id, req)
		if err != nil {
			writeGatewayError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func handleGetHistory(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
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
		id := chi.URLParam(r, "id")
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
		name := chi.URLParam(r, "name")
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
		name := chi.URLParam(r, "name")

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
		name := chi.URLParam(r, "name")
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
		name := chi.URLParam(r, "name")
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
		name := chi.URLParam(r, "name")

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
	if pending, ok := err.(*OperationPending); ok {
		writeJSON(w, http.StatusAccepted, map[string]string{
			"operationID": pending.OperationID,
			"status":      "running",
		})
		return
	}
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
		id := chi.URLParam(r, "id")
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
		name := chi.URLParam(r, "name")
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
		id := chi.URLParam(r, "id")
		sessions := gw.ListExperimentSessions(id)
		writeJSON(w, http.StatusOK, sessions)
	}
}

func handleDeleteExperiment(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		deleted, err := gw.DeleteExperiment(r.Context(), id)
		resp := map[string]any{"deleted": deleted}
		if err != nil {
			resp["error"] = err.Error()
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
