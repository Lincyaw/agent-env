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
}
