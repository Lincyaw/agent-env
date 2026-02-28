package client

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
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

// HealthCheck checks if sidecar is healthy by verifying gRPC connection state
func (c *GRPCSidecarClient) HealthCheck(ctx context.Context, podIP string) error {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return fmt.Errorf("health check failed: cannot connect to %s: %w", podIP, err)
	}

	// Check connection state - only Ready is considered healthy
	state := conn.GetState()
	switch state {
	case connectivity.Ready:
		return nil
	case connectivity.Idle:
		// Trigger connection attempt and check
		conn.Connect()
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if conn.WaitForStateChange(ctx, connectivity.Idle) {
			if conn.GetState() == connectivity.Ready {
				return nil
			}
		}
		return fmt.Errorf("sidecar not ready, state: %s", conn.GetState())
	default:
		return fmt.Errorf("sidecar unhealthy, state: %s", state)
	}
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

// grpcShellStream wraps a gRPC bidi stream as interfaces.ShellStream.
type grpcShellStream struct {
	stream grpc.BidiStreamingClient[pb.ShellInput, pb.ShellOutput]
}

func (s *grpcShellStream) Send(input interfaces.ShellInput) error {
	pbInput := &pb.ShellInput{
		Resize: input.Resize,
		Rows:   input.Rows,
		Cols:   input.Cols,
	}
	if input.Signal != "" {
		pbInput.Input = &pb.ShellInput_Signal{Signal: input.Signal}
	} else if input.Data != "" {
		pbInput.Input = &pb.ShellInput_Data{Data: input.Data}
	}
	return s.stream.Send(pbInput)
}

func (s *grpcShellStream) Recv() (interfaces.ShellOutput, error) {
	out, err := s.stream.Recv()
	if err != nil {
		return interfaces.ShellOutput{}, err
	}
	return interfaces.ShellOutput{
		Data:     out.GetData(),
		ExitCode: out.GetExitCode(),
		Closed:   out.GetClosed(),
	}, nil
}

func (s *grpcShellStream) Close() error {
	return s.stream.CloseSend()
}

// InteractiveShell opens a bidirectional shell session via sidecar gRPC
func (c *GRPCSidecarClient) InteractiveShell(ctx context.Context, podIP string) (interfaces.ShellStream, error) {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return nil, err
	}

	client := pb.NewAgentServiceClient(conn)

	stream, err := client.InteractiveShell(ctx)
	if err != nil {
		return nil, fmt.Errorf("gRPC InteractiveShell failed: %w", err)
	}

	return &grpcShellStream{stream: stream}, nil
}
