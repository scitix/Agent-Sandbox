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
	"net"
	"testing"
	"time"

	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

func TestDNSCache_CachesResult(t *testing.T) {
	c := newDNSCache(time.Minute)

	ip1 := c.resolve("localhost")
	if ip1 == "" {
		t.Fatal("expected non-empty IP for localhost")
	}

	// Corrupt the entry expiry to something far in the future to confirm
	// the second call returns the same value from cache.
	c.mu.Lock()
	c.entries["localhost"] = dnsEntry{ip: "9.9.9.9", expiresAt: time.Now().Add(time.Minute)}
	c.mu.Unlock()

	ip2 := c.resolve("localhost")
	if ip2 != "9.9.9.9" {
		t.Errorf("expected cached IP 9.9.9.9, got %q", ip2)
	}
}

func TestDNSCache_ExpiresAfterTTL(t *testing.T) {
	c := newDNSCache(10 * time.Millisecond)

	c.resolve("localhost") // populate

	// Expire the entry manually.
	c.mu.Lock()
	c.entries["localhost"] = dnsEntry{ip: "1.2.3.4", expiresAt: time.Now().Add(-time.Second)}
	c.mu.Unlock()

	// After expiry a fresh DNS lookup should replace the stale entry.
	ip := c.resolve("localhost")
	if ip == "1.2.3.4" {
		t.Error("stale cache entry used after TTL expired")
	}
	if net.ParseIP(ip) == nil {
		t.Errorf("resolve returned non-IP %q after re-lookup", ip)
	}
}

func TestDNSCache_UnknownHostReturnsEmpty(t *testing.T) {
	c := newDNSCache(time.Minute)
	ip := c.resolve("this-host-does-not-exist.invalid")
	if ip != "" {
		t.Errorf("expected empty string for unresolvable host, got %q", ip)
	}
}

// TestHandleCrossClusterRequest_DNSResolution checks that x-envoy-original-dst-host
// receives an IP:port (not hostname:port) when dnsCache is attached, while
// :authority keeps the original hostname for TLS SNI.
func TestHandleCrossClusterRequest_DNSResolution(t *testing.T) {
	store := cluster.NewStore()
	store.Set([]cluster.ClusterEntry{
		{
			ID: "cluster-b",
			Gateway: &cluster.GatewayConfig{
				// Use "localhost" — guaranteed resolvable — as the gateway host.
				DataURL: "http://localhost:9999/clusters/cluster-b/data",
			},
		},
	})

	s := &Server{
		clusterStore:   store,
		localClusterID: "cluster-a",
		dnsCache:       newDNSCache(time.Minute),
	}

	hdrMap := map[string]string{
		":path":      "/sandboxes/cluster-b.abc/8080/exec",
		":authority": "original",
	}
	target := RouteTarget{SandboxID: "cluster-b.abc", Port: 8080}

	resp := s.handleCrossClusterRequest(hdrMap, target, "cluster-b")

	rh, ok := resp.Response.(*extProcPb.ProcessingResponse_RequestHeaders)
	if !ok {
		t.Fatalf("expected RequestHeaders, got %T", resp.Response)
	}
	headers := make(map[string]string)
	for _, h := range rh.RequestHeaders.Response.HeaderMutation.SetHeaders {
		headers[h.Header.Key] = string(h.Header.RawValue)
	}

	dstHost := headers[upstreamHostHeader]
	if dstHost == "" {
		t.Fatalf("%s not set", upstreamHostHeader)
	}
	host, port, err := net.SplitHostPort(dstHost)
	if err != nil {
		t.Fatalf("%s %q not valid host:port: %v", upstreamHostHeader, dstHost, err)
	}
	if net.ParseIP(host) == nil {
		t.Errorf("%s host part %q is not an IP (DNS not resolved)", upstreamHostHeader, host)
	}
	if port != "9999" {
		t.Errorf("expected port 9999, got %q", port)
	}

	// :authority must remain the original hostname so TLS SNI is correct.
	if got := headers[":authority"]; got != "localhost:9999" {
		t.Errorf(":authority = %q, want %q", got, "localhost:9999")
	}
}
