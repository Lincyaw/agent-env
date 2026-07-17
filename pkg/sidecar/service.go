package sidecar

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/google/uuid"
)

// ExecRequest specifies a command to execute
type ExecRequest struct {
	Command        []string
	Env            map[string]string
	WorkingDir     string
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

// FileWriteResult is an alias for the canonical definition in pkg/interfaces.
type FileWriteResult = interfaces.FileWriteResult

// FileReadResult is an alias for the canonical definition in pkg/interfaces.
type FileReadResult = interfaces.FileReadResult

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
	executorClient *ExecutorClient
	Logs           *LogBuffer
}

// NewAgentService creates a new agent service
func NewAgentService(workspaceDir string) *AgentService {
	return &AgentService{
		workspaceDir: workspaceDir,
		Logs:         NewLogBuffer(2000),
	}
}

// NewAgentServiceWithExecutor creates a new agent service that proxies to an executor agent.
func NewAgentServiceWithExecutor(workspaceDir, executorSocket string) *AgentService {
	return &AgentService{
		workspaceDir:   workspaceDir,
		executorClient: NewExecutorClient(executorSocket),
		Logs:           NewLogBuffer(2000),
	}
}

func (s *AgentService) logInfo(source, msg string) {
	s.Logs.Append(LogLine{Timestamp: time.Now(), Level: "info", Message: msg, Source: source})
}

func (s *AgentService) logError(source, msg string) {
	s.Logs.Append(LogLine{Timestamp: time.Now(), Level: "error", Message: msg, Source: source})
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
		s.logError("sidecar", "executor agent not configured")
		stream <- &ExecLog{
			Stderr:   "executor agent not configured: sidecar started without --executor-socket",
			ExitCode: 1,
			Done:     true,
		}
		return nil
	}

	cmdStr := strings.Join(req.Command, " ")
	s.logInfo("exec", fmt.Sprintf("exec: %s", cmdStr))
	err := s.executeViaAgent(ctx, req, stream)
	if err != nil {
		s.logError("exec", fmt.Sprintf("exec failed: %s: %v", cmdStr, err))
	}
	return err
}

// executeViaAgent forwards execution to the executor agent in the main container.
func (s *AgentService) executeViaAgent(ctx context.Context, req *ExecRequest, stream chan<- *ExecLog) error {
	agentReq := executorRequest{
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

// WriteFile streams one file into the executor container workspace.
func (s *AgentService) WriteFile(ctx context.Context, path string, content io.Reader, expectedSHA256 string) (*FileWriteResult, error) {
	if s.executorClient == nil {
		return nil, fmt.Errorf("executor agent not configured: sidecar started without --executor-socket")
	}
	s.logInfo("sidecar", fmt.Sprintf("write file: %s", path))
	return s.executorClient.WriteFile(ctx, path, content, expectedSHA256)
}

func (s *AgentService) ReadFile(ctx context.Context, path string, dst io.Writer) (*FileReadResult, error) {
	if s.executorClient == nil {
		return nil, fmt.Errorf("executor agent not configured: sidecar started without --executor-socket")
	}
	s.logInfo("sidecar", fmt.Sprintf("read file: %s", path))
	return s.executorClient.ReadFile(ctx, path, dst)
}

// Stat returns file metadata by running stat(1) via the executor.
func (s *AgentService) Stat(ctx context.Context, path string) (*interfaces.StatResult, error) {
	if s.executorClient == nil {
		return nil, fmt.Errorf("executor agent not configured: sidecar started without --executor-socket")
	}
	s.logInfo("sidecar", fmt.Sprintf("stat: %s", path))

	cmd := []string{"sh", "-c", fmt.Sprintf(
		`if [ -e %q ]; then stat -c '%%F %%s %%a %%Y' %q; else echo NOTFOUND; fi`,
		path, path,
	)}
	result, err := s.ExecuteSync(ctx, &ExecRequest{Command: cmd, TimeoutSeconds: 10})
	if err != nil {
		return nil, fmt.Errorf("stat exec: %w", err)
	}
	output := strings.TrimSpace(result.Stdout)
	if output == "NOTFOUND" || result.ExitCode != 0 {
		return &interfaces.StatResult{Exists: false}, nil
	}

	// Output format: "<type> <size> <mode> <mtime>"
	// e.g. "regular file 1234 644 1720000000"
	// The type field may contain spaces ("regular file", "directory"), so we
	// split from the right: mtime, mode, size are single tokens; everything
	// before size is the type string.
	parts := strings.Fields(output)
	if len(parts) < 4 {
		return nil, fmt.Errorf("unexpected stat output: %q", output)
	}
	mtime := parts[len(parts)-1]
	mode := parts[len(parts)-2]
	sizeStr := parts[len(parts)-3]
	typeParts := parts[:len(parts)-3]
	fileType := strings.Join(typeParts, " ")

	size, _ := strconv.ParseUint(sizeStr, 10, 64)

	return &interfaces.StatResult{
		Exists:   true,
		IsDir:    fileType == "directory",
		Size:     size,
		Mode:     mode,
		Modified: mtime,
	}, nil
}

// ListDir lists directory contents by running ls or find via the executor.
func (s *AgentService) ListDir(ctx context.Context, path string, recursive bool) ([]interfaces.DirEntry, error) {
	if s.executorClient == nil {
		return nil, fmt.Errorf("executor agent not configured: sidecar started without --executor-socket")
	}
	s.logInfo("sidecar", fmt.Sprintf("list dir: %s (recursive=%v)", path, recursive))

	var cmd []string
	if recursive {
		// find outputs: type(d/f) size relative-path
		cmd = []string{"find", path, "-mindepth", "1", "-printf", "%y %s %P\n"}
	} else {
		// stat each entry: type size name
		cmd = []string{"sh", "-c", fmt.Sprintf(
			`for f in %q/*; do [ -e "$f" ] && stat -c '%%F %%s %%f' "$f"; done`,
			path,
		)}
	}

	result, err := s.ExecuteSync(ctx, &ExecRequest{Command: cmd, TimeoutSeconds: 30})
	if err != nil {
		return nil, fmt.Errorf("listdir exec: %w", err)
	}
	if result.ExitCode != 0 && strings.TrimSpace(result.Stderr) != "" {
		return nil, fmt.Errorf("listdir: %s", strings.TrimSpace(result.Stderr))
	}

	var entries []interfaces.DirEntry
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if recursive {
			// find -printf "%y %s %P\n" → "d 4096 subdir" or "f 123 file.txt"
			fields := strings.SplitN(line, " ", 3)
			if len(fields) < 3 {
				continue
			}
			size, _ := strconv.ParseUint(fields[1], 10, 64)
			entries = append(entries, interfaces.DirEntry{
				Name:  fields[2],
				IsDir: fields[0] == "d",
				Size:  size,
			})
		} else {
			// stat -c '%F %s %f' → "regular file 123 filename" or "directory 4096 dirname"
			// %f is the basename. Split from right: name (1 token), size (1 token), rest is type.
			fields := strings.Fields(line)
			if len(fields) < 3 {
				continue
			}
			name := fields[len(fields)-1]
			sizeStr := fields[len(fields)-2]
			typeParts := fields[:len(fields)-2]
			fileType := strings.Join(typeParts, " ")

			size, _ := strconv.ParseUint(sizeStr, 10, 64)
			entries = append(entries, interfaces.DirEntry{
				Name:  name,
				IsDir: fileType == "directory",
				Size:  size,
			})
		}
	}

	return entries, nil
}

// WriteStdin sends data to a running process identified by handle.
// The V1 executor protocol does not support sending stdin to arbitrary
// process handles outside of a shell session, so this returns an error.
func (s *AgentService) WriteStdin(ctx context.Context, handle string, data []byte) error {
	if s.executorClient == nil {
		return fmt.Errorf("executor agent not configured: sidecar started without --executor-socket")
	}
	s.logInfo("sidecar", fmt.Sprintf("write stdin: handle=%s len=%d", handle, len(data)))
	return fmt.Errorf("WriteStdin is not supported in V1 executor mode; use interactive shell instead")
}
