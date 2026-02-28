package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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

	// Create K8s client
	k8sConfig := ctrl.GetConfigOrDie()
	k8sConfig.QPS = cfg.K8sClientQPS
	k8sConfig.Burst = cfg.K8sClientBurst
	k8sClient, err := ctrlclient.New(k8sConfig, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		log.Fatalf("Failed to create K8s client: %v", err)
	}

	// Create sidecar gRPC client
	sidecarClient := client.NewGRPCSidecarClient(grpcPort, cfg.HTTPClientTimeout)

	// Create trajectory writer (optional)
	var trajectoryWriter *audit.TrajectoryWriter
	if cfg.TrajectoryEnabled {
		tw, err := audit.NewTrajectoryWriter(audit.TrajectoryConfig{
			Addr:     cfg.ClickHouseAddr,
			Database: cfg.ClickHouseDatabase,
			Username: cfg.ClickHouseUsername,
			Password: cfg.ClickHousePassword,
			Debug:    cfg.TrajectoryDebug,
		})
		if err != nil {
			log.Printf("Warning: Trajectory writer init failed: %v (trajectory disabled)", err)
		} else {
			trajectoryWriter = tw
			log.Println("Trajectory writer enabled")
		}
	}

	gw := gateway.New(k8sClient, sidecarClient, metrics.NewPrometheusCollector(), trajectoryWriter)

	mux := http.NewServeMux()
	gateway.SetupRoutes(mux, gw)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 600 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("Gateway listening on :%d", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down gateway...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server.Shutdown(shutdownCtx)
	sidecarClient.Close()
	if trajectoryWriter != nil {
		trajectoryWriter.Close()
	}

	log.Println("Gateway stopped")
}
