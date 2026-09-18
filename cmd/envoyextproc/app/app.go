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
	"net/http"
	"os"
	"sync/atomic"
	"time"

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
	var metricsAddr string
	var activityTrackerGCInterval time.Duration
	var activityFlushRateLimit int
	var activityFlushWorkers int
	var localClusterID string
	var clustersConfigMapName string

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
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8083",
		"The address the metrics endpoint binds to. Exposes the activity-flush counters "+
			"that the rate limit (see --activity-flush-rate-limit) has to be sized against. "+
			"Set to \"0\" to disable.")
	flag.DurationVar(&activityTrackerGCInterval, "activity-tracker-gc-interval", 5*time.Minute,
		"Interval at which ActivityTracker GC runs to remove stale sandbox entries.")
	flag.IntVar(&activityFlushRateLimit, "activity-flush-rate-limit", 0,
		"Maximum last-active annotation writes per second. "+
			"Zero (the default) means unlimited: the correct value is a property of how many "+
			"sandboxes this deployment keeps busy, and a limit below the steady-state rate makes the "+
			"deferred writes age past the slack the controller can absorb, which releases sandboxes "+
			"that are still in use. Measure agentbox_gateway_activity_flush_written_total first.")
	flag.IntVar(&activityFlushWorkers, "activity-flush-workers", 4,
		"Concurrent last-active annotation writes.")
	defaultLocalClusterID := os.Getenv("LOCAL_CLUSTER_ID")
	flag.StringVar(&localClusterID, "local-cluster-id", defaultLocalClusterID,
		"Identifier of the local cluster (e.g. cluster-1). Used for cross-cluster sandbox routing. "+
			"When empty, cross-cluster features are disabled.")
	flag.StringVar(&clustersConfigMapName, "clusters-configmap-name", "agentbox-clusters-config",
		"Name of the ConfigMap (in the operator namespace) that contains cross-cluster gateway configuration. "+
			"The ConfigMap should have a 'clusters.yaml' key. Reloaded every 30s.")
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(klog.NewKlogr())

	cfg := ctrl.GetConfigOrDie()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			// Enabled for the activity-flush counters: they are how an operator
			// sizes --activity-flush-rate-limit against real traffic instead of
			// guessing, and how they notice when a limit is too tight.
			BindAddress: metricsAddr,
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

	// The router holds no state of its own: it answers from the Pod informer's
	// sandbox-id index, which the claim populates before the Pod leaves
	// Starting. Nothing has to be pushed here for routing to be correct, which
	// is what lets this process run as many replicas as it likes.
	sandboxRouter := extprocsvc.NewK8sSandboxRouter(mgr.GetClient(), sandboxPort)

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

	// Readiness is deliberately not healthz.Ping.
	//
	// With more than one replica a rolling update adds a *new* pod to the
	// Service while the old one drains, and everything that makes this pod
	// useful — the Pod informer index behind routing, and the tracker seed
	// behind activity — is populated asynchronously after start. A pod that
	// reports ready before the cache is warm accepts requests it cannot route
	// and answers 502, handing the caller a failure that a single-replica
	// deployment could not produce. Staying out of the endpoints until the
	// cache has synced costs a few seconds of rollout time and removes that
	// window.
	warm := &atomic.Bool{}
	if err := mgr.AddReadyzCheck("readyz", func(*http.Request) error {
		if !warm.Load() {
			return errors.New("informer cache not synced yet")
		}
		return nil
	}); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	// The ActivityFlusher is the only writer of the last-active annotation: it
	// publishes what this replica saw so the controller can read the union of
	// every replica's view. There is no RPC back to the control plane any more —
	// a single gRPC connection would pin to one replica and hand the controller
	// a 1/N picture, which is exactly the bug this replaces.
	flusher := extprocsvc.NewActivityFlusher(tracker, mgr.GetClient(), extprocsvc.ActivityFlusherConfig{
		RateLimitPerSecond: activityFlushRateLimit,
		Workers:            activityFlushWorkers,
	})

	errCh := make(chan error, 2)

	// Seed the ActivityTracker from K8s once the manager cache is warm. We run
	// this in a dedicated goroutine because WaitForCacheSync blocks until the
	// Pod informer has populated.
	//
	// Only the tracker needs seeding. Routing is derived from the informer, so
	// it is correct the moment the cache is warm and needs no warm-up of its
	// own; activity is observed traffic, which no amount of reading Kubernetes
	// can reconstruct, so it is restored from the annotations instead.
	go func() {
		if !mgr.GetCache().WaitForCacheSync(ctx) {
			// Only reachable when the context is done (shutdown), in which case
			// the process is going away anyway — leaving `warm` false keeps the
			// pod out of the endpoints, which is the right answer for a replica
			// that cannot see the cluster.
			setupLog.Info("cache sync did not complete; replica stays unready", "ctxErr", ctx.Err())
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

		// Cache is warm and the seed is complete — start background GC, then
		// the flusher. Both need Working index lookups: before the cache syncs
		// every sandbox lookup misses and the flusher would spin for nothing.
		tracker.StartGC(ctx)

		// Only now is this replica able to answer both of the questions it
		// exists to answer, so only now does it belong in the endpoints.
		warm.Store(true)
		setupLog.Info("ready to serve traffic", "cache", "synced", "tracker", "seeded")
		flusher.Run(ctx)
	}()

	go func() {
		setupLog.Info("Starting ExtProc gRPC server", "address", extprocBindAddress)
		errCh <- extprocServer.Start(ctx)
	}()

	go func() {
		setupLog.Info("Starting manager")
		errCh <- mgr.Start(ctx)
	}()

	for range 2 {
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
