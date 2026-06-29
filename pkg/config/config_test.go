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
			name: "forward auth missing user header",
			mutate: func(cfg *Config) {
				cfg.AuthForwardHeadersEnabled = true
				cfg.AuthForwardUserHeader = ""
				cfg.AuthForwardTrustedProxies = "10.0.0.0/8"
			},
			wantErr: "auth forward user header",
		},
		{
			name: "forward auth missing trusted proxies",
			mutate: func(cfg *Config) {
				cfg.AuthForwardHeadersEnabled = true
				cfg.AuthForwardTrustedProxies = ""
			},
			wantErr: "auth forward trusted proxies",
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
	if cfg.HTTPClientTimeout != 30*time.Second {
		t.Errorf("HTTPClientTimeout = %v, want 30s", cfg.HTTPClientTimeout)
	}
	if cfg.ImagePullPolicy != "Always" {
		t.Errorf("ImagePullPolicy = %q, want Always", cfg.ImagePullPolicy)
	}
	if cfg.GRPCAuthSecretName != "agent-env-grpc-token" {
		t.Errorf("GRPCAuthSecretName = %q, want agent-env-grpc-token", cfg.GRPCAuthSecretName)
	}
	if cfg.AdmissionDisableColdStart {
		t.Error("AdmissionDisableColdStart = true, want false")
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

func TestLoadFromEnvGatewaySettings(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("GATEWAY_IDLE_TIMEOUT", "45s")
	t.Setenv("K8S_CLIENT_QPS", "123")
	t.Setenv("K8S_CLIENT_BURST", "456")
	t.Setenv("GRPC_AUTH_SECRET_NAME", "custom-grpc-token")
	t.Setenv("ADMISSION_DISABLE_COLD_START", "true")
	t.Setenv("ADMISSION_QUEUE_TIMEOUT", "2s")
	t.Setenv("ADMISSION_QUEUE_POLL_INTERVAL", "100ms")
	t.Setenv("POOL_AUTOSCALER_ENABLED", "true")
	t.Setenv("POOL_AUTOSCALER_INTERVAL", "15s")
	t.Setenv("POOL_AUTOSCALER_BUFFER", "4")
	t.Setenv("POOL_AUTOSCALER_MIN_REPLICAS", "2")
	t.Setenv("POOL_AUTOSCALER_MAX_REPLICAS", "20")
	t.Setenv("SCHEDULER_NAME", "agent-env-image-locality")
	t.Setenv("IMAGE_LOCALITY_ENABLED", "true")

	cfg := LoadFromEnv()
	if cfg.AuthEnabled {
		t.Fatal("AuthEnabled = true, want false")
	}
	if cfg.GatewayIdleTimeout != 45*time.Second {
		t.Fatalf("GatewayIdleTimeout = %v, want 45s", cfg.GatewayIdleTimeout)
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
	if !cfg.AdmissionDisableColdStart {
		t.Fatal("AdmissionDisableColdStart = false, want true")
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
}

func TestLoadFromEnvForwardAuthSettings(t *testing.T) {
	t.Setenv("AUTH_FORWARD_HEADERS_ENABLED", "true")
	t.Setenv("AUTH_FORWARD_USER_HEADER", "X-Remote-User")
	t.Setenv("AUTH_FORWARD_TRUSTED_PROXIES", "10.0.0.0/8")
	t.Setenv("AUTH_FORWARD_ADMIN_USERS", "alice,bob")

	cfg := LoadFromEnv()
	if !cfg.AuthForwardHeadersEnabled {
		t.Fatal("AuthForwardHeadersEnabled = false, want true")
	}
	if cfg.AuthForwardUserHeader != "X-Remote-User" {
		t.Fatalf("AuthForwardUserHeader = %q, want X-Remote-User", cfg.AuthForwardUserHeader)
	}
	if cfg.AuthForwardTrustedProxies != "10.0.0.0/8" {
		t.Fatalf("AuthForwardTrustedProxies = %q, want 10.0.0.0/8", cfg.AuthForwardTrustedProxies)
	}
	if cfg.AuthForwardAdminUsers != "alice,bob" {
		t.Fatalf("AuthForwardAdminUsers = %q, want alice,bob", cfg.AuthForwardAdminUsers)
	}
}
