package execagent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Agent listens on a Unix socket and executes commands in the current container.
type Agent struct {
	socketPath   string
	workspaceDir string
	listener     net.Listener

	mu        sync.Mutex
	processes map[int]*exec.Cmd
}

// New creates a new executor agent.
func New(socketPath, workspaceDir string) *Agent {
	return &Agent{
		socketPath:   socketPath,
		workspaceDir: workspaceDir,
		processes:    make(map[int]*exec.Cmd),
	}
}

// Run starts the agent, listening on the Unix socket until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	// Remove stale socket file
	os.Remove(a.socketPath)

	var err error
	a.listener, err = net.Listen("unix", a.socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", a.socketPath, err)
	}
	defer a.listener.Close()

	// Make socket world-writable so sidecar container can connect
	if err := os.Chmod(a.socketPath, 0777); err != nil {
		return fmt.Errorf("chmod socket: %w", err)
	}

	log.Printf("executor-agent listening on %s", a.socketPath)

	go func() {
		<-ctx.Done()
		a.listener.Close()
	}()

	for {
		conn, err := a.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				log.Printf("accept error: %v", err)
				continue
			}
		}
		go a.handleConn(ctx, conn)
	}
}

func (a *Agent) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line

	encoder := json.NewEncoder(conn)

	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			resp := Response{Error: fmt.Sprintf("invalid request: %v", err), Done: true}
			encoder.Encode(resp)
			continue
		}

		switch req.Type {
		case "exec":
			a.handleExec(ctx, req, encoder)
		case "signal":
			a.handleSignal(req, encoder)
		case "ping":
			encoder.Encode(Response{ID: req.ID, Done: true})
		default:
			encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("unknown type: %s", req.Type), Done: true})
		}
	}
}

func (a *Agent) handleExec(ctx context.Context, req Request, encoder *json.Encoder) {
	if len(req.Cmd) == 0 {
		encoder.Encode(Response{ID: req.ID, Error: "empty command", Done: true})
		return
	}

	execCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
		defer cancel()
	}

	workDir := req.WorkDir
	if workDir == "" {
		workDir = a.workspaceDir
	}

	cmd := exec.CommandContext(execCtx, req.Cmd[0], req.Cmd[1:]...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("stdout pipe: %v", err), Done: true})
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("stderr pipe: %v", err), Done: true})
		return
	}

	if err := cmd.Start(); err != nil {
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("start: %v", err), Done: true})
		return
	}

	// Track the process
	a.mu.Lock()
	a.processes[cmd.Process.Pid] = cmd
	a.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)

	// Stream stdout
	go func() {
		defer wg.Done()
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			encoder.Encode(Response{ID: req.ID, Stdout: s.Text() + "\n"})
		}
	}()

	// Stream stderr
	go func() {
		defer wg.Done()
		s := bufio.NewScanner(stderr)
		for s.Scan() {
			encoder.Encode(Response{ID: req.ID, Stderr: s.Text() + "\n"})
		}
	}()

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	wg.Wait()

	// Remove from tracked processes
	a.mu.Lock()
	delete(a.processes, cmd.Process.Pid)
	a.mu.Unlock()

	encoder.Encode(Response{ID: req.ID, ExitCode: &exitCode, Done: true})
}

func (a *Agent) handleSignal(req Request, encoder *json.Encoder) {
	process, err := os.FindProcess(req.PID)
	if err != nil {
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("find process: %v", err), Done: true})
		return
	}

	var sig syscall.Signal
	switch strings.ToUpper(req.Signal) {
	case "SIGTERM":
		sig = syscall.SIGTERM
	case "SIGKILL":
		sig = syscall.SIGKILL
	case "SIGINT":
		sig = syscall.SIGINT
	default:
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("unsupported signal: %s", req.Signal), Done: true})
		return
	}

	if err := process.Signal(sig); err != nil {
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("signal: %v", err), Done: true})
		return
	}

	encoder.Encode(Response{ID: req.ID, Done: true})
}
