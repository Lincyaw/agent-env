package main

import (
	"flag"
	"os"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	"github.com/Lincyaw/agent-env/pkg/audit"
	"github.com/Lincyaw/agent-env/pkg/config"
	"github.com/Lincyaw/agent-env/pkg/controller"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/Lincyaw/agent-env/pkg/metrics"
	"github.com/Lincyaw/agent-env/pkg/middleware"
	"github.com/Lincyaw/agent-env/pkg/scheduler"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(arlv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Load configuration
	cfg := config.LoadFromEnv()
	if metricsAddr != "" {
		cfg.MetricsAddr = metricsAddr
	}
	if probeAddr != "" {
		cfg.ProbeAddr = probeAddr
	}
	cfg.EnableLeaderElection = enableLeaderElection

	if err := cfg.Validate(); err != nil {
		setupLog.Error(err, "invalid configuration")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: cfg.MetricsAddr},
		HealthProbeBindAddress: cfg.ProbeAddr,
		LeaderElection:         cfg.EnableLeaderElection,
		LeaderElectionID:       "arl-operator.infra.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Initialize shared dependencies
	var metricsCollector interfaces.MetricsCollector
	if cfg.EnableMetrics {
		metricsCollector = metrics.NewPrometheusCollector()
		setupLog.Info("metrics collection enabled")
	} else {
		metricsCollector = &interfaces.NoOpMetricsCollector{}
		setupLog.Info("metrics collection disabled")
	}

	// Initialize audit writer
	var auditWriter interfaces.AuditWriter
	if cfg.ClickHouseEnabled {
		chWriter, err := audit.NewClickHouseWriter(audit.ClickHouseConfig{
			Addr:          cfg.ClickHouseAddr,
			Database:      cfg.ClickHouseDatabase,
			Username:      cfg.ClickHouseUsername,
			Password:      cfg.ClickHousePassword,
			BatchSize:     cfg.ClickHouseBatchSize,
			FlushInterval: cfg.ClickHouseFlushInterval,
		})
		if err != nil {
			setupLog.Error(err, "failed to initialize ClickHouse audit writer, using no-op")
			auditWriter = audit.NewNoOpWriter()
		} else {
			auditWriter = chWriter
			setupLog.Info("ClickHouse audit writer enabled")
		}
	} else {
		auditWriter = audit.NewNoOpWriter()
		setupLog.Info("audit logging disabled")
	}

	// Setup middleware chains for each controller
	warmPoolMiddleware := middleware.NewChain()
	sandboxMiddleware := middleware.NewChain()

	if cfg.EnableMiddleware {
		warmPoolMiddleware.AddBefore(middleware.NewLoggingHook("WarmPool")).
			AddAfter(middleware.NewLoggingHook("WarmPool"))
		sandboxMiddleware.AddBefore(middleware.NewLoggingHook("Sandbox")).
			AddAfter(middleware.NewLoggingHook("Sandbox"))

		warmPoolMiddleware.AddBefore(middleware.NewMetricsHook("WarmPool", metricsCollector)).
			AddAfter(middleware.NewMetricsHook("WarmPool", metricsCollector))
		sandboxMiddleware.AddBefore(middleware.NewMetricsHook("Sandbox", metricsCollector)).
			AddAfter(middleware.NewMetricsHook("Sandbox", metricsCollector))
	}

	// Initialize image-locality scheduler
	imageScheduler := scheduler.NewImageScheduler(mgr.GetClient())

	// Register controllers (Task and TTL removed — execution via Gateway)
	controllers := []interfaces.ControllerRegistrar{
		&controller.WarmPoolReconciler{
			Client:         mgr.GetClient(),
			Scheme:         mgr.GetScheme(),
			Config:         cfg,
			Metrics:        metricsCollector,
			Middleware:     warmPoolMiddleware,
			ImageScheduler: imageScheduler,
		},
		&controller.SandboxReconciler{
			Client:      mgr.GetClient(),
			Scheme:      mgr.GetScheme(),
			Config:      cfg,
			Metrics:     metricsCollector,
			AuditWriter: auditWriter,
			Middleware:  sandboxMiddleware,
		},
	}

	// Setup all controllers
	for _, c := range controllers {
		if err := c.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", c.Name())
			os.Exit(1)
		}
		setupLog.Info("registered controller", "controller", c.Name())
	}

	// Setup image-locality scheduler node watcher
	if err := imageScheduler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to setup image scheduler")
		os.Exit(1)
	}
	setupLog.Info("registered image-locality scheduler")

	// Add health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
