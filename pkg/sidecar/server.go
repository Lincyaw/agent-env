package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

// Server provides HTTP API for health checks and checkpoint data export.
type Server struct {
	port          int
	httpServer    *http.Server
	ready         atomic.Bool
	checkpointDir string
}

// NewServer creates a new HTTP server for health checks and optional
// checkpoint data export. When checkpointDir is non-empty the server
// registers /v1/checkpoints endpoints.
func NewServer(port int, checkpointDir ...string) *Server {
	mux := http.NewServeMux()
	srv := &Server{
		port: port,
	}
	if len(checkpointDir) > 0 {
		srv.checkpointDir = checkpointDir[0]
	}

	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/healthz", srv.handleHealthz)
	mux.HandleFunc("/readyz", srv.handleReadyz)

	if srv.checkpointDir != "" {
		mux.HandleFunc("/v1/checkpoints/combined", srv.handleCombinedCheckpoint)
		mux.HandleFunc("/v1/checkpoints/", srv.handleGetCheckpoint)
		mux.HandleFunc("/v1/checkpoints", srv.handleListCheckpoints)
		log.Printf("Checkpoint endpoints enabled (dir=%s)", srv.checkpointDir)
	}

	srv.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	return srv
}

// SetReady marks the sidecar as ready to serve traffic.
func (s *Server) SetReady(ready bool) {
	s.ready.Store(ready)
	if ready {
		log.Println("Sidecar marked ready")
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Printf("HTTP server starting on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if !s.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
