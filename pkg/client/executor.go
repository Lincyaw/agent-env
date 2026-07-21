package client

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	pb "github.com/Lincyaw/agent-env/pkg/pb/executorv2"
	"google.golang.org/protobuf/proto"
)

const fileChunkSize = interfaces.FileTransferChunkSize

// Wire-level message type bytes, matching the executor agent.
const (
	msgTypeRequest  byte = 0x01
	msgTypeResponse byte = 0x02
	msgTypeEvent    byte = 0x03
)

// TCPExecutorClient speaks the executor framed protocol over TCP,
// connecting directly to executor agents.
type TCPExecutorClient struct {
	port    int
	timeout time.Duration

	mu    sync.RWMutex
	conns map[string]net.Conn
}

// NewExecutorClient creates a new executor client that connects directly
// to executor agents over TCP using the framed protobuf protocol.
func NewExecutorClient(port int, timeout time.Duration) interfaces.ExecutorClient {
	return &TCPExecutorClient{
		port:    port,
		timeout: timeout,
		conns:   make(map[string]net.Conn),
	}
}

// dial opens a fresh TCP connection to the executor at podIP:port.
func (c *TCPExecutorClient) dial(podIP string) (net.Conn, error) {
	addr := net.JoinHostPort(podIP, strconv.Itoa(c.port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to executor at %s: %w", addr, err)
	}
	return conn, nil
}

// ---------------------------------------------------------------------------
// Wire protocol helpers
// Frame format: [1B type][4B big-endian length][protobuf bytes]
// ---------------------------------------------------------------------------

func writeFrame(conn net.Conn, msgType byte, data []byte) error {
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

func readFrame(conn net.Conn) (byte, []byte, error) {
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
	return writeFrame(conn, msgTypeRequest, data)
}

func readResponse(conn net.Conn) (*pb.Response, error) {
	msgType, data, err := readFrame(conn)
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

// serverMessage is either a Response or an Event from the executor.
type serverMessage struct {
	Response *pb.Response
	Event    *pb.Event
}

func readServerMessage(conn net.Conn) (*serverMessage, error) {
	msgType, data, err := readFrame(conn)
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
	buf := make([]byte, fileChunkSize)
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

// readDataFrames reads raw data frames until zero-length terminator.
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

// callTimeout returns the per-call timeout. When the step declares its own
// timeout, that wins (plus 10s buffer). When no step timeout is set and the
// context has a deadline, we fall back to c.timeout. Otherwise we return 0
// to skip adding a deadline.
func (c *TCPExecutorClient) callTimeout(ctx context.Context, requested int32) time.Duration {
	if requested > 0 {
		stepTimeout := time.Duration(requested)*time.Second + 10*time.Second
		if stepTimeout > c.timeout {
			return stepTimeout
		}
		return c.timeout
	}
	if _, ok := ctx.Deadline(); ok {
		return c.timeout
	}
	return 0
}

// ---------------------------------------------------------------------------
// Execute
// ---------------------------------------------------------------------------

func (c *TCPExecutorClient) Execute(ctx context.Context, podIP string, req *interfaces.ExecRequest) (*interfaces.ExecResponse, error) {
	conn, err := c.dial(podIP)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if timeout := c.callTimeout(ctx, req.TimeoutSeconds); timeout > 0 {
		conn.SetDeadline(time.Now().Add(timeout))
	}

	var tag uint32 = 1
	spawnReq := &pb.SpawnRequest{
		Command:        req.Command,
		Env:            req.Env,
		WorkingDir:     req.WorkingDir,
		TimeoutSeconds: req.TimeoutSeconds,
	}

	if err := sendRequest(conn, &pb.Request{
		Tag:  tag,
		Kind: &pb.Request_Spawn{Spawn: spawnReq},
	}); err != nil {
		return nil, fmt.Errorf("send spawn request: %w", err)
	}

	var stdout, stderr strings.Builder
	var exitCode int32
	var done bool

	for {
		msg, err := readServerMessage(conn)
		if err != nil {
			return nil, fmt.Errorf("read executor message: %w", err)
		}

		if msg.Response != nil {
			r := msg.Response
			switch result := r.GetKind().(type) {
			case *pb.Response_Spawn:
				_ = result.Spawn.GetProcessTag()
				continue
			case *pb.Response_Error:
				return nil, fmt.Errorf("executor error: [%d] %s", result.Error.GetCode(), result.Error.GetMessage())
			default:
				continue
			}
		}

		if msg.Event != nil {
			switch ev := msg.Event.GetKind().(type) {
			case *pb.Event_Stdout:
				stdout.Write(ev.Stdout.GetData())
			case *pb.Event_Stderr:
				stderr.Write(ev.Stderr.GetData())
			case *pb.Event_Exit:
				exitCode = ev.Exit.ExitCode
				done = true
			default:
				continue
			}
		}

		if done {
			break
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}

	return &interfaces.ExecResponse{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Done:     true,
	}, nil
}

// ---------------------------------------------------------------------------
// ExecuteStream
// ---------------------------------------------------------------------------

func (c *TCPExecutorClient) ExecuteStream(ctx context.Context, podIP string, req *interfaces.ExecRequest) (<-chan interfaces.ExecResponse, error) {
	conn, err := c.dial(podIP)
	if err != nil {
		return nil, err
	}

	if timeout := c.callTimeout(ctx, req.TimeoutSeconds); timeout > 0 {
		conn.SetDeadline(time.Now().Add(timeout))
	}

	var tag uint32 = 1
	spawnReq := &pb.SpawnRequest{
		Command:        req.Command,
		Env:            req.Env,
		WorkingDir:     req.WorkingDir,
		TimeoutSeconds: req.TimeoutSeconds,
	}

	if err := sendRequest(conn, &pb.Request{
		Tag:  tag,
		Kind: &pb.Request_Spawn{Spawn: spawnReq},
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send spawn request: %w", err)
	}

	resultChan := make(chan interfaces.ExecResponse, 100)

	go func() {
		defer close(resultChan)
		defer conn.Close()

		for {
			msg, err := readServerMessage(conn)
			if err != nil {
				resultChan <- interfaces.ExecResponse{
					Stderr:   fmt.Sprintf("read: %v", err),
					ExitCode: 1,
					Done:     true,
				}
				return
			}

			var resp interfaces.ExecResponse
			var hasResp bool

			hasResp = false
			if msg.Response != nil {
				r := msg.Response
				switch result := r.GetKind().(type) {
				case *pb.Response_Spawn:
					_ = result.Spawn.GetProcessTag()
					continue
				case *pb.Response_Error:
					resp = interfaces.ExecResponse{
						Stderr:   result.Error.GetMessage(),
						ExitCode: 1,
						Done:     true,
					}
					hasResp = true
				default:
					continue
				}
			} else if msg.Event != nil {
				switch ev := msg.Event.GetKind().(type) {
				case *pb.Event_Stdout:
					resp = interfaces.ExecResponse{Stdout: string(ev.Stdout.GetData())}
					hasResp = true
				case *pb.Event_Stderr:
					resp = interfaces.ExecResponse{Stderr: string(ev.Stderr.GetData())}
					hasResp = true
				case *pb.Event_Exit:
					resp = interfaces.ExecResponse{ExitCode: ev.Exit.ExitCode, Done: true}
					hasResp = true
				default:
					continue
				}
			} else {
				continue
			}

			if !hasResp {
				continue
			}
			select {
			case resultChan <- resp:
			case <-ctx.Done():
				return
			}
			if resp.Done {
				return
			}
		}
	}()

	return resultChan, nil
}

// ---------------------------------------------------------------------------
// WriteFile
// ---------------------------------------------------------------------------

func (c *TCPExecutorClient) WriteFile(ctx context.Context, podIP string, path string, content io.Reader, expectedSHA256 string) (*interfaces.FileWriteResult, error) {
	conn, err := c.dial(podIP)
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
		return &interfaces.FileWriteResult{
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

func (c *TCPExecutorClient) ReadFile(ctx context.Context, podIP string, path string, dst io.Writer) (*interfaces.FileReadResult, error) {
	conn, err := c.dial(podIP)
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
		if _, err := readDataFrames(conn, dst); err != nil {
			return nil, fmt.Errorf("read file data: %w", err)
		}
		return &interfaces.FileReadResult{
			Path:      path,
			SizeBytes: result.Read.GetSizeBytes(),
			SHA256:    result.Read.GetSha256(),
		}, nil
	default:
		return nil, fmt.Errorf("unexpected read response: %T", result)
	}
}

// ---------------------------------------------------------------------------
// DownloadCheckpoint
// ---------------------------------------------------------------------------

func (c *TCPExecutorClient) DownloadCheckpoint(ctx context.Context, podIP string, through int, dst io.Writer) error {
	conn, err := c.dial(podIP)
	if err != nil {
		return err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Minute))

	if err := sendRequest(conn, &pb.Request{
		Tag: 0,
		Kind: &pb.Request_CheckpointDownload{CheckpointDownload: &pb.CheckpointDownloadRequest{
			Through: int32(through),
		}},
	}); err != nil {
		return fmt.Errorf("send checkpoint download request: %w", err)
	}

	resp, err := readResponse(conn)
	if err != nil {
		return fmt.Errorf("read checkpoint download response: %w", err)
	}

	switch result := resp.GetKind().(type) {
	case *pb.Response_Error:
		return fmt.Errorf("checkpoint download error: [%d] %s", result.Error.GetCode(), result.Error.GetMessage())
	case *pb.Response_CheckpointDownload:
		_ = result.CheckpointDownload.GetSizeBytes()
		if _, err := readDataFrames(conn, dst); err != nil {
			return fmt.Errorf("read checkpoint data: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unexpected checkpoint download response: %T", result)
	}
}

// ---------------------------------------------------------------------------
// ListCheckpointSteps
// ---------------------------------------------------------------------------

func (c *TCPExecutorClient) ListCheckpointSteps(ctx context.Context, podIP string) ([]int, error) {
	conn, err := c.dial(podIP)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	if err := sendRequest(conn, &pb.Request{
		Tag:  0,
		Kind: &pb.Request_CheckpointList{CheckpointList: &pb.CheckpointListRequest{}},
	}); err != nil {
		return nil, fmt.Errorf("send checkpoint list request: %w", err)
	}

	resp, err := readResponse(conn)
	if err != nil {
		return nil, fmt.Errorf("read checkpoint list response: %w", err)
	}

	switch result := resp.GetKind().(type) {
	case *pb.Response_Error:
		return nil, fmt.Errorf("checkpoint list error: [%d] %s", result.Error.GetCode(), result.Error.GetMessage())
	case *pb.Response_CheckpointList:
		pbSteps := result.CheckpointList.GetSteps()
		steps := make([]int, len(pbSteps))
		for i, s := range pbSteps {
			steps[i] = int(s)
		}
		return steps, nil
	default:
		return nil, fmt.Errorf("unexpected checkpoint list response: %T", result)
	}
}

// ---------------------------------------------------------------------------
// InteractiveShell
// ---------------------------------------------------------------------------

// executorShellStream wraps a TCP connection to the executor as interfaces.ShellStream.
type executorShellStream struct {
	conn       net.Conn
	mu         sync.Mutex
	processTag uint32
}

func (s *executorShellStream) Send(input interfaces.ShellInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if input.Resize {
		return sendRequest(s.conn, &pb.Request{
			Tag: 0,
			Kind: &pb.Request_Resize{Resize: &pb.ResizeRequest{
				ProcessTag: s.processTag,
				Rows:       input.Rows,
				Cols:       input.Cols,
			}},
		})
	}
	if input.Signal != "" {
		return sendRequest(s.conn, &pb.Request{
			Tag: 0,
			Kind: &pb.Request_Signal{Signal: &pb.SignalRequest{
				ProcessTag: s.processTag,
				Signal:     input.Signal,
			}},
		})
	}
	return sendRequest(s.conn, &pb.Request{
		Tag: 0,
		Kind: &pb.Request_WriteIn{WriteIn: &pb.WriteInRequest{
			ProcessTag: s.processTag,
			Data:       []byte(input.Data),
		}},
	})
}

func (s *executorShellStream) Recv() (interfaces.ShellOutput, error) {
	for {
		msg, err := readServerMessage(s.conn)
		if err != nil {
			return interfaces.ShellOutput{}, err
		}

		if msg.Event != nil {
			switch ev := msg.Event.GetKind().(type) {
			case *pb.Event_Stdout:
				return interfaces.ShellOutput{Data: string(ev.Stdout.GetData())}, nil
			case *pb.Event_Stderr:
				return interfaces.ShellOutput{Data: string(ev.Stderr.GetData())}, nil
			case *pb.Event_Exit:
				return interfaces.ShellOutput{
					ExitCode: ev.Exit.ExitCode,
					Closed:   true,
				}, nil
			}
		}
		if msg.Response != nil {
			if errResp := msg.Response.GetError(); errResp != nil {
				return interfaces.ShellOutput{}, fmt.Errorf("shell error: [%d] %s", errResp.GetCode(), errResp.GetMessage())
			}
		}
	}
}

func (s *executorShellStream) Close() error {
	return s.conn.Close()
}

func (c *TCPExecutorClient) InteractiveShell(ctx context.Context, podIP string) (interfaces.ShellStream, error) {
	conn, err := c.dial(podIP)
	if err != nil {
		return nil, err
	}

	var tag uint32 = 1
	spawnReq := &pb.SpawnRequest{
		Command:    []string{"/bin/bash", "-i"},
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

	return &executorShellStream{
		conn:       conn,
		processTag: spawnResp.GetProcessTag(),
	}, nil
}

// ---------------------------------------------------------------------------
// GetIrohAddr
// ---------------------------------------------------------------------------

func (c *TCPExecutorClient) GetIrohAddr(ctx context.Context, podIP string) (string, error) {
	conn, err := c.dial(podIP)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	if err := sendRequest(conn, &pb.Request{
		Tag:  0,
		Kind: &pb.Request_Ping{Ping: &pb.PingRequest{}},
	}); err != nil {
		return "", fmt.Errorf("send ping: %w", err)
	}

	resp, err := readResponse(conn)
	if err != nil {
		return "", fmt.Errorf("read ping response: %w", err)
	}
	if errResp := resp.GetError(); errResp != nil {
		return "", fmt.Errorf("ping error: [%d] %s", errResp.GetCode(), errResp.GetMessage())
	}
	// The executor's PingResponse does not carry iroh addr;
	// return empty string. The executor would need to expose it
	// through a dedicated request type.
	return "", nil
}

// ---------------------------------------------------------------------------
// HealthCheck
// ---------------------------------------------------------------------------

func (c *TCPExecutorClient) HealthCheck(ctx context.Context, podIP string) error {
	conn, err := c.dial(podIP)
	if err != nil {
		return fmt.Errorf("health check failed: cannot connect to %s: %w", podIP, err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))

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
// Connection management
// ---------------------------------------------------------------------------

func (c *TCPExecutorClient) CloseConnection(podIP string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, ok := c.conns[podIP]
	if !ok {
		return nil
	}
	delete(c.conns, podIP)
	return conn.Close()
}

func (c *TCPExecutorClient) CleanupStale() int {
	// TCP connections are per-operation; no persistent connections to clean up.
	return 0
}

func (c *TCPExecutorClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var lastErr error
	for podIP, conn := range c.conns {
		if err := conn.Close(); err != nil {
			lastErr = fmt.Errorf("failed to close connection to %s: %w", podIP, err)
		}
		delete(c.conns, podIP)
	}
	return lastErr
}
