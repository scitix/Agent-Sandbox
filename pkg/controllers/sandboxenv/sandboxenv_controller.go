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
// Responsibilities:
//
//  1. Pool materialisation — render each Env.Spec.Clusters[].Members[]
//     entry into a live SandboxPool and propagate drift (labels,
//     annotations, default timeouts, replicas, embedded template).
//     Pool.Spec.Replicas drift is the channel through which the
//     per-Pool autoscaler pushes its scale decisions back onto the
//     live Pool — the autoscaler writes Member.Spec.Replicas on the
//     Env CR, and this reconciler stamps it onto the Pool.
//  2. Status aggregation — mirror idle/running/pending counts and the
//     per-Pool SaturatedUntil from member Pools into
//     Env.Status.Clusters[local].ObservedMembers, plus per-group
//     totals. Cross-member aggregation only; per-Pool autoscaling
//     bookkeeping (LastScale*Time, IdleZeroSince, ...) lives on
//     SandboxPool.Status.AutoScaling, not here.
//
// Every SandboxPool is derived from a SandboxEnv: the Env is the source of
// truth and this Reconciler materialises member Pools from it. There is no
// reverse path that creates an Env from a pre-existing Pool.
//
// Multi-cluster prep:
//   - Spec/status segments are organised by ClusterID; the Reconciler only
//     mutates the segment matching its own LocalClusterID. See ownership.go.
//   - A Hub-driven Sync (not implemented) will populate foreign segments;
//     this Reconciler ignores them.
package sandboxenv

import (
	"context"
	"math/rand/v2"
	"time"

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
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/federation"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
)

const (
	// RequeueAfter is the *base* periodic re-evaluation interval for an
	// Env that otherwise sees no events. jitteredRequeueAfter spreads
	// the actual wake times over a ±20 % window so a fleet of Envs that
	// all reconciled at startup doesn't keep hammering the API server
	// in lockstep.
	RequeueAfter = 10 * time.Second

	// RequeueJitter is the relative jitter applied to RequeueAfter.
	RequeueJitter = 0.20
)

// jitteredRequeueAfter returns RequeueAfter shifted by a uniform random
// fraction in [-RequeueJitter, +RequeueJitter].
func jitteredRequeueAfter() time.Duration {
	delta := float64(RequeueAfter) * RequeueJitter * (2*rand.Float64() - 1)
	return RequeueAfter + time.Duration(delta)
}

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

	// Recorder, when non-nil, emits Kubernetes Events on member-Pool
	// materialisation / drift outcomes. Initialised in SetupWithManager
	// when not pre-set.
	Recorder events.EventRecorder

	// PluginManager, when non-nil, gates Pool materialisation via
	// PreCreatePool / PreUpdatePool admission (scheduler reservation,
	// quota, ...). When nil (test mode) every materialisation is
	// admitted unconditionally.
	PluginManager *plugins.PluginManager

	// EnvRouterSync, when non-nil, is invoked at the end of every successful
	// Reconcile so the in-process Env router observes the freshest spec
	// even if it missed an informer event.
	EnvRouterSync EnvRouterSync

	// Federation, when non-nil, supplies the cross-cluster capacity view so
	// the reconciler can mirror other clusters' members into
	// status.clusters[isLocal=false], making the federated decision input
	// visible via kubectl. nil in single-cluster mode.
	Federation FederationReader

	// ImageRegistry, when non-nil, rewrites container images to this cluster's
	// registry while re-rendering auto-update members.
	//
	// It MUST be the same value the API service's envmember.WithImageRegistry
	// holds. The API freezes a member's Pool spec and this Reconciler
	// re-renders it on every pass; if the two disagree, the revision hash
	// differs every time and idle Pods roll forever.
	ImageRegistry *sandboxrender.RegistryRewrite
}

// FederationReader is the read side of the cross-cluster capacity registry the
// reconciler mirrors into status. Implemented by federation.Registry.
type FederationReader interface {
	// ForeignMembers returns the fresh member records for the Env from every
	// cluster other than the local one.
	ForeignMembers(namespace, env string) []federation.Capacity
}

// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxenvs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxenvs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxenvs/finalizers,verbs=update
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxpools/status,verbs=get
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is the entry point of the controller-runtime reconcile loop.
//
// The request always names a SandboxEnv. (Same-name Pool events also enqueue
// under the Env's name via the secondary Watch; the resolver handles both
// "Env exists" and "Pool changed, refresh Env status" cases.)
//
// A missing Env is "nothing to do here": the Env is created through the
// platform API, and member Pools are materialised from it — never the reverse.
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

	// Keep autoscaling groups in lockstep with the ScalingGroups members
	// reference: create any missing group, garbage-collect any orphan. Runs
	// before Pool materialisation so status aggregation sees the converged
	// group set in the same reconcile pass.
	if err := r.reconcileScalingGroups(ctx, env); err != nil {
		return ctrl.Result{}, err
	}

	// Detect Template / Env-overrides changes and re-render auto-update members'
	// frozen Spec snapshots (revision-hash gated). Runs before reconcilePools so
	// the drift loop propagates the refreshed spec; when it patched the Env we
	// requeue rather than continue against the now-stale in-memory copy.
	if changed, err := r.refreshAutoUpdateMembers(ctx, env); err != nil {
		return ctrl.Result{}, err
	} else if changed {
		return ctrl.Result{Requeue: true}, nil
	}

	// Materialise / reconcile member Pools from spec.Clusters[local].Members.
	// An Env with no declared local members reconciles to zero Pools — the
	// shell exists, but members must be added explicitly through the
	// /envs/{name}/sandboxpools CRUD path so plugin admission can run.
	if err := r.reconcilePools(ctx, env); err != nil {
		return ctrl.Result{}, err
	}

	// Aggregate status from member Pools (writes status.clusters[local]).
	if err := r.syncStatus(ctx, env); err != nil {
		return ctrl.Result{}, err
	}

	res := ctrl.Result{RequeueAfter: jitteredRequeueAfter()}
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
//   - SandboxEnv (primary): generation changes only — status-only updates
//     do not re-enqueue, which prevents a reconcile storm from
//     Status().Patch() re-triggering itself.
//   - SandboxPool (secondary): enqueues a reconcile under the Pool's
//     name so the Reconciler can refresh status aggregates and propagate
//     spec drift to the live Pool when member fields move. The Pool name
//     equals the Env name when the Env owns exactly one same-named Pool.
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

	// Index Envs by their referenced Template name so a SandboxTemplate change
	// fans out to every referencing Env (cluster-scoped Template → all namespaces).
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &agentsv1alpha1.SandboxEnv{}, templateRefNameIndexKey,
		func(obj client.Object) []string {
			env, ok := obj.(*agentsv1alpha1.SandboxEnv)
			if !ok || env.Spec.TemplateRef.Name == "" {
				return nil
			}
			return []string{env.Spec.TemplateRef.Name}
		}); err != nil {
		return err
	}

	enqueueByName := func(obj client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}})
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		Named("sandboxenv").
		For(&agentsv1alpha1.SandboxEnv{}, ctrlbuilder.WithPredicates(predicate.GenerationChangedPredicate{})).
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
		}).
		// SandboxTemplate (cluster-scoped): a spec change enqueues every Env
		// that references it so refreshAutoUpdateMembers can re-render. Gated on
		// GenerationChanged so status-only template writes don't churn.
		Watches(&agentsv1alpha1.SandboxTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.mapTemplateToEnvs),
			ctrlbuilder.WithPredicates(predicate.GenerationChangedPredicate{}))

	return builder.Complete(r)
}
