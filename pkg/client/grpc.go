// Copyright 2024 ARL-Infra Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/Lincyaw/agent-env/pkg/pb"
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

// GRPCSidecarClient is a gRPC-based implementation of SidecarClient
type GRPCSidecarClient struct {
	port    int
	timeout time.Duration

	mu    sync.RWMutex
	conns map[string]*grpc.ClientConn
}

// NewGRPCSidecarClient creates a new gRPC sidecar client
func NewGRPCSidecarClient(port int, timeout time.Duration) interfaces.SidecarClient {
	return &GRPCSidecarClient{
		port:    port,
		timeout: timeout,
		conns:   make(map[string]*grpc.ClientConn),
	}
}

// getOrCreateConn gets an existing connection or creates a new one
func (c *GRPCSidecarClient) getOrCreateConn(podIP string) (*grpc.ClientConn, error) {
	c.mu.RLock()
	conn, ok := c.conns[podIP]
	c.mu.RUnlock()

	if ok && conn != nil {
		return conn, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if conn, ok := c.conns[podIP]; ok && conn != nil {
		return conn, nil
	}

	addr := fmt.Sprintf("%s:%d", podIP, c.port)
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to %s: %w", addr, err)
	}

	c.conns[podIP] = conn
	return conn, nil
}

// UpdateFiles sends file update request to sidecar
func (c *GRPCSidecarClient) UpdateFiles(ctx context.Context, podIP string, req interfaces.FileUpdateRequest) (interfaces.FileUpdateResponse, error) {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return nil, err
	}

	client := pb.NewAgentServiceClient(conn)

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	pbReq := &pb.FileRequest{
		BasePath: req.GetBasePath(),
		Files:    req.GetFiles(),
		Patch:    req.GetPatch(),
	}

	resp, err := client.UpdateFiles(ctx, pbReq)
	if err != nil {
		return nil, fmt.Errorf("gRPC UpdateFiles failed: %w", err)
	}

	return &sidecar.FileResponse{
		Success: resp.GetSuccess(),
		Message: resp.GetMessage(),
	}, nil
}

// Execute sends command execution request and returns aggregated result
func (c *GRPCSidecarClient) Execute(ctx context.Context, podIP string, req interfaces.ExecRequest) (interfaces.ExecResponse, error) {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return nil, err
	}

	client := pb.NewAgentServiceClient(conn)

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	pbReq := &pb.ExecRequest{
		Command:        req.GetCommand(),
		Env:            req.GetEnv(),
		WorkingDir:     req.GetWorkingDir(),
		TimeoutSeconds: req.GetTimeout(),
	}

	stream, err := client.Execute(ctx, pbReq)
	if err != nil {
		return nil, fmt.Errorf("gRPC Execute failed: %w", err)
	}

	var stdout, stderr strings.Builder
	var exitCode int32
	var done bool

	for {
		log, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gRPC stream receive failed: %w", err)
		}

		if log.GetStdout() != "" {
			stdout.WriteString(log.GetStdout())
		}
		if log.GetStderr() != "" {
			stderr.WriteString(log.GetStderr())
		}
		if log.GetDone() {
			exitCode = log.GetExitCode()
			done = true
		}
	}

	return &sidecar.ExecLog{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Done:     done,
	}, nil
}

// ExecuteStream sends command execution request and streams output via channel
func (c *GRPCSidecarClient) ExecuteStream(ctx context.Context, podIP string, req interfaces.ExecRequest) (<-chan interfaces.ExecResponse, error) {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return nil, err
	}

	client := pb.NewAgentServiceClient(conn)

	pbReq := &pb.ExecRequest{
		Command:        req.GetCommand(),
		Env:            req.GetEnv(),
		WorkingDir:     req.GetWorkingDir(),
		TimeoutSeconds: req.GetTimeout(),
	}

	stream, err := client.Execute(ctx, pbReq)
	if err != nil {
		return nil, fmt.Errorf("gRPC Execute failed: %w", err)
	}

	resultChan := make(chan interfaces.ExecResponse, 100)

	go func() {
		defer close(resultChan)
		for {
			log, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				// Send error as final message
				resultChan <- &sidecar.ExecLog{
					Stderr:   err.Error(),
					ExitCode: 1,
					Done:     true,
				}
				return
			}

			select {
			case resultChan <- &sidecar.ExecLog{
				Stdout:   log.GetStdout(),
				Stderr:   log.GetStderr(),
				ExitCode: log.GetExitCode(),
				Done:     log.GetDone(),
			}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return resultChan, nil
}

// Reset sends reset request to sidecar
func (c *GRPCSidecarClient) Reset(ctx context.Context, podIP string, req interfaces.ResetRequest) (interfaces.ResetResponse, error) {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return nil, err
	}

	client := pb.NewAgentServiceClient(conn)

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	pbReq := &pb.ResetRequest{
		PreserveFiles: req.ShouldPreserveFiles(),
	}

	resp, err := client.Reset(ctx, pbReq)
	if err != nil {
		return nil, fmt.Errorf("gRPC Reset failed: %w", err)
	}

	return &sidecar.ResetResponse{
		Success: resp.GetSuccess(),
		Message: resp.GetMessage(),
	}, nil
}

// HealthCheck checks if sidecar is healthy by verifying gRPC connection state
func (c *GRPCSidecarClient) HealthCheck(ctx context.Context, podIP string) error {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return fmt.Errorf("health check failed: cannot connect to %s: %w", podIP, err)
	}

	// Check connection state
	state := conn.GetState()
	if state.String() == "SHUTDOWN" {
		return fmt.Errorf("connection to %s is shutdown", podIP)
	}

	return nil
}

// Close cleans up all gRPC connections
func (c *GRPCSidecarClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var lastErr error
	for podIP, conn := range c.conns {
		if err := conn.Close(); err != nil {
			lastErr = fmt.Errorf("failed to close connection to %s: %w", podIP, err)
		}
		delete(c.conns, podIP)
	}
	return lastErr
}
