package sidecar

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

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

// HasExecutor reports whether an executor agent is configured.
func (s *AgentService) HasExecutor() bool {
	return s.executorClient != nil
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

	// All commands must execute in the executor (main) container
	if s.executorClient == nil {
		stream <- &ExecLog{
			Stderr:   "executor agent not configured: sidecar started without --executor-socket",
			ExitCode: 1,
			Done:     true,
		}
		return nil
	}

	return s.executeViaAgent(ctx, req, stream)
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
