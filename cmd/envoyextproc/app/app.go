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

// Package extproc contains the bootstrap logic for the envoyextproc binary
// (cmd/envoyextproc). It parses flags, sets up the controller-runtime manager,
// starts the ExtProc gRPC server, and exposes the internal control-plane gRPC
// API used by the Controller.
//
// Downstream (closed-source) distributions should import this package and
// invoke Run() from their thin main() wrapper so they stay in sync with
// upstream flag additions and wiring changes.
package extproc

import (
	"context"
	"errors"
	"flag"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	extprocsvc "github.com/scitix/agent-sandbox/pkg/envoy/extproc"
	ctrlplanev1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/ctrlplane/v1"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
	// +kubebuilder:scaffold:imports
)

// Run is the single entry point for cmd/envoyextproc. It parses flags,
// bootstraps the controller-runtime manager, starts the ExtProc gRPC data
// plane plus the internal control-plane gRPC server, and blocks until a
// fatal error or signal.
//
// nolint:gocyclo
func Run() {
	setupLog := ctrl.Log.WithName("setup")
	scheme := buildScheme()

	var probeAddr string
	var extprocBindAddress string
	var extprocEnableAuth bool
	var sandboxPort int
	var adminKey string
	var apikeyNamespace string
	var apikeyCacheTTL time.Duration
	var internalAPIBindAddress string
	var activityTrackerGCInterval time.Duration
	var localClusterID string
	var clustersConfigMapName string
	var enableSandboxIndexerFallback bool

	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&extprocBindAddress, "extproc-bind-address", ":9002",
		"The address the ExtProc gRPC server (Envoy ExternalProcessor) binds to.")
	flag.BoolVar(&extprocEnableAuth, "extproc-enable-auth", false,
		"If set, the ExtProc gRPC server will require authentication using the same admin key as the REST API.")
	flag.IntVar(&sandboxPort, "sandbox-port", 0,
		"Default port to use when routing to sandbox pods (0 = use port from URL or pod spec).")
	defaultAdminKey := os.Getenv("AGENTBOX_ADMIN_KEY")
	flag.StringVar(&adminKey, "admin-key", defaultAdminKey,
		"Admin key for authentication. When empty, authentication is disabled.")
	flag.StringVar(&apikeyNamespace, "apikey-namespace", "agentbox-system",
		"Kubernetes namespace where API key Secrets are stored.")
	flag.DurationVar(&apikeyCacheTTL, "apikey-cache-ttl", time.Minute,
		"Duration for which API key Validate results are cached in memory.")
	flag.StringVar(&internalAPIBindAddress, "internal-api-bind-address", ":9003",
		"The address the internal gRPC control-plane server binds to. "+
			"Exposes ControlPlaneService to the Controller for route push and idle-timeout polling.")
	flag.DurationVar(&activityTrackerGCInterval, "activity-tracker-gc-interval", 5*time.Minute,
		"Interval at which ActivityTracker GC runs to remove stale sandbox entries.")
	defaultLocalClusterID := os.Getenv("LOCAL_CLUSTER_ID")
	flag.StringVar(&localClusterID, "local-cluster-id", defaultLocalClusterID,
		"Identifier of the local cluster (e.g. cluster-1). Used for cross-cluster sandbox routing. "+
			"When empty, cross-cluster features are disabled.")
	flag.StringVar(&clustersConfigMapName, "clusters-configmap-name", "agentbox-clusters-config",
		"Name of the ConfigMap (in the operator namespace) that contains cross-cluster gateway configuration. "+
			"The ConfigMap should have a 'clusters.yaml' key. Reloaded every 30s.")
	flag.BoolVar(&enableSandboxIndexerFallback, "extproc-sandbox-indexer-fallback", true,
		"If true, fall back to the Pod informer's sandbox-id index when the in-memory route cache misses. "+
			"Set to false to serve only from the route cache for testing pure-push mode.")
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(klog.NewKlogr())

	cfg := ctrl.GetConfigOrDie()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics; extproc is stateless
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false, // ExtProc is stateless; no leader election needed
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

	// Build AdminKeyManager and KeyStore for extproc authentication.
	var adminKeyMgr *apikey.AdminKeyManager
	if adminKey != "" {
		adminKeyMgr = apikey.NewAdminKeyManager(adminKey)
	}
	keyStore := apikey.NewSecretKeyStore(apikey.SecretKeyStoreConfig{
		Client:           mgr.GetClient(),
		SecretsNamespace: apikeyNamespace,
		CacheTTL:         apikeyCacheTTL,
	})

	// ActivityTracker: in-memory last-active map, seeded from K8s annotations at startup.
	// Background GC removes stale entries for pods that are no longer Running.
	tracker := extprocsvc.NewActivityTrackerWithGC(mgr.GetClient(), activityTrackerGCInterval)

	// RouteCache: Controller pushes sandbox routes here via the internal gRPC
	// API so ExtProc can serve traffic without waiting for its informer cache
	// to catch up to the sandbox-id label write. Entries expire after 1 min;
	// the informer path is the correctness fallback.
	routeCache := extprocsvc.NewRouteCache(1 * time.Minute)

	sandboxRouter := extprocsvc.NewK8sSandboxRouter(mgr.GetClient(), sandboxPort, routeCache, enableSandboxIndexerFallback)

	// Load cluster config for cross-cluster data-plane routing (optional).
	// Uses informer watch to reload automatically when the ConfigMap changes.
	var clusterStore *cluster.Store
	if localClusterID != "" {
		clusterStore = cluster.NewStore()
		if err := clusterStore.WatchConfigMap(ctx, mgr.GetCache(), apikeyNamespace, clustersConfigMapName); err != nil {
			setupLog.Error(err, "Failed to set up cluster config watch")
			os.Exit(1)
		}
	}

	extprocServer := extprocsvc.New(extprocsvc.ServerConfig{
		BindAddr:   extprocBindAddress,
		EnableAuth: extprocEnableAuth,
	}, adminKeyMgr, keyStore, sandboxRouter, tracker, clusterStore, localClusterID)

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	// Internal gRPC server: exposes ControlPlaneService so the Controller can
	// push new sandbox routes (PushRoute) and poll last-active timestamps
	// (GetLastActive). Auth uses the shared admin key via a unary interceptor.
	internalLis, err := net.Listen("tcp", internalAPIBindAddress)
	if err != nil {
		setupLog.Error(err, "Failed to listen on internal API address", "address", internalAPIBindAddress)
		os.Exit(1)
	}
	var internalGRPCOpts []grpc.ServerOption
	if adminKeyMgr != nil {
		internalGRPCOpts = append(internalGRPCOpts, grpc.UnaryInterceptor(extprocsvc.AdminKeyUnaryInterceptor(adminKeyMgr)))
	} else {
		setupLog.Info("admin key empty; internal gRPC server will accept unauthenticated requests (dev mode)")
	}
	internalGRPC := grpc.NewServer(internalGRPCOpts...)
	ctrlplanev1.RegisterControlPlaneServiceServer(internalGRPC, extprocsvc.NewInternalGRPCServer(routeCache, tracker))

	errCh := make(chan error, 3)

	// Seed ActivityTracker + RouteCache from K8s once the manager cache is
	// warm. We run this in a dedicated goroutine because WaitForCacheSync
	// blocks until the Pod informer has populated.
	go func() {
		if !mgr.GetCache().WaitForCacheSync(ctx) {
			setupLog.Info("cache sync timed out, skipping seed")
			return
		}

		// ActivityTracker seed: needs Running pods only (Starting pods have no
		// meaningful last-active yet; Stopping pods are about to be recycled).
		runningPods := &corev1.PodList{}
		if listErr := mgr.GetClient().List(ctx, runningPods,
			client.MatchingFields{indexer.IndexFieldSandboxPhase: agentsv1alpha1.SandboxPhaseRunning},
		); listErr != nil {
			setupLog.Error(listErr, "ActivityTracker: failed to list Running pods for seed")
		} else {
			trackerSeeded := 0
			for i := range runningPods.Items {
				pod := &runningPods.Items[i]
				sandboxID := pod.Labels[agentsv1alpha1.SandboxIDLabelKey]
				if sandboxID == "" {
					continue
				}
				// Take max(last-active, started-at) so that a stale annotation cannot
				// cause the timestamp to appear earlier than the actual start time.
				var lastActive, startedAt time.Time
				if v := pod.Annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey]; v != "" {
					lastActive, _ = time.Parse(time.RFC3339, v)
				}
				if v := pod.Annotations[agentsv1alpha1.SandboxStartedAtAnnotationKey]; v != "" {
					startedAt, _ = time.Parse(time.RFC3339, v)
				}
				ts := lastActive
				if startedAt.After(ts) {
					ts = startedAt
				}
				if ts.IsZero() {
					ts = pod.CreationTimestamp.Time
				}
				tracker.InitFromAnnotations(sandboxID, ts)
				trackerSeeded++
			}
			setupLog.Info("ActivityTracker seeded from K8s", "sandboxes", trackerSeeded)
		}

		// RouteCache seed: needs ALL claimed pods regardless of phase. The
		// router checks phase live on every request, so seeding a Starting
		// pod is correct — it returns 502 until the Pod reaches Running.
		// A separate list scan is cheap because it hits the informer cache.
		allPods := &corev1.PodList{}
		if listErr := mgr.GetClient().List(ctx, allPods); listErr != nil {
			setupLog.Error(listErr, "RouteCache: failed to list pods for seed")
		} else {
			routeSeeded := 0
			for i := range allPods.Items {
				pod := &allPods.Items[i]
				sandboxID := pod.Labels[agentsv1alpha1.SandboxIDLabelKey]
				if sandboxID == "" {
					continue
				}
				routeCache.Put(sandboxID, extprocsvc.RouteEntry{
					Namespace: pod.Namespace,
					PodName:   pod.Name,
				})
				routeSeeded++
			}
			setupLog.Info("RouteCache seeded from K8s", "routes", routeSeeded)
		}

		// Caches are warm and seeds are complete — start background GC.
		tracker.StartGC(ctx)
		routeCache.StartGC(ctx, 30*time.Second)
	}()

	go func() {
		setupLog.Info("Starting internal gRPC server", "address", internalAPIBindAddress)
		if serveErr := internalGRPC.Serve(internalLis); serveErr != nil && !errors.Is(serveErr, grpc.ErrServerStopped) {
			errCh <- serveErr
		} else {
			errCh <- nil
		}
	}()

	go func() {
		<-ctx.Done()
		internalGRPC.GracefulStop()
	}()

	go func() {
		setupLog.Info("Starting ExtProc gRPC server", "address", extprocBindAddress)
		errCh <- extprocServer.Start(ctx)
	}()

	go func() {
		setupLog.Info("Starting manager")
		errCh <- mgr.Start(ctx)
	}()

	for range 3 {
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

// buildScheme returns the scheme registered with the manager. ExtProc only
// needs core Kubernetes types plus the agents.navix.sh CRDs so it can watch
// Pods and (in the cross-cluster case) the ConfigMap.
func buildScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(agentsv1alpha1.AddToScheme(s))
	return s
}
