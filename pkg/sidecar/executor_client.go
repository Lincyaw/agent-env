package sidecar

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/Lincyaw/agent-env/pkg/execagent"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
	pb "github.com/Lincyaw/agent-env/pkg/pb/executor_v2"
	"google.golang.org/protobuf/proto"
)

const fileTransferChunkSize = interfaces.FileTransferChunkSize

// executorProtocol selects the wire format for sidecar-to-executor communication.
type executorProtocol int

const (
	protocolV1JSON     executorProtocol = iota // JSON-over-Unix-socket (legacy)
	protocolV2Protobuf                         // length-delimited protobuf Envelope
)

// ExecutorClient communicates with the executor agent over a Unix socket.
// It supports both the V1 JSON protocol and the V2 protobuf protocol,
// selected by the EXECUTOR_PROTOCOL environment variable ("v1" or "v2").
type ExecutorClient struct {
	socketPath string
	protocol   executorProtocol
	mu         sync.Mutex
}

// NewExecutorClient creates a new executor client.
// The wire protocol is selected by the EXECUTOR_PROTOCOL env var:
// "v2" selects protobuf, anything else keeps the legacy JSON protocol.
func NewExecutorClient(socketPath string) *ExecutorClient {
	p := protocolV1JSON
	if os.Getenv("EXECUTOR_PROTOCOL") == "v2" {
		p = protocolV2Protobuf
	}
	return &ExecutorClient{socketPath: socketPath, protocol: p}
}

func (c *ExecutorClient) dial() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to executor agent at %s: %w", c.socketPath, err)
	}
	return conn, nil
}

// ---------------------------------------------------------------------------
// Protobuf envelope helpers (V2)
// ---------------------------------------------------------------------------

func writeEnvelope(conn net.Conn, env *pb.Envelope) error {
	data, err := proto.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))
	if _, err := conn.Write(lenBuf); err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}

func readEnvelope(conn net.Conn) (*pb.Envelope, error) {
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return nil, err
	}
	msgLen := binary.BigEndian.Uint32(lenBuf)
	if msgLen > 128*1024*1024 {
		return nil, fmt.Errorf("envelope too large: %d bytes", msgLen)
	}
	data := make([]byte, msgLen)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, err
	}
	env := &pb.Envelope{}
	if err := proto.Unmarshal(data, env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return env, nil
}

// sendRequest wraps a Request in an Envelope and writes it.
func sendRequest(conn net.Conn, req *pb.Request) error {
	return writeEnvelope(conn, &pb.Envelope{
		Payload: &pb.Envelope_Request{Request: req},
	})
}

// ---------------------------------------------------------------------------
// Ping
// ---------------------------------------------------------------------------

func (c *ExecutorClient) Ping() error {
	if c.protocol == protocolV2Protobuf {
		return c.pingV2()
	}
	return c.pingV1()
}

func (c *ExecutorClient) pingV1() error {
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

func (c *ExecutorClient) pingV2() error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := sendRequest(conn, &pb.Request{
		Id:     "ping-0",
		Method: &pb.Request_Ping{Ping: &pb.PingRequest{}},
	}); err != nil {
		return fmt.Errorf("send ping: %w", err)
	}

	env, err := readEnvelope(conn)
	if err != nil {
		return fmt.Errorf("read ping response: %w", err)
	}

	resp := env.GetResponse()
	if resp == nil {
		return fmt.Errorf("unexpected envelope type in ping response")
	}
	if errResp := resp.GetError(); errResp != nil {
		return fmt.Errorf("ping error: [%s] %s", errResp.GetCode(), errResp.GetMessage())
	}
	return nil
}

// ---------------------------------------------------------------------------
// Execute
// ---------------------------------------------------------------------------

func (c *ExecutorClient) Execute(ctx context.Context, req execagent.Request) (<-chan execagent.Response, error) {
	if c.protocol == protocolV2Protobuf {
		return c.executeV2(ctx, req)
	}
	return c.executeV1(ctx, req)
}

func (c *ExecutorClient) executeV1(ctx context.Context, req execagent.Request) (<-chan execagent.Response, error) {
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

func (c *ExecutorClient) executeV2(ctx context.Context, req execagent.Request) (<-chan execagent.Response, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}

	spawnReq := &pb.SpawnRequest{
		Cmd:         req.Cmd,
		Env:         req.Env,
		Workdir:     req.WorkDir,
		TimeoutSecs: uint64(req.Timeout),
	}

	if err := sendRequest(conn, &pb.Request{
		Id:     req.ID,
		Method: &pb.Request_Spawn{Spawn: spawnReq},
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send spawn request: %w", err)
	}

	ch := make(chan execagent.Response, 100)

	go func() {
		defer close(ch)
		defer conn.Close()

		// The first response should be a SpawnResponse with the handle.
		var handle string

		for {
			env, err := readEnvelope(conn)
			if err != nil {
				ch <- execagent.Response{ID: req.ID, Error: fmt.Sprintf("read: %v", err), Done: true}
				return
			}

			var resp execagent.Response
			resp.ID = req.ID

			switch p := env.GetPayload().(type) {
			case *pb.Envelope_Response:
				r := p.Response
				switch result := r.GetResult().(type) {
				case *pb.Response_Spawn:
					handle = result.Spawn.GetHandle()
					_ = handle
					continue
				case *pb.Response_Ok:
					resp.Done = true
				case *pb.Response_Error:
					resp.Error = result.Error.GetMessage()
					resp.Done = true
				default:
					resp.Error = fmt.Sprintf("unexpected response type: %T", result)
					resp.Done = true
				}

			case *pb.Envelope_Event:
				e := p.Event
				switch ev := e.GetEvent().(type) {
				case *pb.Event_Stdout:
					resp.Stdout = string(ev.Stdout.GetData())
				case *pb.Event_Stderr:
					resp.Stderr = string(ev.Stderr.GetData())
				case *pb.Event_Exit:
					code := int(ev.Exit.GetExitCode())
					resp.ExitCode = &code
					resp.Done = true
				default:
					continue
				}

			default:
				resp.Error = fmt.Sprintf("unexpected envelope payload: %T", p)
				resp.Done = true
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

// ---------------------------------------------------------------------------
// Signal
// ---------------------------------------------------------------------------

func (c *ExecutorClient) Signal(pid int, signal string) error {
	if c.protocol == protocolV2Protobuf {
		return c.signalV2(fmt.Sprintf("pid-%d", pid), signal)
	}
	return c.signalV1(pid, signal)
}

func (c *ExecutorClient) signalV1(pid int, signal string) error {
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

func (c *ExecutorClient) signalV2(handle, signal string) error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := sendRequest(conn, &pb.Request{
		Id: "sig-0",
		Method: &pb.Request_Signal{Signal: &pb.SignalRequest{
			Handle: handle,
			Signal: signal,
		}},
	}); err != nil {
		return fmt.Errorf("send signal request: %w", err)
	}

	env, err := readEnvelope(conn)
	if err != nil {
		return fmt.Errorf("read signal response: %w", err)
	}
	resp := env.GetResponse()
	if resp == nil {
		return fmt.Errorf("unexpected envelope type in signal response")
	}
	if errResp := resp.GetError(); errResp != nil {
		return fmt.Errorf("signal error: [%s] %s", errResp.GetCode(), errResp.GetMessage())
	}
	return nil
}

// ---------------------------------------------------------------------------
// WriteFile
// ---------------------------------------------------------------------------

func (c *ExecutorClient) WriteFile(ctx context.Context, path string, content io.Reader, expectedSHA256 string) (*FileWriteResult, error) {
	if c.protocol == protocolV2Protobuf {
		return c.writeFileV2(ctx, path, content, expectedSHA256)
	}
	return c.writeFileV1(ctx, path, content, expectedSHA256)
}

func (c *ExecutorClient) writeFileV1(ctx context.Context, path string, content io.Reader, expectedSHA256 string) (*FileWriteResult, error) {
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

func (c *ExecutorClient) writeFileV2(ctx context.Context, path string, content io.Reader, expectedSHA256 string) (*FileWriteResult, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reqID := fmt.Sprintf("write-%d", time.Now().UnixNano())

	if err := sendRequest(conn, &pb.Request{
		Id: reqID,
		Method: &pb.Request_WriteFile{WriteFile: &pb.WriteFileRequest{
			Path:           path,
			ExpectedSha256: expectedSHA256,
		}},
	}); err != nil {
		return nil, fmt.Errorf("send write_file request: %w", err)
	}

	buf := make([]byte, fileTransferChunkSize)
	for {
		n, readErr := content.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if err := sendRequest(conn, &pb.Request{
				Id: reqID,
				Method: &pb.Request_FileChunk{FileChunk: &pb.FileChunkData{
					Content: chunk,
				}},
			}); err != nil {
				return nil, fmt.Errorf("send file chunk: %w", err)
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

	if err := sendRequest(conn, &pb.Request{
		Id:     reqID,
		Method: &pb.Request_FileDone{FileDone: &pb.FileDoneRequest{}},
	}); err != nil {
		return nil, fmt.Errorf("send file done: %w", err)
	}

	for {
		env, err := readEnvelope(conn)
		if err != nil {
			return nil, fmt.Errorf("read write_file response: %w", err)
		}

		resp := env.GetResponse()
		if resp == nil {
			continue
		}

		switch result := resp.GetResult().(type) {
		case *pb.Response_Error:
			return nil, fmt.Errorf("write_file error: [%s] %s", result.Error.GetCode(), result.Error.GetMessage())
		case *pb.Response_WriteFile:
			return &FileWriteResult{
				Path:         path,
				BytesWritten: result.WriteFile.GetBytesWritten(),
				SHA256:       result.WriteFile.GetSha256(),
			}, nil
		case *pb.Response_Ok:
			continue
		default:
			return nil, fmt.Errorf("unexpected write_file response: %T", result)
		}
	}
}

// ---------------------------------------------------------------------------
// ReadFile
// ---------------------------------------------------------------------------

func (c *ExecutorClient) ReadFile(ctx context.Context, path string, dst io.Writer) (*FileReadResult, error) {
	if c.protocol == protocolV2Protobuf {
		return c.readFileV2(ctx, path, dst)
	}
	return c.readFileV1(ctx, path, dst)
}

func (c *ExecutorClient) readFileV1(ctx context.Context, path string, dst io.Writer) (*FileReadResult, error) {
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

func (c *ExecutorClient) readFileV2(ctx context.Context, path string, dst io.Writer) (*FileReadResult, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reqID := fmt.Sprintf("read-%d", time.Now().UnixNano())

	if err := sendRequest(conn, &pb.Request{
		Id: reqID,
		Method: &pb.Request_ReadFile{ReadFile: &pb.ReadFileRequest{
			Path: path,
		}},
	}); err != nil {
		return nil, fmt.Errorf("send read_file request: %w", err)
	}

	for {
		env, err := readEnvelope(conn)
		if err != nil {
			return nil, fmt.Errorf("read read_file response: %w", err)
		}

		resp := env.GetResponse()
		if resp == nil {
			continue
		}

		switch result := resp.GetResult().(type) {
		case *pb.Response_Error:
			return nil, fmt.Errorf("read_file error: [%s] %s", result.Error.GetCode(), result.Error.GetMessage())
		case *pb.Response_FileChunk:
			if len(result.FileChunk.GetContent()) > 0 {
				if _, err := dst.Write(result.FileChunk.GetContent()); err != nil {
					return nil, fmt.Errorf("write downloaded content: %w", err)
				}
			}
		case *pb.Response_FileDone:
			return &FileReadResult{
				Path:      path,
				SizeBytes: result.FileDone.GetSizeBytes(),
				SHA256:    result.FileDone.GetSha256(),
			}, nil
		default:
			return nil, fmt.Errorf("unexpected read_file response: %T", result)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
}

// ---------------------------------------------------------------------------
// WaitForReady
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// ShellSession
// ---------------------------------------------------------------------------

// ShellSession represents an interactive shell running in the executor container.
// It works with both V1 JSON and V2 protobuf protocols; the protocol is fixed
// at creation time based on the parent ExecutorClient.
type ShellSession struct {
	conn     net.Conn
	mu       sync.Mutex
	protocol executorProtocol

	// V1 fields
	encoder *json.Encoder

	// V2 fields
	handle string

	id     string
	Output chan execagent.Response
}

// StartShell opens a connection to the executor agent and starts an interactive shell.
func (c *ExecutorClient) StartShell(ctx context.Context, workDir string, env map[string]string) (*ShellSession, error) {
	if c.protocol == protocolV2Protobuf {
		return c.startShellV2(ctx, workDir, env)
	}
	return c.startShellV1(ctx, workDir, env)
}

func (c *ExecutorClient) startShellV1(ctx context.Context, workDir string, env map[string]string) (*ShellSession, error) {
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
		conn:     conn,
		protocol: protocolV1JSON,
		encoder:  encoder,
		id:       id,
		Output:   make(chan execagent.Response, 100),
	}

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

func (c *ExecutorClient) startShellV2(ctx context.Context, workDir string, env map[string]string) (*ShellSession, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}

	id := fmt.Sprintf("shell-%d", time.Now().UnixNano())

	spawnReq := &pb.SpawnRequest{
		Cmd:          []string{"/bin/bash", "-i"},
		Env:          env,
		Workdir:      workDir,
		StdinEnabled: true,
		Pty:          &pb.PtyConfig{Rows: 24, Cols: 80},
	}

	if err := sendRequest(conn, &pb.Request{
		Id:     id,
		Method: &pb.Request_Spawn{Spawn: spawnReq},
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send shell spawn request: %w", err)
	}

	// Read the SpawnResponse to get the handle.
	env2, err := readEnvelope(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read shell spawn response: %w", err)
	}

	resp := env2.GetResponse()
	if resp == nil {
		conn.Close()
		return nil, fmt.Errorf("unexpected envelope type in shell spawn response")
	}
	spawnResp := resp.GetSpawn()
	if spawnResp == nil {
		if errResp := resp.GetError(); errResp != nil {
			conn.Close()
			return nil, fmt.Errorf("shell spawn error: [%s] %s", errResp.GetCode(), errResp.GetMessage())
		}
		conn.Close()
		return nil, fmt.Errorf("unexpected response type in shell spawn: %T", resp.GetResult())
	}

	session := &ShellSession{
		conn:     conn,
		protocol: protocolV2Protobuf,
		handle:   spawnResp.GetHandle(),
		id:       id,
		Output:   make(chan execagent.Response, 100),
	}

	go func() {
		defer close(session.Output)

		for {
			env, err := readEnvelope(conn)
			if err != nil {
				session.Output <- execagent.Response{ID: id, Error: fmt.Sprintf("read: %v", err), Done: true}
				return
			}

			var out execagent.Response
			out.ID = id

			switch p := env.GetPayload().(type) {
			case *pb.Envelope_Event:
				switch ev := p.Event.GetEvent().(type) {
				case *pb.Event_Stdout:
					out.Stdout = string(ev.Stdout.GetData())
				case *pb.Event_Stderr:
					out.Stderr = string(ev.Stderr.GetData())
				case *pb.Event_Exit:
					code := int(ev.Exit.GetExitCode())
					out.ExitCode = &code
					out.Done = true
				default:
					continue
				}
			case *pb.Envelope_Response:
				r := p.Response
				if errResp := r.GetError(); errResp != nil {
					out.Error = errResp.GetMessage()
					out.Done = true
				} else {
					continue
				}
			default:
				continue
			}

			select {
			case session.Output <- out:
			case <-ctx.Done():
				return
			}

			if out.Done {
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

	if s.protocol == protocolV2Protobuf {
		return writeEnvelope(s.conn, &pb.Envelope{
			Payload: &pb.Envelope_Request{Request: &pb.Request{
				Id: s.id,
				Method: &pb.Request_Stdin{Stdin: &pb.StdinRequest{
					Handle: s.handle,
					Data:   []byte(data),
				}},
			}},
		})
	}

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

	if s.protocol == protocolV2Protobuf {
		return writeEnvelope(s.conn, &pb.Envelope{
			Payload: &pb.Envelope_Request{Request: &pb.Request{
				Id: s.id,
				Method: &pb.Request_Signal{Signal: &pb.SignalRequest{
					Handle: s.handle,
					Signal: signal,
				}},
			}},
		})
	}

	return s.encoder.Encode(execagent.Request{
		ID:     s.id,
		Type:   "signal",
		Signal: signal,
	})
}

// Resize updates the remote shell terminal size.
func (s *ShellSession) Resize(rows, cols int32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.protocol == protocolV2Protobuf {
		return writeEnvelope(s.conn, &pb.Envelope{
			Payload: &pb.Envelope_Request{Request: &pb.Request{
				Id: s.id,
				Method: &pb.Request_Resize{Resize: &pb.ResizeRequest{
					Handle: s.handle,
					Rows:   uint32(rows),
					Cols:   uint32(cols),
				}},
			}},
		})
	}

	return s.encoder.Encode(execagent.Request{
		ID:   s.id,
		Type: "resize",
		Rows: rows,
		Cols: cols,
	})
}

// Close closes the underlying connection, which causes the shell to exit.
func (s *ShellSession) Close() error {
	return s.conn.Close()
}
