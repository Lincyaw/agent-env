package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type Client struct {
	base   string
	apiKey string
	http   *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		base:   strings.TrimRight(baseURL, "/"),
		apiKey: apiKey,
		http:   &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *Client) do(method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.base+path, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return responseError(resp)
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func responseError(resp *http.Response) error {
	data, _ := io.ReadAll(resp.Body)
	var errResp ErrorResponse
	if json.Unmarshal(data, &errResp) == nil && errResp.Error != "" {
		return &HTTPError{StatusCode: resp.StatusCode, Message: errResp.Error}
	}
	if msg := strings.TrimSpace(string(data)); msg != "" {
		return &HTTPError{StatusCode: resp.StatusCode, Message: msg}
	}
	return &HTTPError{StatusCode: resp.StatusCode}
}

func (c *Client) rawGet(path string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", c.base+path, nil)
	if err != nil {
		return nil, 0, err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

// --- Session API ---

func (c *Client) ListSessions() ([]SessionListItem, error) {
	var sessions []SessionListItem
	return sessions, c.do("GET", "/v1/sessions", nil, &sessions)
}

func (c *Client) CreateSession(req CreateSessionRequest) (*SessionInfo, error) {
	var s SessionInfo
	return &s, c.do("POST", "/v1/sessions", req, &s)
}

func (c *Client) GetSession(id string) (*SessionInfo, error) {
	var s SessionInfo
	return &s, c.do("GET", "/v1/sessions/"+id, nil, &s)
}

func (c *Client) DeleteSession(id string) error {
	return c.do("DELETE", "/v1/sessions/"+id, nil, nil)
}

func (c *Client) Execute(sessionID string, req ExecuteRequest) (*ExecuteResponse, error) {
	var resp ExecuteResponse
	return &resp, c.do("POST", "/v1/sessions/"+sessionID+"/execute", req, &resp)
}

func (c *Client) UploadFile(sessionID, remotePath string, content io.Reader, sha256 string) (*UploadFileResponse, error) {
	quotedPath, err := quoteSessionFilePath(remotePath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("PUT", c.base+"/v1/sessions/"+url.PathEscape(sessionID)+"/files/"+quotedPath, content)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if sha256 != "" {
		req.Header.Set("X-ARL-SHA256", sha256)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, responseError(resp)
	}

	var upload UploadFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&upload); err != nil {
		return nil, err
	}
	return &upload, nil
}

func (c *Client) DownloadFile(sessionID, remotePath string) (*http.Response, error) {
	quotedPath, err := quoteSessionFilePath(remotePath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", c.base+"/v1/sessions/"+url.PathEscape(sessionID)+"/files/"+quotedPath, nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, responseError(resp)
	}
	return resp, nil
}

func quoteSessionFilePath(remotePath string) (string, error) {
	normalized := strings.TrimSpace(strings.ReplaceAll(remotePath, "\\", "/"))
	normalized = path.Clean(normalized)
	normalized = strings.TrimPrefix(normalized, "/")
	if normalized == "" || normalized == "." {
		return "", usageError("remote path is required")
	}
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", usageError("remote path must stay within the session workspace")
	}

	parts := strings.Split(normalized, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/"), nil
}

func (c *Client) Restore(sessionID, snapshotID string) error {
	return c.do("POST", "/v1/sessions/"+url.PathEscape(sessionID)+"/restore", RestoreRequest{
		SnapshotID: snapshotID,
	}, nil)
}

func (c *Client) Replay(sessionID string, req ReplayRequest) (*ReplayResponse, error) {
	var resp ReplayResponse
	return &resp, c.do("POST", "/v1/sessions/"+url.PathEscape(sessionID)+"/replay", req, &resp)
}

func (c *Client) GetHistory(sessionID string) ([]StepRecord, error) {
	var records []StepRecord
	return records, c.do("GET", "/v1/sessions/"+sessionID+"/history", nil, &records)
}

func (c *Client) GetTrajectory(sessionID string) ([]byte, error) {
	data, code, err := c.rawGet("/v1/sessions/" + sessionID + "/trajectory")
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, &HTTPError{StatusCode: code, Message: strings.TrimSpace(string(data))}
	}
	return data, nil
}

// --- Pool API ---

func (c *Client) ListPools(namespace string) ([]PoolInfo, error) {
	path := "/v1/pools"
	if namespace != "" {
		path += "?namespace=" + url.QueryEscape(namespace)
	}
	var pools []PoolInfo
	return pools, c.do("GET", path, nil, &pools)
}

func (c *Client) GetPool(name, namespace string) (*PoolInfo, error) {
	path := "/v1/pools/" + name
	if namespace != "" {
		path += "?namespace=" + url.QueryEscape(namespace)
	}
	var p PoolInfo
	return &p, c.do("GET", path, nil, &p)
}

func (c *Client) CreatePool(req CreatePoolRequest) error {
	return c.do("POST", "/v1/pools", req, nil)
}

func (c *Client) ScalePool(name string, req ScalePoolRequest) (*PoolInfo, error) {
	var p PoolInfo
	return &p, c.do("PATCH", "/v1/pools/"+name, req, &p)
}

func (c *Client) DeletePool(name, namespace string) error {
	path := "/v1/pools/" + name
	if namespace != "" {
		path += "?namespace=" + url.QueryEscape(namespace)
	}
	return c.do("DELETE", path, nil, nil)
}

// --- Experiment API ---

func (c *Client) ListExperiments() ([]ExperimentSummary, error) {
	var exps []ExperimentSummary
	return exps, c.do("GET", "/v1/managed/experiments", nil, &exps)
}

func (c *Client) ListExperimentSessions(experimentID string) ([]ManagedSessionInfo, error) {
	var sessions []ManagedSessionInfo
	return sessions, c.do("GET", "/v1/managed/experiments/"+experimentID+"/sessions", nil, &sessions)
}

func (c *Client) CreateManagedSession(req CreateManagedSessionRequest) (*ManagedSessionInfo, error) {
	var info ManagedSessionInfo
	return &info, c.do("POST", "/v1/managed/sessions", req, &info)
}

func (c *Client) DeleteExperiment(experimentID string) (map[string]any, error) {
	var resp map[string]any
	return resp, c.do("DELETE", "/v1/managed/experiments/"+experimentID, nil, &resp)
}

// --- Health ---

func (c *Client) HealthCheck() error {
	_, code, err := c.rawGet("/healthz")
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("unhealthy: HTTP %d", code)
	}
	return nil
}
