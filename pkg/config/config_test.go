package config

import (
	"testing"
	"time"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "invalid sidecar HTTP port - too low",
			config: &Config{
				SidecarHTTPPort:     0,
				SidecarGRPCPort:     9090,
				HTTPClientTimeout:   30 * time.Second,
				DefaultRequeueDelay: 10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid sidecar HTTP port - too high",
			config: &Config{
				SidecarHTTPPort:     65536,
				SidecarGRPCPort:     9090,
				HTTPClientTimeout:   30 * time.Second,
				DefaultRequeueDelay: 10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid sidecar gRPC port",
			config: &Config{
				SidecarHTTPPort:     8080,
				SidecarGRPCPort:     -1,
				HTTPClientTimeout:   30 * time.Second,
				DefaultRequeueDelay: 10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "negative default pool replicas",
			config: &Config{
				SidecarHTTPPort:     8080,
				SidecarGRPCPort:     9090,
				DefaultPoolReplicas: -1,
				HTTPClientTimeout:   30 * time.Second,
				DefaultRequeueDelay: 10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid HTTP client timeout",
			config: &Config{
				SidecarHTTPPort:     8080,
				SidecarGRPCPort:     9090,
				HTTPClientTimeout:   0,
				DefaultRequeueDelay: 10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "ClickHouse enabled without address",
			config: &Config{
				SidecarHTTPPort:     8080,
				SidecarGRPCPort:     9090,
				HTTPClientTimeout:   30 * time.Second,
				DefaultRequeueDelay: 10 * time.Second,
				ClickHouseEnabled:   true,
				ClickHouseAddr:      "",
			},
			wantErr: true,
		},
		{
			name: "ClickHouse enabled without database",
			config: &Config{
				SidecarHTTPPort:     8080,
				SidecarGRPCPort:     9090,
				HTTPClientTimeout:   30 * time.Second,
				DefaultRequeueDelay: 10 * time.Second,
				ClickHouseEnabled:   true,
				ClickHouseAddr:      "localhost:9000",
				ClickHouseDatabase:  "",
			},
			wantErr: true,
		},
		{
			name: "ClickHouse invalid batch size",
			config: &Config{
				SidecarHTTPPort:     8080,
				SidecarGRPCPort:     9090,
				HTTPClientTimeout:   30 * time.Second,
				DefaultRequeueDelay: 10 * time.Second,
				ClickHouseEnabled:   true,
				ClickHouseAddr:      "localhost:9000",
				ClickHouseDatabase:  "arl",
				ClickHouseBatchSize: 0,
			},
			wantErr: true,
		},
		{
			name: "valid ClickHouse config",
			config: &Config{
				SidecarHTTPPort:           8080,
				SidecarGRPCPort:           9090,
				HTTPClientTimeout:         30 * time.Second,
				DefaultRequeueDelay:       10 * time.Second,
				ClickHouseEnabled:         true,
				ClickHouseAddr:            "localhost:9000",
				ClickHouseDatabase:        "arl",
				ClickHousePassword:        "secret",
				ClickHouseBatchSize:       100,
				ClickHouseFlushInterval:   10 * time.Second,
				GatewayPort:               8080,
				WarmPoolMaxConcurrent:     20,
				K8sClientQPS:              100,
				K8sClientBurst:            200,
				WarmPoolBaseDelayMs:       500,
				WarmPoolMaxDelayMs:        30000,
				WarmPoolRateLimitQPS:      50,
				WarmPoolRateLimitBurst:    100,
				ImageLocalitySpreadFactor: 0.25,
				ImageLocalityWeight:       80,
				ManagedPoolMaxReplicas:    10,
				ManagedPoolScaleUpStep:    1,
				ManagedPoolSweepInterval:  time.Second,
				ManagedPoolEmptyTTL:       time.Minute,
				GatewaySweepInterval:      time.Second,
				InternalPort:              9091,
				RateLimitRPS:              1,
				RateLimitBurst:            1,
			},
			wantErr: false,
		},
		{
			name: "invalid gateway port - too low",
			config: &Config{
				SidecarHTTPPort:     8080,
				SidecarGRPCPort:     9090,
				HTTPClientTimeout:   30 * time.Second,
				DefaultRequeueDelay: 10 * time.Second,
				GatewayPort:         0,
			},
			wantErr: true,
		},
		{
			name: "invalid gateway port - too high",
			config: &Config{
				SidecarHTTPPort:     8080,
				SidecarGRPCPort:     9090,
				HTTPClientTimeout:   30 * time.Second,
				DefaultRequeueDelay: 10 * time.Second,
				GatewayPort:         70000,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Verify default config is valid
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig() should be valid, got error: %v", err)
	}

	// Verify some key defaults
	if cfg.SidecarHTTPPort != 8080 {
		t.Errorf("Expected SidecarHTTPPort = 8080, got %d", cfg.SidecarHTTPPort)
	}

	if cfg.SidecarGRPCPort != 9090 {
		t.Errorf("Expected SidecarGRPCPort = 9090, got %d", cfg.SidecarGRPCPort)
	}

	if cfg.DefaultPoolReplicas != 0 {
		t.Errorf("Expected DefaultPoolReplicas = 0, got %d", cfg.DefaultPoolReplicas)
	}

	if cfg.EnableMetrics != true {
		t.Error("Expected EnableMetrics = true")
	}
}

func TestLoadFromEnv_ImagePullPolicy(t *testing.T) {
	if got := DefaultConfig().ImagePullPolicy; got != "Always" {
		t.Fatalf("default ImagePullPolicy = %q, want Always", got)
	}
	// Unset env keeps the default.
	t.Setenv("IMAGE_PULL_POLICY", "")
	if got := LoadFromEnv().ImagePullPolicy; got != "Always" {
		t.Fatalf("empty IMAGE_PULL_POLICY = %q, want Always", got)
	}
	// Set env overrides (used on local kind to consume side-loaded images).
	t.Setenv("IMAGE_PULL_POLICY", "IfNotPresent")
	if got := LoadFromEnv().ImagePullPolicy; got != "IfNotPresent" {
		t.Fatalf("IMAGE_PULL_POLICY override = %q, want IfNotPresent", got)
	}
}
