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

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	pb "github.com/Lincyaw/agent-env/pkg/pb/executorv2"
	"google.golang.org/protobuf/proto"
)

// executorRequest is the V1 JSON-over-socket protocol request from sidecar to
// executor agent. Kept for backward compatibility with legacy Go executors.
type executorRequest struct {
	ID             string            `json:"id"`
	Type           string            `json:"type"`
	Cmd            []string          `json:"cmd,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	WorkDir        string            `json:"workdir,omitempty"`
	Timeout        int               `json:"timeout,omitempty"`
	PID            int               `json:"pid,omitempty"`
	Signal         string            `json:"signal,omitempty"`
	Data           string            `json:"data,omitempty"`
	Rows           int32             `json:"rows,omitempty"`
	Cols           int32             `json:"cols,omitempty"`
	Path           string            `json:"path,omitempty"`
	Content        []byte            `json:"content,omitempty"`
	ExpectedSHA256 string            `json:"expected_sha256,omitempty"`
}

// executorResponse is the V1 JSON-over-socket protocol response from executor
// agent to sidecar.
type executorResponse struct {
	ID           string `json:"id"`
	Stdout       string `json:"stdout,omitempty"`
	Stderr       string `json:"stderr,omitempty"`
	ExitCode     *int   `json:"exit_code,omitempty"`
	BytesWritten *int64 `json:"bytes_written,omitempty"`
	SizeBytes    *int64 `json:"size_bytes,omitempty"`
	Offset       int64  `json:"offset,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	Content      []byte `json:"content,omitempty"`
	Done         bool   `json:"done,omitempty"`
	Error        string `json:"error,omitempty"`
}

const fileTransferChunkSize = interfaces.FileTransferChunkSize

// Wire-level message type bytes, matching the Rust executor agent.
const (
	msgTypeRequest  byte = 0x01
	msgTypeResponse byte = 0x02
	msgTypeEvent    byte = 0x03
)

// executorProtocol selects the wire format for sidecar-to-executor communication.
type executorProtocol int

const (
	protocolV1JSON     executorProtocol = iota // JSON-over-Unix-socket (legacy)
	protocolV2Protobuf                         // typed length-delimited protobuf
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
// Protobuf typed message helpers (V2)
// Wire format: [1B type][4B big-endian length][protobuf bytes]
// ---------------------------------------------------------------------------

func writeTypedMessage(conn net.Conn, msgType byte, data []byte) error {
	if _, err := conn.Write([]byte{msgType}); err != nil {
		return err
	}
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))
	if _, err := conn.Write(lenBuf); err != nil {
		return err
	}
	_, err := conn.Write(data)
	return err
}

func readTypedMessage(conn net.Conn) (byte, []byte, error) {
	typeBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, typeBuf); err != nil {
		return 0, nil, err
	}
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return 0, nil, err
	}
	msgLen := binary.BigEndian.Uint32(lenBuf)
	if msgLen > 128*1024*1024 {
		return 0, nil, fmt.Errorf("message too large: %d bytes", msgLen)
	}
	data := make([]byte, msgLen)
	if _, err := io.ReadFull(conn, data); err != nil {
		return 0, nil, err
	}
	return typeBuf[0], data, nil
}

func sendRequest(conn net.Conn, req *pb.Request) error {
	data, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	return writeTypedMessage(conn, msgTypeRequest, data)
}

func readResponse(conn net.Conn) (*pb.Response, error) {
	msgType, data, err := readTypedMessage(conn)
	if err != nil {
		return nil, err
	}
	if msgType != msgTypeResponse {
		return nil, fmt.Errorf("expected message type 0x%02x, got 0x%02x", msgTypeResponse, msgType)
	}
	resp := &pb.Response{}
	if err := proto.Unmarshal(data, resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return resp, nil
}

// readServerMessage reads the next message from the executor (Response or Event).
type serverMessage struct {
	Response *pb.Response
	Event    *pb.Event
}

func readServerMessage(conn net.Conn) (*serverMessage, error) {
	msgType, data, err := readTypedMessage(conn)
	if err != nil {
		return nil, err
	}
	switch msgType {
	case msgTypeResponse:
		resp := &pb.Response{}
		if err := proto.Unmarshal(data, resp); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		return &serverMessage{Response: resp}, nil
	case msgTypeEvent:
		evt := &pb.Event{}
		if err := proto.Unmarshal(data, evt); err != nil {
			return nil, fmt.Errorf("unmarshal event: %w", err)
		}
		return &serverMessage{Event: evt}, nil
	default:
		return nil, fmt.Errorf("unexpected message type: 0x%02x", msgType)
	}
}

// writeDataFrames writes raw data frames: [4B len][data]...[4B zero]
func writeDataFrames(conn net.Conn, content io.Reader) (int64, error) {
	buf := make([]byte, fileTransferChunkSize)
	lenBuf := make([]byte, 4)
	var total int64
	for {
		n, readErr := content.Read(buf)
		if n > 0 {
			binary.BigEndian.PutUint32(lenBuf, uint32(n))
			if _, err := conn.Write(lenBuf); err != nil {
				return total, err
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return total, err
			}
			total += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return total, readErr
		}
	}
	// terminator
	binary.BigEndian.PutUint32(lenBuf, 0)
	_, err := conn.Write(lenBuf)
	return total, err
}

// readDataFrames reads raw data frames until zero-length terminator,
// writing data to dst.
func readDataFrames(conn net.Conn, dst io.Writer) (int64, error) {
	lenBuf := make([]byte, 4)
	var total int64
	for {
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return total, err
		}
		frameLen := binary.BigEndian.Uint32(lenBuf)
		if frameLen == 0 {
			break
		}
		n, err := io.CopyN(dst, conn, int64(frameLen))
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
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

	req := executorRequest{ID: "ping-0", Type: "ping"}
	if err := encoder.Encode(req); err != nil {
		return fmt.Errorf("send ping: %w", err)
	}

	var resp executorResponse
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
		Tag:  0,
		Kind: &pb.Request_Ping{Ping: &pb.PingRequest{}},
	}); err != nil {
		return fmt.Errorf("send ping: %w", err)
	}

	resp, err := readResponse(conn)
	if err != nil {
		return fmt.Errorf("read ping response: %w", err)
	}
	if errResp := resp.GetError(); errResp != nil {
		return fmt.Errorf("ping error: [%d] %s", errResp.GetCode(), errResp.GetMessage())
	}
	return nil
}

// ---------------------------------------------------------------------------
// Execute
// ---------------------------------------------------------------------------

func (c *ExecutorClient) Execute(ctx context.Context, req executorRequest) (<-chan executorResponse, error) {
	if c.protocol == protocolV2Protobuf {
		return c.executeV2(ctx, req)
	}
	return c.executeV1(ctx, req)
}

func (c *ExecutorClient) executeV1(ctx context.Context, req executorRequest) (<-chan executorResponse, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send exec request: %w", err)
	}

	ch := make(chan executorResponse, 100)

	go func() {
		defer close(ch)
		defer conn.Close()

		decoder := json.NewDecoder(conn)

		for {
			var resp executorResponse
			if err := decoder.Decode(&resp); err != nil {
				ch <- executorResponse{ID: req.ID, Error: fmt.Sprintf("decode: %v", err), Done: true}
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

func (c *ExecutorClient) executeV2(ctx context.Context, req executorRequest) (<-chan executorResponse, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}

	var tag uint32 = 1

	spawnReq := &pb.SpawnRequest{
		Command:        req.Cmd,
		Env:            req.Env,
		WorkingDir:     req.WorkDir,
		TimeoutSeconds: int32(req.Timeout),
	}

	if err := sendRequest(conn, &pb.Request{
		Tag:  tag,
		Kind: &pb.Request_Spawn{Spawn: spawnReq},
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send spawn request: %w", err)
	}

	ch := make(chan executorResponse, 100)

	go func() {
		defer close(ch)
		defer conn.Close()

		for {
			msg, err := readServerMessage(conn)
			if err != nil {
				ch <- executorResponse{ID: req.ID, Error: fmt.Sprintf("read: %v", err), Done: true}
				return
			}

			var resp executorResponse
			resp.ID = req.ID

			if msg.Response != nil {
				r := msg.Response
				switch result := r.GetKind().(type) {
				case *pb.Response_Spawn:
					_ = result.Spawn.GetProcessTag()
					continue
				case *pb.Response_Ping:
					resp.Done = true
				case *pb.Response_Error:
					resp.Error = result.Error.GetMessage()
					resp.Done = true
				default:
					resp.Error = fmt.Sprintf("unexpected response type: %T", result)
					resp.Done = true
				}
			} else if msg.Event != nil {
				e := msg.Event
				switch ev := e.GetKind().(type) {
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
			} else {
				continue
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
		return c.signalV2(uint32(pid), signal)
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

	req := executorRequest{ID: "sig-0", Type: "signal", PID: pid, Signal: signal}
	if err := encoder.Encode(req); err != nil {
		return fmt.Errorf("send signal request: %w", err)
	}

	var resp executorResponse
	if err := decoder.Decode(&resp); err != nil {
		return fmt.Errorf("decode signal response: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("signal error: %s", resp.Error)
	}
	return nil
}

func (c *ExecutorClient) signalV2(processTag uint32, signal string) error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := sendRequest(conn, &pb.Request{
		Tag: 0,
		Kind: &pb.Request_Signal{Signal: &pb.SignalRequest{
			ProcessTag: processTag,
			Signal:     signal,
		}},
	}); err != nil {
		return fmt.Errorf("send signal request: %w", err)
	}

	resp, err := readResponse(conn)
	if err != nil {
		return fmt.Errorf("read signal response: %w", err)
	}
	if errResp := resp.GetError(); errResp != nil {
		return fmt.Errorf("signal error: [%d] %s", errResp.GetCode(), errResp.GetMessage())
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

	req := executorRequest{
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
			if err := encoder.Encode(executorRequest{
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

	if err := encoder.Encode(executorRequest{ID: req.ID, Type: "write_file_finish"}); err != nil {
		return nil, fmt.Errorf("send write_file finish: %w", err)
	}

	for {
		var resp executorResponse
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

	if err := sendRequest(conn, &pb.Request{
		Tag: 0,
		Kind: &pb.Request_Write{Write: &pb.WriteRequest{
			Path:           path,
			ExpectedSha256: expectedSHA256,
		}},
	}); err != nil {
		return nil, fmt.Errorf("send write request: %w", err)
	}

	// Send raw data frames
	if _, err := writeDataFrames(conn, content); err != nil {
		return nil, fmt.Errorf("send file data: %w", err)
	}

	resp, err := readResponse(conn)
	if err != nil {
		return nil, fmt.Errorf("read write response: %w", err)
	}

	switch result := resp.GetKind().(type) {
	case *pb.Response_Error:
		return nil, fmt.Errorf("write error: [%d] %s", result.Error.GetCode(), result.Error.GetMessage())
	case *pb.Response_Write:
		return &FileWriteResult{
			Path:         path,
			BytesWritten: result.Write.GetBytesWritten(),
			SHA256:       result.Write.GetSha256(),
		}, nil
	default:
		return nil, fmt.Errorf("unexpected write response: %T", result)
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

	req := executorRequest{
		ID:   fmt.Sprintf("read-%d", time.Now().UnixNano()),
		Type: "read_file_stream",
		Path: path,
	}
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("send read_file request: %w", err)
	}

	for {
		var resp executorResponse
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

	if err := sendRequest(conn, &pb.Request{
		Tag: 0,
		Kind: &pb.Request_Read{Read: &pb.ReadRequest{
			Path: path,
		}},
	}); err != nil {
		return nil, fmt.Errorf("send read request: %w", err)
	}

	resp, err := readResponse(conn)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	switch result := resp.GetKind().(type) {
	case *pb.Response_Error:
		return nil, fmt.Errorf("read error: [%d] %s", result.Error.GetCode(), result.Error.GetMessage())
	case *pb.Response_Read:
		// Read raw data frames
		if _, err := readDataFrames(conn, dst); err != nil {
			return nil, fmt.Errorf("read file data: %w", err)
		}
		return &FileReadResult{
			Path:      path,
			SizeBytes: result.Read.GetSizeBytes(),
			SHA256:    result.Read.GetSha256(),
		}, nil
	default:
		return nil, fmt.Errorf("unexpected read response: %T", result)
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
	processTag uint32

	id     string
	Output chan executorResponse
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

	req := executorRequest{
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
		Output:   make(chan executorResponse, 100),
	}

	go func() {
		defer close(session.Output)

		decoder := json.NewDecoder(conn)

		for {
			var resp executorResponse
			if err := decoder.Decode(&resp); err != nil {
				session.Output <- executorResponse{ID: id, Error: fmt.Sprintf("decode: %v", err), Done: true}
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
	var tag uint32 = 1

	spawnReq := &pb.SpawnRequest{
		Command:    []string{"/bin/bash", "-i"},
		Env:        env,
		WorkingDir: workDir,
		Stdin:      true,
		Pty:        true,
		Rows:       24,
		Cols:       80,
	}

	if err := sendRequest(conn, &pb.Request{
		Tag:  tag,
		Kind: &pb.Request_Spawn{Spawn: spawnReq},
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send shell spawn request: %w", err)
	}

	// Read the SpawnResponse to get the process tag.
	resp, err := readResponse(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read shell spawn response: %w", err)
	}
	spawnResp := resp.GetSpawn()
	if spawnResp == nil {
		if errResp := resp.GetError(); errResp != nil {
			conn.Close()
			return nil, fmt.Errorf("shell spawn error: [%d] %s", errResp.GetCode(), errResp.GetMessage())
		}
		conn.Close()
		return nil, fmt.Errorf("unexpected response type in shell spawn: %T", resp.GetKind())
	}

	session := &ShellSession{
		conn:       conn,
		protocol:   protocolV2Protobuf,
		processTag: spawnResp.GetProcessTag(),
		id:         id,
		Output:     make(chan executorResponse, 100),
	}

	go func() {
		defer close(session.Output)

		for {
			msg, err := readServerMessage(conn)
			if err != nil {
				session.Output <- executorResponse{ID: id, Error: fmt.Sprintf("read: %v", err), Done: true}
				return
			}

			var out executorResponse
			out.ID = id

			if msg.Event != nil {
				switch ev := msg.Event.GetKind().(type) {
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
			} else if msg.Response != nil {
				r := msg.Response
				if errResp := r.GetError(); errResp != nil {
					out.Error = errResp.GetMessage()
					out.Done = true
				} else {
					continue
				}
			} else {
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
		return sendRequest(s.conn, &pb.Request{
			Tag: 0,
			Kind: &pb.Request_WriteIn{WriteIn: &pb.WriteInRequest{
				ProcessTag: s.processTag,
				Data:       []byte(data),
			}},
		})
	}

	return s.encoder.Encode(executorRequest{
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
		return sendRequest(s.conn, &pb.Request{
			Tag: 0,
			Kind: &pb.Request_Signal{Signal: &pb.SignalRequest{
				ProcessTag: s.processTag,
				Signal:     signal,
			}},
		})
	}

	return s.encoder.Encode(executorRequest{
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
		return sendRequest(s.conn, &pb.Request{
			Tag: 0,
			Kind: &pb.Request_Resize{Resize: &pb.ResizeRequest{
				ProcessTag: s.processTag,
				Rows:       rows,
				Cols:       cols,
			}},
		})
	}

	return s.encoder.Encode(executorRequest{
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
