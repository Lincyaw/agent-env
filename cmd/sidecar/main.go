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
		workspaceDir   string
		httpPort       int
		grpcPort       int
		executorSocket string
	)

	flag.StringVar(&workspaceDir, "workspace", "/workspace", "Workspace directory")
	flag.IntVar(&httpPort, "http-port", 8080, "HTTP server port (health checks)")
	flag.IntVar(&grpcPort, "grpc-port", 9090, "gRPC server port")
	flag.StringVar(&executorSocket, "executor-socket", "/var/run/arl/exec.sock", "Unix socket path for executor agent")
	flag.Parse()

	// Ensure workspace exists
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		log.Fatalf("Failed to create workspace directory: %v", err)
	}

	httpServer := sidecar.NewServer(httpPort)

	grpcServer := sidecar.NewGRPCServerWithExecutor(workspaceDir, grpcPort, executorSocket)
	log.Printf("Executor agent socket: %s", executorSocket)

	// Wait for executor agent to be ready
	execClient := sidecar.NewExecutorClient(executorSocket)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 60*time.Second)
	if err := execClient.WaitForReady(waitCtx, 60*time.Second); err != nil {
		log.Printf("ERROR: executor agent not ready after 60s: %v (Execute requests will fail until agent connects)", err)
	} else {
		log.Println("Executor agent connected")
	}
	waitCancel()

	// Setup signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start HTTP server in a goroutine
	go func() {
		if err := httpServer.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	// Start gRPC server in a goroutine
	go func() {
		if err := grpcServer.Start(); err != nil {
			log.Fatalf("Failed to start gRPC server: %v", err)
		}
	}()

	log.Printf("Sidecar started: HTTP on :%d (health), gRPC on :%d", httpPort, grpcPort)

	// Wait for interrupt signal
	<-ctx.Done()
	log.Println("Shutting down gracefully...")

	// Give servers 10 seconds to finish current requests
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Shutdown both servers
	grpcServer.Stop()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Servers stopped")
}
