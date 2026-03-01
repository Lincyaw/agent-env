package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the operator configuration
type Config struct {
	// Sidecar configuration
	SidecarImage      string
	SidecarHTTPPort   int
	SidecarGRPCPort   int
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

	// Trajectory storage configuration (uses ClickHouse with GORM)
	TrajectoryEnabled bool
	TrajectoryDebug   bool

	// Sandbox lifecycle configuration
	SandboxIdleTimeoutSeconds int32
	SandboxMaxLifetimeSeconds int32

	// Executor agent configuration
	ExecutorAgentImage string

	// Gateway configuration
	GatewayPort int

	// Control-plane tuning
	WarmPoolMaxConcurrent  int
	SandboxMaxConcurrent   int
	K8sClientQPS           float32
	K8sClientBurst         int
	WarmPoolBaseDelayMs    int
	WarmPoolMaxDelayMs     int
	WarmPoolRateLimitQPS   float64
	WarmPoolRateLimitBurst int

	// Image-locality scheduling defaults.
	// These are used when the WarmPool CRD does not specify imageLocality fields.
	//
	// ImageLocalitySpreadFactor controls how many nodes to prefer:
	//   k = ceil(replicas × spreadFactor)
	// A smaller value concentrates pods on fewer nodes, maximising image cache
	// hits and reducing average pod startup latency (fewer image pulls).
	// Examples with 8 replicas:
	//   0.125 → 1 node   (maximum locality, risk of single-node failure)
	//   0.25  → 2 nodes  (good balance: high cache hit, some spread)
	//   0.5   → 4 nodes  (moderate spread)
	//   1.0   → 8 nodes  (one pod per node, most image pulls)
	//
	// ImageLocalityWeight is the Kubernetes preferredDuringScheduling weight
	// (1-100). Higher weight makes the scheduler try harder to place pods on
	// the preferred nodes, but will still fall back to other nodes if resources
	// are insufficient (soft affinity).
	ImageLocalitySpreadFactor float64
	ImageLocalityWeight       int32
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		SidecarImage:              "arl-sidecar:latest",
		SidecarHTTPPort:           8080,
		SidecarGRPCPort:           9090,
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
		TrajectoryEnabled:         false,
		TrajectoryDebug:           false,
		SandboxIdleTimeoutSeconds: 600,
		SandboxMaxLifetimeSeconds: 3600,
		ExecutorAgentImage:        "arl-executor-agent:latest",
		GatewayPort:               8080,
		WarmPoolMaxConcurrent:     50,
		SandboxMaxConcurrent:      20,
		K8sClientQPS:              200,
		K8sClientBurst:            400,
		WarmPoolBaseDelayMs:       200,
		WarmPoolMaxDelayMs:        15000,
		WarmPoolRateLimitQPS:      200,
		WarmPoolRateLimitBurst:    500,

		ImageLocalitySpreadFactor: 0.25,
		ImageLocalityWeight:       100,
	}
}

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() *Config {
	cfg := DefaultConfig()

	if image := os.Getenv("SIDECAR_IMAGE"); image != "" {
		cfg.SidecarImage = image
	}

	if port := os.Getenv("SIDECAR_HTTP_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.SidecarHTTPPort = p
		}
	}

	if port := os.Getenv("SIDECAR_GRPC_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.SidecarGRPCPort = p
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

	// Trajectory configuration
	if enable := os.Getenv("TRAJECTORY_ENABLED"); enable == "true" {
		cfg.TrajectoryEnabled = true
	}

	if debug := os.Getenv("TRAJECTORY_DEBUG"); debug == "true" {
		cfg.TrajectoryDebug = true
	}

	// Sandbox lifecycle configuration
	if timeout := os.Getenv("SANDBOX_IDLE_TIMEOUT_SECONDS"); timeout != "" {
		if t, err := strconv.ParseInt(timeout, 10, 32); err == nil {
			cfg.SandboxIdleTimeoutSeconds = int32(t)
		}
	}

	if lifetime := os.Getenv("SANDBOX_MAX_LIFETIME_SECONDS"); lifetime != "" {
		if l, err := strconv.ParseInt(lifetime, 10, 32); err == nil {
			cfg.SandboxMaxLifetimeSeconds = int32(l)
		}
	}

	// Executor agent configuration
	if image := os.Getenv("EXECUTOR_AGENT_IMAGE"); image != "" {
		cfg.ExecutorAgentImage = image
	}

	// Gateway configuration
	if port := os.Getenv("GATEWAY_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.GatewayPort = p
		}
	}

	if v := os.Getenv("WARMPOOL_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.WarmPoolMaxConcurrent = n
		}
	}

	if v := os.Getenv("SANDBOX_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.SandboxMaxConcurrent = n
		}
	}

	if v := os.Getenv("K8S_CLIENT_QPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			cfg.K8sClientQPS = float32(f)
		}
	}

	if v := os.Getenv("K8S_CLIENT_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.K8sClientBurst = n
		}
	}

	if v := os.Getenv("WARMPOOL_BASE_DELAY_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.WarmPoolBaseDelayMs = n
		}
	}

	if v := os.Getenv("WARMPOOL_MAX_DELAY_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.WarmPoolMaxDelayMs = n
		}
	}

	if v := os.Getenv("WARMPOOL_RATE_QPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.WarmPoolRateLimitQPS = f
		}
	}

	if v := os.Getenv("WARMPOOL_RATE_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.WarmPoolRateLimitBurst = n
		}
	}

	if v := os.Getenv("IMAGE_LOCALITY_SPREAD_FACTOR"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.ImageLocalitySpreadFactor = f
		}
	}

	if v := os.Getenv("IMAGE_LOCALITY_WEIGHT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 100 {
			cfg.ImageLocalityWeight = int32(n)
		}
	}

	return cfg
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate port ranges
	if c.SidecarHTTPPort < 1 || c.SidecarHTTPPort > 65535 {
		return fmt.Errorf("invalid sidecar HTTP port: %d (must be 1-65535)", c.SidecarHTTPPort)
	}

	if c.SidecarGRPCPort < 1 || c.SidecarGRPCPort > 65535 {
		return fmt.Errorf("invalid sidecar gRPC port: %d (must be 1-65535)", c.SidecarGRPCPort)
	}

	// Validate replica count
	if c.DefaultPoolReplicas < 0 {
		return fmt.Errorf("default pool replicas cannot be negative: %d", c.DefaultPoolReplicas)
	}

	// Validate timeouts
	if c.HTTPClientTimeout <= 0 {
		return fmt.Errorf("HTTP client timeout must be positive: %v", c.HTTPClientTimeout)
	}

	if c.DefaultRequeueDelay <= 0 {
		return fmt.Errorf("default requeue delay must be positive: %v", c.DefaultRequeueDelay)
	}

	if c.SandboxCheckInterval <= 0 {
		return fmt.Errorf("sandbox check interval must be positive: %v", c.SandboxCheckInterval)
	}

	// Validate ClickHouse configuration if enabled
	if c.ClickHouseEnabled {
		if c.ClickHouseAddr == "" {
			return fmt.Errorf("ClickHouse address is required when ClickHouse is enabled")
		}

		if c.ClickHouseDatabase == "" {
			return fmt.Errorf("ClickHouse database name is required when ClickHouse is enabled")
		}

		if c.ClickHousePassword == "" {
			return fmt.Errorf("ClickHouse password is required when ClickHouse is enabled (set CLICKHOUSE_PASSWORD)")
		}

		if c.ClickHouseBatchSize < 1 {
			return fmt.Errorf("ClickHouse batch size must be positive: %d", c.ClickHouseBatchSize)
		}

		if c.ClickHouseFlushInterval <= 0 {
			return fmt.Errorf("ClickHouse flush interval must be positive: %v", c.ClickHouseFlushInterval)
		}
	}

	// Validate sandbox lifecycle configuration
	if c.SandboxIdleTimeoutSeconds < 0 {
		return fmt.Errorf("sandbox idle timeout cannot be negative: %d", c.SandboxIdleTimeoutSeconds)
	}

	if c.SandboxMaxLifetimeSeconds < 0 {
		return fmt.Errorf("sandbox max lifetime cannot be negative: %d", c.SandboxMaxLifetimeSeconds)
	}

	// Validate gateway configuration
	if c.GatewayPort < 1 || c.GatewayPort > 65535 {
		return fmt.Errorf("invalid gateway port: %d (must be 1-65535)", c.GatewayPort)
	}

	if c.WarmPoolMaxConcurrent < 1 {
		return fmt.Errorf("warm pool max concurrent must be >= 1: %d", c.WarmPoolMaxConcurrent)
	}

	if c.SandboxMaxConcurrent < 1 {
		return fmt.Errorf("sandbox max concurrent must be >= 1: %d", c.SandboxMaxConcurrent)
	}

	if c.K8sClientQPS <= 0 {
		return fmt.Errorf("k8s client QPS must be > 0: %v", c.K8sClientQPS)
	}

	if c.K8sClientBurst < 1 {
		return fmt.Errorf("k8s client burst must be >= 1: %d", c.K8sClientBurst)
	}

	if c.WarmPoolBaseDelayMs < 1 {
		return fmt.Errorf("warm pool base delay ms must be >= 1: %d", c.WarmPoolBaseDelayMs)
	}

	if c.WarmPoolMaxDelayMs < c.WarmPoolBaseDelayMs {
		return fmt.Errorf("warm pool max delay ms must be >= base delay ms: %d < %d", c.WarmPoolMaxDelayMs, c.WarmPoolBaseDelayMs)
	}

	if c.WarmPoolRateLimitQPS <= 0 {
		return fmt.Errorf("warm pool rate limit QPS must be > 0: %v", c.WarmPoolRateLimitQPS)
	}

	if c.WarmPoolRateLimitBurst < 1 {
		return fmt.Errorf("warm pool rate limit burst must be >= 1: %d", c.WarmPoolRateLimitBurst)
	}

	if c.ImageLocalitySpreadFactor < 0 || c.ImageLocalitySpreadFactor > 10 {
		return fmt.Errorf("image locality spread factor must be 0-10: %v", c.ImageLocalitySpreadFactor)
	}

	if c.ImageLocalityWeight < 1 || c.ImageLocalityWeight > 100 {
		return fmt.Errorf("image locality weight must be 1-100: %d", c.ImageLocalityWeight)
	}

	return nil
}
