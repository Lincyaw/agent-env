package sidecar

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

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
func (s *GRPCServer) InteractiveShell(stream grpc.BidiStreamingServer[pb.ShellInput, pb.ShellOutput]) error {
	ctx := stream.Context()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	cmd := exec.CommandContext(ctx, shell)
	cmd.Dir = s.service.workspaceDir
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start shell: %w", err)
	}

	var wg sync.WaitGroup

	// Stream stdout to client
	wg.Add(2)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				if sendErr := stream.Send(&pb.ShellOutput{Data: string(buf[:n])}); sendErr != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	// Stream stderr to client
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := stderr.Read(buf)
			if n > 0 {
				if sendErr := stream.Send(&pb.ShellOutput{Data: string(buf[:n])}); sendErr != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	// Read input from client
	go func() {
		for {
			input, recvErr := stream.Recv()
			if recvErr == io.EOF {
				stdin.Close()
				return
			}
			if recvErr != nil {
				stdin.Close()
				return
			}

			switch v := input.GetInput().(type) {
			case *pb.ShellInput_Data:
				if _, writeErr := stdin.Write([]byte(v.Data)); writeErr != nil {
					return
				}
			case *pb.ShellInput_Signal:
				signalStr := strings.ToUpper(v.Signal)
				var sig syscall.Signal
				switch signalStr {
				case "SIGINT":
					sig = syscall.SIGINT
				case "SIGTERM":
					sig = syscall.SIGTERM
				case "SIGKILL":
					sig = syscall.SIGKILL
				default:
					continue
				}
				if cmd.Process != nil {
					cmd.Process.Signal(sig)
				}
			}
		}
	}()

	exitCode := int32(0)
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		} else {
			exitCode = 1
		}
	}

	wg.Wait()

	return stream.Send(&pb.ShellOutput{
		ExitCode: exitCode,
		Closed:   true,
	})
}
