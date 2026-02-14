package sidecar

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"

	"google.golang.org/grpc"

	"github.com/Lincyaw/agent-env/pkg/pb"
)

// GRPCServer implements the gRPC AgentService
type GRPCServer struct {
	pb.UnimplementedAgentServiceServer
	service    *AgentService
	port       int
	grpcServer *grpc.Server
}

// NewGRPCServer creates a new gRPC server (without executor agent).
func NewGRPCServer(workspaceDir string, port int) *GRPCServer {
	return &GRPCServer{
		service: NewAgentService(workspaceDir),
		port:    port,
	}
}

// NewGRPCServerWithExecutor creates a new gRPC server that proxies to an executor agent.
func NewGRPCServerWithExecutor(workspaceDir string, port int, executorSocket string) *GRPCServer {
	return &GRPCServer{
		service: NewAgentServiceWithExecutor(workspaceDir, executorSocket),
		port:    port,
	}
}

// Service returns the underlying agent service (for init operations).
func (s *GRPCServer) Service() *AgentService {
	return s.service
}

// Start starts the gRPC server
func (s *GRPCServer) Start() error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.grpcServer = grpc.NewServer()
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
