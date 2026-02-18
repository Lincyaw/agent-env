package gateway

import (
	"encoding/json"
	"net/http"
)

// SetupRoutes registers all gateway routes.
func SetupRoutes(mux *http.ServeMux, gw *Gateway) {
	// Session management
	mux.HandleFunc("POST /v1/sessions", handleCreateSession(gw))
	mux.HandleFunc("GET /v1/sessions/{id}", handleGetSession(gw))
	mux.HandleFunc("DELETE /v1/sessions/{id}", handleDeleteSession(gw))

	// Execution
	mux.HandleFunc("POST /v1/sessions/{id}/execute", handleExecute(gw))
	mux.HandleFunc("POST /v1/sessions/{id}/restore", handleRestore(gw))

	// Interactive shell (WebSocket)
	mux.HandleFunc("/v1/sessions/{id}/shell", handleShell(gw))

	// History and trajectory
	mux.HandleFunc("GET /v1/sessions/{id}/history", handleGetHistory(gw))
	mux.HandleFunc("GET /v1/sessions/{id}/trajectory", handleGetTrajectory(gw))

	// Pool management
	mux.HandleFunc("POST /v1/pools", handleCreatePool(gw))
	mux.HandleFunc("GET /v1/pools/{name}", handleGetPool(gw))
	mux.HandleFunc("PATCH /v1/pools/{name}", handleScalePool(gw))
	mux.HandleFunc("DELETE /v1/pools/{name}", handleDeletePool(gw))

	// Health
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

func handleCreateSession(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if req.PoolRef == "" {
			writeError(w, http.StatusBadRequest, "pool_ref is required")
			return
		}

		info, err := gw.CreateSession(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, info)
	}
}

func handleGetSession(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		info, err := gw.GetSession(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

func handleDeleteSession(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := gw.DeleteSession(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleExecute(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		var req ExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if len(req.Steps) == 0 {
			writeError(w, http.StatusBadRequest, "steps is required")
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

func handleRestore(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

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
			writeError(w, http.StatusInternalServerError, err.Error())
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
			writeError(w, http.StatusInternalServerError, err.Error())
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
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.WriteHeader(http.StatusNoContent)
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
