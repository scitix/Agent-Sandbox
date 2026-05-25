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

// Package sandboxenv implements the SandboxEnv Reconciler.
//
// Responsibilities (Phase 1):
//
//  1. Autoscaling — when Autoscaling.Enabled=true, the Reconciler runs
//     scale-up/down decisions against the single member Pool and patches the
//     Pool's spec.replicas accordingly. The Pool Reconciler skips its own
//     legacy autoscaler whenever an Env OwnerReference is present.
//  2. Status aggregation — the Reconciler mirrors idle/running counts from the
//     member Pool into Env.status.clusters[local].observedMembers and
//     publishes time-based fields used by the autoscaler (IdleZeroSince,
//     LastScaleUpTime, LastScaleDownTime).
//
// Adoption (Pool → same-named Env) is handled by an independent transitional
// reconciler in poolmigration/ — see that package for migration semantics.
//
// Multi-cluster prep:
//   - Spec/status segments are organised by ClusterID; the Reconciler only
//     mutates the segment matching its own LocalClusterID. See ownership.go.
//   - Phase 2 will introduce a Hub-driven Sync that populates foreign segments;
//     this Reconciler ignores them.
package sandboxenv

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
)

const (
	// RequeueAfter is the periodic re-evaluation interval for an Env that
	// otherwise sees no events. Matches the SandboxPool controller's cadence.
	RequeueAfter = 10 * time.Second
)

// EnvRouterSync is the minimal contract the SandboxEnv Reconciler uses to
// keep the in-process Env router's cache in sync with K8s state. The
// informer event handler covers the steady-state case; this hook fires at
// the end of every successful Reconcile as a fallback against missed
// events (rare, but cheap to defend against). Implemented by
// *envscheduler.Manager.
type EnvRouterSync interface {
	OnEnvUpsert(env *agentsv1alpha1.SandboxEnv)
}

// SandboxEnvReconciler reconciles a SandboxEnv object.
type SandboxEnvReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// LocalClusterID identifies the cluster this Reconciler runs in. Used to
	// decide which spec.clusters[*] / status.clusters[*] segments may be
	// mutated. Required; the controller refuses to start when empty.
	LocalClusterID string

	// Recorder, when non-nil, emits Kubernetes Events on autoscaler decisions.
	// Initialised in SetupWithManager when not pre-set.
	Recorder events.EventRecorder

	// PluginManager, when non-nil, gates scale-up via PreUpdatePool admission
	// probes (scheduler reservation / quota plugins). The autoscaler binary-
	// searches the probe range when a plugin reports InsufficientResources to
	// converge on a scheduler-acceptable target. When nil (test mode), every
	// candidate replicas value is admitted unconditionally.
	PluginManager *plugins.PluginManager

	// EnvRouterSync, when non-nil, is invoked at the end of every successful
	// Reconcile so the in-process Env router observes the freshest spec
	// even if it missed an informer event.
	EnvRouterSync EnvRouterSync
}

// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxenvs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxenvs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxenvs/finalizers,verbs=update
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxpools/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is the entry point of the controller-runtime reconcile loop.
//
// The request always names a SandboxEnv. (Same-name Pool events also enqueue
// under the Env's name via the secondary Watch; the resolver handles both
// "Env exists" and "Pool changed, refresh Env status" cases.)
//
// Adoption — creating the Env in the first place — is owned by the
// PoolAdoptionReconciler in poolmigration/. This Reconciler treats a missing
// Env as "nothing to do here yet".
func (r *SandboxEnvReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := klog.FromContext(ctx).WithValues("env", req.NamespacedName)

	env := &agentsv1alpha1.SandboxEnv{}
	if err := r.Get(ctx, req.NamespacedName, env); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Sanity guard: Phase 1 must not encounter foreign cluster segments.
	// When it happens (e.g. someone edited the Env in the future Hub mode),
	// emit an informational log; never refuse to reconcile the local segment.
	if hasForeignClusterSegments(env, r.LocalClusterID) {
		log.V(3).Info("Env contains foreign cluster segments; local Reconciler will only touch the local segment")
	}

	// Materialise / reconcile member Pools from spec.Clusters[local].Members.
	// Falls back to a single namesake Pool when no members are declared, so
	// adopter-created Envs continue to converge.
	if err := r.reconcilePools(ctx, env); err != nil {
		return ctrl.Result{}, err
	}

	// Aggregate status from member Pools (writes status.clusters[local]).
	if err := r.syncStatus(ctx, env); err != nil {
		return ctrl.Result{}, err
	}

	// Autoscaling decision.
	res, err := r.syncAutoscaling(ctx, env)
	if err != nil {
		return ctrl.Result{}, err
	}
	if res.RequeueAfter == 0 {
		res.RequeueAfter = RequeueAfter
	}
	if r.EnvRouterSync != nil {
		// Belt-and-braces resync of the in-process router cache. Cheap (one
		// RWMutex.Lock + map write); guarantees the router never lags the
		// authoritative K8s state by more than one reconcile cycle even if
		// the informer event was dropped.
		r.EnvRouterSync.OnEnvUpsert(env)
	}
	return res, nil
}

// SetupWithManager wires the Reconciler into the controller-runtime manager.
//
// Watches:
//   - SandboxEnv (primary): generation + annotation changes (the latter so the
//     EnvScaleUpPendingAnnotationKey written by EnvScheduler wakes us up).
//   - SandboxPool (secondary): enqueues a reconcile under the Pool's name so
//     the Reconciler can refresh status / drive the autoscaler when status
//     fields move. The Pool name is used as the Env name in Phase 1 (1:1
//     adoption guarantees this).
func (r *SandboxEnvReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.LocalClusterID == "" {
		// Open-source single-cluster default. Reuse "local" sentinel so the
		// Reconciler can still write the spec.clusters segment without
		// arbitrary garbage. Operators running multi-cluster MUST set the
		// LOCAL_CLUSTER_ID flag/env.
		r.LocalClusterID = "local"
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorder("sandboxenv-controller")
	}

	enqueueByName := func(obj client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}})
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		Named("sandboxenv").
		For(&agentsv1alpha1.SandboxEnv{}, ctrlbuilder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		))).
		WithOptions(controller.Options{MaxConcurrentReconciles: 4}).
		Watches(&agentsv1alpha1.SandboxPool{}, handler.Funcs{
			CreateFunc: func(_ context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				enqueueByName(e.Object, q)
			},
			UpdateFunc: func(_ context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				enqueueByName(e.ObjectNew, q)
			},
			DeleteFunc: func(_ context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				enqueueByName(e.Object, q)
			},
			GenericFunc: func(_ context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				enqueueByName(e.Object, q)
			},
		})

	return builder.Complete(r)
}

// emitEvent is a thin wrapper that gracefully handles a nil Recorder so tests
// can construct the Reconciler without one.
func (r *SandboxEnvReconciler) emitEvent(env *agentsv1alpha1.SandboxEnv, action, reason, format string, args ...any) {
	if r.Recorder == nil || env == nil {
		return
	}
	r.Recorder.Eventf(env, nil, corev1.EventTypeNormal, reason, action, format, args...)
}
