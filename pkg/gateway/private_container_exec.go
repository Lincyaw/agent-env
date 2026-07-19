package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	k8sexec "k8s.io/client-go/util/exec"
)

// ExecuteContainerSteps executes control-plane steps inside a configured
// private container. These steps are intentionally not recorded in agent
// history, so ReplayFrom cannot mix evaluator-only commands into trajectories.
func (g *Gateway) ExecuteContainerSteps(
	ctx context.Context,
	sessionID string,
	container string,
	req ContainerExecuteRequest,
) (*ContainerExecuteResponse, error) {
	if g.k8sRESTConfig == nil {
		return nil, fmt.Errorf("kubernetes REST config is not configured")
	}
	container = strings.TrimSpace(container)
	if container == "" {
		return nil, fmt.Errorf("container is required")
	}
	if container == executorContainerName {
		return nil, fmt.Errorf("container %q is not a private container", container)
	}

	s, _, releaseSession, err := g.acquireSessionPodIP(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	s.mu.RLock()
	info := s.Info
	_, ok := s.privateContainers[container]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("private container %q is not configured for session %s", container, sessionID)
	}
	if info.PodName == "" || info.Namespace == "" {
		return nil, fmt.Errorf("session %s has incomplete pod binding", sessionID)
	}

	clientset, err := kubernetes.NewForConfig(g.k8sRESTConfig)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes clientset: %w", err)
	}

	resp := &ContainerExecuteResponse{
		SessionID: sessionID,
		Container: container,
		Results:   make([]StepResult, 0, len(req.Steps)),
	}
	totalStart := time.Now()
	for idx, step := range req.Steps {
		result := g.executeContainerStep(
			ctx,
			clientset,
			info.Namespace,
			info.PodName,
			container,
			idx,
			step,
		)
		resp.Results = append(resp.Results, result)
	}
	resp.TotalDurationMs = time.Since(totalStart).Milliseconds()
	g.touchLastTaskTime(sessionID)
	return resp, nil
}

func (g *Gateway) executeContainerStep(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	podName string,
	container string,
	index int,
	step StepRequest,
) StepResult {
	start := time.Now()
	inputJSON, _ := json.Marshal(step)
	result := StepResult{
		Index:     index,
		Name:      step.Name,
		Input:     inputJSON,
		Timestamp: start,
	}
	command := buildContainerExecCommand(step)
	if len(command) == 0 {
		result.Output.Stderr = "no command specified"
		result.Output.ExitCode = 1
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	stepCtx := ctx
	cancel := func() {}
	if timeout := resolveStepTimeoutSeconds(step); timeout > 0 {
		stepCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	}
	defer cancel()

	var stdout, stderr bytes.Buffer
	request := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, clientgoscheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(g.k8sRESTConfig, http.MethodPost, request.URL())
	if err != nil {
		result.Output.Stderr = err.Error()
		result.Output.ExitCode = 1
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	err = executor.StreamWithContext(stepCtx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})
	result.Output.Stdout = stdout.String()
	result.Output.Stderr = stderr.String()
	if err != nil {
		result.Output.ExitCode = 1
		if exitErr, ok := err.(k8sexec.ExitError); ok && exitErr.Exited() {
			result.Output.ExitCode = int32(exitErr.ExitStatus())
		} else if result.Output.Stderr == "" {
			result.Output.Stderr = err.Error()
		}
	}
	result.DurationMs = time.Since(start).Milliseconds()
	return result
}

func buildContainerExecCommand(step StepRequest) []string {
	if len(step.Command) == 0 {
		return nil
	}
	if strings.TrimSpace(step.WorkDir) == "" && len(step.Env) == 0 {
		return step.Command
	}

	var parts []string
	if workDir := strings.TrimSpace(step.WorkDir); workDir != "" {
		parts = append(parts, "cd "+shellQuote(workDir), "&&")
	}
	if len(step.Env) > 0 {
		parts = append(parts, "env")
		keys := make([]string, 0, len(step.Env))
		for key := range step.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, shellQuote(key+"="+step.Env[key]))
		}
	}
	for _, arg := range step.Command {
		parts = append(parts, shellQuote(arg))
	}
	return []string{"sh", "-c", strings.Join(parts, " ")}
}
