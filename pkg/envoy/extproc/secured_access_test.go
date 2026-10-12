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
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/utils/accesstoken"
)

const securedSecret = "test-shared-secret"

// headers builds an ExtProc RequestHeaders message from a :path plus extras.
func headers(path string, extra map[string]string) *extProcPb.HttpHeaders {
	hs := make([]*corev3.HeaderValue, 0, 1+len(extra))
	hs = append(hs, &corev3.HeaderValue{Key: ":path", RawValue: []byte(path)})
	for k, v := range extra {
		hs = append(hs, &corev3.HeaderValue{Key: k, RawValue: []byte(v)})
	}
	return &extProcPb.HttpHeaders{Headers: &corev3.HeaderMap{Headers: hs}}
}

// isRouted reports whether the response is a (successful) header mutation rather
// than an immediate error.
func isRouted(resp *extProcPb.ProcessingResponse) bool {
	_, ok := resp.Response.(*extProcPb.ProcessingResponse_RequestHeaders)
	return ok
}

func securedPod(name, sandboxID string) *corev1.Pod {
	p := makePod(name, sandboxID, string(agentsv1alpha1.SandboxPhaseRunning), testPodIP)
	if p.Annotations == nil {
		p.Annotations = map[string]string{}
	}
	p.Annotations[agentsv1alpha1.SandboxTrafficSecuredAnnotationKey] = "true"
	return p
}

// An envd control path arriving through the gateway must be refused: the
// legitimate /init is dialled straight at the pod, so one reaching here is an
// attempt to claim the sandbox's envd before arming delivers its token.
func TestHandleRequestHeaders_RefusesEnvdControlPath(t *testing.T) {
	r := newTestRouter(t, makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), testPodIP))
	s := &Server{router: r, tokenGen: accesstoken.New(securedSecret)}

	resp := s.handleRequestHeaders(context.Background(),
		headers("/sandboxes/sb1/49983/init", nil))
	if got := statusOf(t, resp); got != 403 {
		t.Fatalf("envd /init through the gateway must be 403, got %d", got)
	}
}

// A normal envd RPC (not a control path) still routes.
func TestHandleRequestHeaders_AllowsEnvdRPC(t *testing.T) {
	r := newTestRouter(t, makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), testPodIP))
	s := &Server{router: r, tokenGen: accesstoken.New(securedSecret)}

	resp := s.handleRequestHeaders(context.Background(),
		headers("/sandboxes/sb1/49983/process.Process/Start", nil))
	if !isRouted(resp) {
		t.Fatalf("a non-control envd RPC must route, got status %d", statusOf(t, resp))
	}
}

// A traffic-secured sandbox refuses a user port without the token, accepts it
// with the correct token, and rejects a wrong one.
func TestHandleRequestHeaders_TrafficToken(t *testing.T) {
	gen := accesstoken.New(securedSecret)
	r := newTestRouter(t, securedPod("pod-1", "sb1"))
	s := &Server{router: r, tokenGen: gen}

	// No token → 401.
	resp := s.handleRequestHeaders(context.Background(), headers("/sandboxes/sb1/8090/", nil))
	if got := statusOf(t, resp); got != 401 {
		t.Fatalf("secured user port without token must be 401, got %d", got)
	}

	// Wrong token → 401.
	resp = s.handleRequestHeaders(context.Background(),
		headers("/sandboxes/sb1/8090/", map[string]string{accesstoken.TrafficHeader: "wrong"}))
	if got := statusOf(t, resp); got != 401 {
		t.Fatalf("secured user port with wrong token must be 401, got %d", got)
	}

	// Correct token → routed.
	resp = s.handleRequestHeaders(context.Background(),
		headers("/sandboxes/sb1/8090/", map[string]string{accesstoken.TrafficHeader: gen.TrafficToken("sb1")}))
	if !isRouted(resp) {
		t.Fatalf("secured user port with the correct token must route, got status %d", statusOf(t, resp))
	}
}

// The envd port is exempt from the traffic-token check: envd authenticates it
// with its own token, which the gateway does not hold.
func TestHandleRequestHeaders_TrafficTokenExemptsEnvdPort(t *testing.T) {
	r := newTestRouter(t, securedPod("pod-1", "sb1"))
	s := &Server{router: r, tokenGen: accesstoken.New(securedSecret)}

	resp := s.handleRequestHeaders(context.Background(),
		headers("/sandboxes/sb1/49983/files", nil))
	if !isRouted(resp) {
		t.Fatalf("the envd port must not require the traffic token, got status %d", statusOf(t, resp))
	}
}

// An unsecured sandbox is reachable on its user ports without any token (the
// default, public-traffic behaviour is preserved).
func TestHandleRequestHeaders_UnsecuredNeedsNoToken(t *testing.T) {
	r := newTestRouter(t, makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), testPodIP))
	s := &Server{router: r, tokenGen: accesstoken.New(securedSecret)}

	resp := s.handleRequestHeaders(context.Background(), headers("/sandboxes/sb1/8090/", nil))
	if !isRouted(resp) {
		t.Fatalf("an unsecured user port must route without a token, got status %d", statusOf(t, resp))
	}
}
