// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package controller contains the core bootstrap logic for the agentbox
// operator (cmd/sandbox). It sets up the controller-runtime manager, wires
// all services and controllers, and starts all server processes.
//
// Callers (cmd/sandbox) provide an Options struct to register
// out-of-tree factories. Extension-specific Args come from the
// --extension-config YAML file, not from CLI flags, keeping proprietary
// parameters out of the open-source binary's flag set.
package controller

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"os"
	"time"

	"github.com/scitix/agent-sandbox/cmd/sandbox/app/extconfig"
	"github.com/scitix/agent-sandbox/pkg/apiserver"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envscheduler"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/federation"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxenv"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxpool"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxpool/autoscalingstate"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxpool/poststarthooks"
	"github.com/scitix/agent-sandbox/pkg/e2bcompat"
	"github.com/scitix/agent-sandbox/pkg/framework"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins/egress"
	plugininstancetype "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
	pluginquota "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
	"github.com/scitix/agent-sandbox/pkg/framework/providerset"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/lastcreate"
	"github.com/scitix/agent-sandbox/pkg/store"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/utils/imageresolver"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
	"github.com/scitix/agent-sandbox/pkg/version"

	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// Run is the single entry point for cmd/sandbox.
// It parses flags, bootstraps the controller-runtime manager, wires all
// services, and blocks until a fatal error or signal.
//
// nolint:gocyclo
func Run(opts Options) {
	scheme := buildScheme(opts.ExtraSchemeBuilders)
	setupLog := ctrl.Log.WithName("setup")

	// ---- flags ----------------------------------------------------------------
	var (
		metricsAddr                                      string
		metricsCertPath, metricsCertName, metricsCertKey string
		webhookCertPath, webhookCertName, webhookCertKey string
		enableLeaderElection                             bool
		probeAddr                                        string
		secureMetrics                                    bool
		enableHTTP2                                      bool
		apiBindAddress                                   string
		adminKey                                         string
		sandboxHistoryTTL                                time.Duration
		extensionConfig                                  string
		envoyGatewayBaseURL                              string
		apikeyNamespace                                  string
		apikeyCacheTTL                                   time.Duration
		idleCheckInterval                                time.Duration
		extprocInternalAPIURL                            string
		e2bBindAddress                                   string
		e2bDomain                                        string
		secret                                           string
		localClusterID                                   string
		clustersConfigMapName                            string
		egressProxyImage                                 string
		tlsOpts                                          []func(*tls.Config)
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8082", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8082 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "", "The directory that contains the metrics server certificate.") //nolint:lll
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers.")
	flag.StringVar(&apiBindAddress, "api-bind-address", ":8080", "The address the REST API server binds to.")
	flag.StringVar(&adminKey, "admin-key", os.Getenv("AGENTBOX_ADMIN_KEY"),
		"Admin key for the REST API server. When empty, authentication is disabled.")
	flag.DurationVar(&sandboxHistoryTTL, "sandbox-history-ttl", 24*time.Hour,
		"TTL for sandbox history records in the in-process store.")
	flag.StringVar(&extensionConfig, "extension-config", "",
		"Path to the extension configuration YAML file. "+
			"Provides out-of-tree plugin/provider Args. Empty = no extensions loaded.")
	flag.StringVar(&envoyGatewayBaseURL, "envoy-gateway-base-url", "",
		"Base URL of the Envoy gateway (e.g. http://gateway.example.com). Used to build sandbox endpoint URLs.")
	flag.StringVar(&apikeyNamespace, "apikey-namespace", "agentbox-system",
		"Kubernetes namespace where API key Secrets are stored.")
	flag.DurationVar(&apikeyCacheTTL, "apikey-cache-ttl", time.Minute,
		"Duration for which API key Validate results are cached in memory.")
	flag.DurationVar(&idleCheckInterval, "idle-check-interval", 2*time.Minute,
		"How often to check for idle sandboxes that have exceeded their idle timeout.")
	flag.StringVar(&extprocInternalAPIURL, "extproc-internal-api-url", "",
		"gRPC dial target of the ExtProc control-plane server "+
			"(e.g. agentbox-extproc.agentbox-system.svc:9003). Required.")
	flag.StringVar(&e2bBindAddress, "e2b-bind-address", "",
		"The address the E2B-compatible API server binds to (e.g. :8090). Empty = disabled.")
	flag.StringVar(&e2bDomain, "e2b-domain", "",
		"Domain name returned in E2B API responses for building sandbox connection URLs.")
	flag.StringVar(&secret, "secret", os.Getenv("AGENTBOX_SECRET"),
		"Shared secret used for IAM JWT verification (HS256) and ws-proxy /v1/ws/sync auth. "+
			"When empty, JWT auth is disabled and cross-cluster sync is not advertised.")
	flag.StringVar(&localClusterID, "local-cluster-id", os.Getenv("LOCAL_CLUSTER_ID"),
		"Identifier of the local cluster. When empty, cross-cluster features are disabled.")
	flag.StringVar(&clustersConfigMapName, "clusters-configmap-name", "agentbox-clusters-config",
		"Name of the ConfigMap containing cross-cluster gateway configuration.")
	flag.StringVar(&egressProxyImage, "egress-proxy-image", os.Getenv("AGENTBOX_EGRESS_PROXY_IMAGE"),
		"Container image carrying the egress-proxy binary, injected as the filter sidecar/init into "+
			"sandbox Pods of Pools with a networkPolicy. Defaults to the idle image. Required when "+
			"egress filtering is used; a Pool with a networkPolicy but no image fails pod creation (fail-closed).")
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(klog.NewKlogr())

	if adminKey == "" {
		setupLog.Info("" +
			"################################################################################\n" +
			"#                          !! SECURITY WARNING !!                              #\n" +
			"#                                                                              #\n" +
			"#  AGENTBOX_ADMIN_KEY is not set. Running in DEV MODE: ALL requests are        #\n" +
			"#  granted anonymous admin access regardless of the API key provided.          #\n" +
			"#                                                                              #\n" +
			"#  DO NOT run in this mode in production. Set --admin-key or the              #\n" +
			"#  AGENTBOX_ADMIN_KEY environment variable to enable authentication.           #\n" +
			"################################################################################")
	}

	if extprocInternalAPIURL == "" {
		setupLog.Error(nil, "--extproc-internal-api-url is required (gRPC dial target, e.g. agentbox-extproc.agentbox-system.svc:9003)") //nolint:lll
		os.Exit(1)
	}

	// ---- extension config ----------------------------------------------------
	// Load after flag.Parse so the path is resolved. extconfig.Load returns an
	// empty config (no extensions) when extensionConfig is "".
	extCfg, err := extconfig.Load(extensionConfig)
	if err != nil {
		setupLog.Error(err, "Failed to load extension config", "path", extensionConfig)
		os.Exit(1)
	}

	// ---- TLS -----------------------------------------------------------------
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("Disabling HTTP/2")
		c.NextProtos = []string{"http/1.1"}
	}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// ---- manager -------------------------------------------------------------
	webhookServerOptions := webhook.Options{TLSOpts: tlsOpts}
	if webhookCertPath != "" {
		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}
	if secureMetrics {
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}
	if metricsCertPath != "" {
		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	restCfg := ctrl.GetConfigOrDie()
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		setupLog.Error(err, "Failed to create Kubernetes clientset")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhook.NewServer(webhookServerOptions),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "4f3fb01f.navix.sh",
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(ctrl.SetupSignalHandler())
	defer cancel()

	if err := indexer.SetupIndexers(ctx, mgr); err != nil {
		setupLog.Error(err, "Failed to set up indexers")
		os.Exit(1)
	}

	sandboxStore, storeErr := store.NewSandboxStore(sandboxHistoryTTL)
	if storeErr != nil {
		setupLog.Error(storeErr, "Failed to create sandbox store")
		os.Exit(1)
	}
	defer func() {
		if err := sandboxStore.Close(); err != nil {
			setupLog.Error(err, "Failed to close sandbox store")
		}
	}()

	// ---- framework handle ----------------------------------------------------
	handle := &framework.DefaultHandle{
		C:      mgr.GetClient(),
		Cch:    mgr.GetCache(),
		Logger: ctrl.Log.WithName("extensions"),
	}

	// ---- quota provider ------------------------------------------------------
	// Register the out-of-tree factory (if any) then build. Args come from the
	// extension config file, not from CLI flags, so this package stays vendor-neutral.
	quotaPluginProvider := buildQuotaProvider(setupLog, opts.QuotaProvider, extCfg, handle)

	// ---- instancetype provider ----------------------------------------------
	// Same shape as the quota provider: out-of-tree factory + extension config
	// args. The provider feeds SandboxEnv adoption (round-trip Pool resources
	// back to (InstanceType, Multiplier)) and, downstream, catalog-driven Pod
	// sizing in PoolTemplateOverrides.
	itProvider := buildInstanceTypeProvider(setupLog, opts.InstanceTypeProvider, extCfg, handle)

	// ---- lifecycle plugins ---------------------------------------------------
	// Register all out-of-tree factories, then build those whose name appears in
	// the extension config plugins[] list. A factory registered here but absent
	// from the config file is silently skipped — explicit config = explicit opt-in.
	providerSet := providerset.Set{
		Quota:        quotaPluginProvider,
		InstanceType: itProvider,
	}.Normalize()

	builtPlugins := buildPlugins(setupLog, opts.OutOfTreePlugins, extCfg, handle, providerSet)

	// The egress filter is an in-tree plugin, always registered. It self-gates
	// on pool.Spec.NetworkPolicy, so Pools without a policy are untouched; a
	// policy-bearing Pool with no configured image fails pod creation (closed).
	builtPlugins = append(builtPlugins, egress.New(egress.Config{Image: egressProxyImage}))

	pluginManager := plugins.NewPluginManager(builtPlugins...)
	if err := pluginManager.Start(ctx, handle); err != nil {
		setupLog.Error(err, "Failed to start plugin manager")
		os.Exit(1)
	}

	// ---- cross-cluster -------------------------------------------------------
	var clusterStore *cluster.Store
	if localClusterID != "" {
		clusterStore = cluster.NewStore()
		if err := clusterStore.WatchConfigMap(ctx, mgr.GetCache(), apikeyNamespace, clustersConfigMapName); err != nil {
			setupLog.Error(err, "Failed to set up cluster config watch")
			os.Exit(1)
		}
	}
	ccForwarder := service.NewCrossClusterForwarder(clusterStore, localClusterID)

	var clusterConfigSink service.ClusterConfigSink
	if secret != "" && localClusterID != "" {
		clusterConfigSink = cluster.NewConfigMapWriter(mgr.GetClient(), apikeyNamespace, clustersConfigMapName)
		setupLog.Info("cluster config sink enabled", "configmap", clustersConfigMapName)
	}

	// Cross-cluster capacity federation shares same-named SandboxEnv runtime
	// capacity between clusters over the ws-proxy sync connection. Enabled only
	// in multi-cluster mode (sync secret + local cluster ID both set).
	var fedRegistry *federation.Registry
	var fedSource federation.CapacitySource
	if secret != "" && localClusterID != "" {
		fedRegistry = federation.NewRegistry(localClusterID, 30*time.Second)
		fedSource = federation.NewCapacitySource(mgr.GetClient(), localClusterID)
		setupLog.Info("cross-cluster capacity federation enabled", "localCluster", localClusterID)
	}

	// ---- services ------------------------------------------------------------
	// ExtProc control-plane client. Shared between sandboxSvc (for route push
	// on Create) and the IdleTimeoutReconciler (for polling last-active).
	extprocClient, err := service.NewExtProcClient(extprocInternalAPIURL, adminKey)
	if err != nil {
		setupLog.Error(err, "Failed to create ExtProc client", "target", extprocInternalAPIURL)
		os.Exit(1)
	}
	defer func() {
		if err := extprocClient.Close(); err != nil {
			setupLog.Error(err, "Failed to close ExtProc client")
		}
	}()

	sandboxSvc := service.NewSandboxService(
		mgr.GetClient(), clientset, restCfg, sandboxStore, envoyGatewayBaseURL, localClusterID, extprocClient, clusterStore,
	)
	// Drain pending claim requests after the HTTP server stops accepting new ones.
	if shutdownable, ok := sandboxSvc.(interface{ Shutdown() }); ok {
		defer shutdownable.Shutdown()
	}
	var idleNotifier sandboxpool.IdleNotifier
	if n, ok := sandboxSvc.(sandboxpool.IdleNotifier); ok {
		idleNotifier = n
	}

	// ---- in-process Sandbox.Create timestamp tracker -------------------------
	// Bumps from sandbox.Create live in memory and flush to the
	// LastSandboxCreateTimeAnnotationKey on Pool every 5 s. The Pool
	// autoscaler reads the in-memory value directly; the persisted
	// annotation is the restart-survival mirror. Manager owns the loop's
	// lifecycle via mgr.Add so it shuts down with the controller.
	lastCreateTracker := lastcreate.NewTracker(mgr.GetClient(), 0)
	if r, ok := sandboxSvc.(interface {
		SetLastCreateTracker(service.LastCreateBumper)
	}); ok {
		r.SetLastCreateTracker(lastCreateTracker)
	}
	if err := mgr.Add(lastCreateTracker); err != nil {
		setupLog.Error(err, "Failed to add LastCreateTracker to manager")
		os.Exit(1)
	}

	// ---- Env-level request router -------------------------------------------
	// envRouter resolves bare template names through SandboxEnv before the
	// request reaches a Pool's scheduler. The Manager itself is in-memory and
	// goroutine-free; the request hot path takes a single RWMutex RLock.
	//
	// We need the sandbox_service to also implement
	// envscheduler.SchedulerLookup so the router can read PoolScheduler
	// snapshots without re-keying. The concrete *k8sSandboxService type
	// satisfies the interface; cast through the interface to expose it.
	var envRouter *envscheduler.Manager
	schedulerLookup, ok := sandboxSvc.(envscheduler.SchedulerLookup)
	if !ok {
		setupLog.Error(nil, "sandbox service does not satisfy envscheduler.SchedulerLookup — Env routing disabled")
	} else {
		envRouter = envscheduler.New(localClusterID, schedulerLookup, &envGetterFromCache{client: mgr.GetClient()})
		if r, ok := sandboxSvc.(interface{ SetEnvRouter(service.EnvRouter) }); ok {
			r.SetEnvRouter(envRouter)
		}
		// Feed the cross-cluster capacity view so the router can forward a
		// create to a same-named Env in another cluster when the local one has
		// no idle capacity. nil in single-cluster mode → local-only routing.
		if fedRegistry != nil {
			envRouter.SetFederationView(fedRegistry)
		}
		// Subscribe to SandboxEnv changes so the router's cache stays fresh.
		// The Reconciler also calls OnEnvUpsert at the end of Reconcile as a
		// belt-and-braces guarantee, but the informer event is faster.
		if err := setupEnvRouterInformer(mgr, envRouter); err != nil {
			setupLog.Error(err, "Failed to wire SandboxEnv informer to env router")
			os.Exit(1)
		}
	}

	// ---- shared auth infrastructure ------------------------------------------
	// keyStore, adminKeyMgr, and iamSvc are shared by both the native and E2B
	// API servers so they validate against the same Secret-backed cache.
	keyStore := apikey.NewSecretKeyStore(apikey.SecretKeyStoreConfig{
		Client:           mgr.GetClient(),
		SecretsNamespace: apikeyNamespace,
		CacheTTL:         apikeyCacheTTL,
	})
	var adminKeyMgr *apikey.AdminKeyManager
	if adminKey != "" {
		adminKeyMgr = apikey.NewAdminKeyManager(adminKey)
	}
	iamSvc := service.NewIAMService(mgr.GetClient())

	digestResolver := imageresolver.NewResolver(mgr.GetClient(), 3*24*time.Hour)
	hooksRunner := poststarthooks.NewRunner(envoyGatewayBaseURL, clientset, restCfg)

	// ---- controllers ---------------------------------------------------------
	// Autoscaler wiring: the Loader reads the K8s cache, the in-process
	// PoolScheduler registry (via schedulerLookup, which is the same
	// interface envscheduler.Manager uses for routing), and the
	// LastCreateTracker we just constructed. nil schedulerLookup is
	// tolerated by the Loader — it simply leaves reactive demand
	// signals unobserved, which degrades to "proactive-only" behaviour
	// instead of crashing.
	// The scale-down session tracker is shared by reference between the
	// Loader (reads a session View into each Snapshot) and the reconciler
	// (applies the resulting transition after Commit). They MUST be the
	// same instance.
	scaleDownTracker := autoscalingstate.NewScaleDownTracker()
	autoscalingLoader := &autoscalingstate.Loader{
		Client:     mgr.GetClient(),
		Schedulers: schedulerLookup,
		LastCreate: lastCreateTracker,
		Prober:     &sandboxpool.PluginProber{PluginManager: pluginManager},
		Clock:      autoscalingstate.SystemClock(),
		ScaleDown:  scaleDownTracker,
	}
	if err := (&sandboxpool.SandboxPoolReconciler{
		Client:                   mgr.GetClient(),
		Scheme:                   mgr.GetScheme(),
		Clientset:                clientset,
		SandboxStore:             sandboxStore,
		PluginManager:            pluginManager,
		IdleNotifier:             idleNotifier,
		DigestResolver:           digestResolver,
		SandboxReadyHook:         hooksRunner,
		SandboxReleaseHook:       hooksRunner,
		AutoscalingLoader:        autoscalingLoader,
		AutoscalingEventRecorder: mgr.GetEventRecorder("sandboxpool-autoscaler"),
		ScaleDown:                scaleDownTracker,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "SandboxPool")
		os.Exit(1)
	}

	envReconciler := &sandboxenv.SandboxEnvReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		LocalClusterID: localClusterID,
		PluginManager:  pluginManager,
	}
	if envRouter != nil {
		envReconciler.EnvRouterSync = envRouter
	}
	// Mirror the cross-cluster capacity view into status.clusters so the
	// federated routing input is visible via kubectl. nil in single-cluster.
	if fedRegistry != nil {
		envReconciler.Federation = fedRegistry
	}
	if err := envReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "SandboxEnv")
		os.Exit(1)
	}

	idleTimeoutReconciler := sandboxpool.NewIdleTimeoutReconciler(
		mgr.GetClient(), sandboxStore, idleCheckInterval, extprocClient,
	)
	if err := mgr.Add(idleTimeoutReconciler); err != nil {
		setupLog.Error(err, "Failed to add IdleTimeoutReconciler")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	// ---- servers -------------------------------------------------------------
	apiServer := apiserver.New(apiserver.Config{
		BindAddress:          apiBindAddress,
		KeyStore:             keyStore,
		AdminKeyManager:      adminKeyMgr,
		IAMService:           iamSvc,
		Secret:               secret,
		ClusterConfigSink:    clusterConfigSink,
		RestConfig:           restCfg,
		Forwarder:            ccForwarder,
		ClusterStore:         clusterStore,
		LocalClusterID:       localClusterID,
		QuotaProvider:        quotaPluginProvider,
		InstanceTypeProvider: itProvider,
		ServerVersion:        version.Version,
		FederationRegistry:   fedRegistry,
		FederationSource:     fedSource,
	}, mgr.GetClient(), clientset, sandboxStore, pluginManager, envoyGatewayBaseURL, sandboxSvc)

	numProcesses := 2
	if e2bBindAddress != "" {
		numProcesses = 3
	}
	errCh := make(chan error, numProcesses)

	go func() { errCh <- apiServer.Start(ctx) }()
	go func() {
		setupLog.Info("Starting manager")
		errCh <- mgr.Start(ctx)
	}()

	if e2bBindAddress != "" {
		e2bServer := e2bcompat.New(e2bcompat.Config{
			BindAddress:   e2bBindAddress,
			Domain:        e2bDomain,
			ServerVersion: version.Version,
		}, mgr.GetClient(), keyStore, adminKeyMgr, iamSvc, sandboxSvc, ccForwarder)
		go func() { errCh <- e2bServer.Start(ctx) }()
	}

	for range numProcesses {
		err := <-errCh
		if err == nil {
			cancel()
			continue
		}
		if errors.Is(err, context.Canceled) {
			continue
		}
		setupLog.Error(err, "Failed to run process")
		cancel()
		os.Exit(1)
	}
}

// buildInstanceTypeProvider registers the out-of-tree InstanceType catalog
// provider factory (if any) and builds the provider. Returns Noop when no
// factory is configured. Intentionally parallel to buildQuotaProvider.
//
//nolint:dupl // parallel to buildQuotaProvider; cannot share via generics
func buildInstanceTypeProvider(
	log interface {
		Info(string, ...any)
		Error(error, string, ...any)
	},
	factory *InstanceTypeProviderFactory,
	extCfg *extconfig.ExtensionConfig,
	handle framework.Handle,
) plugininstancetype.Provider {
	cfg := (*extconfig.ProviderConfig)(nil)
	if extCfg.Providers != nil {
		cfg = extCfg.Providers.InstanceType
	}
	if factory == nil || cfg == nil {
		log.Info("InstanceType provider: noop (no out-of-tree provider configured)")
		return plugininstancetype.NewNoop()
	}
	if factory.Name != cfg.Name {
		log.Info("InstanceType provider: noop (registered factory name does not match config)",
			"registered", factory.Name, "configured", cfg.Name)
		return plugininstancetype.NewNoop()
	}

	plugininstancetype.Register(factory.Name, factory.Factory)

	args, err := factory.DecodeArgs(cfg.Args)
	if err != nil {
		log.Error(err, "Failed to decode instancetype provider args", "name", factory.Name)
		os.Exit(1)
	}

	p, err := plugininstancetype.Build(factory.Name, handle, args)
	if err != nil {
		log.Error(err, "Failed to build instancetype provider", "name", factory.Name)
		os.Exit(1)
	}
	log.Info("InstanceType provider enabled", "name", factory.Name)
	return p
}

// buildQuotaProvider registers the out-of-tree factory (if any) and builds the
// provider using Args decoded from the extension config file.
//
//nolint:dupl // parallel to buildInstanceTypeProvider; cannot share via generics
func buildQuotaProvider(
	log interface {
		Info(string, ...any)
		Error(error, string, ...any)
	},
	factory *QuotaProviderFactory,
	extCfg *extconfig.ExtensionConfig,
	handle framework.Handle,
) pluginquota.Provider {
	cfg := (*extconfig.ProviderConfig)(nil)
	if extCfg.Providers != nil {
		cfg = extCfg.Providers.Quota
	}
	if factory == nil || cfg == nil {
		log.Info("Quota provider: noop (no out-of-tree provider configured)")
		return pluginquota.NewNoop()
	}
	if factory.Name != cfg.Name {
		log.Info("Quota provider: noop (registered factory name does not match config)",
			"registered", factory.Name, "configured", cfg.Name)
		return pluginquota.NewNoop()
	}

	pluginquota.Register(factory.Name, factory.Factory)

	args, err := factory.DecodeArgs(cfg.Args)
	if err != nil {
		log.Error(err, "Failed to decode quota provider args", "name", factory.Name)
		os.Exit(1)
	}

	p, err := pluginquota.Build(factory.Name, handle, args)
	if err != nil {
		log.Error(err, "Failed to build quota provider", "name", factory.Name)
		os.Exit(1)
	}
	log.Info("Quota provider enabled", "name", factory.Name)
	return p
}

// buildPlugins registers all out-of-tree factories and builds those whose name
// appears in the extension config plugins[] section.
func buildPlugins(
	log interface {
		Info(string, ...any)
		Error(error, string, ...any)
	},
	factories []PluginFactory,
	extCfg *extconfig.ExtensionConfig,
	handle framework.Handle,
	providerSet providerset.Set,
) []plugins.Plugin {
	// Register all factories first so the registry is complete before any Build.
	byName := make(map[string]PluginFactory, len(factories))
	for _, f := range factories {
		plugins.Register(f.Name, f.Factory)
		byName[f.Name] = f
	}

	var built []plugins.Plugin
	for _, pcfg := range extCfg.Plugins {
		f, ok := byName[pcfg.Name]
		if !ok {
			log.Error(nil, "Plugin listed in extension config has no registered factory; skipping",
				"name", pcfg.Name)
			continue
		}
		args, err := f.DecodeArgs(pcfg.Args)
		if err != nil {
			log.Error(err, "Failed to decode plugin args", "name", pcfg.Name)
			os.Exit(1)
		}
		p, err := plugins.Build(pcfg.Name, handle, providerSet, args)
		if err != nil {
			log.Error(err, "Failed to build plugin", "name", pcfg.Name)
			os.Exit(1)
		}
		built = append(built, p)
		log.Info("Plugin enabled", "name", pcfg.Name)
	}
	if len(built) == 0 {
		log.Info("No lifecycle plugins registered")
	}
	return built
}

// buildScheme constructs the runtime scheme with base types plus any extra
// builders supplied by the caller.
func buildScheme(extras []func(*runtime.Scheme) error) *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(agentsv1alpha1.AddToScheme(s))
	for _, fn := range extras {
		utilruntime.Must(fn(s))
	}
	return s
}
