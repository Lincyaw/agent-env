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
	// Re-exec overlay child: mount overlay on / and exec the real command.
	// This branch is entered when WrapExec re-invokes the binary with
	// _ARL_OVERLAY_CHILD=1 inside a new mount namespace.
	if os.Getenv("_ARL_OVERLAY_CHILD") == "1" {
		execagent.RunOverlayChild()
		os.Exit(127) // unreachable on success
	}

	var (
		socketPath    string
		workspaceDir  string
		checkpointDir string
	)

	flag.StringVar(&socketPath, "socket", "/var/run/arl/exec.sock", "Unix socket path")
	flag.StringVar(&workspaceDir, "workspace", "/workspace", "Default workspace directory")
	flag.StringVar(&checkpointDir, "checkpoint-dir", "", "Overlay checkpoint scratch directory (empty = disabled)")
	flag.Parse()

	// Ensure socket directory exists
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		log.Fatalf("Failed to create socket directory: %v", err)
	}

	// Ensure workspace exists
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		log.Fatalf("Failed to create workspace directory: %v", err)
	}

	// Enable checkpoint via flag or env var.
	if checkpointDir == "" && os.Getenv("ARL_CHECKPOINT_ENABLED") == "1" {
		checkpointDir = "/mnt/arl-checkpoint"
	}

	var opts []execagent.Option
	if checkpointDir != "" {
		log.Printf("Filesystem checkpoint enabled, scratch dir: %s", checkpointDir)
		opts = append(opts, execagent.WithCheckpointer(execagent.NewCheckpointer(checkpointDir)))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	agent := execagent.New(socketPath, workspaceDir, opts...)
	if err := agent.Run(ctx); err != nil {
		log.Fatalf("Agent error: %v", err)
	}
}
