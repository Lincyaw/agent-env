// Copyright 2024 ARL-Infra Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

// HTTPSidecarClient is an HTTP-based implementation of SidecarClient
// Deprecated: Use GRPCSidecarClient for streaming support
type HTTPSidecarClient struct {
	httpClient *http.Client
	port       int
}

// NewHTTPSidecarClient creates a new HTTP sidecar client
// Deprecated: Use NewGRPCSidecarClient for streaming support
func NewHTTPSidecarClient(port int, timeout time.Duration) interfaces.SidecarClient {
	return &HTTPSidecarClient{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		port: port,
	}
}

// UpdateFiles sends file update request to sidecar
func (c *HTTPSidecarClient) UpdateFiles(ctx context.Context, podIP string, req interfaces.FileUpdateRequest) (interfaces.FileUpdateResponse, error) {
	url := fmt.Sprintf("http://%s:%d/files", podIP, c.port)

	// Convert interface to concrete type
	fileReq := &sidecar.FileRequest{
		BasePath: req.GetBasePath(),
		Files:    req.GetFiles(),
		Patch:    req.GetPatch(),
	}

	var resp sidecar.FileResponse
	if err := c.doRequest(ctx, url, fileReq, &resp); err != nil {
		return nil, fmt.Errorf("failed to update files: %w", err)
	}

	return &resp, nil
}

// Execute sends command execution request to sidecar
func (c *HTTPSidecarClient) Execute(ctx context.Context, podIP string, req interfaces.ExecRequest) (interfaces.ExecResponse, error) {
	url := fmt.Sprintf("http://%s:%d/execute", podIP, c.port)

	// Convert interface to concrete type
	execReq := &sidecar.ExecRequest{
		Command:        req.GetCommand(),
		Env:            req.GetEnv(),
		WorkingDir:     req.GetWorkingDir(),
		TimeoutSeconds: req.GetTimeout(),
	}

	var resp sidecar.ExecLog
	if err := c.doRequest(ctx, url, execReq, &resp); err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	return &resp, nil
}

// ExecuteStream returns a channel but only sends a single aggregated response
// This is for interface compatibility; for true streaming, use GRPCSidecarClient
func (c *HTTPSidecarClient) ExecuteStream(ctx context.Context, podIP string, req interfaces.ExecRequest) (<-chan interfaces.ExecResponse, error) {
	resp, err := c.Execute(ctx, podIP, req)
	if err != nil {
		return nil, err
	}

	ch := make(chan interfaces.ExecResponse, 1)
	ch <- resp
	close(ch)
	return ch, nil
}

// Reset sends reset request to sidecar
func (c *HTTPSidecarClient) Reset(ctx context.Context, podIP string, req interfaces.ResetRequest) (interfaces.ResetResponse, error) {
	url := fmt.Sprintf("http://%s:%d/reset", podIP, c.port)

	// Convert interface to concrete type
	resetReq := &sidecar.ResetRequest{
		PreserveFiles: req.ShouldPreserveFiles(),
	}

	var resp sidecar.ResetResponse
	if err := c.doRequest(ctx, url, resetReq, &resp); err != nil {
		return nil, fmt.Errorf("failed to reset: %w", err)
	}

	return &resp, nil
}

// HealthCheck checks if sidecar is healthy
func (c *HTTPSidecarClient) HealthCheck(ctx context.Context, podIP string) error {
	url := fmt.Sprintf("http://%s:%d/health", podIP, c.port)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	return nil
}

// Close is a no-op for HTTP client
func (c *HTTPSidecarClient) Close() error {
	return nil
}

// doRequest performs HTTP request to sidecar
func (c *HTTPSidecarClient) doRequest(ctx context.Context, url string, reqBody interface{}, respBody interface{}) error {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("request failed with status %d: failed to read body: %w", resp.StatusCode, readErr)
		}
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}
