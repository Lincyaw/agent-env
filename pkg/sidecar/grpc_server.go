package sidecar

import (
	"bufio"
	"context"
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

// NewGRPCServer creates a new gRPC server
func NewGRPCServer(workspaceDir string, port int) *GRPCServer {
	srv := &GRPCServer{
		service: NewAgentService(workspaceDir),
		port:    port,
	}
	return srv
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

// UpdateFiles implements the gRPC UpdateFiles method
func (s *GRPCServer) UpdateFiles(ctx context.Context, req *pb.FileRequest) (*pb.FileResponse, error) {
	fileReq := &FileRequest{
		BasePath: req.GetBasePath(),
		Files:    req.GetFiles(),
		Patch:    req.GetPatch(),
	}

	resp, err := s.service.UpdateFiles(ctx, fileReq)
	if err != nil {
		return nil, err
	}

	return &pb.FileResponse{
		Success: resp.Success,
		Message: resp.Message,
	}, nil
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

// SignalProcess implements the gRPC SignalProcess method
func (s *GRPCServer) SignalProcess(ctx context.Context, req *pb.SignalRequest) (*pb.SignalResponse, error) {
	signalReq := &SignalRequest{
		PID:    req.GetPid(),
		Signal: req.GetSignal(),
	}

	resp, err := s.service.SignalProcess(ctx, signalReq)
	if err != nil {
		return nil, err
	}

	return &pb.SignalResponse{
		Success: resp.Success,
		Message: resp.Message,
	}, nil
}

// Reset implements the gRPC Reset method
func (s *GRPCServer) Reset(ctx context.Context, req *pb.ResetRequest) (*pb.ResetResponse, error) {
	resetReq := &ResetRequest{
		PreserveFiles: req.GetPreserveFiles(),
	}

	resp, err := s.service.Reset(ctx, resetReq)
	if err != nil {
		return nil, err
	}

	return &pb.ResetResponse{
		Success: resp.Success,
		Message: resp.Message,
	}, nil
}

// InteractiveShell implements bidirectional streaming for interactive shell sessions
func (s *GRPCServer) InteractiveShell(stream grpc.BidiStreamingServer[pb.ShellInput, pb.ShellOutput]) error {
	ctx := stream.Context()

	// Start a shell process
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
	done := make(chan struct{})

	// Read stdout and stderr
	wg.Add(2)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stdout)
		buf := make([]byte, 4096)
		for {
			select {
			case <-done:
				return
			default:
				n, err := reader.Read(buf)
				if n > 0 {
					if sendErr := stream.Send(&pb.ShellOutput{Data: string(buf[:n])}); sendErr != nil {
						return
					}
				}
				if err == io.EOF {
					return
				}
				if err != nil {
					return
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stderr)
		buf := make([]byte, 4096)
		for {
			select {
			case <-done:
				return
			default:
				n, err := reader.Read(buf)
				if n > 0 {
					if sendErr := stream.Send(&pb.ShellOutput{Data: string(buf[:n])}); sendErr != nil {
						return
					}
				}
				if err == io.EOF {
					return
				}
				if err != nil {
					return
				}
			}
		}
	}()

	// Handle input from client
	go func() {
		for {
			input, err := stream.Recv()
			if err == io.EOF {
				stdin.Close()
				return
			}
			if err != nil {
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

	// Wait for shell to exit
	exitCode := int32(0)
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		} else {
			exitCode = 1
		}
	}

	close(done)
	wg.Wait()

	// Send final message with exit code
	return stream.Send(&pb.ShellOutput{
		ExitCode: exitCode,
		Closed:   true,
	})
}
