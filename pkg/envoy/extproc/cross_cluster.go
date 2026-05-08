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
	"fmt"
	"net/url"
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// crossClusterHeader is injected by ExtProc to signal Envoy that this request
// should be routed to a cross-cluster gateway via a dedicated cluster definition.
const crossClusterHeader = "x-agentbox-cross-cluster"

// crossClusterSchemeHeader carries the gateway URL scheme ("https" or "http").
// Envoy uses this header to select between the TLS and plain-text cross-cluster
// gateway clusters, because ORIGINAL_DST applies transport_socket statically
// and cannot vary per-request.
const crossClusterSchemeHeader = "x-agentbox-cluster-scheme"

// upstreamHostHeader carries the resolved gateway IP:port for the local
// cross_cluster_gateway_{tls,plain} ORIGINAL_DST cluster. Intentionally NOT
// named x-envoy-original-dst-host: that name is the Envoy-wide default
// ORIGINAL_DST header, so if it leaked to the remote cluster via nginx-ingress
// the remote Envoy's own original_dst_cluster would read it and connect
// verbatim to the (remote-public-IP:443), bypassing ExtProc and looping back
// to itself — producing TLS_WRONG_VERSION_NUMBER → 503.
//
// Using a private header name keeps ORIGINAL_DST working locally while making
// the value meaningless to any remote Envoy that happens to receive it.
const upstreamHostHeader = "x-agentbox-upstream-host"

// handleCrossClusterRequest builds a ProcessingResponse that rewrites the
// request headers so Envoy forwards it to the target cluster's data-plane
// gateway instead of resolving the sandbox locally.
//
// The response:
//  1. Sets :authority to the gateway host (for TLS SNI and HTTP Host matching).
//  2. Prepends the data-plane path prefix to :path.
//  3. Injects x-agentbox-cross-cluster: true so Envoy matches the
//     header-matched route for the cross_cluster_gateway cluster.
//  4. Injects any gateway-specific auth headers.
//  5. Sets ClearRouteCache = true so Envoy re-evaluates the route table
//     and picks the cross_cluster_gateway cluster.
//
// ORIGINAL_DST does not perform DNS resolution, so the gateway hostname is
// resolved to an IP via dnsCache before being written to x-envoy-original-dst-host.
// The :authority header retains the original hostname so TLS SNI is correct.
func (s *Server) handleCrossClusterRequest(hdrMap map[string]string, _ RouteTarget, clusterID string) *extProcPb.ProcessingResponse {
	log := ctrl.Log.WithName("extproc-cross-cluster")
	entry, ok := s.clusterStore.Get(clusterID)
	if !ok || entry.Gateway == nil {
		log.Info("No gateway configured", "clusterID", clusterID)
		return immediateError(typev3.StatusCode_BadGateway,
			fmt.Sprintf("unknown target cluster %q or gateway not configured", clusterID))
	}

	dpURL := entry.Gateway.DataPlaneBaseURL()
	parsed, err := url.Parse(dpURL)
	if err != nil {
		return immediateError(typev3.StatusCode_InternalServerError,
			fmt.Sprintf("invalid data plane URL for cluster %q: %v", clusterID, err))
	}

	// gatewayHost is the original hostname (e.g. "gateway-cluster1.example.com")
	// This is kept intact for :authority / TLS SNI.
	gatewayHost := parsed.Host

	// Build the rewritten path: data-plane prefix + original request path.
	originalPath := hdrMap[":path"]
	gatewayPathPrefix := parsed.Path // e.g. "/agentbox/api/data"
	newPath := gatewayPathPrefix + originalPath

	// Determine host:port for the ORIGINAL_DST header.
	// ORIGINAL_DST cluster treats this value as a literal IP:port, so we
	// resolve the hostname to an IP via the DNS cache first.
	dstHost := gatewayHost
	if !strings.Contains(dstHost, ":") {
		if parsed.Scheme == "https" {
			dstHost += ":443"
		} else {
			dstHost += ":80"
		}
	}

	// Resolve hostname → IP using the DNS cache so that ORIGINAL_DST can connect.
	// The :authority header above retains the original hostname for SNI / Host matching.
	if s.dnsCache != nil {
		if colonIdx := strings.LastIndex(dstHost, ":"); colonIdx != -1 {
			hostname := dstHost[:colonIdx]
			portPart := dstHost[colonIdx+1:]
			if ip := s.dnsCache.resolve(hostname); ip != "" {
				dstHost = ip + ":" + portPart
			}
		}
	}

	log.Info("Forwarding cross-cluster request",
		"clusterID", clusterID,
		"scheme", parsed.Scheme,
		"authority", gatewayHost,
		"dstHost", dstHost,
		"path", newPath,
	)

	headerMutations := []*corev3.HeaderValueOption{
		// Rewrite :authority so TLS SNI and HTTP Host match the target gateway.
		{
			Header: &corev3.HeaderValue{
				Key:      ":authority",
				RawValue: []byte(gatewayHost),
			},
		},
		{
			Header: &corev3.HeaderValue{
				Key:      ":path",
				RawValue: []byte(newPath),
			},
		},
		// Tell Envoy's ORIGINAL_DST cluster where to actually connect.
		// Uses a private header name (see upstreamHostHeader doc) to avoid
		// leaking the address to the remote cluster's standard ORIGINAL_DST.
		{
			Header: &corev3.HeaderValue{
				Key:      upstreamHostHeader,
				RawValue: []byte(dstHost),
			},
		},
		{
			Header: &corev3.HeaderValue{
				Key:      crossClusterHeader,
				RawValue: []byte("true"),
			},
		},
		// Signal whether the gateway uses TLS so Envoy selects the correct cluster.
		{
			Header: &corev3.HeaderValue{
				Key:      crossClusterSchemeHeader,
				RawValue: []byte(parsed.Scheme),
			},
		},
		// Tell the remote nginx-ingress the original scheme so it does not issue
		// a 308 redirect to https:// when the dataURL uses https.
		// Without this header, nginx-ingress sees x-forwarded-proto: http (set by
		// the downstream Envoy listener) and redirects, causing the SDK's httpx
		// client to follow the redirect and connect directly to the public HTTPS
		// endpoint — bypassing Envoy and failing TLS verification.
		{
			Header: &corev3.HeaderValue{
				Key:      "x-forwarded-proto",
				RawValue: []byte(parsed.Scheme),
			},
		},
	}

	// Inject gateway-specific auth headers (common + per-plane data overrides).
	for k, v := range entry.Gateway.MergedHeaders(cluster.URLKindData) {
		headerMutations = append(headerMutations, &corev3.HeaderValueOption{
			Header: &corev3.HeaderValue{
				Key:      k,
				RawValue: []byte(v),
			},
		})
	}

	return &extProcPb.ProcessingResponse{
		Response: &extProcPb.ProcessingResponse_RequestHeaders{
			RequestHeaders: &extProcPb.HeadersResponse{
				Response: &extProcPb.CommonResponse{
					// ClearRouteCache = true: Envoy must re-evaluate route matching
					// with the new x-agentbox-cross-cluster header so it selects the
					// cross_cluster_gateway cluster instead of ORIGINAL_DST.
					ClearRouteCache: true,
					HeaderMutation: &extProcPb.HeaderMutation{
						SetHeaders: headerMutations,
					},
				},
			},
		},
	}
}
