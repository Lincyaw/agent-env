package execagent

import (
	// "bufio" removed: chunk-based I/O replaced line-based Scanner
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const fileChunkSize = 1024 * 1024
const sidecarSocketGID = 65532

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

	// The sidecar image runs as distroless nonroot (gid 65532). The executor
	// usually runs as root in the user image, so make the pod-local Unix socket
	// group-readable by the sidecar. Fall back to a pod-local world-writable
	// socket if the executor image is non-root and cannot chown.
	socketMode := os.FileMode(0660)
	if err := os.Chown(a.socketPath, -1, sidecarSocketGID); err != nil {
		log.Printf("warning: chown socket group to %d failed: %v; falling back to 0666", sidecarSocketGID, err)
		socketMode = 0666
	}
	if err := os.Chmod(a.socketPath, socketMode); err != nil {
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

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req Request
		if err := decoder.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			resp := Response{Error: fmt.Sprintf("invalid request: %v", err), Done: true}
			encoder.Encode(resp)
			return
		}

		switch req.Type {
		case "exec":
			log.Printf("[exec] id=%s cmd=%v workdir=%s", req.ID, req.Cmd, req.WorkDir)
			a.handleExec(ctx, req, encoder)
		case "write_file_stream":
			log.Printf("[write_file_stream] id=%s path=%s", req.ID, req.Path)
			a.handleWriteFileStream(ctx, req, decoder, encoder)
		case "read_file_stream":
			log.Printf("[read_file] id=%s path=%s", req.ID, req.Path)
			a.handleReadFileStream(ctx, req, encoder)
		case "shell":
			log.Printf("[shell] id=%s workdir=%s", req.ID, req.WorkDir)
			a.handleShell(ctx, req, decoder, encoder)
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

	// Mutex-protected send to avoid concurrent writes to the encoder
	var encMu sync.Mutex
	send := func(resp Response) {
		encMu.Lock()
		encoder.Encode(resp)
		encMu.Unlock()
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Stream stdout in fixed-size chunks (no line-length limit).
	go func() {
		defer wg.Done()
		buf := make([]byte, 64*1024)
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

	// Stream stderr in fixed-size chunks.
	go func() {
		defer wg.Done()
		buf := make([]byte, 64*1024)
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
	send(Response{ID: req.ID, ExitCode: &exitCode, Done: true})
}

func (a *Agent) handleWriteFileStream(ctx context.Context, req Request, decoder *json.Decoder, encoder *json.Encoder) {
	if req.Path == "" {
		encoder.Encode(Response{ID: req.ID, Error: "path is required", Done: true})
		return
	}

	targetPath, err := a.resolveWorkspacePath(req.Path)
	if err != nil {
		encoder.Encode(Response{ID: req.ID, Error: err.Error(), Done: true})
		return
	}

	parent := filepath.Dir(targetPath)
	if err := os.MkdirAll(parent, 0755); err != nil {
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("mkdir parent: %v", err), Done: true})
		return
	}

	tmp, err := os.CreateTemp(parent, "."+filepath.Base(targetPath)+".*.tmp")
	if err != nil {
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("create temp file: %v", err), Done: true})
		return
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		tmp.Close()
		if !committed {
			os.Remove(tmpPath)
		}
	}()

	hash := sha256.New()
	var written int64
	for {
		select {
		case <-ctx.Done():
			encoder.Encode(Response{ID: req.ID, Error: ctx.Err().Error(), Done: true})
			return
		default:
		}

		var chunk Request
		if err := decoder.Decode(&chunk); err != nil {
			encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("read file chunk: %v", err), Done: true})
			return
		}
		if chunk.ID != "" && chunk.ID != req.ID {
			encoder.Encode(Response{ID: req.ID, Error: "file chunk id mismatch", Done: true})
			return
		}
		switch chunk.Type {
		case "write_file_chunk":
			if len(chunk.Content) == 0 {
				continue
			}
			n, err := tmp.Write(chunk.Content)
			if err != nil {
				encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("write file chunk: %v", err), Done: true})
				return
			}
			if n != len(chunk.Content) {
				encoder.Encode(Response{ID: req.ID, Error: "short write file chunk", Done: true})
				return
			}
			if _, err := hash.Write(chunk.Content); err != nil {
				encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("hash file chunk: %v", err), Done: true})
				return
			}
			written += int64(n)
		case "write_file_finish":
			actualSHA := hex.EncodeToString(hash.Sum(nil))
			if req.ExpectedSHA256 != "" && !strings.EqualFold(req.ExpectedSHA256, actualSHA) {
				encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("sha256 mismatch: expected %s got %s", req.ExpectedSHA256, actualSHA), Done: true})
				return
			}
			if err := tmp.Chmod(0644); err != nil {
				encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("chmod temp file: %v", err), Done: true})
				return
			}
			if err := tmp.Close(); err != nil {
				encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("close temp file: %v", err), Done: true})
				return
			}
			if err := os.Rename(tmpPath, targetPath); err != nil {
				encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("commit file: %v", err), Done: true})
				return
			}
			committed = true
			encoder.Encode(Response{ID: req.ID, BytesWritten: &written, SHA256: actualSHA, Done: true})
			return
		default:
			encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("unexpected file stream message: %s", chunk.Type), Done: true})
			return
		}
	}
}

func (a *Agent) handleReadFileStream(ctx context.Context, req Request, encoder *json.Encoder) {
	if req.Path == "" {
		encoder.Encode(Response{ID: req.ID, Error: "path is required", Done: true})
		return
	}

	targetPath, err := a.resolveWorkspacePath(req.Path)
	if err != nil {
		encoder.Encode(Response{ID: req.ID, Error: err.Error(), Done: true})
		return
	}

	file, err := os.Open(targetPath)
	if err != nil {
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("open file: %v", err), Done: true})
		return
	}
	defer file.Close()

	hash := sha256.New()
	buf := make([]byte, fileChunkSize)
	var offset int64
	for {
		select {
		case <-ctx.Done():
			encoder.Encode(Response{ID: req.ID, Error: ctx.Err().Error(), Done: true})
			return
		default:
		}

		n, err := file.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			if _, hashErr := hash.Write(chunk); hashErr != nil {
				encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("hash file chunk: %v", hashErr), Done: true})
				return
			}
			if encodeErr := encoder.Encode(Response{ID: req.ID, Offset: offset, Content: chunk}); encodeErr != nil {
				return
			}
			offset += int64(n)
		}
		if errors.Is(err, io.EOF) {
			size := offset
			encoder.Encode(Response{
				ID:        req.ID,
				SizeBytes: &size,
				SHA256:    hex.EncodeToString(hash.Sum(nil)),
				Done:      true,
			})
			return
		}
		if err != nil {
			encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("read file: %v", err), Done: true})
			return
		}
	}
}

func (a *Agent) resolveWorkspacePath(relPath string) (string, error) {
	if strings.ContainsRune(relPath, 0) {
		return "", fmt.Errorf("path must not contain NUL bytes")
	}
	clean := filepath.Clean(relPath)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("path must be relative to the workspace")
	}

	workspaceRoot, err := filepath.Abs(a.workspaceDir)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(workspaceRoot); err == nil {
		workspaceRoot = resolved
	}

	targetPath := filepath.Join(workspaceRoot, clean)

	// Resolve symlinks on the target to prevent escaping the workspace via symlink.
	// Use the longest existing prefix for paths that don't fully exist yet.
	resolvedTarget := targetPath
	if resolved, err := filepath.EvalSymlinks(targetPath); err == nil {
		resolvedTarget = resolved
	} else {
		dir := filepath.Dir(targetPath)
		if resolvedDir, dirErr := filepath.EvalSymlinks(dir); dirErr == nil {
			resolvedTarget = filepath.Join(resolvedDir, filepath.Base(targetPath))
		}
	}

	relToRoot, err := filepath.Rel(workspaceRoot, resolvedTarget)
	if err != nil {
		return "", fmt.Errorf("validate target path: %w", err)
	}
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", errors.New("path must stay within the workspace")
	}

	return resolvedTarget, nil
}

func (a *Agent) handleSignal(req Request, encoder *json.Encoder) {
	// Only allow signaling PIDs that we are tracking
	a.mu.Lock()
	cmd, tracked := a.processes[req.PID]
	a.mu.Unlock()

	if !tracked {
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("unknown or untracked PID: %d", req.PID), Done: true})
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

	if err := cmd.Process.Signal(sig); err != nil {
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("signal: %v", err), Done: true})
		return
	}

	encoder.Encode(Response{ID: req.ID, Done: true})
}

func (a *Agent) handleShell(ctx context.Context, req Request, decoder *json.Decoder, encoder *json.Encoder) {
	// Prefer bash, fall back to /bin/sh
	shellPath := "/bin/bash"
	if _, err := exec.LookPath("bash"); err != nil {
		shellPath = "/bin/sh"
	}

	workDir := req.WorkDir
	if workDir == "" {
		workDir = a.workspaceDir
	}

	cmd := exec.CommandContext(ctx, shellPath, "-i")
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	if os.Getenv("TERM") == "" {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	}
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	rows, cols := uint16(24), uint16(80)
	if req.Rows > 0 {
		rows = uint16(req.Rows)
	}
	if req.Cols > 0 {
		cols = uint16(req.Cols)
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		encoder.Encode(Response{ID: req.ID, Error: fmt.Sprintf("start shell: %v", err), Done: true})
		return
	}
	defer ptmx.Close()

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
	wg.Add(1)

	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := ptmx.Read(buf)
			if n > 0 {
				send(Response{ID: req.ID, Stdout: string(buf[:n])})
			}
			if readErr != nil {
				return
			}
		}
	}()

	go func() {
		for {
			var sub Request
			if err := decoder.Decode(&sub); err != nil {
				ptmx.Close()
				return
			}
			switch sub.Type {
			case "stdin":
				if _, err := io.WriteString(ptmx, sub.Data); err != nil {
					return
				}
			case "resize":
				if sub.Rows <= 0 || sub.Cols <= 0 {
					continue
				}
				_ = pty.Setsize(ptmx, &pty.Winsize{
					Rows: uint16(sub.Rows),
					Cols: uint16(sub.Cols),
				})
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
	}()

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	ptmx.Close()

	wg.Wait()

	a.mu.Lock()
	delete(a.processes, cmd.Process.Pid)
	a.mu.Unlock()

	log.Printf("[shell] id=%s exit_code=%d", req.ID, exitCode)
	send(Response{ID: req.ID, ExitCode: &exitCode, Done: true})
}
