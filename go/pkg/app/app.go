/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package app

import (
	"context"
	"crypto/tls"
	"flag"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/kagent-dev/kagent/go/internal/version"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/internal/a2a"
	"github.com/kagent-dev/kagent/go/internal/database"
	versionmetrics "github.com/kagent-dev/kagent/go/internal/metrics"

	a2a_reconciler "github.com/kagent-dev/kagent/go/internal/controller/a2a"
	"github.com/kagent-dev/kagent/go/internal/controller/reconciler"
	reconcilerutils "github.com/kagent-dev/kagent/go/internal/controller/reconciler/utils"
	agent_translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
	mcp_translator "github.com/kagent-dev/kagent/go/internal/controller/translator/mcp"
	"github.com/kagent-dev/kagent/go/internal/httpserver"
	common "github.com/kagent-dev/kagent/go/internal/utils"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/kagent-dev/kagent/go/pkg/auth"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kagent-dev/kagent/go/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/controller"
	"github.com/kagent-dev/kagent/go/internal/goruntime"
	// +kubebuilder:scaffold:imports
)

var (
	scheme          = runtime.NewScheme()
	setupLog        = ctrl.Log.WithName("setup")
	kagentNamespace = common.GetResourceNamespace()

	// These variables should be set during build time using -ldflags
	Version   = version.Version
	GitCommit = version.GitCommit
	BuildDate = version.BuildDate
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
}

type Config struct {
	Metrics struct {
		Addr     string
		CertPath string
		CertName string
		CertKey  string
	}
	Streaming struct {
		MaxBufSize     resource.QuantityValue `default:"1Mi"`
		InitialBufSize resource.QuantityValue `default:"4Ki"`
		Timeout        time.Duration          `default:"60s"`
	}
	LeaderElection     bool
	ProbeAddr          string
	SecureMetrics      bool
	EnableHTTP2        bool
	DefaultModelConfig types.NamespacedName
	HttpServerAddr     string
	WatchNamespaces    string
	A2ABaseUrl         string
	Database           struct {
		Type string
		Path string
		Url  string
	}
}

func (cfg *Config) SetFlags(commandLine *flag.FlagSet) {
	commandLine.StringVar(&cfg.Metrics.Addr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	commandLine.StringVar(&cfg.ProbeAddr, "health-probe-bind-address", ":8082", "The address the probe endpoint binds to.")
	commandLine.BoolVar(&cfg.LeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	commandLine.BoolVar(&cfg.SecureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	commandLine.StringVar(&cfg.Metrics.CertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	commandLine.StringVar(&cfg.Metrics.CertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	commandLine.StringVar(&cfg.Metrics.CertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	commandLine.BoolVar(&cfg.EnableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	commandLine.StringVar(&cfg.DefaultModelConfig.Name, "default-model-config-name", "default-model-config", "The name of the default model config.")
	commandLine.StringVar(&cfg.DefaultModelConfig.Namespace, "default-model-config-namespace", kagentNamespace, "The namespace of the default model config.")
	commandLine.StringVar(&cfg.HttpServerAddr, "http-server-address", ":8083", "The address the HTTP server binds to.")
	commandLine.StringVar(&cfg.A2ABaseUrl, "a2a-base-url", "http://127.0.0.1:8083", "The base URL of the A2A Server endpoint, as advertised to clients.")
	commandLine.StringVar(&cfg.Database.Type, "database-type", "sqlite", "The type of the database to use. Supported values: sqlite, postgres.")
	commandLine.StringVar(&cfg.Database.Path, "sqlite-database-path", "./kagent.db", "The path to the SQLite database file.")
	commandLine.StringVar(&cfg.Database.Url, "postgres-database-url", "postgres://postgres:kagent@db.kagent.svc.cluster.local:5432/crud", "The URL of the PostgreSQL database.")

	commandLine.StringVar(&cfg.WatchNamespaces, "watch-namespaces", "", "The namespaces to watch for .")

	commandLine.Var(&cfg.Streaming.MaxBufSize, "streaming-max-buf-size", "The maximum size of the streaming buffer.")
	commandLine.Var(&cfg.Streaming.InitialBufSize, "streaming-initial-buf-size", "The initial size of the streaming buffer.")
	commandLine.DurationVar(&cfg.Streaming.Timeout, "streaming-timeout", 60*time.Second, "The timeout for the streaming connection.")
}

type BootstrapConfig struct {
	Ctx     context.Context
	Manager manager.Manager
	Router  *mux.Router
}

type CtrlManagerConfigFunc func(manager.Manager) error

type ExtensionConfig struct {
	Authenticator    auth.AuthProvider
	Authorizer       auth.Authorizer
	AgentPlugins     []agent_translator.TranslatorPlugin
	MCPServerPlugins []mcp_translator.TranslatorPlugin
}

type GetExtensionConfig func(bootstrap BootstrapConfig) (*ExtensionConfig, error)

func Start(getExtensionConfig GetExtensionConfig) {
	var tlsOpts []func(*tls.Config)
	var cfg Config

	// TODO setup signal handlers
	ctx := context.Background()

	cfg.SetFlags(flag.CommandLine)
	flag.StringVar(&agent_translator.DefaultImageConfig.Registry, "image-registry", agent_translator.DefaultImageConfig.Registry, "The registry to use for the image.")
	flag.StringVar(&agent_translator.DefaultImageConfig.Tag, "image-tag", agent_translator.DefaultImageConfig.Tag, "The tag to use for the image.")
	flag.StringVar(&agent_translator.DefaultImageConfig.PullPolicy, "image-pull-policy", agent_translator.DefaultImageConfig.PullPolicy, "The pull policy to use for the image.")
	flag.StringVar(&agent_translator.DefaultImageConfig.PullSecret, "image-pull-secret", "", "The pull secret name for the agent image.")
	flag.StringVar(&agent_translator.DefaultImageConfig.Repository, "image-repository", agent_translator.DefaultImageConfig.Repository, "The repository to use for the agent image.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&opts))

	logger.Info("Starting KAgent Controller", "version", Version, "git_commit", GitCommit, "build_date", BuildDate)
	logger.Info("Config", "config", cfg)

	ctrl.SetLogger(logger)

	goruntime.SetMaxProcs(logger)

	setupLog.Info("Starting KAgent Controller", "version", Version, "git_commit", GitCommit, "build_date", BuildDate)

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !cfg.EnableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	ctrlmetrics.Registry.MustRegister(versionmetrics.NewBuildInfoCollector())

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   cfg.Metrics.Addr,
		SecureServing: cfg.SecureMetrics,
		TLSOpts:       tlsOpts,
	}

	if cfg.SecureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(cfg.Metrics.CertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", cfg.Metrics.CertPath, "metrics-cert-name", cfg.Metrics.CertName, "metrics-cert-key", cfg.Metrics.CertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(cfg.Metrics.CertPath, cfg.Metrics.CertName),
			filepath.Join(cfg.Metrics.CertPath, cfg.Metrics.CertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	// filter out invalid namespaces from the watchNamespaces flag (comma separated list)
	watchNamespacesList := filterValidNamespaces(strings.Split(cfg.WatchNamespaces, ","))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		HealthProbeBindAddress: cfg.ProbeAddr,
		LeaderElection:         cfg.LeaderElection,
		LeaderElectionID:       "0e9f6799.kagent.dev",
		Cache: cache.Options{
			DefaultNamespaces: configureNamespaceWatching(watchNamespacesList),
		},
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// Initialize database
	dbManager, err := database.NewManager(&database.Config{
		DatabaseType: database.DatabaseType(cfg.Database.Type),
		SqliteConfig: &database.SqliteConfig{
			DatabasePath: cfg.Database.Path,
		},
		PostgresConfig: &database.PostgresConfig{
			URL: cfg.Database.Url,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to initialize database")
		os.Exit(1)
	}

	// Initialize database tables
	if err := dbManager.Initialize(); err != nil {
		setupLog.Error(err, "unable to initialize database")
		os.Exit(1)
	}

	dbClient := database.NewClient(dbManager)
	router := mux.NewRouter()
	extensionCfg, err := getExtensionConfig(BootstrapConfig{
		Ctx:     ctx,
		Manager: mgr,
		Router:  router,
	})
	if err != nil {
		setupLog.Error(err, "unable to get start config")
		os.Exit(1)
	}

	apiTranslator := agent_translator.NewAdkApiTranslator(
		mgr.GetClient(),
		cfg.DefaultModelConfig,
		extensionCfg.AgentPlugins,
	)

	a2aHandler := a2a.NewA2AHttpMux(httpserver.APIPathA2A, extensionCfg.Authenticator)

	a2aReconciler := a2a_reconciler.NewReconciler(
		a2aHandler,
		cfg.A2ABaseUrl+httpserver.APIPathA2A,
		a2a_reconciler.ClientOptions{
			StreamingMaxBufSize:     int(cfg.Streaming.MaxBufSize.Value()),
			StreamingInitialBufSize: int(cfg.Streaming.InitialBufSize.Value()),
			Timeout:                 cfg.Streaming.Timeout,
		},
		extensionCfg.Authenticator,
	)

	mcpTranslator := mcp_translator.NewTransportAdapterTranslator(
		mgr.GetScheme(),
		extensionCfg.MCPServerPlugins,
	)

	rcnclr := reconciler.NewKagentReconciler(
		apiTranslator,
		mcpTranslator,
		mgr.GetClient(),
		dbClient,
		cfg.DefaultModelConfig,
		a2aReconciler,
	)

	if err = (&controller.MCPServerController{
		Scheme:     mgr.GetScheme(),
		Reconciler: rcnclr,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MCPServer")
		os.Exit(1)
	}

	if err := (&controller.MCPServerToolController{
		Scheme:     mgr.GetScheme(),
		Reconciler: rcnclr,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MCPServerToolDiscovery")
		os.Exit(1)
	}

	if err := (&controller.ServiceController{
		Scheme:     mgr.GetScheme(),
		Reconciler: rcnclr,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Service")
		os.Exit(1)
	}

	if err = (&controller.AgentController{
		Scheme:        mgr.GetScheme(),
		Reconciler:    rcnclr,
		AdkTranslator: apiTranslator,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Agent")
		os.Exit(1)
	}
	if err = (&controller.ModelConfigController{
		Scheme:     mgr.GetScheme(),
		Reconciler: rcnclr,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ModelConfig")
		os.Exit(1)
	}
	if err = (&controller.RemoteMCPServerController{
		Scheme:     mgr.GetScheme(),
		Reconciler: rcnclr,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RemoteMCPServer")
		os.Exit(1)
	}
	if err = (&controller.MemoryController{
		Scheme:     mgr.GetScheme(),
		Reconciler: rcnclr,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Memory")
		os.Exit(1)
	}

	if err = (&controller.AgentTemplateReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AgentTemplate")
		os.Exit(1)
	}

	if err := reconcilerutils.SetupOwnerIndexes(mgr, rcnclr.GetOwnedResourceTypes()); err != nil {
		setupLog.Error(err, "failed to setup indexes for owned resources")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder
	if metricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "unable to add metrics certificate watcher to manager")
			os.Exit(1)
		}
	}

	if webhookCertWatcher != nil {
		setupLog.Info("Adding webhook certificate watcher to manager")
		if err := mgr.Add(webhookCertWatcher); err != nil {
			setupLog.Error(err, "unable to add webhook certificate watcher to manager")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if err := mgr.Add(&adminServer{port: ":6060"}); err != nil {
		setupLog.Error(err, "unable to set up admin server")
		os.Exit(1)
	}

	httpServer, err := httpserver.NewHTTPServer(httpserver.ServerConfig{
		Router:            router,
		BindAddr:          cfg.HttpServerAddr,
		KubeClient:        mgr.GetClient(),
		A2AHandler:        a2aHandler,
		WatchedNamespaces: watchNamespacesList,
		DbClient:          dbClient,
		Authorizer:        extensionCfg.Authorizer,
		Authenticator:     extensionCfg.Authenticator,
	})
	if err != nil {
		setupLog.Error(err, "unable to create HTTP server")
		os.Exit(1)
	}
	if err := mgr.Add(httpServer); err != nil {
		setupLog.Error(err, "unable to set up HTTP server")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// configureNamespaceWatching sets up the controller manager to watch specific namespaces
// based on the provided configuration. It returns the list of namespaces being watched,
// or nil if watching all namespaces.
func configureNamespaceWatching(watchNamespacesList []string) map[string]cache.Config {
	if len(watchNamespacesList) == 0 {
		setupLog.Info("Watching all namespaces (no valid namespaces specified)")
		return map[string]cache.Config{"": {}}

	}
	setupLog.Info("Watching specific namespaces at cache level", "namespaces", watchNamespacesList)

	namespacesMap := make(map[string]cache.Config)
	for _, ns := range watchNamespacesList {
		namespacesMap[ns] = cache.Config{}
	}

	return namespacesMap
}

// filterValidNamespaces removes invalid namespace names from the provided list.
// A valid namespace must be a valid DNS-1123 label.
func filterValidNamespaces(namespaces []string) []string {
	var validNamespaces []string

	for _, ns := range namespaces {
		if strings.TrimSpace(ns) == "" {
			continue
		}

		if errs := validation.IsDNS1123Label(ns); len(errs) > 0 {
			setupLog.Info("Ignoring invalid namespace name",
				"namespace", ns,
				"validation_errors", strings.Join(errs, ", "))
		} else {
			validNamespaces = append(validNamespaces, ns)
		}
	}

	return validNamespaces
}

var _ manager.Runnable = &adminServer{}

type adminServer struct {
	port string
}

func (a *adminServer) Start(ctx context.Context) error {
	setupLog.Info("starting pprof server")
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("/debug/pprof/goroutine", pprof.Handler("goroutine").ServeHTTP)
	mux.HandleFunc("/debug/pprof/heap", pprof.Handler("heap").ServeHTTP)
	mux.HandleFunc("/debug/pprof/block", pprof.Handler("block").ServeHTTP)
	mux.HandleFunc("/debug/pprof/threadcreate", pprof.Handler("threadcreate").ServeHTTP)
	mux.HandleFunc("/debug/pprof/mutex", pprof.Handler("mutex").ServeHTTP)
	mux.HandleFunc("/debug/pprof/allocs", pprof.Handler("allocs").ServeHTTP)
	setupLog.Info("pprof server started", "address", a.port)
	return http.ListenAndServe(a.port, mux)
}
