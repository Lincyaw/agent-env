package sidecar

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// FileRequest contains file operations to perform
type FileRequest struct {
	BasePath string
	Files    map[string]string
	Patch    string
}

// GetBasePath implements interfaces.FileUpdateRequest
func (r *FileRequest) GetBasePath() string {
	return r.BasePath
}

// GetFiles implements interfaces.FileUpdateRequest
func (r *FileRequest) GetFiles() map[string]string {
	return r.Files
}

// GetPatch implements interfaces.FileUpdateRequest
func (r *FileRequest) GetPatch() string {
	return r.Patch
}

// FileResponse indicates success or failure
type FileResponse struct {
	Success bool
	Message string
}

// IsSuccess implements interfaces.FileUpdateResponse
func (r *FileResponse) IsSuccess() bool {
	return r.Success
}

// GetMessage implements interfaces.FileUpdateResponse
func (r *FileResponse) GetMessage() string {
	return r.Message
}

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

// SignalRequest specifies a signal to send
type SignalRequest struct {
	PID    int32
	Signal string
}

// SignalResponse indicates success or failure
type SignalResponse struct {
	Success bool
	Message string
}

// ResetRequest triggers workspace cleanup
type ResetRequest struct {
	PreserveFiles bool
}

// ShouldPreserveFiles implements interfaces.ResetRequest
func (r *ResetRequest) ShouldPreserveFiles() bool {
	return r.PreserveFiles
}

// ResetResponse indicates success or failure
type ResetResponse struct {
	Success bool
	Message string
}

// IsSuccess implements interfaces.ResetResponse
func (r *ResetResponse) IsSuccess() bool {
	return r.Success
}

// GetMessage implements interfaces.ResetResponse
func (r *ResetResponse) GetMessage() string {
	return r.Message
}

// AgentService implements the sidecar functionality
type AgentService struct {
	workspaceDir string
	processes    map[int]*exec.Cmd
}

// NewAgentService creates a new agent service
func NewAgentService(workspaceDir string) *AgentService {
	return &AgentService{
		workspaceDir: workspaceDir,
		processes:    make(map[int]*exec.Cmd),
	}
}

// UpdateFiles applies file patches or overwrites
func (s *AgentService) UpdateFiles(ctx context.Context, req *FileRequest) (*FileResponse, error) {
	basePath := req.BasePath
	if basePath == "" {
		basePath = s.workspaceDir
	}

	// Ensure base path exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return &FileResponse{
			Success: false,
			Message: fmt.Sprintf("failed to create base path: %v", err),
		}, nil
	}

	// Write files
	for path, content := range req.Files {
		fullPath := filepath.Join(basePath, path)
		dir := filepath.Dir(fullPath)

		if err := os.MkdirAll(dir, 0755); err != nil {
			return &FileResponse{
				Success: false,
				Message: fmt.Sprintf("failed to create directory %s: %v", dir, err),
			}, nil
		}

		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return &FileResponse{
				Success: false,
				Message: fmt.Sprintf("failed to write file %s: %v", fullPath, err),
			}, nil
		}
	}

	// TODO: Apply patch if provided (would need patch utility)

	return &FileResponse{
		Success: true,
		Message: fmt.Sprintf("successfully updated %d files", len(req.Files)),
	}, nil
}

// Execute runs a command and streams output
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

// SignalProcess sends a signal to a process
func (s *AgentService) SignalProcess(ctx context.Context, req *SignalRequest) (*SignalResponse, error) {
	process, err := os.FindProcess(int(req.PID))
	if err != nil {
		return &SignalResponse{
			Success: false,
			Message: fmt.Sprintf("failed to find process: %v", err),
		}, nil
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
		return &SignalResponse{
			Success: false,
			Message: fmt.Sprintf("unsupported signal: %s", req.Signal),
		}, nil
	}

	if err := process.Signal(sig); err != nil {
		return &SignalResponse{
			Success: false,
			Message: fmt.Sprintf("failed to send signal: %v", err),
		}, nil
	}

	return &SignalResponse{
		Success: true,
		Message: fmt.Sprintf("signal %s sent to process %d", req.Signal, req.PID),
	}, nil
}

// Reset cleans the workspace
func (s *AgentService) Reset(ctx context.Context, req *ResetRequest) (*ResetResponse, error) {
	// Kill all tracked processes
	for pid, cmd := range s.processes {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		delete(s.processes, pid)
	}

	// Clean workspace if not preserving files
	if !req.PreserveFiles {
		if err := os.RemoveAll(s.workspaceDir); err != nil {
			return &ResetResponse{
				Success: false,
				Message: fmt.Sprintf("failed to clean workspace: %v", err),
			}, nil
		}
		if err := os.MkdirAll(s.workspaceDir, 0755); err != nil {
			return &ResetResponse{
				Success: false,
				Message: fmt.Sprintf("failed to recreate workspace: %v", err),
			}, nil
		}
	}

	return &ResetResponse{
		Success: true,
		Message: "workspace reset successfully",
	}, nil
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
