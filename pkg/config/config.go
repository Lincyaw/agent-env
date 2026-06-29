package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the gateway configuration.
type Config struct {
	// Sidecar configuration
	SidecarImage      string
	SidecarHTTPPort   int
	SidecarGRPCPort   int
	WorkspaceDir      string
	HTTPClientTimeout time.Duration

	// ClickHouse configuration
	ClickHouseEnabled  bool
	ClickHouseAddr     string
	ClickHouseDatabase string
	ClickHouseUsername string
	ClickHousePassword string

	// Trajectory storage configuration (uses ClickHouse with GORM)
	TrajectoryEnabled bool
	TrajectoryDebug   bool

	// gRPC authentication token (shared between gateway and sidecar)
	GRPCAuthToken      string
	GRPCAuthSecretName string

	// Executor agent configuration
	ExecutorAgentImage string

	// ImagePullPolicy is applied to the gateway-injected sidecar and
	// executor-agent init containers. Defaults to "Always" (production:
	// always fetch the latest pushed sidecar). Set to "IfNotPresent" for
	// local clusters (kind/minikube) where images are side-loaded and never
	// pushed to a registry — otherwise kubelet ignores the local image and
	// fails with ImagePullBackOff. Env: IMAGE_PULL_POLICY.
	ImagePullPolicy string

	// Gateway configuration
	GatewayPort int

	// Kubernetes client tuning.
	K8sClientQPS   float32
	K8sClientBurst int

	// Gateway session lifecycle configuration
	GatewayIdleTimeout   time.Duration
	GatewayMaxLifetime   time.Duration
	GatewaySweepInterval time.Duration
	GatewayWriteTimeout  time.Duration

	// Redis session store configuration
	RedisEnabled  bool
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// Authentication configuration
	AuthEnabled    bool
	AuthAPIKeys    string
	InternalPort   int
	RateLimitRPS   float64
	RateLimitBurst int
	AllowedOrigins string

	// HTTP proxy injected into warm pool pods (all containers).
	// When non-empty, HTTP_PROXY/HTTPS_PROXY/NO_PROXY env vars are set.
	PodHTTPProxy string
	PodNoProxy   string

	// Admission control and warm-pool autoscaling.
	AdmissionDisableColdStart  bool
	AdmissionQueueTimeout      time.Duration
	AdmissionQueuePollInterval time.Duration
	PoolAutoscalerEnabled      bool
	PoolAutoscalerInterval     time.Duration
	PoolAutoscalerBuffer       int32
	PoolAutoscalerMinReplicas  int32
	PoolAutoscalerMaxReplicas  int32

	// Scheduler integration.
	SchedulerName        string
	ImageLocalityEnabled bool

	// Sandbox security policy applied to generated SandboxTemplates.
	SandboxNetworkPolicyManagement  string
	SandboxRuntimeClassName         string
	SandboxSeccompProfileType       string
	SandboxSeccompLocalhostProfile  string
	SandboxAllowPrivilegeEscalation bool
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		SidecarImage:       "arl-sidecar:latest",
		SidecarHTTPPort:    8080,
		SidecarGRPCPort:    9090,
		WorkspaceDir:       "/workspace",
		HTTPClientTimeout:  30 * time.Second,
		ClickHouseEnabled:  false,
		ClickHouseAddr:     "localhost:9000",
		ClickHouseDatabase: "arl",
		ClickHouseUsername: "default",
		ClickHousePassword: "",
		GRPCAuthToken:      "",
		GRPCAuthSecretName: "agent-env-grpc-token",
		TrajectoryEnabled:  false,
		TrajectoryDebug:    false,
		ExecutorAgentImage: "arl-executor-agent:latest",
		ImagePullPolicy:    "Always",
		GatewayPort:        8080,
		K8sClientQPS:       10000,
		K8sClientBurst:     20000,

		GatewayIdleTimeout:   600 * time.Second,
		GatewayMaxLifetime:   3600 * time.Second,
		GatewaySweepInterval: 30 * time.Second,
		GatewayWriteTimeout:  0,

		RedisEnabled:  false,
		RedisAddr:     "localhost:6379",
		RedisPassword: "",
		RedisDB:       0,

		AuthEnabled:    true,
		AuthAPIKeys:    "",
		InternalPort:   9091,
		RateLimitRPS:   2048,
		RateLimitBurst: 4096,
		AllowedOrigins: "",

		AdmissionDisableColdStart:       false,
		AdmissionQueueTimeout:           0,
		AdmissionQueuePollInterval:      500 * time.Millisecond,
		PoolAutoscalerEnabled:           false,
		PoolAutoscalerInterval:          30 * time.Second,
		PoolAutoscalerBuffer:            1,
		PoolAutoscalerMinReplicas:       0,
		PoolAutoscalerMaxReplicas:       0,
		SchedulerName:                   "",
		ImageLocalityEnabled:            false,
		SandboxNetworkPolicyManagement:  "Unmanaged",
		SandboxRuntimeClassName:         "",
		SandboxSeccompProfileType:       "RuntimeDefault",
		SandboxSeccompLocalhostProfile:  "",
		SandboxAllowPrivilegeEscalation: false,
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

	// Trajectory configuration
	if enable := os.Getenv("TRAJECTORY_ENABLED"); enable == "true" {
		cfg.TrajectoryEnabled = true
	}

	if debug := os.Getenv("TRAJECTORY_DEBUG"); debug == "true" {
		cfg.TrajectoryDebug = true
	}

	if v := os.Getenv("GRPC_AUTH_TOKEN"); v != "" {
		cfg.GRPCAuthToken = v
	}
	if v := os.Getenv("GRPC_AUTH_SECRET_NAME"); v != "" {
		cfg.GRPCAuthSecretName = v
	}

	// Executor agent configuration
	if image := os.Getenv("EXECUTOR_AGENT_IMAGE"); image != "" {
		cfg.ExecutorAgentImage = image
	}

	if v := os.Getenv("IMAGE_PULL_POLICY"); v != "" {
		cfg.ImagePullPolicy = v
	}

	// Gateway configuration
	if port := os.Getenv("GATEWAY_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.GatewayPort = p
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

	// Gateway session lifecycle configuration
	if v := os.Getenv("GATEWAY_IDLE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.GatewayIdleTimeout = d
		}
	}

	if v := os.Getenv("GATEWAY_MAX_LIFETIME"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.GatewayMaxLifetime = d
		}
	}

	if v := os.Getenv("GATEWAY_SWEEP_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.GatewaySweepInterval = d
		}
	}

	if v := os.Getenv("GATEWAY_WRITE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.GatewayWriteTimeout = d
		}
	}

	// Redis session store configuration
	if enable := os.Getenv("REDIS_ENABLED"); enable == "true" {
		cfg.RedisEnabled = true
	}

	if v := os.Getenv("REDIS_ADDR"); v != "" {
		cfg.RedisAddr = v
	}

	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		cfg.RedisPassword = v
	}

	if v := os.Getenv("REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RedisDB = n
		}
	}

	// Authentication configuration.
	// Auth is on by default (fail-closed); disabling it requires an explicit
	// AUTH_ENABLED=false, never an omitted or malformed value.
	if v := os.Getenv("AUTH_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.AuthEnabled = b
		}
	}

	if v := os.Getenv("AUTH_API_KEYS"); v != "" {
		cfg.AuthAPIKeys = v
	}

	if v := os.Getenv("INTERNAL_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.InternalPort = n
		}
	}

	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.RateLimitRPS = f
		}
	}

	if v := os.Getenv("RATE_LIMIT_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimitBurst = n
		}
	}

	if v := os.Getenv("ALLOWED_ORIGINS"); v != "" {
		cfg.AllowedOrigins = v
	}

	if v := os.Getenv("POD_HTTP_PROXY"); v != "" {
		cfg.PodHTTPProxy = v
	}
	if v := os.Getenv("POD_NO_PROXY"); v != "" {
		cfg.PodNoProxy = v
	}

	if v := os.Getenv("ADMISSION_DISABLE_COLD_START"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.AdmissionDisableColdStart = b
		}
	}
	if v := os.Getenv("ADMISSION_QUEUE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.AdmissionQueueTimeout = d
		}
	}
	if v := os.Getenv("ADMISSION_QUEUE_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.AdmissionQueuePollInterval = d
		}
	}
	if v := os.Getenv("POOL_AUTOSCALER_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.PoolAutoscalerEnabled = b
		}
	}
	if v := os.Getenv("POOL_AUTOSCALER_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.PoolAutoscalerInterval = d
		}
	}
	if v := os.Getenv("POOL_AUTOSCALER_BUFFER"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			cfg.PoolAutoscalerBuffer = int32(n)
		}
	}
	if v := os.Getenv("POOL_AUTOSCALER_MIN_REPLICAS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			cfg.PoolAutoscalerMinReplicas = int32(n)
		}
	}
	if v := os.Getenv("POOL_AUTOSCALER_MAX_REPLICAS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			cfg.PoolAutoscalerMaxReplicas = int32(n)
		}
	}
	if v := os.Getenv("SCHEDULER_NAME"); v != "" {
		cfg.SchedulerName = v
	}
	if v := os.Getenv("IMAGE_LOCALITY_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.ImageLocalityEnabled = b
		}
	}
	if v := os.Getenv("SANDBOX_NETWORK_POLICY_MANAGEMENT"); v != "" {
		cfg.SandboxNetworkPolicyManagement = v
	}
	if v := os.Getenv("SANDBOX_RUNTIME_CLASS_NAME"); v != "" {
		cfg.SandboxRuntimeClassName = v
	}
	if v := os.Getenv("SANDBOX_SECCOMP_PROFILE_TYPE"); v != "" {
		cfg.SandboxSeccompProfileType = v
	}
	if v := os.Getenv("SANDBOX_SECCOMP_LOCALHOST_PROFILE"); v != "" {
		cfg.SandboxSeccompLocalhostProfile = v
	}
	if v := os.Getenv("SANDBOX_ALLOW_PRIVILEGE_ESCALATION"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.SandboxAllowPrivilegeEscalation = b
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

	// Validate timeouts
	if c.HTTPClientTimeout <= 0 {
		return fmt.Errorf("HTTP client timeout must be positive: %v", c.HTTPClientTimeout)
	}
	if c.GRPCAuthSecretName == "" {
		return fmt.Errorf("gRPC auth secret name is required")
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

	}

	// Validate gateway configuration
	if c.GatewayPort < 1 || c.GatewayPort > 65535 {
		return fmt.Errorf("invalid gateway port: %d (must be 1-65535)", c.GatewayPort)
	}
	if c.K8sClientQPS <= 0 {
		return fmt.Errorf("k8s client QPS must be > 0: %v", c.K8sClientQPS)
	}

	if c.K8sClientBurst < 1 {
		return fmt.Errorf("k8s client burst must be >= 1: %d", c.K8sClientBurst)
	}

	// Validate gateway session lifecycle configuration
	if c.GatewayIdleTimeout < 0 {
		return fmt.Errorf("gateway idle timeout cannot be negative: %v", c.GatewayIdleTimeout)
	}

	if c.GatewayMaxLifetime < 0 {
		return fmt.Errorf("gateway max lifetime cannot be negative: %v", c.GatewayMaxLifetime)
	}

	if c.GatewayWriteTimeout < 0 {
		return fmt.Errorf("gateway write timeout cannot be negative: %v", c.GatewayWriteTimeout)
	}

	if c.GatewaySweepInterval <= 0 {
		return fmt.Errorf("gateway sweep interval must be positive: %v", c.GatewaySweepInterval)
	}

	// Auth key validation is deferred to cmd/gateway/main.go which checks
	// both AUTH_API_KEYS and AUTH_KEY_FILE before starting.

	if c.InternalPort < 1 || c.InternalPort > 65535 {
		return fmt.Errorf("invalid internal port: %d (must be 1-65535)", c.InternalPort)
	}

	if c.InternalPort == c.GatewayPort {
		return fmt.Errorf("internal port (%d) must differ from gateway port (%d)", c.InternalPort, c.GatewayPort)
	}

	if c.RateLimitRPS <= 0 {
		return fmt.Errorf("rate limit RPS must be > 0: %v", c.RateLimitRPS)
	}

	if c.RateLimitBurst < 1 {
		return fmt.Errorf("rate limit burst must be >= 1: %d", c.RateLimitBurst)
	}

	if c.AdmissionQueueTimeout < 0 {
		return fmt.Errorf("admission queue timeout cannot be negative: %v", c.AdmissionQueueTimeout)
	}
	if c.AdmissionQueuePollInterval <= 0 {
		return fmt.Errorf("admission queue poll interval must be positive: %v", c.AdmissionQueuePollInterval)
	}
	if c.PoolAutoscalerInterval <= 0 {
		return fmt.Errorf("pool autoscaler interval must be positive: %v", c.PoolAutoscalerInterval)
	}
	if c.PoolAutoscalerBuffer < 0 {
		return fmt.Errorf("pool autoscaler buffer cannot be negative: %d", c.PoolAutoscalerBuffer)
	}
	if c.PoolAutoscalerMinReplicas < 0 {
		return fmt.Errorf("pool autoscaler min replicas cannot be negative: %d", c.PoolAutoscalerMinReplicas)
	}
	if c.PoolAutoscalerMaxReplicas < 0 {
		return fmt.Errorf("pool autoscaler max replicas cannot be negative: %d", c.PoolAutoscalerMaxReplicas)
	}
	if c.PoolAutoscalerMaxReplicas > 0 && c.PoolAutoscalerMaxReplicas < c.PoolAutoscalerMinReplicas {
		return fmt.Errorf("pool autoscaler max replicas (%d) must be >= min replicas (%d)", c.PoolAutoscalerMaxReplicas, c.PoolAutoscalerMinReplicas)
	}
	switch strings.ToLower(strings.TrimSpace(c.SandboxNetworkPolicyManagement)) {
	case "", "managed", "unmanaged":
	default:
		return fmt.Errorf("sandbox network policy management must be Managed or Unmanaged: %q", c.SandboxNetworkPolicyManagement)
	}
	switch strings.ToLower(strings.TrimSpace(c.SandboxSeccompProfileType)) {
	case "", "runtimedefault", "unconfined", "localhost":
	default:
		return fmt.Errorf("sandbox seccomp profile type must be RuntimeDefault, Unconfined, or Localhost: %q", c.SandboxSeccompProfileType)
	}
	if strings.EqualFold(strings.TrimSpace(c.SandboxSeccompProfileType), "Localhost") && strings.TrimSpace(c.SandboxSeccompLocalhostProfile) == "" {
		return fmt.Errorf("sandbox seccomp localhost profile is required when seccomp profile type is Localhost")
	}

	return nil
}
