package main

import (
	"flag"
	"log"
	"os"

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
	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
