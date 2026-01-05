package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds the operator configuration
type Config struct {
	// Sidecar configuration
	SidecarPort       int
	WorkspaceDir      string
	HTTPClientTimeout time.Duration

	// Pool configuration
	DefaultPoolReplicas  int32
	DefaultRequeueDelay  time.Duration
	SandboxCheckInterval time.Duration

	// Operator configuration
	MetricsAddr          string
	ProbeAddr            string
	EnableLeaderElection bool
	EnableWebhooks       bool
	EnableMetrics        bool

	// Feature flags
	EnableMiddleware bool
	EnableValidation bool
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		SidecarPort:          8080,
		WorkspaceDir:         "/workspace",
		HTTPClientTimeout:    30 * time.Second,
		DefaultPoolReplicas:  3,
		DefaultRequeueDelay:  10 * time.Second,
		SandboxCheckInterval: 2 * time.Second,
		MetricsAddr:          ":8080",
		ProbeAddr:            ":8081",
		EnableLeaderElection: false,
		EnableWebhooks:       false,
		EnableMetrics:        true,
		EnableMiddleware:     true,
		EnableValidation:     true,
	}
}

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() *Config {
	cfg := DefaultConfig()

	if port := os.Getenv("SIDECAR_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.SidecarPort = p
		}
	}

	if dir := os.Getenv("WORKSPACE_DIR"); dir != "" {
		cfg.WorkspaceDir = dir
	}

	if timeout := os.Getenv("HTTP_CLIENT_TIMEOUT"); timeout != "" {
		if d, err := time.ParseDuration(timeout); err == nil {
			cfg.HTTPClientTimeout = d
		}
	}

	if replicas := os.Getenv("DEFAULT_POOL_REPLICAS"); replicas != "" {
		if r, err := strconv.ParseInt(replicas, 10, 32); err == nil {
			cfg.DefaultPoolReplicas = int32(r)
		}
	}

	if addr := os.Getenv("METRICS_ADDR"); addr != "" {
		cfg.MetricsAddr = addr
	}

	if addr := os.Getenv("PROBE_ADDR"); addr != "" {
		cfg.ProbeAddr = addr
	}

	if enable := os.Getenv("ENABLE_LEADER_ELECTION"); enable == "true" {
		cfg.EnableLeaderElection = true
	}

	if enable := os.Getenv("ENABLE_WEBHOOKS"); enable == "true" {
		cfg.EnableWebhooks = true
	}

	if enable := os.Getenv("ENABLE_METRICS"); enable == "false" {
		cfg.EnableMetrics = false
	}

	if enable := os.Getenv("ENABLE_MIDDLEWARE"); enable == "false" {
		cfg.EnableMiddleware = false
	}

	return cfg
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Add validation logic here if needed
	return nil
}
