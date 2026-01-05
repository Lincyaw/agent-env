package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

func main() {
	var (
		workspaceDir string
		port         int
	)

	flag.StringVar(&workspaceDir, "workspace", "/workspace", "Workspace directory")
	flag.IntVar(&port, "port", 8080, "HTTP server port")
	flag.Parse()

	// Ensure workspace exists
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		log.Fatalf("Failed to create workspace directory: %v", err)
	}

	server := sidecar.NewServer(workspaceDir, port)

	// Setup signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start server in a goroutine
	go func() {
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	log.Println("Sidecar server started, waiting for shutdown signal")

	// Wait for interrupt signal
	<-ctx.Done()
	log.Println("Shutting down gracefully...")

	// Give server 10 seconds to finish current requests
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	log.Println("Server stopped")
}
