package sidecar

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Lincyaw/agent-env/pkg/execagent"
	"github.com/google/uuid"
)

// ExecRequest specifies a command to execute
type ExecRequest struct {
	Command        []string
	Env            map[string]string
	WorkingDir     string
	Background     bool
	TimeoutSeconds int32
}

// GetCommand implements interfaces.ExecRequest
func (r *ExecRequest) GetCommand() []string {
	return r.Command
}

// GetEnv implements interfaces.ExecRequest
func (r *ExecRequest) GetEnv() map[string]string {
	return r.Env
}

// GetWorkingDir implements interfaces.ExecRequest
func (r *ExecRequest) GetWorkingDir() string {
	return r.WorkingDir
}

// GetTimeout implements interfaces.ExecRequest
func (r *ExecRequest) GetTimeout() int32 {
	return r.TimeoutSeconds
}

// ExecLog streams output from command execution
type ExecLog struct {
	Stdout   string
	Stderr   string
	ExitCode int32
	Done     bool
}

// GetStdout implements interfaces.ExecResponse
func (r *ExecLog) GetStdout() string {
	return r.Stdout
}

// GetStderr implements interfaces.ExecResponse
func (r *ExecLog) GetStderr() string {
	return r.Stderr
}

// GetExitCode implements interfaces.ExecResponse
func (r *ExecLog) GetExitCode() int32 {
	return r.ExitCode
}

// IsDone implements interfaces.ExecResponse
func (r *ExecLog) IsDone() bool {
	return r.Done
}

// AgentService implements the sidecar functionality
type AgentService struct {
	workspaceDir   string
	processes      map[int]*exec.Cmd
	executorClient *ExecutorClient
}

// NewAgentService creates a new agent service
func NewAgentService(workspaceDir string) *AgentService {
	return &AgentService{
		workspaceDir: workspaceDir,
		processes:    make(map[int]*exec.Cmd),
	}
}

// NewAgentServiceWithExecutor creates a new agent service that proxies to an executor agent.
func NewAgentServiceWithExecutor(workspaceDir, executorSocket string) *AgentService {
	return &AgentService{
		workspaceDir:   workspaceDir,
		processes:      make(map[int]*exec.Cmd),
		executorClient: NewExecutorClient(executorSocket),
	}
}

// Execute runs a command and streams output.
// If an executor client is configured, commands are forwarded to the executor agent.
func (s *AgentService) Execute(ctx context.Context, req *ExecRequest, stream chan<- *ExecLog) error {
	defer close(stream)

	if len(req.Command) == 0 {
		stream <- &ExecLog{
			Stderr:   "no command specified",
			ExitCode: 1,
			Done:     true,
		}
		return nil
	}

	// Proxy to executor agent if available
	if s.executorClient != nil {
		return s.executeViaAgent(ctx, req, stream)
	}

	return s.executeLocal(ctx, req, stream)
}

// executeViaAgent forwards execution to the executor agent in the main container.
func (s *AgentService) executeViaAgent(ctx context.Context, req *ExecRequest, stream chan<- *ExecLog) error {
	agentReq := execagent.Request{
		ID:      uuid.New().String(),
		Type:    "exec",
		Cmd:     req.Command,
		Env:     req.Env,
		WorkDir: req.WorkingDir,
		Timeout: int(req.TimeoutSeconds),
	}

	respChan, err := s.executorClient.Execute(ctx, agentReq)
	if err != nil {
		stream <- &ExecLog{
			Stderr:   fmt.Sprintf("executor agent error: %v", err),
			ExitCode: 1,
			Done:     true,
		}
		return nil
	}

	for resp := range respChan {
		log := &ExecLog{}
		if resp.Stdout != "" {
			log.Stdout = resp.Stdout
		}
		if resp.Stderr != "" {
			log.Stderr = resp.Stderr
		}
		if resp.Error != "" {
			log.Stderr = resp.Error
		}
		if resp.ExitCode != nil {
			log.ExitCode = int32(*resp.ExitCode)
		}
		log.Done = resp.Done

		select {
		case stream <- log:
		case <-ctx.Done():
			return nil
		}
	}

	return nil
}

// executeLocal runs the command directly in the sidecar container (fallback).
func (s *AgentService) executeLocal(ctx context.Context, req *ExecRequest, stream chan<- *ExecLog) error {

	workDir := req.WorkingDir
	if workDir == "" {
		workDir = s.workspaceDir
	}

	cmd := exec.CommandContext(ctx, req.Command[0], req.Command[1:]...)
	cmd.Dir = workDir

	// Set environment variables
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stream <- &ExecLog{
			Stderr:   fmt.Sprintf("failed to create stdout pipe: %v", err),
			ExitCode: 1,
			Done:     true,
		}
		return nil
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stream <- &ExecLog{
			Stderr:   fmt.Sprintf("failed to create stderr pipe: %v", err),
			ExitCode: 1,
			Done:     true,
		}
		return nil
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		stream <- &ExecLog{
			Stderr:   fmt.Sprintf("failed to start command: %v", err),
			ExitCode: 1,
			Done:     true,
		}
		return nil
	}

	// Store process if background
	if req.Background {
		s.processes[cmd.Process.Pid] = cmd
	}

	// Use WaitGroup to ensure all goroutines complete
	var wg sync.WaitGroup
	wg.Add(2)

	// Read stdout
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case stream <- &ExecLog{
				Stdout: scanner.Text() + "\n",
				Done:   false,
			}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Read stderr
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			select {
			case stream <- &ExecLog{
				Stderr: scanner.Text() + "\n",
				Done:   false,
			}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Apply timeout if specified
	if req.TimeoutSeconds > 0 {
		timer := time.AfterFunc(time.Duration(req.TimeoutSeconds)*time.Second, func() {
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		})
		defer timer.Stop()
	}

	// Wait for command to complete
	exitCode := int32(0)
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		} else {
			exitCode = 1
		}
	}

	// Wait for all output to be read
	wg.Wait()

	// Remove from processes map
	if cmd.Process != nil {
		delete(s.processes, cmd.Process.Pid)
	}

	stream <- &ExecLog{
		ExitCode: exitCode,
		Done:     true,
	}

	return nil
}

// ExecuteSync is a synchronous version of Execute
func (s *AgentService) ExecuteSync(ctx context.Context, req *ExecRequest) (*ExecLog, error) {
	stream := make(chan *ExecLog, 100)

	go s.Execute(ctx, req, stream)

	var result ExecLog
	var stdout, stderr strings.Builder

	for log := range stream {
		if log.Stdout != "" {
			stdout.WriteString(log.Stdout)
		}
		if log.Stderr != "" {
			stderr.WriteString(log.Stderr)
		}
		if log.Done {
			result.ExitCode = log.ExitCode
			result.Done = true
		}
	}

	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	return &result, nil
}
