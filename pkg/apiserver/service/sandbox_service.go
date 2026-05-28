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

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envscheduler"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxpool"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/schedule"
	pkgmetrics "github.com/scitix/agent-sandbox/pkg/metrics"
	"github.com/scitix/agent-sandbox/pkg/store"
	utilresource "github.com/scitix/agent-sandbox/pkg/utils/resource"
)

// ListSandboxesResult wraps the paginated result and total count for List operations.
type ListSandboxesResult struct {
	Items []gen.Sandbox
	Total int
}

// SandboxListFilter is the service-side projection of gen.ListSandboxesParams: the
// (often pointer-typed) query params are dereferenced and combined with the
// auth-derived namespace/team/user.
type SandboxListFilter struct {
	Namespace string
	PoolName  string
	Status    string // comma-separated multi-value supported (e.g. "Running,Failed")
	Team      string // when non-empty, only return sandboxes with this team label
	User      string // when non-empty, only return sandboxes with this user label
	Limit     int    // default 20, max 100; 0 means no limit
	Offset    int
}

// SandboxService defines business operations for sandboxes.
type SandboxService interface {
	Create(ctx context.Context, input CreateSandboxInput) (*gen.Sandbox, *domain.AppError)
	List(ctx context.Context, filter SandboxListFilter) (*ListSandboxesResult, *domain.AppError)
	Get(ctx context.Context, namespace, sandboxID string) (*gen.Sandbox, *domain.AppError)
	Delete(ctx context.Context, namespace, sandboxID string) (*gen.DeleteSandboxResult, *domain.AppError)
	// SetTimeout updates the idle timeout annotation on the sandbox pod.
	// A timeout of 0 removes the annotation (no expiry).
	SetTimeout(ctx context.Context, namespace, sandboxID string, timeout time.Duration) *domain.AppError
	// GetLogs retrieves logs for a sandbox. For active sandboxes it fetches live
	// logs from the K8s API; for runtime sources it reads the log file via exec.
	GetLogs(ctx context.Context, namespace, sandboxID string, params gen.GetSandboxLogsParams) (*gen.SandboxLogsResult, *domain.AppError)
	// CreateExecToken generates a single-use exec token (TTL 30 s) for the given sandbox.
	// The sandbox must be in the Running phase.
	CreateExecToken(ctx context.Context, namespace, sandboxID string) (string, *domain.AppError)
	// ValidateExecToken validates and consumes the token.
	// Returns ExecTokenInfo on success, or a 401 AppError if the token is invalid / expired.
	ValidateExecToken(tokenStr string) (*ExecTokenInfo, *domain.AppError)
	// ExecCommand runs a one-shot command inside the sandbox pod (non-interactive, no TTY).
	// The sandbox must be in Running phase. clientset must be non-nil.
	ExecCommand(ctx context.Context, namespace, sandboxID string, req gen.ExecCommandRequest) (*gen.ExecCommandResult, *domain.AppError)
	// IsReady checks if all runtime readiness probes for a sandbox pass.
	// If a runtime has no ReadinessProbe configured, it is considered ready.
	IsReady(ctx context.Context, namespace, sandboxID string) (*gen.SandboxReadinessResult, *domain.AppError)
}

// EnvRouter is the subset of envscheduler.Manager that sandbox_service uses
// at request-entry time to translate bare template names into a target
// Pool. It is an interface (rather than a concrete *envscheduler.Manager)
// so unit tests can substitute a fake without standing up a real Manager.
//
// May be nil — when nil, sandbox_service skips Env routing entirely and
// treats every template as a direct Pool reference (legacy behaviour, used
// by tests that don't wire the Env layer).
type EnvRouter interface {
	Resolve(ns, raw string) envscheduler.ResolveResult
	SelectPool(envKey types.NamespacedName) string
}

type k8sSandboxService struct {
	client         client.Client
	clientset      kubernetes.Interface // may be nil; used for live log retrieval and exec
	restConfig     *rest.Config         // may be nil; used for exec via SPDY
	store          store.SandboxStore   // may be nil
	gatewayBaseURL string               // may be empty; used to build sandbox endpoint URLs
	localClusterID string               // may be empty; used for cross-cluster sandbox ID prefixing
	execTokens     *execTokenStore
	httpClient     *http.Client
	extprocClient  ExtProcClient // may be nil in tests; used to push new routes to ExtProc
	registryStore  RegistryStore // may be nil; used for per-cluster image registry rewriting

	envRouter EnvRouter // may be nil; set via SetEnvRouter at startup

	// lastCreateTracker accumulates the most recent Sandbox.Create
	// timestamp per Pool in process memory and flushes it to the
	// LastSandboxCreateTimeAnnotationKey on a 5 s cadence. The Pool
	// autoscaler reads the in-memory value directly (the same process
	// runs both the API server and the controller manager); the
	// annotation is the persisted mirror surviving restarts. May be
	// nil in unit tests that don't exercise the autoscaling path.
	lastCreateTracker LastCreateBumper

	schedulersMu sync.RWMutex
	schedulers   map[string]*schedule.PoolScheduler // key: "namespace/name"
}

// LastCreateBumper is the minimal interface k8sSandboxService needs from
// pkg/lifecycle/lastcreate.Tracker. Defined here (rather than imported)
// so the service package stays free of the concrete dependency and the
// tracker can be swapped/disabled in tests.
type LastCreateBumper interface {
	// Bump records the current time as the most-recent Sandbox.Create
	// for the named Pool. Must be safe to call from the request hot
	// path; the production implementation takes a brief mutex on an
	// in-memory map.
	Bump(namespace, name string)
}

// SetLastCreateTracker wires the in-process Sandbox.Create timestamp
// tracker. Called once at startup from cmd/sandbox/app. Nil disables
// the bookkeeping (Bump becomes a no-op via nil-receiver semantics).
func (s *k8sSandboxService) SetLastCreateTracker(t LastCreateBumper) {
	s.lastCreateTracker = t
}

// SetEnvRouter wires the Env-level request router so Sandbox.Create can
// resolve bare template names through SandboxEnv before falling back to
// direct Pool dispatch. Safe to call at most once during startup.
func (s *k8sSandboxService) SetEnvRouter(r EnvRouter) {
	s.envRouter = r
}

// GetScheduler implements envscheduler.SchedulerLookup. Returns the
// existing per-pool scheduler when present; nil otherwise. Used by the
// Env router's Snapshot-based ranking on the request hot path.
func (s *k8sSandboxService) GetScheduler(ns, poolName string) *schedule.PoolScheduler {
	s.schedulersMu.RLock()
	defer s.schedulersMu.RUnlock()
	return s.schedulers[ns+"/"+poolName]
}

// GetSchedulerOrCreate implements envscheduler.SchedulerLookup. Mirrors
// getOrCreateScheduler with public access; team/user labels are forwarded
// for metrics. Exported method on the *service* type for the router to
// share the same scheduler registry without re-keying.
func (s *k8sSandboxService) GetOrCreateScheduler(ns, poolName, team, user, env string) *schedule.PoolScheduler {
	return s.getOrCreateScheduler(ns, poolName, team, user, env)
}

const defaultCreateStartupTimeout = 2 * time.Minute

// NewSandboxService creates a new SandboxService backed by the given K8s client and optional stores.
// clientset is used for live log retrieval and exec; it may be nil (live logs and exec will be unavailable).
// restConfig is the Kubernetes REST config for exec via SPDY; it may be nil (exec will be unavailable).
// gatewayBaseURL is the base URL of the Envoy gateway (e.g. http://gateway.example.com); it may be empty.
// localClusterID identifies the local cluster for cross-cluster sandbox ID prefixing; it may be empty.
// extprocClient pushes new sandbox routes to ExtProc so Create can return without polling; may be nil in tests.
// registryStore supplies per-cluster registry metadata for automatic image host rewriting; may be nil (no rewriting).
func NewSandboxService(c client.Client, cs kubernetes.Interface, restCfg *rest.Config, s store.SandboxStore, gatewayBaseURL string, localClusterID string, extprocClient ExtProcClient, registryStore RegistryStore) SandboxService {
	// Use a never-closing channel; the GC goroutine will be cleaned up by the process exiting.
	done := make(chan struct{})

	return &k8sSandboxService{
		client:         c,
		clientset:      cs,
		restConfig:     restCfg,
		store:          s,
		gatewayBaseURL: gatewayBaseURL,
		localClusterID: localClusterID,
		execTokens:     newExecTokenStore(done),
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		extprocClient:  extprocClient,
		registryStore:  registryStore,
		schedulers:     make(map[string]*schedule.PoolScheduler),
	}
}

// getOrCreateScheduler returns the per-pool scheduler for the given pool,
// creating and starting one on first use. The goroutine is permanent and is
// only stopped by Shutdown().
func (s *k8sSandboxService) getOrCreateScheduler(ns, name, team, user, env string) *schedule.PoolScheduler {
	key := ns + "/" + name

	// Fast path: read lock only.
	s.schedulersMu.RLock()
	sched := s.schedulers[key]
	s.schedulersMu.RUnlock()
	if sched != nil {
		return sched
	}

	// Slow path: create and register (double-check under write lock).
	s.schedulersMu.Lock()
	defer s.schedulersMu.Unlock()
	if sched = s.schedulers[key]; sched != nil {
		return sched
	}
	sched = schedule.NewPoolScheduler(
		ns,
		name,
		team,
		user,
		env,
		s.client,
	)
	s.schedulers[key] = sched
	go sched.Run(context.Background())
	return sched
}

// Shutdown stops all per-pool scheduler goroutines, draining their pending
// requests with ErrNoIdlePodsAvailable.  It should be called after the HTTP
// server has stopped accepting new requests so that no new claims can arrive.
func (s *k8sSandboxService) Shutdown() {
	s.schedulersMu.Lock()
	scheds := make([]*schedule.PoolScheduler, 0, len(s.schedulers))
	for _, sc := range s.schedulers {
		scheds = append(scheds, sc)
	}
	s.schedulersMu.Unlock()
	for _, sc := range scheds {
		sc.Shutdown()
	}
}

func resolveCreateStartupTimeout(pool *agentsv1alpha1.SandboxPool, requested time.Duration) time.Duration {
	// defaultTimeout is the fallback when the request did not specify a value.
	// Priority: pool.Spec.DefaultStartupTimeout > hardcoded defaultCreateStartupTimeout.
	defaultTimeout := defaultCreateStartupTimeout
	if pool != nil && pool.Spec.DefaultStartupTimeout != nil && pool.Spec.DefaultStartupTimeout.Duration > 0 {
		defaultTimeout = pool.Spec.DefaultStartupTimeout.Duration
	}

	if requested > 0 {
		return requested
	}
	return defaultTimeout
}

// resolveCreateIdleTimeout resolves the idle timeout for a new sandbox.
// Priority: request-level value > pool DefaultIdleTimeout > 0 (no timeout).
func resolveCreateIdleTimeout(pool *agentsv1alpha1.SandboxPool, requested time.Duration) time.Duration {
	if requested > 0 {
		return requested
	}
	if pool != nil && pool.Spec.DefaultIdleTimeout != nil && pool.Spec.DefaultIdleTimeout.Duration > 0 {
		return pool.Spec.DefaultIdleTimeout.Duration
	}
	return 0
}

// buildAvailablePoolsDetail lists pools visible to the current caller (scoped
// by namespace + team/user labels, matching the default List semantics) and
// returns a compact summary to attach to 404 errors so the caller can retry
// against an existing pool without a second round-trip.
//
// The helper is best-effort: any List error is swallowed (logged) and a nil
// detail is returned, so the 404 path never gets worse due to enrichment.
func (s *k8sSandboxService) buildAvailablePoolsDetail(ctx context.Context, namespace, team, user string) *domain.AvailablePoolsDetail {
	listOpts := []client.ListOption{client.InNamespace(namespace)}
	if team != "" && user != "" {
		listOpts = append(listOpts, client.MatchingLabels{
			agentsv1alpha1.LabelTeam: team,
			agentsv1alpha1.LabelUser: user,
		})
	}
	poolList := &agentsv1alpha1.SandboxPoolList{}
	if err := s.client.List(ctx, poolList, listOpts...); err != nil {
		log.FromContext(ctx).V(1).Info("buildAvailablePoolsDetail: list failed", "err", err)
		return nil
	}
	summaries := make([]domain.AvailablePoolSummary, 0, len(poolList.Items))
	for i := range poolList.Items {
		p := &poolList.Items[i]
		summaries = append(summaries, domain.AvailablePoolSummary{
			Name:      p.Name,
			Namespace: p.Namespace,
			Idle:      p.Status.IdleReplicas,
			Running:   p.Status.RunningReplicas,
			Starting:  p.Status.StartingReplicas,
		})
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Name < summaries[j].Name })
	hint := "Pool not found. Pick a name from availablePools, or call POST /v1/sandboxpools first."
	if len(summaries) == 0 {
		hint = "No pools available for this namespace/team/user. Create one with POST /v1/sandboxpools before retrying."
	}
	return &domain.AvailablePoolsDetail{
		AvailablePools: summaries,
		Hint:           hint,
	}
}

func (s *k8sSandboxService) Create(ctx context.Context, input CreateSandboxInput) (*gen.Sandbox, *domain.AppError) { //nolint:gocyclo // claim path branches by metric outcome; restructuring would obscure ordering
	// Resolve the request's `template` field (carried as input.PoolName for
	// historical reasons) against the Env router. Three outcomes that we
	// care about here:
	//
	//   - ResolveEnv     — bare name matched a SandboxEnv. Pick one of its
	//                       member Pools via SelectPool and rewrite
	//                       input.PoolName so the rest of Create sees a
	//                       concrete pool name. The actual Pool enqueue
	//                       happens further down on the routine fast path.
	//   - ResolveLocalPool — explicit "<localID>::pool" bypasses Env. Use
	//                       the parsed pool name verbatim.
	//   - ResolveNotFound — bare name with no Env. Phase 1 adoption ensures
	//                       every legacy Pool has a same-named Env, so this
	//                       branch is a true 404. ResolveCrossCluster is
	//                       handled in the apiserver handler layer (forwarder).
	//
	// envRouter may be nil (legacy unit-test wiring) — in that case we skip
	// resolution and treat input.PoolName as a direct Pool reference, which
	// is exactly the pre-Env behaviour.
	if s.envRouter != nil {
		res := s.envRouter.Resolve(input.Namespace, input.PoolName)
		switch res.Kind {
		case envscheduler.ResolveEnv:
			selected := s.envRouter.SelectPool(res.EnvKey)
			if selected == "" {
				return nil, domain.NewServiceUnavailable(
					fmt.Sprintf("sandbox env %s/%s has no eligible members", input.Namespace, res.EnvKey.Name))
			}
			input.PoolName = selected
		case envscheduler.ResolveLocalPool:
			input.PoolName = res.PoolName
		case envscheduler.ResolveCrossCluster:
			// The apiserver handler should have forwarded this request before
			// reaching the service. Reaching here indicates a wiring bug.
			return nil, domain.NewBadRequest(
				fmt.Sprintf("cross-cluster pool reference %q must be forwarded by the handler layer", res.ClusterID+"::"+res.PoolName))
		case envscheduler.ResolveNotFound:
			return nil, domain.NewNotFound(
				fmt.Sprintf("sandbox env or pool %q not found in namespace %s", res.PoolName, input.Namespace))
		}
	}

	// envName is populated once the Pool is fetched; the pool-fetch error path
	// observes the metric with sandbox_env="" since no Pool object is available.
	var envName string
	mkCreateLabels := func(result string) prometheus.Labels {
		return prometheus.Labels{
			"namespace":   input.Namespace,
			"pool":        input.PoolName,
			"team":        input.Labels[agentsv1alpha1.LabelTeam],
			"user":        input.Labels[agentsv1alpha1.LabelUser],
			"sandbox_env": envName,
			"result":      result,
		}
	}

	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: input.Namespace, Name: input.PoolName}, pool); err != nil {
		pkgmetrics.SandboxCreateTotal.With(mkCreateLabels("error")).Inc()
		appErr := domain.NewNotFound(fmt.Sprintf("sandbox pool %s/%s not found", input.Namespace, input.PoolName))
		appErr.Detail = s.buildAvailablePoolsDetail(ctx, input.Namespace, input.Labels[agentsv1alpha1.LabelTeam], input.Labels[agentsv1alpha1.LabelUser])
		return nil, appErr
	}
	envName = pool.Labels[agentsv1alpha1.LabelEnv]

	// Record this Create on the in-process LastCreateTracker so the Pool
	// autoscaler's quiet-window gate sees fresh activity. Bump is O(1)
	// in-memory; the periodic flush to the Pool annotation happens in
	// a background goroutine owned by the controller manager. Skipped
	// when the tracker is unwired (unit tests) — interface nil-check
	// because a nil interface value would panic on method dispatch.
	if s.lastCreateTracker != nil {
		s.lastCreateTracker.Bump(pool.Namespace, pool.Name)
	}

	containerImages, err := s.resolveContainerImages(pool, input)
	if err != nil {
		pkgmetrics.SandboxCreateTotal.With(mkCreateLabels("error")).Inc()
		return nil, domain.NewBadRequest(err.Error())
	}

	startupTimeout := resolveCreateStartupTimeout(pool, input.StartupTimeout)

	sandboxUUID, err := uuid.NewV7()
	if err != nil {
		pkgmetrics.SandboxCreateTotal.With(mkCreateLabels("error")).Inc()
		return nil, domain.NewInternal(fmt.Sprintf("failed to generate sandbox UUID: %v", err), err)
	}
	sandboxID := sandboxUUID.String()

	labels := maps.Clone(input.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	labels[agentsv1alpha1.SandboxIDLabelKey] = sandboxID
	labels[agentsv1alpha1.ManagedByLabelKey] = agentsv1alpha1.ManagedBySandboxAPIServer

	annotations := maps.Clone(input.Annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	claimedAt := time.Now().UTC()
	annotations[agentsv1alpha1.SandboxIDAnnotationKey] = sandboxID
	annotations[agentsv1alpha1.SandboxClaimedAtAnnotationKey] = claimedAt.Format(time.RFC3339)
	// idle-timeout: request value takes priority, falls back to pool DefaultIdleTimeout.
	if resolvedIdle := resolveCreateIdleTimeout(pool, input.IdleTimeout); resolvedIdle > 0 {
		annotations[agentsv1alpha1.SandboxIdleTimeoutAnnotationKey] = strconv.FormatInt(int64(resolvedIdle.Seconds()), 10)
	}
	// startup-timeout: persist the resolved value so cleanupTimedOutStartingPods can read it
	// even when the pool is updated or the controller restarts after the pod is claimed.
	if startupTimeout > 0 {
		annotations[agentsv1alpha1.SandboxStartupTimeoutAnnotationKey] = strconv.FormatInt(int64(startupTimeout.Seconds()), 10)
	}
	if len(input.Metadata) > 0 {
		encodedMetadata, encErr := json.Marshal(input.Metadata)
		if encErr != nil {
			pkgmetrics.SandboxCreateTotal.With(mkCreateLabels("error")).Inc()
			return nil, domain.NewBadRequest(fmt.Sprintf("failed to encode metadata: %v", encErr))
		}
		annotations[agentsv1alpha1.SandboxMetadataAnnotationKey] = string(encodedMetadata)
	}
	if len(input.PostStartHooks) > 0 {
		if encodedHooks, encErr := json.Marshal(input.PostStartHooks); encErr == nil {
			annotations[agentsv1alpha1.SandboxPostStartHooksAnnotationKey] = string(encodedHooks)
		}
	}

	// Compute managed label keys from the caller-supplied labels, excluding
	// system labels that must survive ReleaseSandboxPod so that the controller
	// can write them into the history store record.
	systemLabelKeys := sets.New(agentsv1alpha1.LabelTeam, agentsv1alpha1.LabelUser)
	userLabelKeys := make([]string, 0, len(input.Labels))
	for k := range input.Labels {
		if !systemLabelKeys.Has(k) {
			userLabelKeys = append(userLabelKeys, k)
		}
	}
	sort.Strings(userLabelKeys)
	managedLabelKeys, encErr := json.Marshal(userLabelKeys)
	if encErr != nil {
		pkgmetrics.SandboxCreateTotal.With(mkCreateLabels("error")).Inc()
		return nil, domain.NewBadRequest(fmt.Sprintf("failed to encode managed labels: %v", encErr))
	}
	managedAnnotationKeys, encErr := json.Marshal(sortedKeys(input.Annotations))
	if encErr != nil {
		pkgmetrics.SandboxCreateTotal.With(mkCreateLabels("error")).Inc()
		return nil, domain.NewBadRequest(fmt.Sprintf("failed to encode managed annotations: %v", encErr))
	}
	annotations[agentsv1alpha1.SandboxManagedLabelKeysAnnotationKey] = string(managedLabelKeys)
	annotations[agentsv1alpha1.SandboxManagedAnnotationKeysAnnotationKey] = string(managedAnnotationKeys)

	// Submit the claim to the per-pool scheduler.  The scheduler serialises
	// concurrent claim attempts so that the API server is not flooded with
	// redundant List+Patch calls when many Create requests arrive simultaneously.
	claimStart := time.Now()
	claimOutcome := "error" // default; overwritten on success / specific failures
	defer func() {
		pkgmetrics.SandboxClaimDuration.With(prometheus.Labels{
			"namespace":   input.Namespace,
			"pool":        input.PoolName,
			"team":        input.Labels[agentsv1alpha1.LabelTeam],
			"user":        input.Labels[agentsv1alpha1.LabelUser],
			"sandbox_env": envName,
			"outcome":     claimOutcome,
		}).Observe(time.Since(claimStart).Seconds())
	}()
	resultCh := make(chan schedule.ClaimResult, 1)
	reqCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	req := &schedule.ClaimRequest{
		Ctx: reqCtx,
		Opts: schedule.ClaimOptions{
			ContainerImages: containerImages,
			Labels:          labels,
			Annotations:     annotations,
			TargetPodPhase:  agentsv1alpha1.SandboxPhaseRunning,
		},
		Deadline: time.Now().Add(startupTimeout),
		ResultCh: resultCh,
	}
	sched := s.getOrCreateScheduler(
		input.Namespace,
		input.PoolName,
		input.Labels[agentsv1alpha1.LabelTeam],
		input.Labels[agentsv1alpha1.LabelUser],
		envName,
	)
	if !sched.Enqueue(req) {
		pkgmetrics.SandboxCreateTotal.With(mkCreateLabels("no_idle")).Inc()
		return nil, domain.NewTooManyRequests("scheduler queue is full, try again later", nil, nil)
	}

	var pod *corev1.Pod
	select {
	case res := <-resultCh:
		if res.Err != nil {
			if errors.Is(res.Err, inplaceupdate.ErrNoIdlePodsAvailable) {
				claimOutcome = "no_idle"
				pkgmetrics.SandboxCreateTotal.With(mkCreateLabels("no_idle")).Inc()
				appErr := domain.NewTooManyRequests("no idle sandboxes available in the pool", res.Err, nil)
				appErr.Detail = buildPoolStatusDetail(ctx, s.client, pool)
				return nil, appErr
			}
			// claimOutcome stays "error" (the default)
			pkgmetrics.SandboxCreateTotal.With(mkCreateLabels("error")).Inc()
			return nil, domain.NewInternal(res.Err.Error(), res.Err)
		}
		claimOutcome = "success"
		pod = res.Pod

	case <-ctx.Done():
		// The HTTP client disconnected before a pod was claimed.
		//
		// Race: doCAS may be in-flight (between isExpired() check and writing to
		// ResultCh). A simple non-blocking drain misses the pod if doCAS hasn't
		// written yet, leaving the pod stranded in Starting until startup-timeout
		// fires. We therefore wait briefly for a late dispatch before giving up.
		//
		// The wait window is the sum of a single apiserver round-trip (typically
		// 10–50 ms) plus the inflightCAS semaphore wait. In the absolute worst
		// case (maxInflightCAS all occupied, each ~100 ms) that is several seconds,
		// but we cap at startupTimeout to avoid holding the handler goroutine
		// longer than necessary. In practice the window is almost always
		// sub-second.
		releaseIfClaimed := func(res schedule.ClaimResult) {
			if res.Pod == nil {
				return
			}
			releaseCtx := context.Background()
			if _, releaseErr := sandboxpool.ReleaseSandboxPod(releaseCtx, s.client, res.Pod, pool, sandboxpool.ReleaseSandboxPodOptions{
				StopReason:   agentsv1alpha1.SandboxStopReasonCanceled,
				TerminatedAt: time.Now().UTC().Format(time.RFC3339),
			}); releaseErr != nil {
				klog.ErrorS(releaseErr, "Create: failed to release claimed pod after context cancellation",
					"namespace", input.Namespace, "pool", input.PoolName)
			} else {
				klog.InfoS("Create: released claimed pod due to context cancellation",
					"namespace", input.Namespace, "pool", input.PoolName)
			}
		}

		// Use a short drain window: the time a single CAS round-trip takes plus
		// some slack. We don't need the full startupTimeout here — if nothing
		// arrives within a few seconds the doCAS goroutine will also see
		// req.Ctx.Err() != nil and requeue the request back to the pool.
		drainTimeout := 3 * time.Second
		drainTimer := time.NewTimer(drainTimeout)
		defer drainTimer.Stop()
		select {
		case res := <-resultCh:
			releaseIfClaimed(res)
		case <-drainTimer.C:
			// doCAS will detect req.isExpired() and requeue the pod on its next
			// iteration — nothing leaked (pod stays reserved until TTL sweep).
		}

		claimOutcome = "timeout"
		pkgmetrics.SandboxCreateTotal.With(mkCreateLabels("error")).Inc()
		return nil, domain.NewInternal("request canceled by client", ctx.Err())
	}

	pkgmetrics.SandboxCreateTotal.With(mkCreateLabels("success")).Inc()

	result := sandboxFromPod(pod)
	// When the caller explicitly targeted a cluster (cross-cluster request),
	// prefix the returned sandbox ID so that subsequent data-plane requests
	// carry the cluster info for routing. Same-cluster requests return a plain UUID.
	if input.ClusterID != "" {
		result.SandboxId = s.prefixSandboxID(result.SandboxId)
	}
	if eps := buildEndpoints(pool, result.SandboxId, s.gatewayBaseURL); len(eps) > 0 {
		result.Endpoints = &eps
	}
	if result.SandboxId == "" {
		// Should never happen since we set the label before claiming, but guard against it just in case.
		klog.ErrorS(nil, "claimed pod is missing sandbox ID label", "namespace", pod.Namespace, "name", pod.Name)
		return nil, domain.NewInternal("claimed pod is missing sandbox ID label", nil)
	}

	// Push the new route to ExtProc so it can serve traffic immediately
	// without waiting for its informer cache to observe the sandbox-id label.
	// On push failure, fall back to a single 500 ms probe — the informer
	// almost always catches up within that window. On happy path, no probe.
	pushErr := s.pushRouteToExtProc(ctx, result.SandboxId, pod)
	if pushErr != nil {
		klog.InfoS("Create: ExtProc push failed, running fallback endpoint probe",
			"sandboxID", result.SandboxId, "error", pushErr)
		s.probeEndpointReady(ctx, pool, result.Endpoints)
	}

	// If the client disconnected while we were pushing/probing the route, release
	// the sandbox so the pod returns to Idle instead of being stranded until idle-timeout.
	if ctx.Err() != nil {
		releaseCtx := context.Background()
		if _, releaseErr := sandboxpool.ReleaseSandboxPod(releaseCtx, s.client, pod, pool, sandboxpool.ReleaseSandboxPodOptions{
			StopReason:   agentsv1alpha1.SandboxStopReasonCanceled,
			TerminatedAt: time.Now().UTC().Format(time.RFC3339),
		}); releaseErr != nil {
			klog.ErrorS(releaseErr, "Create: failed to release sandbox after context cancellation",
				"namespace", input.Namespace, "pool", input.PoolName, "sandboxID", sandboxID)
		} else {
			klog.InfoS("Create: released sandbox due to context cancellation during route propagation",
				"namespace", input.Namespace, "pool", input.PoolName, "sandboxID", sandboxID)
		}
		pkgmetrics.SandboxCreateTotal.With(mkCreateLabels("error")).Inc()
		return nil, domain.NewInternal("request canceled by client", ctx.Err())
	}

	// Populate CPU/Memory from pool spec
	cpu, memory := computePoolResources(ctx, pool)
	result.Cpu = ptr.To(cpu)
	result.Memory = ptr.To(memory)
	result.DurationSeconds = computeSandboxDuration(&result, time.Now())
	return &result, nil
}

func (s *k8sSandboxService) List(ctx context.Context, filter SandboxListFilter) (*ListSandboxesResult, *domain.AppError) {
	// Step 1: Fetch active pods from K8s, filtered by team/user if provided
	pods, err := sandboxpool.ListClaimedPodsWithFilter(ctx, s.client, filter.Namespace, filter.Team, filter.User)
	if err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}

	// Build a pool map for endpoint construction and CPU/Memory (batch-load all pools in namespace)
	poolMap := make(map[string]*agentsv1alpha1.SandboxPool)
	if len(pods) > 0 || s.gatewayBaseURL != "" {
		poolList := &agentsv1alpha1.SandboxPoolList{}
		if listErr := s.client.List(ctx, poolList, client.InNamespace(filter.Namespace)); listErr == nil {
			for i := range poolList.Items {
				poolMap[poolList.Items[i].Name] = &poolList.Items[i]
			}
		}
	}

	// Build a dedup map keyed by sandboxID; K8s records take precedence
	byID := make(map[string]gen.Sandbox, len(pods))
	for i := range pods {
		sb := sandboxFromPod(&pods[i])
		// Build endpoints for active sandboxes
		if pool, ok := poolMap[sb.PoolName]; ok {
			if s.gatewayBaseURL != "" {
				if eps := buildEndpoints(pool, sb.SandboxId, s.gatewayBaseURL); len(eps) > 0 {
					sb.Endpoints = &eps
				}
			}
			cpu, memory := computePoolResources(ctx, pool)
			sb.Cpu = ptr.To(cpu)
			sb.Memory = ptr.To(memory)
		}
		byID[sb.SandboxId] = sb
	}

	// Step 2: Merge historical records from the store (if configured)
	if s.store != nil {
		histRecords, storeErr := s.store.List(filter.Namespace)
		if storeErr != nil {
			return nil, domain.NewInternal(storeErr.Error(), storeErr)
		}
		for _, r := range histRecords {
			if _, exists := byID[r.SandboxId]; !exists {
				// Post-filter historical records by team/user when requested.
				if filter.Team != "" && (r.Team == nil || *r.Team != filter.Team) {
					continue
				}
				if filter.User != "" && (r.User == nil || *r.User != filter.User) {
					continue
				}
				byID[r.SandboxId] = r
			}
		}
	}

	// Step 3: Build slice, apply filters
	now := time.Now()
	statusSet := parseStatusSet(filter.Status)
	all := make([]gen.Sandbox, 0, len(byID))
	for _, sb := range byID {
		if filter.PoolName != "" && sb.PoolName != filter.PoolName {
			continue
		}
		if len(statusSet) > 0 {
			if _, ok := statusSet[string(sb.Status)]; !ok {
				continue
			}
		}
		sb.DurationSeconds = computeSandboxDuration(&sb, now)
		all = append(all, sb)
	}

	// Step 4: Sort by claimedAt descending (most recent first)
	// If claimedAt is same, then sort by sandboxID for consistent ordering
	sort.Slice(all, func(i, j int) bool {
		if all[i].ClaimedAt.Equal(all[j].ClaimedAt) {
			return all[i].SandboxId < all[j].SandboxId
		}
		return all[i].ClaimedAt.After(all[j].ClaimedAt)
	})

	total := len(all)

	// Step 5: Apply pagination
	if filter.Offset > 0 {
		if filter.Offset >= len(all) {
			all = nil
		} else {
			all = all[filter.Offset:]
		}
	}
	if filter.Limit > 0 && len(all) > filter.Limit {
		all = all[:filter.Limit]
	}

	return &ListSandboxesResult{Items: all, Total: total}, nil
}

func (s *k8sSandboxService) Get(ctx context.Context, namespace, sandboxID string) (*gen.Sandbox, *domain.AppError) {
	rawID := s.stripSandboxID(sandboxID)
	pod, err := sandboxpool.FindClaimedPodBySandboxID(ctx, s.client, namespace, rawID)
	if err != nil {
		if !errors.Is(err, sandboxpool.ErrSandboxNotFound) {
			return nil, domain.NewInternal(err.Error(), err)
		}

		// K8s not found — fall back to the history store
		if s.store != nil {
			record, storeErr := s.store.Get(namespace, rawID)
			if storeErr != nil {
				return nil, domain.NewInternal(storeErr.Error(), storeErr)
			}
			if record != nil {
				return record, nil
			}
		}
		return nil, domain.NewNotFound(err.Error())
	}
	result := sandboxFromPod(pod)
	// Build endpoints by loading the pool
	poolName := pod.Labels[agentsv1alpha1.SandboxPoolLabelKey]
	if poolName != "" {
		pool := &agentsv1alpha1.SandboxPool{}
		if getErr := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: poolName}, pool); getErr == nil {
			if s.gatewayBaseURL != "" {
				if eps := buildEndpoints(pool, result.SandboxId, s.gatewayBaseURL); len(eps) > 0 {
					result.Endpoints = &eps
				}
			}
			cpu, memory := computePoolResources(ctx, pool)
			result.Cpu = ptr.To(cpu)
			result.Memory = ptr.To(memory)
		}
	}
	result.DurationSeconds = computeSandboxDuration(&result, time.Now())
	return &result, nil
}

func (s *k8sSandboxService) Delete(ctx context.Context, namespace, sandboxID string) (*gen.DeleteSandboxResult, *domain.AppError) {
	rawID := s.stripSandboxID(sandboxID)
	pod, err := sandboxpool.FindClaimedPodBySandboxID(ctx, s.client, namespace, rawID)
	if err != nil {
		if errors.Is(err, sandboxpool.ErrSandboxNotFound) {
			return nil, domain.NewNotFound(err.Error())
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	pool := &agentsv1alpha1.SandboxPool{}
	if getErr := s.client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Labels[agentsv1alpha1.SandboxPoolLabelKey]}, pool); getErr != nil {
		return nil, domain.NewInternal(fmt.Sprintf("failed to load sandbox pool: %v", getErr), getErr)
	}

	terminatedAt := time.Now().UTC().Format(time.RFC3339)

	// If the sandbox was deleted before it ever reached Running (i.e. still in Starting
	// phase and started-at annotation is absent), use "Canceled" as the stop reason so
	// callers can distinguish a premature termination from a normal completion.
	stopReason := agentsv1alpha1.SandboxStopReasonCompleted
	if pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] == agentsv1alpha1.SandboxPhaseStarting &&
		pod.Annotations[agentsv1alpha1.SandboxStartedAtAnnotationKey] == "" {
		stopReason = agentsv1alpha1.SandboxStopReasonCanceled
	}

	released, releaseErr := sandboxpool.ReleaseSandboxPod(ctx, s.client, pod, pool, sandboxpool.ReleaseSandboxPodOptions{
		StopReason:                  stopReason,
		TerminatedAt:                terminatedAt,
		ExpectedCurrentSandboxPhase: pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey],
	})
	if releaseErr != nil {
		return nil, domain.NewInternal(releaseErr.Error(), releaseErr)
	}

	// SandboxDeleteTotal is emitted by the controller's writeDeferredStoreRecord
	// when Stopping→Idle completes, avoiding double-counting.

	status := "Terminated"
	if released.Labels[agentsv1alpha1.SandboxPhaseLabelKey] == agentsv1alpha1.SandboxPhaseStopping {
		status = "Stopping"
	}

	return &gen.DeleteSandboxResult{
		SandboxId: sandboxID,
		Namespace: pod.Namespace,
		PoolName:  pool.Name,
		PodName:   pod.Name,
		Status:    status,
	}, nil
}

// ---------------------------------------------------------------------------
// private helpers
// ---------------------------------------------------------------------------

func (s *k8sSandboxService) resolveContainerImages(pool *agentsv1alpha1.SandboxPool, input CreateSandboxInput) (map[string]string, error) {
	// Validate caller-supplied images before proceeding.
	if input.Image != "" {
		if err := ValidateContainerImage(input.Image); err != nil {
			return nil, err
		}
	}
	for name, img := range input.ContainerImages {
		if err := ValidateContainerImage(img); err != nil {
			return nil, fmt.Errorf("containerImages[%s]: %w", name, err)
		}
	}

	// Rewrite private registry hosts for images that belong to a different cluster.
	if s.registryStore != nil {
		if input.Image != "" {
			input.Image = RewriteImageForCluster(input.Image, s.localClusterID, s.registryStore)
		}
		if len(input.ContainerImages) > 0 {
			rewritten := make(map[string]string, len(input.ContainerImages))
			for name, img := range input.ContainerImages {
				rewritten[name] = RewriteImageForCluster(img, s.localClusterID, s.registryStore)
			}
			input.ContainerImages = rewritten
		}
	}

	containerImages := maps.Clone(input.ContainerImages)
	if containerImages == nil {
		containerImages = map[string]string{}
	}

	if pool.Spec.Template == nil || len(pool.Spec.Template.Spec.Containers) == 0 {
		return nil, fmt.Errorf("sandbox pool %s/%s has no containers", pool.Namespace, pool.Name)
	}

	// Guard: idleImage must be set and must differ from the target image.
	// This should already have been enforced at pool creation/update time,
	// but we check here too so that a stale pool cannot bypass the constraint.
	if pool.Spec.IdleImage == "" {
		return nil, fmt.Errorf("sandbox pool %s/%s has no idleImage configured", pool.Namespace, pool.Name)
	}
	defaultContainerName := pool.Spec.Template.Spec.Containers[0].Name
	// Determine the effective target image for the first container.
	effectiveImage := pool.Spec.Template.Spec.Containers[0].Image
	if input.Image != "" {
		effectiveImage = input.Image
	}
	if img, ok := input.ContainerImages[defaultContainerName]; ok {
		effectiveImage = img
	}
	if pool.Spec.IdleImage == effectiveImage {
		return nil, fmt.Errorf("idleImage (%q) must differ from the container image (%q)", pool.Spec.IdleImage, effectiveImage)
	}

	// 1. If input.Image provided, use it for the first container in the pool's template (if any)
	if input.Image != "" {
		containerImages[defaultContainerName] = input.Image
	}

	// 2. If any container images are still missing, fill in from the pool's template
	for _, c := range pool.Spec.Template.Spec.Containers {
		if _, exists := containerImages[c.Name]; !exists {
			containerImages[c.Name] = c.Image
		}
	}

	return containerImages, nil
}

func sandboxFromPod(pod *corev1.Pod) gen.Sandbox {
	sb := sandboxpool.SandboxBaseFromPod(pod)
	sb.Status = gen.SandboxStatus(sandboxStatusFromPod(pod))
	// Derive live diagnostic info from Pod YAML (no annotation cache needed).
	if d := sandboxpool.BuildSandboxStatusDetailFromPod(pod); d != nil {
		sb.StatusDetail = &gen.SandboxStatusDetail{
			Reason:  ptr.To(d.Reason),
			Message: ptr.To(d.Message),
		}
	}
	return sb
}

// computeSandboxDuration returns the sandbox's wall-clock duration in seconds, or nil when
// the duration is not meaningful (Starting, Canceled, Pending, Stopping, or missing startedAt).
// now is passed in so List() can capture it once and reuse across all sandboxes.
func computeSandboxDuration(sb *gen.Sandbox, now time.Time) *int64 {
	switch strings.ToLower(string(sb.Status)) {
	case "running", "failed", "completed", "released":
	default:
		return nil
	}
	if sb.StartedAt == nil {
		return nil
	}
	startedAt := *sb.StartedAt
	end := now
	if strings.ToLower(string(sb.Status)) != "running" {
		if sb.TerminatedAt == nil {
			return nil
		}
		end = *sb.TerminatedAt
	}
	d := max(int64(end.Sub(startedAt).Seconds()), 0)
	return &d
}

// prefixSandboxID adds the localClusterID prefix to a sandbox ID.
// The prefixed format "{localClusterID}.{uuid}" is returned only for cross-cluster
// Create requests so that data-plane requests carry the cluster info for routing.
// Same-cluster operations never call this; List never calls this.
func (s *k8sSandboxService) prefixSandboxID(id string) string {
	if s.localClusterID != "" && id != "" {
		return s.localClusterID + "." + id
	}
	return id
}

// stripSandboxID removes the localClusterID prefix from a sandbox ID received from
// an API caller. If the ID does not start with the expected prefix, it is returned
// unchanged (backward-compatible with plain UUID IDs).
func (s *k8sSandboxService) stripSandboxID(id string) string {
	if s.localClusterID != "" {
		if after, ok := strings.CutPrefix(id, s.localClusterID+"."); ok {
			return after
		}
	}
	return id
}

func sandboxStatusFromPod(pod *corev1.Pod) string {
	switch pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] {
	case agentsv1alpha1.SandboxPhaseRunning:
		return "Running"
	case agentsv1alpha1.SandboxPhaseStarting:
		return "Starting"
	case agentsv1alpha1.SandboxPhaseStopping:
		return "Stopping"
	case agentsv1alpha1.SandboxPhaseFailed:
		return "Failed"
	default:
		return "Pending"
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// parseStatusSet parses a comma-separated status string into a lookup set.
// e.g. "Running,Failed" → {"Running": {}, "Failed": {}}
func parseStatusSet(status string) map[string]struct{} {
	if status == "" {
		return nil
	}
	parts := strings.Split(status, ",")
	result := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result[p] = struct{}{}
		}
	}
	return result
}

// buildEndpoints builds a map of runtime name → SandboxEndpoint for the given sandbox.
// Returns nil if gatewayBaseURL is empty or no runtimes with ports are defined.
func buildEndpoints(pool *agentsv1alpha1.SandboxPool, sandboxID, gatewayBaseURL string) map[string]gen.SandboxEndpoint {
	if gatewayBaseURL == "" || len(pool.Spec.Runtimes) == 0 {
		return nil
	}
	endpoints := make(map[string]gen.SandboxEndpoint, len(pool.Spec.Runtimes))
	for _, rt := range pool.Spec.Runtimes {
		if rt.Port == nil {
			continue
		}
		url := fmt.Sprintf("%s/sandboxes/%s/%d",
			strings.TrimRight(gatewayBaseURL, "/"), sandboxID, *rt.Port)
		ep := gen.SandboxEndpoint{Url: url}
		if rt.LogDir != "" {
			ep.LogDir = ptr.To(rt.LogDir)
		}
		endpoints[rt.Name] = ep
	}
	if len(endpoints) == 0 {
		return nil
	}
	return endpoints
}

// buildPoolStatusDetail fetches the latest pool status and builds a PoolStatusDetail.
// Returns nil if the pool cannot be fetched.
func buildPoolStatusDetail(ctx context.Context, c client.Client, pool *agentsv1alpha1.SandboxPool) *domain.PoolStatusDetail {
	current := &agentsv1alpha1.SandboxPool{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(pool), current); err != nil {
		return nil
	}
	hint := ""
	retryAfter := 0
	if current.Status.StoppingReplicas > 0 {
		hint = fmt.Sprintf("%d pod(s) are currently stopping and should become idle soon",
			current.Status.StoppingReplicas)
		retryAfter = 30
	}
	return &domain.PoolStatusDetail{
		Idle:       current.Status.IdleReplicas,
		Running:    current.Status.RunningReplicas,
		Starting:   current.Status.StartingReplicas,
		Stopping:   current.Status.StoppingReplicas,
		Failed:     current.Status.FailedReplicas,
		Hint:       hint,
		RetryAfter: retryAfter,
	}
}

func (s *k8sSandboxService) GetLogs(ctx context.Context, namespace, sandboxID string, params gen.GetSandboxLogsParams) (*gen.SandboxLogsResult, *domain.AppError) {
	sandboxID = s.stripSandboxID(sandboxID)
	container := ""
	if params.Container != nil {
		container = *params.Container
	}
	lines := 0
	if params.Lines != nil {
		lines = *params.Lines
	}
	source := ""
	if params.Source != nil {
		source = *params.Source
	}
	// If a runtime source is requested, fetch runtime log file via exec.
	if source != "" && source != "stdout" {
		return s.getRuntimeLogs(ctx, namespace, sandboxID, source, lines)
	}

	// Live logs: sandbox must be active (pod found and not terminated).
	pod, err := sandboxpool.FindClaimedPodBySandboxID(ctx, s.client, namespace, sandboxID)
	if err != nil {
		if errors.Is(err, sandboxpool.ErrSandboxNotFound) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox %s/%s not found or already terminated", namespace, sandboxID))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	if s.clientset == nil {
		// Live logs not available without a clientset.
		return &gen.SandboxLogsResult{
			SandboxId: sandboxID,
			Namespace: namespace,
			PodName:   ptr.To(pod.Name),
			Entries:   nil,
			Source:    gen.SandboxLogsResultSource("live"),
		}, nil
	}

	logOpts := &corev1.PodLogOptions{
		Container:  container,
		Timestamps: true,
	}
	if lines > 0 {
		n := int64(lines)
		logOpts.TailLines = &n
	}

	// Collect logs from the requested container(s).
	var entries []gen.SandboxLogEntry
	var totalBytes int64
	containersToFetch := pod.Spec.Containers
	if container != "" {
		for _, c := range pod.Spec.Containers {
			if c.Name == container {
				containersToFetch = []corev1.Container{c}
				break
			}
		}
	}
	for _, c := range containersToFetch {
		podLogOpts := *logOpts
		podLogOpts.Container = c.Name
		req := s.clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
		raw, fetchErr := req.DoRaw(ctx)
		if fetchErr != nil {
			continue
		}
		totalBytes += int64(len(raw))
		for _, line := range splitLogLines(raw) {
			if line == "" {
				continue
			}
			entries = append(entries, gen.SandboxLogEntry{Container: c.Name, Log: line})
		}
	}

	return &gen.SandboxLogsResult{
		SandboxId:  sandboxID,
		Namespace:  namespace,
		PodName:    ptr.To(pod.Name),
		Entries:    entries,
		TotalBytes: &totalBytes,
		Source:     gen.SandboxLogsResultSource("live"),
	}, nil
}

// splitLogLines splits raw Kubernetes log output into individual log lines,
// stripping the optional RFC3339Nano timestamp prefix added when Timestamps=true.
func splitLogLines(raw []byte) []string {
	lines := strings.Split(string(raw), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Strip leading timestamp prefix if present: "<RFC3339Nano> <rest>"
		if idx := strings.IndexByte(line, ' '); idx > 0 {
			if _, err := time.Parse(time.RFC3339Nano, line[:idx]); err == nil {
				line = line[idx+1:]
			}
		}
		result = append(result, line)
	}
	return result
}

// getRuntimeLogs reads a runtime's log file from inside the pod via exec.
// runtimeName is the name of the runtime (e.g. "envd"); lines limits the output (0 = all).
func (s *k8sSandboxService) getRuntimeLogs(ctx context.Context, namespace, sandboxID, runtimeName string, lines int) (*gen.SandboxLogsResult, *domain.AppError) {
	// Runtime logs are only available for active (Running) sandboxes.
	pod, err := sandboxpool.FindClaimedPodBySandboxID(ctx, s.client, namespace, sandboxID)
	if err != nil {
		if errors.Is(err, sandboxpool.ErrSandboxNotFound) {
			return nil, domain.NewBadRequest("runtime logs are only available for active sandboxes; sandbox not found or already terminated")
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	if pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseRunning {
		return nil, domain.NewBadRequest("runtime logs are only available for Running sandboxes")
	}
	if s.clientset == nil || s.restConfig == nil {
		return nil, domain.NewBadRequest("exec is not available: kubernetes clientset or rest config is not configured")
	}

	// Look up the logDir for the requested runtime.
	logDir, appErr := s.getRuntimeLogDir(ctx, namespace, pod, runtimeName)
	if appErr != nil {
		return nil, appErr
	}

	// Build the command: tail last N lines, or cat the whole file.
	var cmd string
	if lines > 0 {
		cmd = fmt.Sprintf("tail -n %d %s 2>/dev/null || true", lines, logDir)
	} else {
		cmd = fmt.Sprintf("cat %s 2>/dev/null || true", logDir)
	}

	timeout := 30
	result, appErr := s.ExecCommand(ctx, namespace, sandboxID, gen.ExecCommandRequest{
		Command:        cmd,
		TimeoutSeconds: &timeout,
	})
	if appErr != nil {
		return nil, appErr
	}

	// Parse output: each line becomes a SandboxLogEntry (no timestamp).
	rawLines := strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n")
	entries := make([]gen.SandboxLogEntry, 0, len(rawLines))
	for _, line := range rawLines {
		if line == "" {
			continue
		}
		entries = append(entries, gen.SandboxLogEntry{
			Container: runtimeName,
			Log:       line,
		})
	}

	return &gen.SandboxLogsResult{
		SandboxId:   sandboxID,
		Namespace:   namespace,
		PodName:     ptr.To(pod.Name),
		Entries:     entries,
		Source:      gen.SandboxLogsResultSource("runtime"),
		RuntimeName: ptr.To(runtimeName),
	}, nil
}

// getRuntimeLogDir finds the logDir for the named runtime by traversing pool → (optional template) → runtimes.
func (s *k8sSandboxService) getRuntimeLogDir(ctx context.Context, namespace string, pod *corev1.Pod, runtimeName string) (string, *domain.AppError) {
	poolName := pod.Labels[agentsv1alpha1.SandboxPoolLabelKey]
	if poolName == "" {
		return "", domain.NewBadRequest("sandbox pod is missing sandbox-pool label")
	}

	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: poolName}, pool); err != nil {
		return "", domain.NewInternal(fmt.Sprintf("failed to load sandbox pool %s/%s: %v", namespace, poolName, err), err)
	}

	// Use pool's inline runtimes. If pool has a templateName set, load the template for its runtimes.
	runtimes := pool.Spec.Runtimes
	if pool.Spec.TemplateName != "" && len(runtimes) == 0 {
		tmpl := &agentsv1alpha1.SandboxTemplate{}
		if err := s.client.Get(ctx, client.ObjectKey{Name: pool.Spec.TemplateName}, tmpl); err != nil {
			return "", domain.NewInternal(fmt.Sprintf("failed to load sandbox template %q: %v", pool.Spec.TemplateName, err), err)
		}
		runtimes = tmpl.Spec.Runtimes
	}

	for _, rt := range runtimes {
		if rt.Name == runtimeName {
			if rt.LogDir == "" {
				return "", domain.NewBadRequest(fmt.Sprintf("runtime %q does not have a logDir configured", runtimeName))
			}
			return rt.LogDir, nil
		}
	}

	return "", domain.NewBadRequest(fmt.Sprintf("runtime %q not found in pool %s", runtimeName, poolName))
}

func (s *k8sSandboxService) SetTimeout(ctx context.Context, namespace, sandboxID string, timeout time.Duration) *domain.AppError {
	pod, err := sandboxpool.FindClaimedPodBySandboxID(ctx, s.client, namespace, s.stripSandboxID(sandboxID))
	if err != nil {
		if errors.Is(err, sandboxpool.ErrSandboxNotFound) {
			return domain.NewNotFound(err.Error())
		}
		return domain.NewInternal(err.Error(), err)
	}

	// Patch the idle-timeout annotation on the Pod.
	podCopy := pod.DeepCopy()
	if podCopy.Annotations == nil {
		podCopy.Annotations = make(map[string]string)
	}
	if timeout <= 0 {
		delete(podCopy.Annotations, agentsv1alpha1.SandboxIdleTimeoutAnnotationKey)
	} else {
		podCopy.Annotations[agentsv1alpha1.SandboxIdleTimeoutAnnotationKey] = strconv.FormatInt(int64(timeout.Seconds()), 10)
	}

	if patchErr := s.client.Patch(ctx, podCopy, client.MergeFrom(pod)); patchErr != nil {
		return domain.NewInternal(fmt.Sprintf("failed to patch idle timeout: %v", patchErr), patchErr)
	}
	return nil
}

// computePoolResources calculates CPU and Memory string values from pool spec.
// Returns ("", "") if the pool has no template or the computation fails.
func computePoolResources(ctx context.Context, pool *agentsv1alpha1.SandboxPool) (cpu, memory string) {
	if pool == nil || pool.Spec.Template == nil {
		return "", ""
	}
	cpuQ, memQ, err := utilresource.SumContainerResources(pool.Spec.Template)
	if err != nil {
		log.FromContext(ctx).V(1).Info("failed to compute sandbox resources", "pool", pool.Name, "error", err)
		return "", ""
	}
	return cpuQ.String(), memQ.String()
}

// CreateExecToken generates a single-use exec token (TTL 30s) for the given sandbox.
// The sandbox must be in Running phase.
func (s *k8sSandboxService) CreateExecToken(ctx context.Context, namespace, sandboxID string) (string, *domain.AppError) {
	pod, err := sandboxpool.FindClaimedPodBySandboxID(ctx, s.client, namespace, s.stripSandboxID(sandboxID))
	if err != nil {
		if errors.Is(err, sandboxpool.ErrSandboxNotFound) {
			return "", domain.NewNotFound(err.Error())
		}
		return "", domain.NewInternal(err.Error(), err)
	}
	if pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseRunning {
		return "", domain.NewBadRequest("sandbox is not in Running phase; exec is only available for running sandboxes")
	}

	containers := make([]string, 0, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		containers = append(containers, c.Name)
	}

	token := uuid.NewString()
	s.execTokens.Set(token, ExecTokenRecord{
		SandboxID:  sandboxID,
		Namespace:  namespace,
		PodName:    pod.Name,
		Containers: containers,
		ExpiresAt:  time.Now().Add(execTokenTTL),
	})
	return token, nil
}

// ValidateExecToken validates and consumes the token.
// Returns ExecTokenInfo on success, or a domain.AppError (Unauthorized) if invalid/expired.
func (s *k8sSandboxService) ValidateExecToken(tokenStr string) (*ExecTokenInfo, *domain.AppError) {
	info := s.execTokens.Consume(tokenStr)
	if info == nil {
		return nil, domain.NewUnauthorized("exec token is invalid or has expired")
	}
	return info, nil
}

const (
	execCommandDefaultTimeout = 30
	execCommandMaxTimeout     = 300
)

// ExecCommand runs a one-shot command inside the sandbox pod (non-interactive, no TTY).
// The sandbox must be in Running phase. clientset and restConfig must be non-nil.
func (s *k8sSandboxService) ExecCommand(ctx context.Context, namespace, sandboxID string, req gen.ExecCommandRequest) (*gen.ExecCommandResult, *domain.AppError) {
	pod, err := sandboxpool.FindClaimedPodBySandboxID(ctx, s.client, namespace, s.stripSandboxID(sandboxID))
	if err != nil {
		if errors.Is(err, sandboxpool.ErrSandboxNotFound) {
			return nil, domain.NewNotFound(err.Error())
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	if pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseRunning {
		return nil, domain.NewBadRequest("sandbox is not in Running phase; exec is only available for running sandboxes")
	}

	if s.clientset == nil || s.restConfig == nil {
		return nil, domain.NewBadRequest("exec is not available: kubernetes clientset or rest config is not configured")
	}

	// Compute timeout: clamp to [1, 300], default 30
	timeoutSec := 0
	if req.TimeoutSeconds != nil {
		timeoutSec = *req.TimeoutSeconds
	}
	if timeoutSec <= 0 {
		timeoutSec = execCommandDefaultTimeout
	} else if timeoutSec > execCommandMaxTimeout {
		timeoutSec = execCommandMaxTimeout
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// Pick first container (consistent with terminal.go)
	container := ""
	if len(pod.Spec.Containers) > 0 {
		container = pod.Spec.Containers[0].Name
	}

	execReq := s.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   []string{"sh", "-c", req.Command},
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, execErr := remotecommand.NewSPDYExecutor(s.restConfig, http.MethodPost, execReq.URL())
	if execErr != nil {
		return nil, domain.NewInternal(fmt.Sprintf("failed to create exec session: %v", execErr), execErr)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	streamErr := executor.StreamWithContext(execCtx, remotecommand.StreamOptions{
		Stdout: &stdoutBuf,
		Stderr: &stderrBuf,
		Tty:    false,
	})

	exitCode := 0
	if streamErr != nil {
		// Check if it's an exit code error (non-zero exit from the command).
		// k8s.io/client-go/util/exec.CodeExitError carries the exit status.
		type exitCoder interface {
			ExitStatus() int
		}
		if ec, ok := streamErr.(exitCoder); ok {
			exitCode = ec.ExitStatus()
		} else {
			return nil, domain.NewInternal(fmt.Sprintf("exec stream error: %v", streamErr), streamErr)
		}
	}

	return &gen.ExecCommandResult{
		ExitCode: exitCode,
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
	}, nil
}

// IsReady checks if all runtime readiness probes for a sandbox pass.
// If a runtime has no ReadinessProbe configured, it is considered ready.
// Only HTTPGet probes are supported; TCPSocket and Exec probes are skipped (considered ready).
func (s *k8sSandboxService) IsReady(ctx context.Context, namespace, sandboxID string) (*gen.SandboxReadinessResult, *domain.AppError) {
	// Retrieve the sandbox to verify it exists and get endpoints.
	sb, appErr := s.Get(ctx, namespace, sandboxID)
	if appErr != nil {
		return nil, appErr
	}

	// If sandbox is not Running, it's not ready.
	if string(sb.Status) != "Running" {
		endpoints := make(map[string]struct {
			Message *string `json:"message,omitempty"`
			Ready   *bool   `json:"ready,omitempty"`
		})
		msg := fmt.Sprintf("sandbox is in %s phase, not Running", sb.Status)
		if sb.Endpoints != nil {
			for name := range *sb.Endpoints {
				endpoints[name] = struct {
					Message *string `json:"message,omitempty"`
					Ready   *bool   `json:"ready,omitempty"`
				}{
					Message: ptr.To(msg),
					Ready:   ptr.To(false),
				}
			}
		}
		return &gen.SandboxReadinessResult{
			SandboxId: sandboxID,
			Ready:     false,
			Endpoints: &endpoints,
		}, nil
	}

	// Load the pool to get runtime ReadinessProbe configurations.
	pod, err := sandboxpool.FindClaimedPodBySandboxID(ctx, s.client, namespace, s.stripSandboxID(sandboxID))
	if err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}
	poolName := pod.Labels[agentsv1alpha1.SandboxPoolLabelKey]
	pool := &agentsv1alpha1.SandboxPool{}
	if getErr := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: poolName}, pool); getErr != nil {
		return nil, domain.NewInternal(fmt.Sprintf("failed to load pool %s: %v", poolName, getErr), getErr)
	}

	// Build a map of runtime name → ReadinessProbe.
	probeByRuntime := make(map[string]*corev1.Probe, len(pool.Spec.Runtimes))
	for _, rt := range pool.Spec.Runtimes {
		probeByRuntime[rt.Name] = rt.ReadinessProbe
	}

	endpoints := make(map[string]struct {
		Message *string `json:"message,omitempty"`
		Ready   *bool   `json:"ready,omitempty"`
	})
	overall := true
	if sb.Endpoints != nil {
		for name, ep := range *sb.Endpoints {
			probe := probeByRuntime[name]
			if probe == nil || probe.HTTPGet == nil {
				// No probe configured or non-HTTP probe — default to ready.
				endpoints[name] = struct {
					Message *string `json:"message,omitempty"`
					Ready   *bool   `json:"ready,omitempty"`
				}{Ready: ptr.To(true)}
				continue
			}

			// Build the probe URL from the endpoint URL + probe path.
			probeURL := ep.Url
			if probe.HTTPGet.Path != "" {
				probeURL = strings.TrimRight(ep.Url, "/") + "/" + strings.TrimLeft(probe.HTTPGet.Path, "/")
			}

			ready, msg := s.checkHTTPProbe(ctx, probeURL)
			entry := struct {
				Message *string `json:"message,omitempty"`
				Ready   *bool   `json:"ready,omitempty"`
			}{Ready: ptr.To(ready)}
			if msg != "" {
				entry.Message = ptr.To(msg)
			}
			endpoints[name] = entry
			if !ready {
				overall = false
			}
		}
	}

	return &gen.SandboxReadinessResult{
		SandboxId: sandboxID,
		Ready:     overall,
		Endpoints: &endpoints,
	}, nil
}

// checkHTTPProbe performs a single HTTP GET probe and returns (ready, message).
func (s *k8sSandboxService) checkHTTPProbe(ctx context.Context, probeURL string) (bool, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return false, fmt.Sprintf("failed to build probe request: %v", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false, fmt.Sprintf("probe request failed: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return true, ""
	}
	return false, fmt.Sprintf("probe returned HTTP %d", resp.StatusCode)
}

// NotifyIdleAvailable implements sandboxpool.IdleNotifier.
// It is called by the SandboxPoolReconciler immediately after a Pod
// transitions Stopping → Idle, so the per-pool scheduler can wake up
// and attempt a fresh dispatch without waiting for the poll timer.
func (s *k8sSandboxService) NotifyIdleAvailable(namespace, poolName string) {
	key := namespace + "/" + poolName
	s.schedulersMu.RLock()
	sched := s.schedulers[key]
	s.schedulersMu.RUnlock()
	if sched != nil {
		sched.NotifyIdle()
	}
}

// OnSandboxReleased implements sandboxpool.IdleNotifier. It invalidates the
// ExtProc route cache entry for the released sandbox so subsequent router
// queries return NotFound rather than briefly hitting a stale entry. The call
// is best-effort: failures are logged and swallowed because TTL (1 min) and
// the router's live label check already provide correctness.
func (s *k8sSandboxService) OnSandboxReleased(ctx context.Context, sandboxID string) {
	if s.extprocClient == nil || sandboxID == "" {
		return
	}
	if err := s.extprocClient.EvictRoute(ctx, sandboxID); err != nil {
		klog.V(2).InfoS("OnSandboxReleased: extproc EvictRoute failed",
			"sandboxID", sandboxID, "error", err)
	}
}

// pushRouteToExtProc registers the freshly-claimed Pod's sandbox-id →
// (ns, pod_name) mapping in the ExtProc route cache so the router can serve
// traffic immediately, without waiting for the ExtProc informer to observe
// the sandbox-id label on the Pod. The payload intentionally does NOT carry
// Phase or PodIP: the router reads both live from its own Pod informer at
// request time, so this push never becomes stale across Pod lifecycle
// transitions (Starting → Running → Stopping).
// The sandbox ID pushed is always the raw UUID (no cluster prefix), because
// cross-cluster prefix stripping happens inside the ExtProc router before it
// consults the cache.
func (s *k8sSandboxService) pushRouteToExtProc(ctx context.Context, prefixedSandboxID string, pod *corev1.Pod) error {
	if s.extprocClient == nil {
		return fmt.Errorf("extproc client not configured")
	}
	rawID := s.stripSandboxID(prefixedSandboxID)
	return s.extprocClient.PushRoute(ctx, RouteInfo{
		SandboxID: rawID,
		Namespace: pod.Namespace,
		PodName:   pod.Name,
	})
}

// probeEndpointReady runs a single 500 ms probe against the first runtime
// endpoint and returns when the endpoint no longer reports "sandbox not
// found". Any other response (including HTTP errors) is considered ready —
// the probe is only a fallback guard, not a real readiness check. The
// endpoint probe only makes sense when a gateway base URL is configured and
// the pool has at least one endpoint.
func (s *k8sSandboxService) probeEndpointReady(ctx context.Context, pool *agentsv1alpha1.SandboxPool, endpoints *map[string]gen.SandboxEndpoint) {
	if s.gatewayBaseURL == "" || endpoints == nil || len(*endpoints) == 0 || pool == nil || len(pool.Spec.Runtimes) == 0 {
		return
	}
	endpoint, ok := (*endpoints)[pool.Spec.Runtimes[0].Name]
	if !ok {
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if endpointReady(probeCtx, s.httpClient, endpoint.Url) {
			return
		}
		select {
		case <-probeCtx.Done():
			return
		case <-ticker.C:
		}
	}
}

// endpointReady returns true when a GET against url does not look like a
// "sandbox not found" gateway 404. It is intentionally lenient: any non-404
// response, any non-JSON body, or any parse error counts as ready. We only
// keep probing on the very specific shape that ExtProc returns for a missing
// route.
func endpointReady(ctx context.Context, httpClient *http.Client, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return true
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return true
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNotFound {
		return true
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return true
	}
	return !strings.Contains(body.Error, "sandbox not found")
}
