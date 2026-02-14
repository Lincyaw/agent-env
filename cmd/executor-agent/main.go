package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Lincyaw/agent-env/pkg/execagent"
)

func main() {
	var (
		socketPath   string
		workspaceDir string
	)

	flag.StringVar(&socketPath, "socket", "/var/run/arl/exec.sock", "Unix socket path")
	flag.StringVar(&workspaceDir, "workspace", "/workspace", "Default workspace directory")
	flag.Parse()

	// Ensure socket directory exists
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		log.Fatalf("Failed to create socket directory: %v", err)
	}

	// Ensure workspace exists
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		log.Fatalf("Failed to create workspace directory: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	agent := execagent.New(socketPath, workspaceDir)
	if err := agent.Run(ctx); err != nil {
		log.Fatalf("Agent error: %v", err)
	}
}
