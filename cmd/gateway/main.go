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

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	"github.com/Lincyaw/agent-env/pkg/audit"
	"github.com/Lincyaw/agent-env/pkg/client"
	"github.com/Lincyaw/agent-env/pkg/config"
	"github.com/Lincyaw/agent-env/pkg/gateway"
	"github.com/Lincyaw/agent-env/pkg/metrics"
	"github.com/Lincyaw/agent-env/pkg/tracing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(arlv1alpha1.AddToScheme(scheme))
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

	// Create PodAllocator with Informer-backed pod cache
	metricsCollector := metrics.NewPrometheusCollector()
	podAllocator, err := gateway.NewPodAllocator(k8sClient, k8sConfig, scheme, metricsCollector)
	if err != nil {
		log.Fatalf("Failed to create PodAllocator: %v", err)
	}

	// Create trajectory writer (optional, with retry for startup ordering)
	var trajectoryWriter *audit.TrajectoryWriter
	if cfg.TrajectoryEnabled {
		trajCfg := audit.TrajectoryConfig{
			Addr:     cfg.ClickHouseAddr,
			Database: cfg.ClickHouseDatabase,
			Username: cfg.ClickHouseUsername,
			Password: cfg.ClickHousePassword,
			Debug:    cfg.TrajectoryDebug,
		}
		const maxRetries = 5
		for i := range maxRetries {
			tw, err := audit.NewTrajectoryWriter(trajCfg)
			if err == nil {
				trajectoryWriter = tw
				log.Println("Trajectory writer enabled")
				break
			}
			if i < maxRetries-1 {
				wait := time.Duration(i+1) * 5 * time.Second
				log.Printf("Warning: Trajectory writer init failed (attempt %d/%d): %v; retrying in %v", i+1, maxRetries, err, wait)
				time.Sleep(wait)
			} else {
				log.Printf("Warning: Trajectory writer init failed after %d attempts: %v (trajectory disabled)", maxRetries, err)
			}
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

	gw := gateway.New(k8sClient, podAllocator, sidecarClient, metricsCollector, trajectoryWriter, &gateway.PoolManagerConfig{
		InitialReplicas: cfg.ManagedPoolInitialReplicas,
		MinReplicas:     cfg.ManagedPoolMinReplicas,
		MaxReplicas:     cfg.ManagedPoolMaxReplicas,
		ScaleUpStep:     cfg.ManagedPoolScaleUpStep,
		IdleCooldown:    cfg.ManagedPoolIdleCooldown,
		EmptyPoolTTL:    cfg.ManagedPoolEmptyTTL,
		SweepInterval:   cfg.ManagedPoolSweepInterval,
	}, gateway.GatewayConfig{
		IdleTimeout:   cfg.GatewayIdleTimeout,
		MaxLifetime:   cfg.GatewayMaxLifetime,
		SweepInterval: cfg.GatewaySweepInterval,
	}, sessionStore)

	// Start PodAllocator cache and event handlers
	allocCtx, allocCancel := context.WithCancel(context.Background())
	if err := podAllocator.Start(allocCtx); err != nil {
		allocCancel()
		log.Fatalf("Failed to start PodAllocator: %v", err)
	}

	// Recover and start pool manager
	if err := gw.StartPoolManager(context.Background()); err != nil {
		log.Printf("Warning: pool manager recovery failed: %v (managed sessions disabled until first request)", err)
	}

	// Start trajectory worker (bounded channel for ClickHouse writes)
	gw.StartTrajectoryWorker()

	// Start session sweep (idle timeout / max lifetime reaper)
	gw.StartSessionSweep()

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
			log.Fatalf("AUTH_ENABLED=true but no valid keys from AUTH_API_KEYS or AUTH_KEY_FILE")
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
		log.Println("WARNING: Authentication is disabled (AUTH_ENABLED=false). All endpoints are publicly accessible.")
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
		WriteTimeout: 600 * time.Second,
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	server.Shutdown(shutdownCtx)
	internalServer.Shutdown(shutdownCtx)
	if stopKeyWatcher != nil {
		stopKeyWatcher()
	}
	healthChecker.Stop()
	gw.StopSessionSweep()
	allocCancel() // Stop PodAllocator cache
	podAllocator.Stop()
	gw.StopPoolManager()
	gw.StopTrajectoryWorker()
	sidecarClient.Close()
	if trajectoryWriter != nil {
		trajectoryWriter.Close()
	}
	if sessionStore != nil {
		sessionStore.Close()
	}

	log.Println("Gateway stopped")
}
