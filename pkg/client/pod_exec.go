package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// PodExecClient implements kubectl exec functionality
type PodExecClient struct {
	clientset  *kubernetes.Clientset
	restConfig *rest.Config
}

// NewPodExecClient creates a new PodExecClient
func NewPodExecClient(config *rest.Config) (*PodExecClient, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	return &PodExecClient{
		clientset:  clientset,
		restConfig: config,
	}, nil
}

// execResponse implements interfaces.ExecResponse
type execResponse struct {
	stdout   string
	stderr   string
	exitCode int32
	done     bool
}

func (r *execResponse) GetStdout() string  { return r.stdout }
func (r *execResponse) GetStderr() string  { return r.stderr }
func (r *execResponse) GetExitCode() int32 { return r.exitCode }
func (r *execResponse) IsDone() bool       { return r.done }

// Execute runs a command in the specified container of a pod
func (c *PodExecClient) Execute(ctx context.Context, namespace, podName, container string, req interfaces.ExecRequest) (interfaces.ExecResponse, error) {
	command := req.GetCommand()
	if len(command) == 0 {
		return &execResponse{
			stderr:   "no command specified",
			exitCode: 1,
			done:     true,
		}, nil
	}

	// Apply timeout if specified
	timeout := req.GetTimeout()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	// Build the command with environment variables and working directory
	var fullCommand []string
	workDir := req.GetWorkingDir()
	env := req.GetEnv()

	if workDir != "" || len(env) > 0 {
		// Use sh -c to handle env vars and working directory
		var cmdParts []string

		// Add environment variables
		for k, v := range env {
			cmdParts = append(cmdParts, fmt.Sprintf("export %s=%q;", k, v))
		}

		// Add working directory change
		if workDir != "" {
			cmdParts = append(cmdParts, fmt.Sprintf("cd %q;", workDir))
		}

		// Add the actual command with proper shell escaping
		var escapedArgs []string
		for _, arg := range command {
			escapedArgs = append(escapedArgs, fmt.Sprintf("%q", arg))
		}
		cmdParts = append(cmdParts, strings.Join(escapedArgs, " "))

		fullCommand = []string{"sh", "-c", strings.Join(cmdParts, " ")}
	} else {
		fullCommand = command
	}

	// Create the exec request
	execReq := c.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   fullCommand,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.restConfig, "POST", execReq.URL())
	if err != nil {
		return &execResponse{
			stderr:   fmt.Sprintf("failed to create executor: %v", err),
			exitCode: 1,
			done:     true,
		}, nil
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	exitCode := int32(0)
	if err != nil {
		// Try to extract exit code from error
		if strings.Contains(err.Error(), "command terminated with exit code") {
			fmt.Sscanf(err.Error(), "command terminated with exit code %d", &exitCode)
		} else {
			exitCode = 1
			stderr.WriteString(fmt.Sprintf("\nexecution error: %v", err))
		}
	}

	return &execResponse{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitCode: exitCode,
		done:     true,
	}, nil
}

// InteractiveShell starts an interactive shell session in the specified container
func (c *PodExecClient) InteractiveShell(
	ctx context.Context,
	namespace, podName, container string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	resize <-chan interfaces.TerminalSize,
) error {
	// Determine which shell to use (try bash first, fallback to sh)
	shells := []string{"/bin/bash", "/bin/sh"}
	var lastErr error

	for _, shell := range shells {
		// Create the exec request with TTY enabled
		execReq := c.clientset.CoreV1().RESTClient().Post().
			Resource("pods").
			Name(podName).
			Namespace(namespace).
			SubResource("exec").
			VersionedParams(&corev1.PodExecOptions{
				Container: container,
				Command:   []string{shell},
				Stdin:     stdin != nil,
				Stdout:    true,
				Stderr:    true,
				TTY:       true, // Enable TTY for interactive session
			}, scheme.ParameterCodec)

		exec, err := remotecommand.NewSPDYExecutor(c.restConfig, "POST", execReq.URL())
		if err != nil {
			lastErr = fmt.Errorf("failed to create executor: %w", err)
			continue
		}

		// Convert resize channel to remotecommand.TerminalSizeQueue if provided
		var sizeQueue remotecommand.TerminalSizeQueue
		if resize != nil {
			sizeQueue = &terminalSizeQueue{resize: resize}
		}

		// Start the interactive session
		err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:             stdin,
			Stdout:            stdout,
			Stderr:            stderr,
			Tty:               true,
			TerminalSizeQueue: sizeQueue,
		})

		if err != nil {
			// If error is about shell not found, try next shell
			if strings.Contains(err.Error(), "executable file not found") ||
				strings.Contains(err.Error(), "no such file") {
				lastErr = err
				continue
			}
			return fmt.Errorf("interactive shell failed: %w", err)
		}

		return nil
	}

	return fmt.Errorf("no shell available (tried %v): %w", shells, lastErr)
}

// terminalSizeQueue implements remotecommand.TerminalSizeQueue
type terminalSizeQueue struct {
	resize <-chan interfaces.TerminalSize
}

func (t *terminalSizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-t.resize
	if !ok {
		return nil
	}
	return &remotecommand.TerminalSize{
		Width:  size.Width,
		Height: size.Height,
	}
}
