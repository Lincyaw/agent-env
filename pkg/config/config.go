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

	// ClickHouse configuration
	ClickHouseEnabled       bool
	ClickHouseAddr          string
	ClickHouseDatabase      string
	ClickHouseUsername      string
	ClickHousePassword      string
	ClickHouseBatchSize     int
	ClickHouseFlushInterval time.Duration

	// Cleanup configuration
	TaskRetentionDays         int
	SandboxIdleTimeoutSeconds int32
	EnableAutoCleanup         bool
	TTLCheckInterval          time.Duration
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		SidecarPort:               8080,
		WorkspaceDir:              "/workspace",
		HTTPClientTimeout:         30 * time.Second,
		DefaultPoolReplicas:       3,
		DefaultRequeueDelay:       10 * time.Second,
		SandboxCheckInterval:      2 * time.Second,
		MetricsAddr:               ":8080",
		ProbeAddr:                 ":8081",
		EnableLeaderElection:      false,
		EnableWebhooks:            false,
		EnableMetrics:             true,
		EnableMiddleware:          true,
		EnableValidation:          true,
		ClickHouseEnabled:         false,
		ClickHouseAddr:            "localhost:9000",
		ClickHouseDatabase:        "arl",
		ClickHouseUsername:        "default",
		ClickHousePassword:        "",
		ClickHouseBatchSize:       100,
		ClickHouseFlushInterval:   10 * time.Second,
		TaskRetentionDays:         30,
		SandboxIdleTimeoutSeconds: 3600,
		EnableAutoCleanup:         true,
		TTLCheckInterval:          30 * time.Second,
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

	// ClickHouse configuration
	if enable := os.Getenv("CLICKHOUSE_ENABLED"); enable == "true" {
		cfg.ClickHouseEnabled = true
	}

	if addr := os.Getenv("CLICKHOUSE_ADDR"); addr != "" {
		cfg.ClickHouseAddr = addr
	}

	if db := os.Getenv("CLICKHOUSE_DATABASE"); db != "" {
		cfg.ClickHouseDatabase = db
	}

	if user := os.Getenv("CLICKHOUSE_USERNAME"); user != "" {
		cfg.ClickHouseUsername = user
	}

	if pass := os.Getenv("CLICKHOUSE_PASSWORD"); pass != "" {
		cfg.ClickHousePassword = pass
	}

	if batchSize := os.Getenv("CLICKHOUSE_BATCH_SIZE"); batchSize != "" {
		if b, err := strconv.Atoi(batchSize); err == nil {
			cfg.ClickHouseBatchSize = b
		}
	}

	if interval := os.Getenv("CLICKHOUSE_FLUSH_INTERVAL"); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			cfg.ClickHouseFlushInterval = d
		}
	}

	// Cleanup configuration
	if days := os.Getenv("TASK_RETENTION_DAYS"); days != "" {
		if d, err := strconv.Atoi(days); err == nil {
			cfg.TaskRetentionDays = d
		}
	}

	if timeout := os.Getenv("SANDBOX_IDLE_TIMEOUT_SECONDS"); timeout != "" {
		if t, err := strconv.ParseInt(timeout, 10, 32); err == nil {
			cfg.SandboxIdleTimeoutSeconds = int32(t)
		}
	}

	if enable := os.Getenv("ENABLE_AUTO_CLEANUP"); enable == "false" {
		cfg.EnableAutoCleanup = false
	}

	if interval := os.Getenv("TTL_CHECK_INTERVAL"); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			cfg.TTLCheckInterval = d
		}
	}

	return cfg
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Add validation logic here if needed
	return nil
}
