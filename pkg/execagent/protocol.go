package execagent

// Request is the JSON-over-socket protocol request from sidecar to executor agent.
type Request struct {
	ID             string            `json:"id"`
	Type           string            `json:"type"` // "exec", "signal", "ping", "shell", "stdin", "write_file_stream", "write_file_chunk", "write_file_finish", "read_file_stream"
	Cmd            []string          `json:"cmd,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	WorkDir        string            `json:"workdir,omitempty"`
	Timeout        int               `json:"timeout,omitempty"`
	PID            int               `json:"pid,omitempty"`
	Signal         string            `json:"signal,omitempty"`
	Data           string            `json:"data,omitempty"` // stdin data for "stdin" type
	Path           string            `json:"path,omitempty"`
	Content        []byte            `json:"content,omitempty"`
	ExpectedSHA256 string            `json:"expected_sha256,omitempty"`
}

// Response is the JSON-over-socket protocol response from executor agent to sidecar.
type Response struct {
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
