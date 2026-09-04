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

package extproc

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// ErrSandboxRouteNotFound signals that the sandbox is unknown to the router:
// neither the cache nor (when fallback is enabled) the sandbox-id informer
// index returned any mapping. Mapped to HTTP 404 by the ExtProc server.
// Callers receiving this should treat the sandbox ID as definitively absent
// (never existed, or evicted long enough ago that every trace is gone).
var ErrSandboxRouteNotFound = errors.New("sandbox route not found")

// ErrSandboxRouteBadGateway signals that the router knows the sandbox exists
// but cannot route to it right now. Mapped to HTTP 502, retryable. Any Pod
// state that is not "Running or Stopping + matching sandbox-id label + PodIP
// set" produces this error, so it also covers informer lag, label race
// windows, Starting (container swap in progress), Idle/Failed phases, and
// missing PodIP.
var ErrSandboxRouteBadGateway = errors.New("sandbox is not in a healthy state")

// SandboxRoute holds the resolved upstream destination for a sandbox request.
type SandboxRoute struct {
	PodIP string
	Port  int
}

// DestHost formats the route as "IP:Port" for the x-envoy-original-dst-host header.
func (r *SandboxRoute) DestHost() string {
	return fmt.Sprintf("%s:%d", r.PodIP, r.Port)
}

// SandboxRouter resolves the destination Pod for a given sandbox ID.
// Implement this interface to swap in a cache-backed or test-double router.
type SandboxRouter interface {
	ResolveSandboxRoute(ctx context.Context, sandboxID string, port int) (*SandboxRoute, error)
}

// K8sSandboxRouter resolves sandbox routes using two independent data sources:
//
//   - RouteCache, populated by Controller push/evict, answers "does sandbox X
//     exist, and if so on which (ns, pod_name)?". It carries NO phase or IP —
//     those would go stale across Pod lifecycle transitions.
//   - Pod informer (via mgr.GetClient()), answers "what is the live state of
//     (ns, pod_name)?". Consulted at request time to check phase + IP.
//
// Each request reads the Pod exactly once: via client.Get by name on cache
// hit, or via the sandbox-id indexer on cache miss (when fallback is on).
// Both paths funnel into finalize() which applies the phase/label/IP checks.
//
// The fallback to indexer is gated by enableFallback. With fallback OFF, cache
// misses are served as NotFound immediately — useful for verifying the
// pure-push model in testing.
type K8sSandboxRouter struct {
	k8sClient      client.Client
	cache          *RouteCache // may be nil in tests / dev
	enableFallback bool
}

// NewK8sSandboxRouter creates a K8sSandboxRouter.
// The defaultPort parameter is kept for compatibility but is not used when the caller
// provides a port via headers or URL.
// cache may be nil, in which case the router only uses the informer path.
// When enableFallback is false, cache misses short-circuit to NotFound without
// consulting the sandbox-id informer index.
func NewK8sSandboxRouter(c client.Client, defaultPort int, cache *RouteCache, enableFallback bool) *K8sSandboxRouter {
	return &K8sSandboxRouter{k8sClient: c, cache: cache, enableFallback: enableFallback}
}

// ResolveSandboxRoute returns the live Pod IP for the given sandbox, or one
// of ErrSandboxRouteNotFound / ErrSandboxRouteBadGateway. Exactly one Pod
// read is performed per request.
func (r *K8sSandboxRouter) ResolveSandboxRoute(ctx context.Context, sandboxID string, reqPort int) (*SandboxRoute, error) {
	if sandboxID == "" || reqPort <= 0 || reqPort > 65535 {
		return nil, ErrSandboxRouteNotFound
	}

	// Fast path: cache hit. Read the Pod by (ns, name) from the informer
	// cache. A missing Pod object means the informer hasn't observed what the
	// Controller push said exists — classic lag window, return 502 so the
	// caller retries instead of treating it as a permanent 404.
	if r.cache != nil {
		if e, ok := r.cache.Get(sandboxID); ok {
			pod := &corev1.Pod{}
			if err := r.k8sClient.Get(ctx, client.ObjectKey{Namespace: e.Namespace, Name: e.PodName}, pod); err != nil {
				return nil, ErrSandboxRouteBadGateway
			}
			return r.finalize(pod, sandboxID, reqPort)
		}
	}

	// Cache miss + fallback disabled: serve NotFound. In pure-push mode the
	// cache is authoritative; if it has no entry, the sandbox is absent.
	if !r.enableFallback {
		return nil, ErrSandboxRouteNotFound
	}

	// Cache miss + fallback enabled: consult the sandbox-id informer index.
	// If the index also has nothing, the sandbox really does not exist.
	pod, err := indexer.GetPodBySandboxID(ctx, r.k8sClient, sandboxID)
	if err != nil {
		return nil, ErrSandboxRouteNotFound
	}
	route, ferr := r.finalize(pod, sandboxID, reqPort)
	// Backfill the cache on a successful fallback resolve so subsequent
	// requests skip the index lookup. Skip on non-success to avoid caching
	// a pod that's about to transition.
	if ferr == nil && r.cache != nil {
		r.cache.Put(sandboxID, RouteEntry{Namespace: pod.Namespace, PodName: pod.Name})
	}
	return route, ferr
}

// finalize applies the label/phase/IP decision table on the single Pod object
// obtained from either the cache path or the indexer path. Keeping this logic
// in one place guarantees the "one Pod read per request" contract.
//
// Decision table:
//   - phase ∈ {Running, Stopping}, sandbox-id label matches, PodIP set,
//     and the sandbox is armed                                        → 200
//     Stopping is still routable because the pod hasn't been recycled yet
//     and its runtime may continue to serve in-flight client traffic.
//   - phase ∈ {Running, Stopping}, sandbox-id label mismatches         → 502
//     Pod already reclaimed by another sandbox; the old ID is briefly
//     stranded in our cache. Caller should retry (and usually will hit a
//     cache-miss / indexer-miss next, yielding the definitive 404).
//   - phase ∈ {Running, Stopping}, PodIP empty                         → 502
//   - phase = Running, not yet armed                                   → 502
//     The image is in place but the sandbox is not set up: its env vars,
//     injected CA, egress policy and credentials are still being delivered.
//     Serving here is how a first command used to see an empty environment,
//     or leave carrying a decoy credential. The create path waits for the
//     mark, so this only fires for callers that got an ID another way
//     (connect by ID, a console session, a retained handle).
//   - any other phase (Starting, Idle, Failed, empty, unknown)         → 502
//     Starting specifically must not route: the container is swapping
//     images and answering there would leak into the previous sandbox's
//     runtime. Idle/Failed/etc. are transient enough that we prefer 502
//     over 404 so callers keep trying until the cache / indexer agrees
//     the sandbox is truly gone.
func (r *K8sSandboxRouter) finalize(pod *corev1.Pod, sandboxID string, reqPort int) (*SandboxRoute, error) {
	switch pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] {
	case string(agentsv1alpha1.SandboxPhaseRunning), string(agentsv1alpha1.SandboxPhaseStopping):
		if pod.Labels[agentsv1alpha1.SandboxIDLabelKey] != sandboxID {
			return nil, ErrSandboxRouteBadGateway
		}
		if pod.Status.PodIP == "" {
			return nil, ErrSandboxRouteBadGateway
		}
		if !sandboxArmed(pod, sandboxID) {
			return nil, ErrSandboxRouteBadGateway
		}
		return &SandboxRoute{PodIP: pod.Status.PodIP, Port: reqPort}, nil
	default:
		return nil, ErrSandboxRouteBadGateway
	}
}

// sandboxArmed reports whether the pod carries the arming mark for this
// sandbox.
//
// Stopping pods are exempt: they were armed while Running, and the release path
// strips the mark, so requiring it would cut off in-flight traffic during
// teardown — the one case where the old behaviour is the correct one.
func sandboxArmed(pod *corev1.Pod, sandboxID string) bool {
	if pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != string(agentsv1alpha1.SandboxPhaseRunning) {
		return true
	}
	return pod.Annotations[agentsv1alpha1.SandboxArmedAnnotationKey] == sandboxID
}
