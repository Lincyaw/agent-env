package sidecar

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Lincyaw/agent-env/pkg/grpcauth"
	"github.com/Lincyaw/agent-env/pkg/pb"
)

// GRPCServer implements the gRPC AgentService
type GRPCServer struct {
	pb.UnimplementedAgentServiceServer
	service    *AgentService
	port       int
	grpcToken  string
	grpcServer *grpc.Server
}

// NewGRPCServer creates a new gRPC server (without executor agent). The token
// is mandatory; every incoming call must present it in metadata.
func NewGRPCServer(workspaceDir string, port int, token string) *GRPCServer {
	return &GRPCServer{
		service:   NewAgentService(workspaceDir),
		port:      port,
		grpcToken: token,
	}
}

// NewGRPCServerWithExecutor creates a new gRPC server that proxies to an
// executor agent. The token is mandatory; see NewGRPCServer.
func NewGRPCServerWithExecutor(workspaceDir string, port int, executorSocket, token string) *GRPCServer {
	return &GRPCServer{
		service:   NewAgentServiceWithExecutor(workspaceDir, executorSocket),
		port:      port,
		grpcToken: token,
	}
}

// Service returns the underlying agent service (for init operations).
func (s *GRPCServer) Service() *AgentService {
	return s.service
}

// Start starts the gRPC server. A token is mandatory: the server refuses to
// start without one so that no unauthenticated execution path is ever exposed.
func (s *GRPCServer) Start() error {
	if s.grpcToken == "" {
		return fmt.Errorf("gRPC auth token is required (set GRPC_AUTH_TOKEN); refusing to start without authentication")
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	opts := []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.UnaryInterceptor(grpcauth.UnaryServerInterceptor(s.grpcToken)),
		grpc.StreamInterceptor(grpcauth.StreamServerInterceptor(s.grpcToken)),
		grpc.MaxRecvMsgSize(64 * 1024 * 1024),
		grpc.MaxSendMsgSize(64 * 1024 * 1024),
	}
	log.Printf("gRPC token authentication enabled")
	s.grpcServer = grpc.NewServer(opts...)
	pb.RegisterAgentServiceServer(s.grpcServer, s)

	log.Printf("gRPC server starting on :%d", s.port)
	return s.grpcServer.Serve(lis)
}

// Stop gracefully stops the gRPC server
func (s *GRPCServer) Stop() {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
}

// Execute implements the gRPC Execute method with streaming
func (s *GRPCServer) Execute(req *pb.ExecRequest, stream grpc.ServerStreamingServer[pb.ExecLog]) error {
	if req.GetBackground() {
		return status.Errorf(codes.Unimplemented, "background execution is not yet supported")
	}

	execReq := &ExecRequest{
		Command:        req.GetCommand(),
		Env:            req.GetEnv(),
		WorkingDir:     req.GetWorkingDir(),
		Background:     req.GetBackground(),
		TimeoutSeconds: req.GetTimeoutSeconds(),
	}

	logChan := make(chan *ExecLog, 100)

	go s.service.Execute(stream.Context(), execReq, logChan)

	for execLog := range logChan {
		pbLog := &pb.ExecLog{
			Stdout:   execLog.Stdout,
			Stderr:   execLog.Stderr,
			ExitCode: execLog.ExitCode,
			Done:     execLog.Done,
		}
		if err := stream.Send(pbLog); err != nil {
			return err
		}
	}

	return nil
}

// WriteFile streams one file into the executor workspace.
func (s *GRPCServer) WriteFile(stream grpc.ClientStreamingServer[pb.WriteFileChunk, pb.WriteFileResponse]) error {
	first, err := stream.Recv()
	if err == io.EOF {
		return status.Error(codes.InvalidArgument, "path is required")
	}
	if err != nil {
		return status.Errorf(codes.Internal, "receive first file chunk: %v", err)
	}
	if first.GetPath() == "" {
		return status.Error(codes.InvalidArgument, "path is required")
	}

	reader, writer := io.Pipe()
	defer reader.Close()
	go func() {
		defer writer.Close()
		if len(first.GetData()) > 0 {
			if _, err := writer.Write(first.GetData()); err != nil {
				return
			}
		}
		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				_ = writer.CloseWithError(err)
				return
			}
			if chunk.GetPath() != "" && chunk.GetPath() != first.GetPath() {
				_ = writer.CloseWithError(fmt.Errorf("file chunk path mismatch"))
				return
			}
			if len(chunk.GetData()) == 0 {
				continue
			}
			if _, err := writer.Write(chunk.GetData()); err != nil {
				return
			}
		}
	}()

	result, err := s.service.WriteFile(stream.Context(), first.GetPath(), reader, first.GetExpectedSha256())
	if err != nil {
		return status.Errorf(codes.Internal, "write file: %v", err)
	}
	return stream.SendAndClose(&pb.WriteFileResponse{
		Path:         result.Path,
		BytesWritten: result.BytesWritten,
		Sha256:       result.SHA256,
	})
}

type fileChunkStreamWriter struct {
	stream grpc.ServerStreamingServer[pb.FileChunk]
	path   string
	offset int64
}

func (w *fileChunkStreamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	data := append([]byte(nil), p...)
	if err := w.stream.Send(&pb.FileChunk{
		Path:   w.path,
		Data:   data,
		Offset: w.offset,
	}); err != nil {
		return 0, err
	}
	w.offset += int64(len(p))
	return len(p), nil
}

// ReadFile streams one file from the executor workspace.
func (s *GRPCServer) ReadFile(req *pb.ReadFileRequest, stream grpc.ServerStreamingServer[pb.FileChunk]) error {
	if req.GetPath() == "" {
		return status.Error(codes.InvalidArgument, "path is required")
	}

	writer := &fileChunkStreamWriter{stream: stream, path: req.GetPath()}
	result, err := s.service.ReadFile(stream.Context(), req.GetPath(), writer)
	if err != nil {
		return status.Errorf(codes.Internal, "read file: %v", err)
	}
	return stream.Send(&pb.FileChunk{
		Path:      result.Path,
		SizeBytes: result.SizeBytes,
		Sha256:    result.SHA256,
		Done:      true,
	})
}

// StreamLogs streams log entries from the sidecar ring buffer.
func (s *GRPCServer) StreamLogs(req *pb.LogsRequest, stream grpc.ServerStreamingServer[pb.LogEntry]) error {
	tailN := int(req.GetTailLines())
	if tailN <= 0 {
		tailN = 100
	}

	// Send buffered tail lines first
	for _, line := range s.service.Logs.Tail(tailN) {
		entry := &pb.LogEntry{
			Timestamp: line.Timestamp.Format(time.RFC3339Nano),
			Level:     line.Level,
			Message:   line.Message,
			Source:    line.Source,
		}
		if err := stream.Send(entry); err != nil {
			return err
		}
	}

	if !req.GetFollow() {
		return nil
	}

	// Follow mode: subscribe and stream until client disconnects
	ch := s.service.Logs.Subscribe()
	defer s.service.Logs.Unsubscribe(ch)

	for {
		select {
		case line := <-ch:
			entry := &pb.LogEntry{
				Timestamp: line.Timestamp.Format(time.RFC3339Nano),
				Level:     line.Level,
				Message:   line.Message,
				Source:    line.Source,
			}
			if err := stream.Send(entry); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

// InteractiveShell implements bidirectional streaming for interactive shell sessions
// by proxying through the executor agent in the main container.
func (s *GRPCServer) InteractiveShell(stream grpc.BidiStreamingServer[pb.ShellInput, pb.ShellOutput]) error {
	ctx := stream.Context()

	if s.service.executorClient == nil {
		return fmt.Errorf("executor agent not configured: sidecar started without --executor-socket")
	}

	session, err := s.service.executorClient.StartShell(ctx, s.service.workspaceDir, nil)
	if err != nil {
		return fmt.Errorf("start executor shell: %w", err)
	}
	defer session.Close()

	// Goroutine: read executor output and forward to gRPC stream
	done := make(chan error, 1)
	go func() {
		for resp := range session.Output {
			out := &pb.ShellOutput{}
			if resp.Stdout != "" {
				out.Data = resp.Stdout
			} else if resp.Stderr != "" {
				out.Data = resp.Stderr
			}
			if resp.Done {
				exitCode := int32(0)
				if resp.ExitCode != nil {
					exitCode = int32(*resp.ExitCode)
				}
				out.ExitCode = exitCode
				out.Closed = true
			}
			if out.Data != "" || out.Closed {
				if sendErr := stream.Send(out); sendErr != nil {
					done <- sendErr
					return
				}
			}
			if resp.Done {
				done <- nil
				return
			}
		}
		done <- nil
	}()

	// Main loop: read gRPC input and forward to executor shell
	go func() {
		for {
			input, recvErr := stream.Recv()
			if recvErr == io.EOF {
				session.Close()
				return
			}
			if recvErr != nil {
				session.Close()
				return
			}

			if input.GetResize() {
				if err := session.Resize(input.GetRows(), input.GetCols()); err != nil {
					return
				}
				continue
			}

			switch v := input.GetInput().(type) {
			case *pb.ShellInput_Data:
				if err := session.SendInput(v.Data); err != nil {
					return
				}
			case *pb.ShellInput_Signal:
				signalStr := strings.ToUpper(v.Signal)
				if err := session.SendSignal(signalStr); err != nil {
					return
				}
			}
		}
	}()

	return <-done
}
