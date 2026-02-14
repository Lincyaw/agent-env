package sidecar

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Lincyaw/agent-env/pkg/execagent"
)

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
	scanner := bufio.NewScanner(conn)

	req := execagent.Request{ID: "ping-0", Type: "ping"}
	if err := encoder.Encode(req); err != nil {
		return fmt.Errorf("send ping: %w", err)
	}

	if !scanner.Scan() {
		return fmt.Errorf("no response to ping")
	}

	var resp execagent.Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
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

		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			var resp execagent.Response
			if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
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
	scanner := bufio.NewScanner(conn)

	req := execagent.Request{ID: "sig-0", Type: "signal", PID: pid, Signal: signal}
	if err := encoder.Encode(req); err != nil {
		return fmt.Errorf("send signal request: %w", err)
	}

	if !scanner.Scan() {
		return fmt.Errorf("no response to signal")
	}

	var resp execagent.Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return fmt.Errorf("decode signal response: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("signal error: %s", resp.Error)
	}

	return nil
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

		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			var resp execagent.Response
			if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
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
