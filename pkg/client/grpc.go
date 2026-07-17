package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/Lincyaw/agent-env/pkg/pb"
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

const tokenMetadataKey = "x-arl-token"

func tokenUnaryInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, tokenMetadataKey, token)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func tokenStreamInterceptor(token string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, tokenMetadataKey, token)
		return streamer(ctx, desc, cc, method, opts...)
	}
}

const fileChunkSize = interfaces.FileTransferChunkSize

// GRPCSidecarClient is a gRPC-based implementation of SidecarClient
type GRPCSidecarClient struct {
	port      int
	timeout   time.Duration
	grpcToken string

	mu    sync.RWMutex
	conns map[string]*grpc.ClientConn
}

// NewGRPCSidecarClient creates a new gRPC sidecar client.
// grpcToken is optional; when non-empty, every call includes the token in
// gRPC metadata for sidecar-side validation.
func NewGRPCSidecarClient(port int, timeout time.Duration, grpcToken ...string) interfaces.SidecarClient {
	token := ""
	if len(grpcToken) > 0 {
		token = grpcToken[0]
	}
	return &GRPCSidecarClient{
		port:      port,
		timeout:   timeout,
		grpcToken: token,
		conns:     make(map[string]*grpc.ClientConn),
	}
}

// getOrCreateConn gets an existing connection or creates a new one
func (c *GRPCSidecarClient) getOrCreateConn(podIP string) (*grpc.ClientConn, error) {
	c.mu.RLock()
	conn, ok := c.conns[podIP]
	c.mu.RUnlock()

	if ok && conn != nil {
		state := conn.GetState()
		if state == connectivity.Shutdown || state == connectivity.TransientFailure {
			_ = c.CloseConnection(podIP)
			// Fall through to create a new connection.
		} else {
			return conn, nil
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if conn, ok := c.conns[podIP]; ok && conn != nil {
		state := conn.GetState()
		if state != connectivity.Shutdown && state != connectivity.TransientFailure {
			return conn, nil
		}
		conn.Close()
		delete(c.conns, podIP)
	}

	addr := fmt.Sprintf("%s:%d", podIP, c.port)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(64*1024*1024),
			grpc.MaxCallSendMsgSize(64*1024*1024),
		),
	}
	if c.grpcToken != "" {
		opts = append(opts,
			grpc.WithChainUnaryInterceptor(tokenUnaryInterceptor(c.grpcToken)),
			grpc.WithChainStreamInterceptor(tokenStreamInterceptor(c.grpcToken)),
		)
	}
	conn, err := grpc.NewClient(addr, opts...)
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

	var cancel context.CancelFunc
	if timeout := c.callTimeout(ctx, req.GetTimeout()); timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	pbReq := &pb.ExecRequest{
		Command:        req.GetCommand(),
		Env:            req.GetEnv(),
		WorkingDir:     req.GetWorkingDir(),
		TimeoutSeconds: req.GetTimeout(),
	}

	stream, err := client.Execute(ctx, pbReq)
	if err != nil {
		_ = c.CloseConnection(podIP)
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
			_ = c.CloseConnection(podIP)
			return nil, fmt.Errorf("gRPC stream receive failed: %w", err)
		}

		if len(log.GetStdout()) > 0 {
			stdout.Write(log.GetStdout())
		}
		if len(log.GetStderr()) > 0 {
			stderr.Write(log.GetStderr())
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
	var cancel context.CancelFunc
	if timeout := c.callTimeout(ctx, req.GetTimeout()); timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	pbReq := &pb.ExecRequest{
		Command:        req.GetCommand(),
		Env:            req.GetEnv(),
		WorkingDir:     req.GetWorkingDir(),
		TimeoutSeconds: req.GetTimeout(),
	}

	stream, err := client.Execute(ctx, pbReq)
	if err != nil {
		cancel()
		_ = c.CloseConnection(podIP)
		return nil, fmt.Errorf("gRPC Execute failed: %w", err)
	}

	resultChan := make(chan interfaces.ExecResponse, 100)

	go func() {
		defer cancel()
		defer close(resultChan)
		for {
			log, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				_ = c.CloseConnection(podIP)
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
				Stdout:   string(log.GetStdout()),
				Stderr:   string(log.GetStderr()),
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

// callTimeout returns the gRPC per-call timeout. When the step declares its
// own timeout, that wins (plus 10s buffer). When no step timeout is set and
// the caller already provided a context with a deadline (e.g. the HTTP
// request context), we fall back to c.timeout. When no step timeout is set
// and the context has NO deadline (e.g. background replay), we return 0 to
// skip adding a gRPC deadline — the step runs until it finishes or the
// sidecar kills it.
func (c *GRPCSidecarClient) callTimeout(ctx context.Context, requested int32) time.Duration {
	if requested > 0 {
		stepTimeout := time.Duration(requested)*time.Second + 10*time.Second
		if stepTimeout > c.timeout {
			return stepTimeout
		}
		return c.timeout
	}
	if _, ok := ctx.Deadline(); ok {
		return c.timeout
	}
	return 0
}

// GetIrohAddr fetches the iroh endpoint address from the sidecar via gRPC.
func (c *GRPCSidecarClient) GetIrohAddr(ctx context.Context, podIP string) (string, error) {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return "", err
	}
	client := pb.NewAgentServiceClient(conn)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := client.GetIrohAddr(ctx, &pb.GetIrohAddrRequest{})
	if err != nil {
		return "", fmt.Errorf("gRPC GetIrohAddr failed: %w", err)
	}
	return resp.GetAddrJson(), nil
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

// CloseConnection closes and removes a single gRPC connection by pod IP.
func (c *GRPCSidecarClient) CloseConnection(podIP string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, ok := c.conns[podIP]
	if !ok {
		return nil
	}
	delete(c.conns, podIP)
	return conn.Close()
}

// CleanupStale removes connections in Shutdown or TransientFailure state.
func (c *GRPCSidecarClient) CleanupStale() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	cleaned := 0
	for podIP, conn := range c.conns {
		state := conn.GetState()
		if state == connectivity.Shutdown || state == connectivity.TransientFailure {
			conn.Close()
			delete(c.conns, podIP)
			cleaned++
		}
	}
	return cleaned
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
		Data:     string(out.GetData()),
		ExitCode: out.GetExitCode(),
		Closed:   out.GetClosed(),
	}, nil
}

func (s *grpcShellStream) Close() error {
	return s.stream.CloseSend()
}

// WriteFile streams one file into the workspace via sidecar gRPC.
func (c *GRPCSidecarClient) WriteFile(ctx context.Context, podIP string, path string, content io.Reader, expectedSHA256 string) (*interfaces.FileWriteResult, error) {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return nil, err
	}

	client := pb.NewAgentServiceClient(conn)
	stream, err := client.WriteFile(ctx)
	if err != nil {
		_ = c.CloseConnection(podIP)
		return nil, fmt.Errorf("gRPC WriteFile failed: %w", err)
	}

	buf := make([]byte, fileChunkSize)
	sum := sha256.New()
	var sent int64
	first := true
	for {
		n, readErr := content.Read(buf)
		if n > 0 {
			chunk := &pb.WriteFileChunk{
				Data: append([]byte(nil), buf[:n]...),
			}
			if first {
				chunk.Path = path
				chunk.ExpectedSha256 = expectedSHA256
				first = false
			}
			if _, err := sum.Write(buf[:n]); err != nil {
				return nil, fmt.Errorf("hash write file chunk: %w", err)
			}
			if err := stream.Send(chunk); err != nil {
				_ = c.CloseConnection(podIP)
				return nil, fmt.Errorf("gRPC WriteFile send failed: %w", err)
			}
			sent += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = stream.CloseSend()
			return nil, fmt.Errorf("read upload content: %w", readErr)
		}
	}

	if first {
		if err := stream.Send(&pb.WriteFileChunk{
			Path:           path,
			ExpectedSha256: expectedSHA256,
		}); err != nil {
			_ = c.CloseConnection(podIP)
			return nil, fmt.Errorf("gRPC WriteFile send metadata failed: %w", err)
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		_ = c.CloseConnection(podIP)
		return nil, fmt.Errorf("gRPC WriteFile receive failed: %w", err)
	}
	localSHA := hex.EncodeToString(sum.Sum(nil))
	if resp.GetSha256() != "" && resp.GetSha256() != localSHA {
		return nil, fmt.Errorf("write file checksum mismatch: gateway=%s sidecar=%s", localSHA, resp.GetSha256())
	}
	if resp.GetBytesWritten() != sent {
		return nil, fmt.Errorf("write file size mismatch: gateway=%d sidecar=%d", sent, resp.GetBytesWritten())
	}
	return &interfaces.FileWriteResult{
		Path:         resp.GetPath(),
		BytesWritten: resp.GetBytesWritten(),
		SHA256:       resp.GetSha256(),
	}, nil
}

func (c *GRPCSidecarClient) ReadFile(ctx context.Context, podIP string, path string, dst io.Writer) (*interfaces.FileReadResult, error) {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return nil, err
	}

	client := pb.NewAgentServiceClient(conn)
	stream, err := client.ReadFile(ctx, &pb.ReadFileRequest{
		Path: path,
	})
	if err != nil {
		_ = c.CloseConnection(podIP)
		return nil, fmt.Errorf("gRPC ReadFile failed: %w", err)
	}
	var result interfaces.FileReadResult
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = c.CloseConnection(podIP)
			return nil, fmt.Errorf("gRPC ReadFile receive failed: %w", err)
		}
		if chunk.GetPath() != "" {
			result.Path = chunk.GetPath()
		}
		if len(chunk.GetData()) > 0 {
			if _, err := dst.Write(chunk.GetData()); err != nil {
				return nil, fmt.Errorf("write downloaded content: %w", err)
			}
		}
		if chunk.GetDone() {
			result.Path = chunk.GetPath()
			result.SizeBytes = chunk.GetSizeBytes()
			result.SHA256 = chunk.GetSha256()
		}
	}
	if result.Path == "" {
		result.Path = path
	}
	return &result, nil
}

// StreamLogs streams log entries from the sidecar ring buffer via gRPC.
func (c *GRPCSidecarClient) StreamLogs(ctx context.Context, podIP string, follow bool, tailLines int32) (<-chan interfaces.LogEntry, error) {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return nil, err
	}

	client := pb.NewAgentServiceClient(conn)
	stream, err := client.StreamLogs(ctx, &pb.LogsRequest{
		Follow:    follow,
		TailLines: tailLines,
	})
	if err != nil {
		_ = c.CloseConnection(podIP)
		return nil, fmt.Errorf("gRPC StreamLogs failed: %w", err)
	}

	ch := make(chan interfaces.LogEntry, 128)
	go func() {
		defer close(ch)
		for {
			entry, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				_ = c.CloseConnection(podIP)
				return
			}
			select {
			case ch <- interfaces.LogEntry{
				Timestamp: entry.GetTimestamp(),
				Level:     entry.GetLevel(),
				Message:   entry.GetMessage(),
				Source:    entry.GetSource(),
			}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// Stat returns file metadata for a path via sidecar gRPC.
func (c *GRPCSidecarClient) Stat(ctx context.Context, podIP string, path string) (*interfaces.StatResult, error) {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return nil, err
	}

	client := pb.NewAgentServiceClient(conn)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := client.Stat(ctx, &pb.StatRequest{Path: path})
	if err != nil {
		return nil, fmt.Errorf("gRPC Stat failed: %w", err)
	}

	return &interfaces.StatResult{
		Exists:   resp.GetExists(),
		IsDir:    resp.GetIsDir(),
		Size:     resp.GetSize(),
		Mode:     resp.GetMode(),
		Modified: resp.GetModified(),
	}, nil
}

// ListDir lists directory contents via sidecar gRPC.
func (c *GRPCSidecarClient) ListDir(ctx context.Context, podIP string, path string, recursive bool) ([]interfaces.DirEntry, error) {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return nil, err
	}

	client := pb.NewAgentServiceClient(conn)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := client.ListDir(ctx, &pb.ListDirRequest{Path: path, Recursive: recursive})
	if err != nil {
		return nil, fmt.Errorf("gRPC ListDir failed: %w", err)
	}

	entries := make([]interfaces.DirEntry, len(resp.GetEntries()))
	for i, e := range resp.GetEntries() {
		entries[i] = interfaces.DirEntry{
			Name:  e.GetName(),
			IsDir: e.GetIsDir(),
			Size:  e.GetSize(),
		}
	}

	return entries, nil
}

// WriteStdin sends data to a running process via sidecar gRPC.
func (c *GRPCSidecarClient) WriteStdin(ctx context.Context, podIP string, handle string, data []byte) error {
	conn, err := c.getOrCreateConn(podIP)
	if err != nil {
		return err
	}

	client := pb.NewAgentServiceClient(conn)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	_, err = client.WriteStdin(ctx, &pb.WriteStdinRequest{Handle: handle, Data: data})
	if err != nil {
		return fmt.Errorf("gRPC WriteStdin failed: %w", err)
	}

	return nil
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
		_ = c.CloseConnection(podIP)
		return nil, fmt.Errorf("gRPC InteractiveShell failed: %w", err)
	}

	return &grpcShellStream{stream: stream}, nil
}
