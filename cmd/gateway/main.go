package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/audit"
	"github.com/Lincyaw/agent-env/pkg/client"
	"github.com/Lincyaw/agent-env/pkg/config"
	"github.com/Lincyaw/agent-env/pkg/gateway"
	"github.com/Lincyaw/agent-env/pkg/metrics"
	"github.com/Lincyaw/agent-env/pkg/tracing"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(sandboxv1beta1.AddToScheme(scheme))
	utilruntime.Must(extensionsv1beta1.AddToScheme(scheme))
}

func main() {
	var (
		port     int
		grpcPort int
	)

	flag.IntVar(&port, "port", 8080, "HTTP gateway port")
	flag.IntVar(&grpcPort, "sidecar-grpc-port", 9090, "Sidecar gRPC port")
	flag.Parse()

	cfg := config.LoadFromEnv()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tracingShutdown, err := tracing.Setup(context.Background(), "arl-gateway")
	if err != nil {
		log.Fatalf("failed to initialise tracing: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tracingShutdown(shutdownCtx); err != nil {
			log.Printf("tracing shutdown: %v", err)
		}
	}()

	// Create K8s client
	k8sConfig := ctrl.GetConfigOrDie()
	k8sConfig.QPS = cfg.K8sClientQPS
	k8sConfig.Burst = cfg.K8sClientBurst
	k8sClient, err := ctrlclient.New(k8sConfig, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		log.Fatalf("Failed to create K8s client: %v", err)
	}

	// Create sidecar gRPC client
	sidecarClient := client.NewGRPCSidecarClient(grpcPort, cfg.HTTPClientTimeout, cfg.GRPCAuthToken)

	// Create the sandbox runtime allocator backed by agent-sandbox CRDs.
	metricsCollector := metrics.NewPrometheusCollector()
	runtimeAllocator := gateway.NewSandboxClaimRuntimeAllocator(k8sClient, cfg.GatewayNamespace)
	log.Println("Runtime allocator backend: sandboxclaim")

	// Trajectory writer is connected asynchronously so ClickHouse startup
	// ordering never blocks the gateway health endpoint.
	var trajectoryConfig *audit.TrajectoryConfig
	if cfg.TrajectoryEnabled {
		trajectoryConfig = &audit.TrajectoryConfig{
			Addr:     cfg.ClickHouseAddr,
			Database: cfg.ClickHouseDatabase,
			Username: cfg.ClickHouseUsername,
			Password: cfg.ClickHousePassword,
			Debug:    cfg.TrajectoryDebug,
		}
	}

	// Create session store (Redis or in-memory, with retry for startup ordering)
	var sessionStore gateway.SessionStore
	if cfg.RedisEnabled {
		rsCfg := gateway.RedisStoreConfig{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		}
		const maxRetries = 5
		for i := range maxRetries {
			rs, rsErr := gateway.NewRedisStore(rsCfg)
			if rsErr == nil {
				sessionStore = rs
				log.Printf("Redis session store enabled (addr=%s, db=%d)", cfg.RedisAddr, cfg.RedisDB)
				break
			}
			if i < maxRetries-1 {
				wait := time.Duration(i+1) * 3 * time.Second
				log.Printf("Warning: Redis store init failed (attempt %d/%d): %v; retrying in %v", i+1, maxRetries, rsErr, wait)
				time.Sleep(wait)
			} else {
				log.Fatalf("Failed to create Redis session store after %d attempts: %v", maxRetries, rsErr)
			}
		}
	}

	gw := gateway.New(k8sClient, runtimeAllocator, sidecarClient, metricsCollector, nil, gateway.GatewayConfig{
		IdleTimeout:                     cfg.GatewayIdleTimeout,
		MaxLifetime:                     cfg.GatewayMaxLifetime,
		DevboxIdleTimeout:               cfg.DevboxIdleTimeout,
		DevboxMaxLifetime:               cfg.DevboxMaxLifetime,
		DevboxStorageClassName:          cfg.DevboxStorageClassName,
		SweepInterval:                   cfg.GatewaySweepInterval,
		Namespace:                       cfg.GatewayNamespace,
		SidecarImage:                    cfg.SidecarImage,
		SidecarHTTPPort:                 cfg.SidecarHTTPPort,
		SidecarGRPCPort:                 cfg.SidecarGRPCPort,
		WorkspaceDir:                    cfg.WorkspaceDir,
		ExecutorAgentImage:              cfg.ExecutorAgentImage,
		ImagePullPolicy:                 cfg.ImagePullPolicy,
		GRPCAuthToken:                   cfg.GRPCAuthToken,
		GRPCAuthSecretName:              cfg.GRPCAuthSecretName,
		PodHTTPProxy:                    cfg.PodHTTPProxy,
		PodNoProxy:                      cfg.PodNoProxy,
		AdmissionQueueTimeout:           cfg.AdmissionQueueTimeout,
		AdmissionQueuePollInterval:      cfg.AdmissionQueuePollInterval,
		PoolAutoscalerEnabled:           cfg.PoolAutoscalerEnabled,
		PoolAutoscalerInterval:          cfg.PoolAutoscalerInterval,
		PoolAutoscalerBuffer:            cfg.PoolAutoscalerBuffer,
		PoolAutoscalerMinReplicas:       cfg.PoolAutoscalerMinReplicas,
		PoolAutoscalerMaxReplicas:       cfg.PoolAutoscalerMaxReplicas,
		SchedulerName:                   cfg.SchedulerName,
		ImageLocalityEnabled:            cfg.ImageLocalityEnabled,
		DefaultSandboxRequestCPU:        cfg.DefaultSandboxRequestCPU,
		DefaultSandboxRequestMemory:     cfg.DefaultSandboxRequestMemory,
		DefaultSandboxLimitCPU:          cfg.DefaultSandboxLimitCPU,
		DefaultSandboxLimitMemory:       cfg.DefaultSandboxLimitMemory,
		SandboxNetworkPolicyManagement:  cfg.SandboxNetworkPolicyManagement,
		SandboxRuntimeClassName:         cfg.SandboxRuntimeClassName,
		SandboxSeccompProfileType:       cfg.SandboxSeccompProfileType,
		SandboxSeccompLocalhostProfile:  cfg.SandboxSeccompLocalhostProfile,
		SandboxAllowPrivilegeEscalation: cfg.SandboxAllowPrivilegeEscalation,
		K8sRESTConfig:                   k8sConfig,
	}, sessionStore)

	// Start runtime allocator cache and event handlers.
	allocCtx, allocCancel := context.WithCancel(context.Background())
	if err := runtimeAllocator.Start(allocCtx); err != nil {
		allocCancel()
		log.Fatalf("Failed to start runtime allocator: %v", err)
	}
	if recovered, err := gw.RecoverSessions(ctx); err != nil {
		log.Printf("Warning: session recovery failed: %v", err)
	} else if recovered > 0 {
		log.Printf("Recovered %d active session(s) from durable store", recovered)
	}

	// Start session sweep (idle timeout / max lifetime reaper)
	gw.StartSessionSweep()
	gw.StartPoolAutoscaler()
	if trajectoryConfig != nil {
		startTrajectoryConnector(ctx, gw, *trajectoryConfig)
	}

	// Start health checker
	feishuURL := os.Getenv("FEISHU_WEBHOOK_URL")
	healthChecker := gateway.NewHealthChecker(gw, metricsCollector, feishuURL)
	healthChecker.Start()

	// --- Authentication & rate limiting ---
	var authCfg *gateway.AuthConfig
	var stopKeyWatcher func()
	if cfg.AuthEnabled {
		keys := gateway.ParseAPIKeys(cfg.AuthAPIKeys)

		// Also load keys from file if configured
		keyFile := os.Getenv("AUTH_KEY_FILE")
		if keyFile != "" {
			fileKeys, err := gateway.ParseAPIKeysFile(keyFile)
			if err != nil {
				log.Fatalf("Failed to read key file %s: %v", keyFile, err)
			}
			for k, v := range fileKeys {
				keys[k] = v
			}
		}

		if len(keys) == 0 {
			log.Fatalf("authentication is enabled but no API keys were provided: set AUTH_API_KEYS (key:role,...) or AUTH_KEY_FILE, " +
				"or explicitly opt out of authentication with AUTH_ENABLED=false")
		}
		var origins []string
		if cfg.AllowedOrigins != "" {
			for _, o := range strings.Split(cfg.AllowedOrigins, ",") {
				o = strings.TrimSpace(o)
				if o != "" {
					origins = append(origins, o)
				}
			}
		}
		authCfg = &gateway.AuthConfig{
			Enabled:        true,
			Keys:           keys,
			AllowedOrigins: origins,
			KeyFile:        keyFile,
		}
		log.Printf("Authentication enabled: %d API key(s) registered", len(keys))

		stopKeyWatcher = gateway.StartKeyFileWatcher(authCfg)
	} else {
		log.Println("SECURITY WARNING: authentication is explicitly disabled (AUTH_ENABLED=false). " +
			"Every endpoint — including session creation, file upload, and command execution — is reachable without credentials. " +
			"Only run this way on a trusted, network-isolated host.")
	}

	rateLimiter := gateway.NewRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst)

	// --- Public server (authenticated, rate-limited) ---
	publicMux := http.NewServeMux()
	gateway.SetupRoutes(publicMux, gw, authCfg)

	publicHandler := rateLimiter.Middleware(publicMux)

	server := &http.Server{
		Addr: fmt.Sprintf(":%d", port),
		Handler: otelhttp.NewHandler(publicHandler, "gateway",
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				if route := r.Pattern; route != "" {
					return r.Method + " " + route
				}
				return r.Method + " " + r.URL.Path
			}),
		),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: cfg.GatewayWriteTimeout,
		IdleTimeout:  60 * time.Second,
	}

	// --- Internal server (metrics, debug, alertmanager — no auth) ---
	internalMux := http.NewServeMux()
	gateway.SetupInternalRoutes(internalMux, healthChecker)

	internalServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.InternalPort),
		Handler:      internalMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Gateway listening on :%d (public)", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	go func() {
		log.Printf("Internal server listening on :%d (metrics, debug)", cfg.InternalPort)
		if err := internalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Internal server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down gateway...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Warning: public server shutdown: %v", err)
	}
	if err := internalServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Warning: internal server shutdown: %v", err)
	}
	if stopKeyWatcher != nil {
		stopKeyWatcher()
	}
	healthChecker.Stop()
	gw.StopPoolAutoscaler()
	gw.StopSessionSweep()
	allocCancel() // Stop runtime allocator cache
	runtimeAllocator.Stop()
	gw.StopTrajectoryWorker()
	sidecarClient.Close()
	if sessionStore != nil {
		sessionStore.Close()
	}

	log.Println("Gateway stopped")
}

func startTrajectoryConnector(ctx context.Context, gw *gateway.Gateway, cfg audit.TrajectoryConfig) {
	go func() {
		for attempt := 1; ; attempt++ {
			tw, err := audit.NewTrajectoryWriter(cfg)
			if err == nil {
				gw.SetTrajectoryWriter(tw)
				log.Println("Trajectory writer enabled")
				return
			}

			wait := time.Duration(attempt) * 5 * time.Second
			if wait > 30*time.Second {
				wait = 30 * time.Second
			}
			log.Printf("Warning: trajectory writer init failed (attempt %d): %v; retrying in %v", attempt, err, wait)
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}()
}
