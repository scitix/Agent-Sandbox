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

// ErrSandboxRouteNotFound signals that no Pod carries this sandbox ID.
//
// Served as HTTP 502, the same as ErrSandboxRouteBadGateway, and the two are
// deliberately not distinguished on the wire. This router cannot actually tell
// "gone" from "not yet visible": its only source is an informer cache, so an
// index miss means *either* the sandbox never existed *or* this replica has
// not seen it yet. A 404 would promise a permanence we are not in a position
// to assert, and the caller's correct move is the same either way — retry, and
// give up on your own schedule.
//
// The distinction survives where it is useful: the two errors carry different
// messages, which is what a human reading a log needs.
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

// K8sSandboxRouter resolves sandbox routes from one data source: the Pod
// informer, queried through the sandbox-id index.
//
// # Why nobody tells the gateway about a sandbox
//
// The mapping this router needs — sandbox ID to Pod — is a label the claim
// writes onto the Pod in the same CAS that moves it Idle → Starting. The
// indexer is built on that label, so every replica of this process can resolve
// a sandbox from the moment it is claimed, without being told. There was once
// a cache the control plane pushed into, to skip the milliseconds between that
// write and the watch event arriving here. It is gone, for two reasons:
//
//   - Nothing is waiting on those milliseconds any more. Create returns only
//     after the sandbox is armed — runtimes answering, env vars delivered, CA
//     installed, egress policy and credentials pushed — which is seconds of
//     round trips, all of it after the label write this router keys on. By the
//     time a caller holds an ID, the informer has long since caught up.
//   - A pushed cache cannot survive replication. The control plane holds one
//     gRPC connection, which pins to a single backend Pod, so a push reaches
//     one replica and the others answer differently for the same sandbox. An
//     informer needs no such coordination: each replica watches the apiserver
//     and arrives at the same answer on its own.
//
// So this process is a pure reader of Kubernetes state. It can be restarted,
// rebuilt, or scaled to any number of replicas with no control-plane
// involvement at all.
type K8sSandboxRouter struct {
	k8sClient client.Client
}

// NewK8sSandboxRouter creates a K8sSandboxRouter.
// The defaultPort parameter is kept for compatibility but is not used when the caller
// provides a port via headers or URL.
func NewK8sSandboxRouter(c client.Client, defaultPort int) *K8sSandboxRouter {
	return &K8sSandboxRouter{k8sClient: c}
}

// ResolveSandboxRoute returns the live Pod IP for the given sandbox, or one
// of ErrSandboxRouteNotFound / ErrSandboxRouteBadGateway. Exactly one Pod
// read is performed per request, served from the informer cache.
func (r *K8sSandboxRouter) ResolveSandboxRoute(ctx context.Context, sandboxID string, reqPort int) (*SandboxRoute, error) {
	if sandboxID == "" || reqPort <= 0 || reqPort > 65535 {
		return nil, ErrSandboxRouteNotFound
	}

	// The sandbox-id index. A miss means no Pod carries this ID: either it
	// never existed, or it was released and the label stripped.
	pod, err := indexer.GetPodBySandboxID(ctx, r.k8sClient, sandboxID)
	if err != nil {
		return nil, ErrSandboxRouteNotFound
	}
	return r.finalize(pod, sandboxID, reqPort)
}

// finalize applies the label/phase/IP decision table on the single Pod object
// obtained from either the cache path or the indexer path. Keeping this logic
// in one place guarantees the "one Pod read per request" contract.
//
// Decision table:
//   - phase ∈ {Running, Stopping}, sandbox-id label matches, PodIP set → 200
//     Stopping is still routable because the pod hasn't been recycled yet
//     and its runtime may continue to serve in-flight client traffic.
//   - phase ∈ {Running, Stopping}, sandbox-id label mismatches         → 502
//     Pod already reclaimed by another sandbox; the old ID is briefly
//     stranded in our cache. Caller should retry (and usually will hit a
//     cache-miss / indexer-miss next, yielding the definitive 404).
//   - phase ∈ {Running, Stopping}, PodIP empty                         → 502
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
		return &SandboxRoute{PodIP: pod.Status.PodIP, Port: reqPort}, nil
	default:
		return nil, ErrSandboxRouteBadGateway
	}
}
