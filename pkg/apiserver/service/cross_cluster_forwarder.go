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
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/utils/hostalias"
)

// URLKind selects which base URL from GatewayConfig to use when forwarding.
// It is an alias for cluster.URLKind so existing callers keep their import surface
// while the header-merge logic shares the canonical enum.
type URLKind = cluster.URLKind

const (
	// URLKindNative forwards to GatewayConfig.NativeAPIBaseURL.
	URLKindNative = cluster.URLKindNative
	// URLKindE2B forwards to GatewayConfig.E2BAPIBaseURL.
	URLKindE2B = cluster.URLKindE2B
)

// CrossClusterForwarder transparently forwards HTTP requests to a remote
// cluster's API endpoint. It is protocol-agnostic: the original gin.Context
// request (method, path, query string, headers, body) is forwarded verbatim;
// only the base URL is swapped according to urlKind and the target cluster's
// gateway configuration.
//
// Callers (handlers) should:
//  1. Detect the cross-cluster case early (right after extracting clusterID).
//  2. Call Forward — it writes the remote response directly to gc.Writer.
//  3. Return from the handler immediately after Forward returns.
type CrossClusterForwarder struct {
	clusterStore   *cluster.Store
	localClusterID string
	httpClient     *http.Client
}

// NewCrossClusterForwarder creates a forwarder. Returns nil if store or
// localClusterID is empty, which disables cross-cluster support.
//
// The forwarder's HTTP client uses a custom Dialer that consults the Manager-
// pushed host-alias resolver before falling back to the system DNS, so that
// gateway hostnames unreachable via kube-dns (e.g. internal zones on the
// worker network) can be resolved purely via the sync channel without
// touching Pod spec.
func NewCrossClusterForwarder(store *cluster.Store, localClusterID string) *CrossClusterForwarder {
	if store == nil || localClusterID == "" {
		return nil
	}
	resolver := hostalias.New()
	resolver.Bind(store)

	// Mirrors http.DefaultTransport but swaps in the hostalias-aware dialer.
	// Settings kept conservative to match DefaultTransport's behaviour on
	// keepalive and TLS — only DialContext is customised.
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: resolver.DialContext(&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &CrossClusterForwarder{
		clusterStore:   store,
		localClusterID: localClusterID,
		// No client-level timeout: rely on context cancellation so long-running
		// operations (e.g. sandbox create) are not cut short.
		httpClient: &http.Client{Transport: transport},
	}
}

// IsCrossCluster reports whether clusterID refers to a remote cluster that
// requires forwarding. Returns false when the forwarder is nil (cross-cluster
// disabled), clusterID is empty, or clusterID equals the local cluster.
func (f *CrossClusterForwarder) IsCrossCluster(clusterID string) bool {
	return f != nil && clusterID != "" && clusterID != f.localClusterID
}

// LocalClusterID returns the configured local cluster ID. Empty when the
// forwarder is nil (cross-cluster disabled) or no local ID was configured.
func (f *CrossClusterForwarder) LocalClusterID() string {
	if f == nil {
		return ""
	}
	return f.localClusterID
}

// Forward transparently proxies the incoming gin request to targetCluster.
// urlKind selects whether to target the Native or E2B endpoint.
//
// body overrides the request body sent to the remote cluster. Pass nil to use
// gc.Request.Body as-is (suitable for GET/DELETE or when the body has not yet
// been consumed). Pass a non-nil reader when the handler has already read the
// body (e.g. strict-server mode where oapi-codegen parses the body before the
// handler runs) — in that case the caller should re-marshal the parsed struct
// and pass it here.
//
// The full remote response (status, headers, body) is written directly to
// gc.Writer. On network/configuration errors, Forward writes a 502 JSON error
// response itself so the caller can always return immediately after this call.
func (f *CrossClusterForwarder) Forward(gc *gin.Context, targetClusterID string, urlKind URLKind, body io.Reader) {
	entry, ok := f.clusterStore.Get(targetClusterID)
	if !ok {
		gc.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("unknown target cluster: %s", targetClusterID)})
		return
	}
	if entry.Gateway == nil {
		gc.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("gateway not configured for cluster: %s", targetClusterID)})
		return
	}

	var baseURL string
	switch urlKind {
	case URLKindE2B:
		baseURL = entry.Gateway.E2BAPIBaseURL()
	default:
		baseURL = entry.Gateway.NativeAPIBaseURL()
	}
	if baseURL == "" {
		gc.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("no URL configured for cluster %s (urlKind=%d)", targetClusterID, urlKind)})
		return
	}

	// Reconstruct the full target URL: base + original path + query string.
	targetURL := baseURL + gc.Request.URL.RequestURI()

	// Use the provided body if given; fall back to the original request body.
	reqBody := body
	if reqBody == nil {
		reqBody = gc.Request.Body
	}

	outReq, err := http.NewRequestWithContext(gc.Request.Context(), gc.Request.Method, targetURL, reqBody)
	if err != nil {
		gc.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("build forward request: %v", err)})
		return
	}

	// Forward relevant headers from the original request.
	for _, h := range []string{"Authorization", "AGENTBOX-API-KEY", "X-API-Key", "Content-Type"} {
		if v := gc.GetHeader(h); v != "" {
			outReq.Header.Set(h, v)
		}
	}
	// Inject gateway-specific headers (common + per-plane overrides for this urlKind).
	for k, v := range entry.Gateway.MergedHeaders(urlKind) {
		outReq.Header.Set(k, v)
	}
	outReq.Header.Set("X-Source-Cluster", f.localClusterID)

	resp, err := f.httpClient.Do(outReq)
	if err != nil {
		gc.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("forward request to %s: %v", targetClusterID, err)})
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	// Copy response headers, then status, then body — order matters.
	for k, vs := range resp.Header {
		for _, v := range vs {
			gc.Writer.Header().Add(k, v)
		}
	}
	gc.Writer.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(gc.Writer, resp.Body)
}
