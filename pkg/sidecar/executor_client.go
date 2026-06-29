package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/Lincyaw/agent-env/pkg/execagent"
)

const fileTransferChunkSize = 1024 * 1024

// ExecutorClient communicates with the executor agent over a Unix socket.
type ExecutorClient struct {
	socketPath string
	mu         sync.Mutex
}

// NewExecutorClient creates a new executor client.
func NewExecutorClient(socketPath string) *ExecutorClient {
	return &ExecutorClient{socketPath: socketPath}
}

// Ping checks if the executor agent is reachable.
func (c *ExecutorClient) Ping() error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	req := execagent.Request{ID: "ping-0", Type: "ping"}
	if err := encoder.Encode(req); err != nil {
		return fmt.Errorf("send ping: %w", err)
	}

	var resp execagent.Response
	if err := decoder.Decode(&resp); err != nil {
		return fmt.Errorf("decode ping response: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("ping error: %s", resp.Error)
	}

	return nil
}

// Execute sends an exec request and streams responses back.
func (c *ExecutorClient) Execute(ctx context.Context, req execagent.Request) (<-chan execagent.Response, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send exec request: %w", err)
	}

	ch := make(chan execagent.Response, 100)

	go func() {
		defer close(ch)
		defer conn.Close()

		decoder := json.NewDecoder(conn)

		for {
			var resp execagent.Response
			if err := decoder.Decode(&resp); err != nil {
				ch <- execagent.Response{ID: req.ID, Error: fmt.Sprintf("decode: %v", err), Done: true}
				return
			}

			select {
			case ch <- resp:
			case <-ctx.Done():
				return
			}

			if resp.Done {
				return
			}
		}
	}()

	return ch, nil
}

// Signal sends a signal to a process in the executor container.
func (c *ExecutorClient) Signal(pid int, signal string) error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	req := execagent.Request{ID: "sig-0", Type: "signal", PID: pid, Signal: signal}
	if err := encoder.Encode(req); err != nil {
		return fmt.Errorf("send signal request: %w", err)
	}

	var resp execagent.Response
	if err := decoder.Decode(&resp); err != nil {
		return fmt.Errorf("decode signal response: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("signal error: %s", resp.Error)
	}

	return nil
}

// WriteFile streams one file into the executor workspace.
func (c *ExecutorClient) WriteFile(ctx context.Context, path string, content io.Reader, expectedSHA256 string) (*FileWriteResult, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	req := execagent.Request{
		ID:             fmt.Sprintf("write-%d", time.Now().UnixNano()),
		Type:           "write_file_stream",
		Path:           path,
		ExpectedSHA256: expectedSHA256,
	}
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("send write_file request: %w", err)
	}

	buf := make([]byte, fileTransferChunkSize)
	for {
		n, readErr := content.Read(buf)
		if n > 0 {
			if err := encoder.Encode(execagent.Request{
				ID:      req.ID,
				Type:    "write_file_chunk",
				Content: append([]byte(nil), buf[:n]...),
			}); err != nil {
				return nil, fmt.Errorf("send write_file chunk: %w", err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read upload content: %w", readErr)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}

	if err := encoder.Encode(execagent.Request{ID: req.ID, Type: "write_file_finish"}); err != nil {
		return nil, fmt.Errorf("send write_file finish: %w", err)
	}

	for {
		var resp execagent.Response
		if err := decoder.Decode(&resp); err != nil {
			return nil, fmt.Errorf("read write_file response: %w", err)
		}
		if resp.Error != "" {
			return nil, fmt.Errorf("write_file error: %s", resp.Error)
		}
		if resp.Done {
			if resp.BytesWritten == nil {
				return nil, fmt.Errorf("write_file response missing bytes_written")
			}
			return &FileWriteResult{
				Path:         path,
				BytesWritten: *resp.BytesWritten,
				SHA256:       resp.SHA256,
			}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
}

func (c *ExecutorClient) ReadFile(ctx context.Context, path string, dst io.Writer) (*FileReadResult, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	req := execagent.Request{
		ID:   fmt.Sprintf("read-%d", time.Now().UnixNano()),
		Type: "read_file_stream",
		Path: path,
	}
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("send read_file request: %w", err)
	}

	for {
		var resp execagent.Response
		if err := decoder.Decode(&resp); err != nil {
			return nil, fmt.Errorf("read read_file response: %w", err)
		}
		if resp.Error != "" {
			return nil, fmt.Errorf("read_file error: %s", resp.Error)
		}
		if len(resp.Content) > 0 {
			if _, err := dst.Write(resp.Content); err != nil {
				return nil, fmt.Errorf("write downloaded content: %w", err)
			}
		}
		if resp.Done {
			var size int64
			if resp.SizeBytes != nil {
				size = *resp.SizeBytes
			}
			return &FileReadResult{
				Path:      path,
				SizeBytes: size,
				SHA256:    resp.SHA256,
			}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
}

func (c *ExecutorClient) dial() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to executor agent at %s: %w", c.socketPath, err)
	}
	return conn, nil
}

// WaitForReady polls the executor agent until it responds to ping or timeout.
func (c *ExecutorClient) WaitForReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("executor agent not ready after %v", timeout)
		case <-ticker.C:
			if err := c.Ping(); err == nil {
				return nil
			}
		}
	}
}

// ShellSession represents an interactive shell running in the executor container.
type ShellSession struct {
	conn    net.Conn
	mu      sync.Mutex
	encoder *json.Encoder
	id      string
	Output  chan execagent.Response
}

// StartShell opens a connection to the executor agent and starts an interactive shell.
func (c *ExecutorClient) StartShell(ctx context.Context, workDir string, env map[string]string) (*ShellSession, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}

	id := fmt.Sprintf("shell-%d", time.Now().UnixNano())
	encoder := json.NewEncoder(conn)

	req := execagent.Request{
		ID:      id,
		Type:    "shell",
		WorkDir: workDir,
		Env:     env,
	}
	if err := encoder.Encode(req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send shell request: %w", err)
	}

	session := &ShellSession{
		conn:    conn,
		encoder: encoder,
		id:      id,
		Output:  make(chan execagent.Response, 100),
	}

	// Read responses from the executor agent into the Output channel
	go func() {
		defer close(session.Output)

		decoder := json.NewDecoder(conn)

		for {
			var resp execagent.Response
			if err := decoder.Decode(&resp); err != nil {
				session.Output <- execagent.Response{ID: id, Error: fmt.Sprintf("decode: %v", err), Done: true}
				return
			}

			select {
			case session.Output <- resp:
			case <-ctx.Done():
				return
			}

			if resp.Done {
				return
			}
		}
	}()

	return session, nil
}

// SendInput sends stdin data to the shell session.
func (s *ShellSession) SendInput(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.encoder.Encode(execagent.Request{
		ID:   s.id,
		Type: "stdin",
		Data: data,
	})
}

// SendSignal sends a signal to the shell process.
func (s *ShellSession) SendSignal(signal string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.encoder.Encode(execagent.Request{
		ID:     s.id,
		Type:   "signal",
		Signal: signal,
	})
}

// Close closes the underlying connection, which causes the shell to exit.
func (s *ShellSession) Close() error {
	return s.conn.Close()
}
