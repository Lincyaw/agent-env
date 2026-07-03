package config

import (
	"strings"
	"testing"
	"time"
)

func TestConfigValidate(t *testing.T) {
	valid := func() *Config {
		cfg := DefaultConfig()
		cfg.AuthAPIKeys = "test:admin"
		return cfg
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "valid default config",
		},
		{
			name: "invalid sidecar HTTP port",
			mutate: func(cfg *Config) {
				cfg.SidecarHTTPPort = 0
			},
			wantErr: "invalid sidecar HTTP port",
		},
		{
			name: "invalid sidecar gRPC port",
			mutate: func(cfg *Config) {
				cfg.SidecarGRPCPort = -1
			},
			wantErr: "invalid sidecar gRPC port",
		},
		{
			name: "invalid HTTP client timeout",
			mutate: func(cfg *Config) {
				cfg.HTTPClientTimeout = 0
			},
			wantErr: "HTTP client timeout must be positive",
		},
		{
			name: "missing gRPC auth secret name",
			mutate: func(cfg *Config) {
				cfg.GRPCAuthSecretName = ""
			},
			wantErr: "gRPC auth secret name is required",
		},
		{
			name: "ClickHouse enabled without address",
			mutate: func(cfg *Config) {
				cfg.ClickHouseEnabled = true
				cfg.ClickHouseAddr = ""
			},
			wantErr: "ClickHouse address is required",
		},
		{
			name: "ClickHouse enabled without database",
			mutate: func(cfg *Config) {
				cfg.ClickHouseEnabled = true
				cfg.ClickHouseDatabase = ""
			},
			wantErr: "ClickHouse database name is required",
		},
		{
			name: "ClickHouse enabled without password",
			mutate: func(cfg *Config) {
				cfg.ClickHouseEnabled = true
				cfg.ClickHousePassword = ""
			},
			wantErr: "ClickHouse password is required",
		},
		{
			name: "invalid gateway port",
			mutate: func(cfg *Config) {
				cfg.GatewayPort = 70000
			},
			wantErr: "invalid gateway port",
		},
		{
			name: "missing gateway namespace",
			mutate: func(cfg *Config) {
				cfg.GatewayNamespace = " "
			},
			wantErr: "gateway namespace is required",
		},
		{
			name: "invalid k8s QPS",
			mutate: func(cfg *Config) {
				cfg.K8sClientQPS = 0
			},
			wantErr: "k8s client QPS must be > 0",
		},
		{
			name: "invalid sweep interval",
			mutate: func(cfg *Config) {
				cfg.GatewaySweepInterval = 0
			},
			wantErr: "gateway sweep interval must be positive",
		},
		{
			name: "invalid internal port conflict",
			mutate: func(cfg *Config) {
				cfg.InternalPort = cfg.GatewayPort
			},
			wantErr: "internal port",
		},
		{
			name: "invalid admission queue timeout",
			mutate: func(cfg *Config) {
				cfg.AdmissionQueueTimeout = -time.Second
			},
			wantErr: "admission queue timeout",
		},
		{
			name: "invalid pool autoscaler max below min",
			mutate: func(cfg *Config) {
				cfg.PoolAutoscalerMinReplicas = 3
				cfg.PoolAutoscalerMaxReplicas = 2
			},
			wantErr: "pool autoscaler max replicas",
		},
		{
			name: "invalid sandbox network policy management",
			mutate: func(cfg *Config) {
				cfg.SandboxNetworkPolicyManagement = "sometimes"
			},
			wantErr: "sandbox network policy management",
		},
		{
			name: "invalid sandbox default resource quantity",
			mutate: func(cfg *Config) {
				cfg.DefaultSandboxLimitMemory = "lots"
			},
			wantErr: "sandbox default limit memory",
		},
		{
			name: "non-positive sandbox default resource quantity",
			mutate: func(cfg *Config) {
				cfg.DefaultSandboxRequestCPU = "0"
			},
			wantErr: "sandbox default request cpu must be positive",
		},
		{
			name: "invalid sandbox seccomp profile type",
			mutate: func(cfg *Config) {
				cfg.SandboxSeccompProfileType = "custom"
			},
			wantErr: "sandbox seccomp profile type",
		},
		{
			name: "localhost seccomp requires profile",
			mutate: func(cfg *Config) {
				cfg.SandboxSeccompProfileType = "Localhost"
			},
			wantErr: "sandbox seccomp localhost profile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid()
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.SidecarHTTPPort != 8080 {
		t.Errorf("SidecarHTTPPort = %d, want 8080", cfg.SidecarHTTPPort)
	}
	if cfg.SidecarGRPCPort != 9090 {
		t.Errorf("SidecarGRPCPort = %d, want 9090", cfg.SidecarGRPCPort)
	}
	if cfg.WorkspaceDir != "/workspace" {
		t.Errorf("WorkspaceDir = %q, want /workspace", cfg.WorkspaceDir)
	}
	if cfg.HTTPClientTimeout != 5*time.Minute {
		t.Errorf("HTTPClientTimeout = %v, want 5m", cfg.HTTPClientTimeout)
	}
	if cfg.ImagePullPolicy != "Always" {
		t.Errorf("ImagePullPolicy = %q, want Always", cfg.ImagePullPolicy)
	}
	if cfg.GatewayNamespace != "default" {
		t.Errorf("GatewayNamespace = %q, want default", cfg.GatewayNamespace)
	}
	if cfg.GRPCAuthSecretName != "agent-env-grpc-token" {
		t.Errorf("GRPCAuthSecretName = %q, want agent-env-grpc-token", cfg.GRPCAuthSecretName)
	}
	if cfg.AdmissionQueueTimeout != 0 {
		t.Errorf("AdmissionQueueTimeout = %v, want 0", cfg.AdmissionQueueTimeout)
	}
	if cfg.AdmissionQueuePollInterval != 500*time.Millisecond {
		t.Errorf("AdmissionQueuePollInterval = %v, want 500ms", cfg.AdmissionQueuePollInterval)
	}
	if cfg.PoolAutoscalerEnabled {
		t.Error("PoolAutoscalerEnabled = true, want false")
	}
	if cfg.PoolAutoscalerBuffer != 1 {
		t.Errorf("PoolAutoscalerBuffer = %d, want 1", cfg.PoolAutoscalerBuffer)
	}
	if cfg.DefaultSandboxRequestCPU != "500m" {
		t.Errorf("DefaultSandboxRequestCPU = %q, want 500m", cfg.DefaultSandboxRequestCPU)
	}
	if cfg.DefaultSandboxRequestMemory != "512Mi" {
		t.Errorf("DefaultSandboxRequestMemory = %q, want 512Mi", cfg.DefaultSandboxRequestMemory)
	}
	if cfg.DefaultSandboxLimitCPU != "8" {
		t.Errorf("DefaultSandboxLimitCPU = %q, want 8", cfg.DefaultSandboxLimitCPU)
	}
	if cfg.DefaultSandboxLimitMemory != "16Gi" {
		t.Errorf("DefaultSandboxLimitMemory = %q, want 16Gi", cfg.DefaultSandboxLimitMemory)
	}
	if cfg.SandboxNetworkPolicyManagement != "Unmanaged" {
		t.Errorf("SandboxNetworkPolicyManagement = %q, want Unmanaged", cfg.SandboxNetworkPolicyManagement)
	}
	if cfg.SandboxSeccompProfileType != "RuntimeDefault" {
		t.Errorf("SandboxSeccompProfileType = %q, want RuntimeDefault", cfg.SandboxSeccompProfileType)
	}
	if cfg.SandboxAllowPrivilegeEscalation {
		t.Error("SandboxAllowPrivilegeEscalation = true, want false")
	}
}

func TestLoadFromEnvImagePullPolicy(t *testing.T) {
	t.Setenv("IMAGE_PULL_POLICY", "")
	if got := LoadFromEnv().ImagePullPolicy; got != "Always" {
		t.Fatalf("empty IMAGE_PULL_POLICY = %q, want Always", got)
	}

	t.Setenv("IMAGE_PULL_POLICY", "IfNotPresent")
	if got := LoadFromEnv().ImagePullPolicy; got != "IfNotPresent" {
		t.Fatalf("IMAGE_PULL_POLICY override = %q, want IfNotPresent", got)
	}
}

func TestLoadFromEnvGatewayNamespaceFallsBackToPodNamespace(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "arl1")

	if got := LoadFromEnv().GatewayNamespace; got != "arl1" {
		t.Fatalf("GatewayNamespace = %q, want arl1", got)
	}
}

func TestLoadFromEnvGatewaySettings(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("POD_NAMESPACE", "pod-ns")
	t.Setenv("GATEWAY_NAMESPACE", "gateway-ns")
	t.Setenv("GATEWAY_IDLE_TIMEOUT", "45s")
	t.Setenv("K8S_CLIENT_QPS", "123")
	t.Setenv("K8S_CLIENT_BURST", "456")
	t.Setenv("GRPC_AUTH_SECRET_NAME", "custom-grpc-token")
	t.Setenv("ADMISSION_QUEUE_TIMEOUT", "2s")
	t.Setenv("ADMISSION_QUEUE_POLL_INTERVAL", "100ms")
	t.Setenv("POOL_AUTOSCALER_ENABLED", "true")
	t.Setenv("POOL_AUTOSCALER_INTERVAL", "15s")
	t.Setenv("POOL_AUTOSCALER_BUFFER", "4")
	t.Setenv("POOL_AUTOSCALER_MIN_REPLICAS", "2")
	t.Setenv("POOL_AUTOSCALER_MAX_REPLICAS", "20")
	t.Setenv("SCHEDULER_NAME", "agent-env-image-locality")
	t.Setenv("IMAGE_LOCALITY_ENABLED", "true")
	t.Setenv("SANDBOX_DEFAULT_REQUEST_CPU", "250m")
	t.Setenv("SANDBOX_DEFAULT_REQUEST_MEMORY", "256Mi")
	t.Setenv("SANDBOX_DEFAULT_LIMIT_CPU", "8")
	t.Setenv("SANDBOX_DEFAULT_LIMIT_MEMORY", "16Gi")
	t.Setenv("SANDBOX_NETWORK_POLICY_MANAGEMENT", "Managed")
	t.Setenv("SANDBOX_RUNTIME_CLASS_NAME", "kata")
	t.Setenv("SANDBOX_SECCOMP_PROFILE_TYPE", "Localhost")
	t.Setenv("SANDBOX_SECCOMP_LOCALHOST_PROFILE", "profiles/agent-env.json")
	t.Setenv("SANDBOX_ALLOW_PRIVILEGE_ESCALATION", "true")

	cfg := LoadFromEnv()
	if cfg.AuthEnabled {
		t.Fatal("AuthEnabled = true, want false")
	}
	if cfg.GatewayIdleTimeout != 45*time.Second {
		t.Fatalf("GatewayIdleTimeout = %v, want 45s", cfg.GatewayIdleTimeout)
	}
	if cfg.GatewayNamespace != "gateway-ns" {
		t.Fatalf("GatewayNamespace = %q, want gateway-ns", cfg.GatewayNamespace)
	}
	if cfg.K8sClientQPS != 123 {
		t.Fatalf("K8sClientQPS = %v, want 123", cfg.K8sClientQPS)
	}
	if cfg.K8sClientBurst != 456 {
		t.Fatalf("K8sClientBurst = %d, want 456", cfg.K8sClientBurst)
	}
	if cfg.GRPCAuthSecretName != "custom-grpc-token" {
		t.Fatalf("GRPCAuthSecretName = %q, want custom-grpc-token", cfg.GRPCAuthSecretName)
	}
	if cfg.AdmissionQueueTimeout != 2*time.Second {
		t.Fatalf("AdmissionQueueTimeout = %v, want 2s", cfg.AdmissionQueueTimeout)
	}
	if cfg.AdmissionQueuePollInterval != 100*time.Millisecond {
		t.Fatalf("AdmissionQueuePollInterval = %v, want 100ms", cfg.AdmissionQueuePollInterval)
	}
	if !cfg.PoolAutoscalerEnabled {
		t.Fatal("PoolAutoscalerEnabled = false, want true")
	}
	if cfg.PoolAutoscalerInterval != 15*time.Second {
		t.Fatalf("PoolAutoscalerInterval = %v, want 15s", cfg.PoolAutoscalerInterval)
	}
	if cfg.PoolAutoscalerBuffer != 4 {
		t.Fatalf("PoolAutoscalerBuffer = %d, want 4", cfg.PoolAutoscalerBuffer)
	}
	if cfg.PoolAutoscalerMinReplicas != 2 {
		t.Fatalf("PoolAutoscalerMinReplicas = %d, want 2", cfg.PoolAutoscalerMinReplicas)
	}
	if cfg.PoolAutoscalerMaxReplicas != 20 {
		t.Fatalf("PoolAutoscalerMaxReplicas = %d, want 20", cfg.PoolAutoscalerMaxReplicas)
	}
	if cfg.SchedulerName != "agent-env-image-locality" {
		t.Fatalf("SchedulerName = %q, want agent-env-image-locality", cfg.SchedulerName)
	}
	if !cfg.ImageLocalityEnabled {
		t.Fatal("ImageLocalityEnabled = false, want true")
	}
	if cfg.DefaultSandboxRequestCPU != "250m" {
		t.Fatalf("DefaultSandboxRequestCPU = %q, want 250m", cfg.DefaultSandboxRequestCPU)
	}
	if cfg.DefaultSandboxRequestMemory != "256Mi" {
		t.Fatalf("DefaultSandboxRequestMemory = %q, want 256Mi", cfg.DefaultSandboxRequestMemory)
	}
	if cfg.DefaultSandboxLimitCPU != "8" {
		t.Fatalf("DefaultSandboxLimitCPU = %q, want 8", cfg.DefaultSandboxLimitCPU)
	}
	if cfg.DefaultSandboxLimitMemory != "16Gi" {
		t.Fatalf("DefaultSandboxLimitMemory = %q, want 16Gi", cfg.DefaultSandboxLimitMemory)
	}
	if cfg.SandboxNetworkPolicyManagement != "Managed" {
		t.Fatalf("SandboxNetworkPolicyManagement = %q, want Managed", cfg.SandboxNetworkPolicyManagement)
	}
	if cfg.SandboxRuntimeClassName != "kata" {
		t.Fatalf("SandboxRuntimeClassName = %q, want kata", cfg.SandboxRuntimeClassName)
	}
	if cfg.SandboxSeccompProfileType != "Localhost" {
		t.Fatalf("SandboxSeccompProfileType = %q, want Localhost", cfg.SandboxSeccompProfileType)
	}
	if cfg.SandboxSeccompLocalhostProfile != "profiles/agent-env.json" {
		t.Fatalf("SandboxSeccompLocalhostProfile = %q, want profiles/agent-env.json", cfg.SandboxSeccompLocalhostProfile)
	}
	if !cfg.SandboxAllowPrivilegeEscalation {
		t.Fatal("SandboxAllowPrivilegeEscalation = false, want true")
	}
}
