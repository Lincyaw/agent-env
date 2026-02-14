package execagent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
			log.Printf("[exec] id=%s cmd=%v workdir=%s", req.ID, req.Cmd, req.WorkDir)
			a.handleExec(ctx, req, encoder)
		case "shell":
			log.Printf("[shell] id=%s workdir=%s", req.ID, req.WorkDir)
			a.handleShell(ctx, req, scanner, encoder)
			return // shell owns this connection until exit
		case "signal":
			log.Printf("[signal] id=%s pid=%d signal=%s", req.ID, req.PID, req.Signal)
			a.handleSignal(req, encoder)
		case "ping":
			log.Printf("[ping] id=%s", req.ID)
			encoder.Encode(Response{ID: req.ID, Done: true})
		default:
			log.Printf("[unknown] id=%s type=%s", req.ID, req.Type)
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

	log.Printf("[exec] id=%s exit_code=%d cmd=%v", req.ID, exitCode, req.Cmd)
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

func (a *Agent) handleShell(ctx context.Context, req Request, scanner *bufio.Scanner, encoder *json.Encoder) {
	// Prefer bash, fall back to /bin/sh
	shellPath := "/bin/bash"
	if _, err := exec.LookPath("bash"); err != nil {
		shellPath = "/bin/sh"
	}

	workDir := req.WorkDir
	if workDir == "" {
		workDir = a.workspaceDir
	}

	cmd := exec.CommandContext(ctx, shellPath)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("stdin pipe: %v", err), Done: true})
		return
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
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("start shell: %v", err), Done: true})
		return
	}

	a.mu.Lock()
	a.processes[cmd.Process.Pid] = cmd
	a.mu.Unlock()

	var mu sync.Mutex // protects encoder
	send := func(resp Response) {
		mu.Lock()
		encoder.Encode(resp)
		mu.Unlock()
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: stream stdout
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				send(Response{ID: req.ID, Stdout: string(buf[:n])})
			}
			if readErr != nil {
				return
			}
		}
	}()

	// Goroutine 2: stream stderr
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := stderr.Read(buf)
			if n > 0 {
				send(Response{ID: req.ID, Stderr: string(buf[:n])})
			}
			if readErr != nil {
				return
			}
		}
	}()

	// Goroutine 3: read subsequent requests (stdin data, signals)
	go func() {
		for scanner.Scan() {
			var sub Request
			if err := json.Unmarshal(scanner.Bytes(), &sub); err != nil {
				continue
			}
			switch sub.Type {
			case "stdin":
				if _, err := io.WriteString(stdin, sub.Data); err != nil {
					return
				}
			case "signal":
				var sig syscall.Signal
				switch strings.ToUpper(sub.Signal) {
				case "SIGTERM":
					sig = syscall.SIGTERM
				case "SIGKILL":
					sig = syscall.SIGKILL
				case "SIGINT":
					sig = syscall.SIGINT
				default:
					continue
				}
				if cmd.Process != nil {
					cmd.Process.Signal(sig)
				}
			}
		}
		// Scanner stopped (connection closed) -> close stdin to let shell exit
		stdin.Close()
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

	a.mu.Lock()
	delete(a.processes, cmd.Process.Pid)
	a.mu.Unlock()

	log.Printf("[shell] id=%s exit_code=%d", req.ID, exitCode)
	send(Response{ID: req.ID, ExitCode: &exitCode, Done: true})
}
